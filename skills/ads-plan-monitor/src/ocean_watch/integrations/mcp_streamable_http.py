import json
import socket
import time
import urllib.error
import urllib.request
from urllib.parse import urlparse

from ocean_watch import __version__
from ocean_watch.core.errors import ApiError, ConfigurationError

MCP_PROTOCOL_VERSION = "2025-03-26"
OFFICIAL_MCP_HOST = "open.oceanengine.com"
DEFAULT_MAX_RESPONSE_BYTES = 4 * 1024 * 1024
READ_CHUNK_BYTES = 64 * 1024
RETRYABLE_HTTP_STATUSES = {408, 425, 429, 500, 502, 503, 504}
MAX_ERROR_MESSAGE_CHARS = 1000

_NO_MESSAGE = object()


class RejectRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, _req, _fp, _code, _msg, _headers, _newurl):
        return None


def default_opener(request, timeout):
    return urllib.request.build_opener(RejectRedirects()).open(request, timeout=timeout)


def _response_header(response, name):
    headers = getattr(response, "headers", {}) or {}
    getter = getattr(headers, "get", None)
    return getter(name) if callable(getter) else None


def _decode_sse_event(data_lines):
    if not data_lines:
        return _NO_MESSAGE
    try:
        return json.loads("\n".join(data_lines))
    except (json.JSONDecodeError, TypeError) as exc:
        raise ApiError("Official MCP returned an invalid response") from exc


def _matching_response(message, expected_id):
    candidates = message if isinstance(message, list) else [message]
    return next(
        (
            candidate
            for candidate in candidates
            if isinstance(candidate, dict) and candidate.get("id") == expected_id
        ),
        _NO_MESSAGE,
    )


def _decode_sse(body, expected_id=None):
    last_message = _NO_MESSAGE
    data_lines = []
    for line in body.splitlines():
        if not line.strip():
            message = _decode_sse_event(data_lines)
            data_lines = []
            if message is _NO_MESSAGE:
                continue
            if expected_id is not None:
                matching = _matching_response(message, expected_id)
                if matching is not _NO_MESSAGE:
                    return matching
            last_message = message
            continue
        if line.startswith("data:"):
            data_lines.append(line[5:].lstrip())
    if data_lines:
        message = _decode_sse_event(data_lines)
        if expected_id is not None:
            matching = _matching_response(message, expected_id)
            if matching is not _NO_MESSAGE:
                return matching
        last_message = message
    if last_message is _NO_MESSAGE:
        raise ApiError("Official MCP returned an empty event stream")
    if expected_id is not None:
        raise ApiError("Official MCP event stream returned no matching JSON-RPC response")
    return last_message


def _decode_response(body, content_type):
    if not body.strip():
        return None
    try:
        if "text/event-stream" in str(content_type or "") or body.lstrip().startswith("event:"):
            return _decode_sse(body)
        return json.loads(body)
    except (json.JSONDecodeError, TypeError) as exc:
        raise ApiError("Official MCP returned an invalid response") from exc


def _response_socket(response):
    pending = [response]
    seen = set()
    while pending:
        current = pending.pop(0)
        if current is None or id(current) in seen:
            continue
        seen.add(id(current))
        candidate = getattr(current, "_sock", None)
        if callable(getattr(candidate, "settimeout", None)):
            return candidate
        for attribute in ("fp", "raw"):
            child = getattr(current, attribute, None)
            if child is not None:
                pending.append(child)
    return None


def _prepare_response_read(response, deadline):
    if deadline is None:
        return
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise TimeoutError("Official MCP response deadline exceeded")
    response_socket = _response_socket(response)
    if response_socket is not None:
        response_socket.settimeout(remaining)


def _check_content_length(response, max_response_bytes):
    content_length = _response_header(response, "Content-Length")
    try:
        too_large = content_length is not None and int(content_length) > max_response_bytes
    except (TypeError, ValueError):
        too_large = False
    if too_large:
        raise ApiError(
            "Official MCP response exceeded the size limit",
            {"max_response_bytes": max_response_bytes},
        )


def _raise_if_too_large(total_bytes, max_response_bytes):
    if total_bytes > max_response_bytes:
        raise ApiError(
            "Official MCP response exceeded the size limit",
            {"max_response_bytes": max_response_bytes},
        )


def _read_bounded_body(response, max_response_bytes, deadline):
    _check_content_length(response, max_response_bytes)
    body = bytearray()
    while True:
        _prepare_response_read(response, deadline)
        read_size = min(READ_CHUNK_BYTES, max_response_bytes - len(body) + 1)
        chunk = response.read(read_size)
        if not chunk:
            return bytes(body)
        body.extend(chunk)
        _raise_if_too_large(len(body), max_response_bytes)


def _read_sse_response(response, expected_id, max_response_bytes, deadline):
    _check_content_length(response, max_response_bytes)
    total_bytes = 0
    data_lines = []
    last_message = _NO_MESSAGE
    partial_line = bytearray()

    while True:
        _prepare_response_read(response, deadline)
        read_size = min(READ_CHUNK_BYTES, max_response_bytes - total_bytes + 1)
        raw_line = response.readline(read_size)
        if raw_line:
            total_bytes += len(raw_line)
            _raise_if_too_large(total_bytes, max_response_bytes)

        partial_line.extend(raw_line)
        if raw_line and not raw_line.endswith((b"\n", b"\r")):
            continue
        if not raw_line and partial_line:
            line = bytes(partial_line).decode("utf-8", errors="replace")
            partial_line.clear()
            if line.startswith("data:"):
                data_lines.append(line[5:].lstrip())
        complete_line = bytes(partial_line)
        partial_line.clear()

        if not raw_line or complete_line in (b"\n", b"\r\n"):
            message = _decode_sse_event(data_lines)
            data_lines = []
            if message is not _NO_MESSAGE:
                if expected_id is not None:
                    matching = _matching_response(message, expected_id)
                    if matching is not _NO_MESSAGE:
                        return matching
                last_message = message
            if not raw_line:
                break
            continue

        line = complete_line.rstrip(b"\r\n").decode("utf-8", errors="replace")
        if line.startswith("data:"):
            data_lines.append(line[5:].lstrip())

    if expected_id is not None and last_message is not _NO_MESSAGE:
        raise ApiError("Official MCP event stream returned no matching JSON-RPC response")
    return None if last_message is _NO_MESSAGE else last_message


def _read_response(response, expected_id, max_response_bytes, deadline):
    content_type = _response_header(response, "Content-Type")
    if "text/event-stream" in str(content_type or "").lower():
        return _read_sse_response(response, expected_id, max_response_bytes, deadline)
    body = _read_bounded_body(response, max_response_bytes, deadline)
    return _decode_response(body.decode("utf-8", errors="replace"), content_type)


def _safe_error_message(decoded, access_token):
    if not isinstance(decoded, dict):
        return None
    error = decoded.get("error")
    if isinstance(error, dict):
        message = error.get("message")
    else:
        message = decoded.get("message")
    if not isinstance(message, str) or not message.strip():
        return None
    sanitized = message.replace(access_token, "[REDACTED]") if access_token else message
    return sanitized[:MAX_ERROR_MESSAGE_CHARS]


def _redact_text(value, access_token):
    if not isinstance(value, str):
        return value
    sanitized = value.replace(access_token, "[REDACTED]") if access_token else value
    return sanitized[:MAX_ERROR_MESSAGE_CHARS]


def _validate_json_rpc_response(response, expected_id):
    if not isinstance(response, dict):
        raise ApiError("Official MCP returned no JSON-RPC response")
    if response.get("jsonrpc") != "2.0":
        raise ApiError("Official MCP returned an invalid JSON-RPC version")
    if response.get("id") != expected_id:
        raise ApiError(
            "Official MCP returned a mismatched JSON-RPC response",
            {"expected_id": expected_id},
        )
    return response


def _is_timeout_error(error):
    reason = getattr(error, "reason", error)
    return isinstance(reason, (TimeoutError, socket.timeout))


def _business_payload(response):
    if not isinstance(response, dict):
        return None
    if "code" in response:
        return response
    result = response.get("result")
    if not isinstance(result, dict):
        return None
    for item in result.get("content") or []:
        if not isinstance(item, dict) or item.get("type") != "text":
            continue
        try:
            payload = json.loads(item.get("text"))
        except (json.JSONDecodeError, TypeError):
            continue
        if isinstance(payload, dict):
            return payload
    return None


class StreamableHttpMcpClient:
    """Minimal MCP client for Ocean Engine's official Streamable HTTP server."""

    def __init__(
        self,
        endpoint,
        access_token,
        *,
        tool_range,
        timeout=60,
        max_response_bytes=DEFAULT_MAX_RESPONSE_BYTES,
        opener=None,
        client_name="ocean-watch",
        client_version=__version__,
        request_throttle=None,
    ):
        parsed = urlparse(str(endpoint))
        if parsed.scheme != "https" or parsed.hostname != OFFICIAL_MCP_HOST:
            raise ConfigurationError("Official MCP endpoint must use open.oceanengine.com over HTTPS")
        if not str(access_token or "").strip():
            raise ConfigurationError("Qianchuan Access-Token is required for the official MCP")
        normalized_tools = list(dict.fromkeys(str(item).strip() for item in tool_range or [] if str(item).strip()))
        if not normalized_tools:
            raise ConfigurationError("Official MCP Tool-Range cannot be empty")
        try:
            normalized_max_response_bytes = int(max_response_bytes)
        except (TypeError, ValueError) as exc:
            raise ConfigurationError("Official MCP response size limit must be an integer") from exc
        if normalized_max_response_bytes <= 0:
            raise ConfigurationError("Official MCP response size limit must be positive")

        self.endpoint = str(endpoint)
        self.access_token = str(access_token)
        self.tool_range = normalized_tools
        self.timeout = timeout
        self.max_response_bytes = normalized_max_response_bytes
        self.opener = opener or default_opener
        self.client_name = client_name
        self.client_version = client_version
        self.request_throttle = request_throttle
        self.session_id = None
        self.initialized = False
        self._request_id = 0

    def _headers(self):
        headers = {
            "Access-Token": self.access_token,
            "Accept": "application/json, text/event-stream",
            "Content-Type": "application/json",
            "Tool-Range": json.dumps(self.tool_range, ensure_ascii=False, separators=(",", ":")),
        }
        if self.session_id:
            headers["Mcp-Session-Id"] = self.session_id
        return headers

    def _post(self, payload, *, allow_empty=False):
        request = urllib.request.Request(
            self.endpoint,
            data=json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8"),
            headers=self._headers(),
            method="POST",
        )
        deadline = None
        try:
            deadline = time.monotonic() + float(self.timeout)
        except (TypeError, ValueError):
            pass
        expected_id = payload.get("id") if isinstance(payload, dict) else None
        throttle_acquired = False
        try:
            if self.request_throttle is not None:
                self.request_throttle.acquire()
                throttle_acquired = True
            with self.opener(request, timeout=self.timeout) as response:
                session_id = _response_header(response, "Mcp-Session-Id")
                if session_id:
                    self.session_id = session_id
                status = getattr(response, "status", None)
                if allow_empty and expected_id is None and status in (202, 204):
                    return None
                decoded = _read_response(
                    response,
                    expected_id,
                    self.max_response_bytes,
                    deadline,
                )
                if decoded is None and not allow_empty:
                    raise ApiError("Official MCP returned an empty response")
                if expected_id is not None:
                    decoded = _validate_json_rpc_response(decoded, expected_id)
                if self.request_throttle is not None:
                    self.request_throttle.observe(
                        _business_payload(decoded),
                        headers=getattr(response, "headers", None),
                    )
                return decoded
        except urllib.error.HTTPError as exc:
            decoded = None
            try:
                decoded = _read_response(
                    exc,
                    expected_id,
                    self.max_response_bytes,
                    deadline,
                )
            except (ApiError, OSError, TimeoutError):
                pass
            status = exc.code
            if self.request_throttle is not None:
                self.request_throttle.observe(
                    {"http_status": status},
                    headers=exc.headers,
                )
            details = {
                "transport_error": "http",
                "http_status": status,
                "retryable": status in RETRYABLE_HTTP_STATUSES,
            }
            message = _safe_error_message(decoded, self.access_token)
            if message is not None:
                details["message"] = message
            raise ApiError(
                "Official MCP request failed",
                details,
            ) from exc
        except urllib.error.URLError as exc:
            transport_error = "timeout" if _is_timeout_error(exc) else "connection"
            raise ApiError(
                "Unable to connect to the official Qianchuan MCP",
                {"transport_error": transport_error, "retryable": True},
            ) from exc
        except (TimeoutError, socket.timeout) as exc:
            raise ApiError(
                "Official Qianchuan MCP request timed out",
                {"transport_error": "timeout", "retryable": True},
            ) from exc
        except OSError as exc:
            raise ApiError(
                "Unable to connect to the official Qianchuan MCP",
                {"transport_error": "connection", "retryable": True},
            ) from exc
        finally:
            if throttle_acquired:
                self.request_throttle.release()

    def _next_id(self):
        self._request_id += 1
        return f"ocean-watch-{self._request_id}"

    def _require_result(self, response, operation):
        if not isinstance(response, dict):
            raise ApiError(f"Official MCP {operation} returned no JSON-RPC response")
        error = response.get("error")
        if error:
            error_details = error if isinstance(error, dict) else {}
            raise ApiError(
                f"Official MCP {operation} failed",
                {
                    "mcp_code": error_details.get("code"),
                    "message": _redact_text(
                        error_details.get("message"),
                        self.access_token,
                    ),
                },
            )
        result = response.get("result")
        if not isinstance(result, dict):
            raise ApiError(f"Official MCP {operation} returned no result")
        return result

    def initialize(self):
        if self.initialized:
            return
        request_id = self._next_id()
        response = self._post({
            "jsonrpc": "2.0",
            "id": request_id,
            "method": "initialize",
            "params": {
                "protocolVersion": MCP_PROTOCOL_VERSION,
                "capabilities": {},
                "clientInfo": {
                    "name": self.client_name,
                    "version": self.client_version,
                },
            },
        })
        result = self._require_result(response, "initialization")
        if not result.get("protocolVersion"):
            raise ApiError("Official MCP initialization omitted protocolVersion")
        self._post(
            {
                "jsonrpc": "2.0",
                "method": "notifications/initialized",
                "params": {},
            },
            allow_empty=True,
        )
        self.initialized = True

    def list_tools(self):
        self.initialize()
        response = self._post({
            "jsonrpc": "2.0",
            "id": self._next_id(),
            "method": "tools/list",
            "params": {},
        })
        return self._require_result(response, "tool listing").get("tools") or []

    def call_tool(self, name, arguments):
        if name not in self.tool_range:
            raise ConfigurationError(
                "Official MCP tool is outside the configured Tool-Range",
                {"tool": name},
            )
        self.initialize()
        response = self._post({
            "jsonrpc": "2.0",
            "id": self._next_id(),
            "method": "tools/call",
            "params": {"name": name, "arguments": arguments},
        })
        result = self._require_result(response, f"tool {name}")
        content = result.get("content") or []
        text = next(
            (
                item.get("text")
                for item in content
                if isinstance(item, dict) and item.get("type") == "text" and item.get("text")
            ),
            None,
        )
        if result.get("isError") or text is None:
            raise ApiError(
                f"Official MCP tool {name} failed",
                {"has_text_response": text is not None},
            )
        try:
            payload = json.loads(text)
        except (json.JSONDecodeError, TypeError) as exc:
            raise ApiError(f"Official MCP tool {name} returned invalid JSON") from exc
        if not isinstance(payload, dict):
            raise ApiError(f"Official MCP tool {name} returned an invalid payload")
        if payload.get("code") != 0:
            raise ApiError(
                f"Official MCP tool {name} returned a business error",
                {
                    "code": payload.get("code"),
                    "message": _redact_text(payload.get("message"), self.access_token),
                    "request_id": payload.get("request_id"),
                },
            )
        return payload
