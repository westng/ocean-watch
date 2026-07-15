#!/usr/bin/env python3
import json
import sys
import threading
import uuid
from urllib.error import HTTPError, URLError
from urllib.parse import urljoin, urlparse
from urllib.request import Request

import ocean_watch.auth.authorization_store as authorization_store
import ocean_watch.auth.credential_store as credential_store
from ocean_watch.api.client import default_opener

MCP_ORIGIN = "https://open.oceanengine.com/sse"
ALLOWED_HOST = "open.oceanengine.com"
DEFAULT_MAX_EVENT_BYTES = 4 * 1024 * 1024
DEFAULT_MAX_RESPONSE_BYTES = 1024 * 1024
READ_CHUNK_BYTES = 64 * 1024


def build_url(app_id, developer_id):
    from urllib.parse import urlencode

    query = urlencode({"app_id": str(app_id), "developer_id": str(developer_id)})
    return f"{MCP_ORIGIN}?{query}"


def validated_message_endpoint(origin, endpoint):
    resolved = urljoin(origin, endpoint)
    parsed = urlparse(resolved)
    if parsed.scheme != "https" or parsed.hostname != ALLOWED_HOST:
        raise RuntimeError("Official MCP returned an untrusted message endpoint")
    return resolved


def response_header(response, name):
    headers = getattr(response, "headers", {}) or {}
    getter = getattr(headers, "get", None)
    return getter(name) if callable(getter) else None


def read_bounded_response(response, max_response_bytes):
    content_length = response_header(response, "Content-Length")
    try:
        too_large = content_length is not None and int(content_length) > max_response_bytes
    except (TypeError, ValueError):
        too_large = False
    if too_large:
        raise RuntimeError("Official MCP response exceeded the size limit")
    body = response.read(max_response_bytes + 1)
    if len(body) > max_response_bytes:
        raise RuntimeError("Official MCP response exceeded the size limit")
    return body


class LegacySseBridge:
    def __init__(
        self,
        origin,
        message_handler=None,
        *,
        opener=None,
        max_event_bytes=DEFAULT_MAX_EVENT_BYTES,
        max_response_bytes=DEFAULT_MAX_RESPONSE_BYTES,
    ):
        parsed = urlparse(str(origin))
        if parsed.scheme != "https" or parsed.hostname != ALLOWED_HOST:
            raise ValueError("Official MCP origin must use open.oceanengine.com over HTTPS")
        self.origin = origin
        self.message_handler = message_handler or self.write_message
        self.opener = opener or default_opener
        self.max_event_bytes = int(max_event_bytes)
        self.max_response_bytes = int(max_response_bytes)
        if self.max_event_bytes <= 0 or self.max_response_bytes <= 0:
            raise ValueError("Official MCP response size limits must be positive")
        self.message_endpoint = None
        self.failure = None
        self.condition = threading.Condition()
        self.output_lock = threading.Lock()

    def set_failure(self, message):
        with self.condition:
            self.failure = message
            self.condition.notify_all()

    def dispatch_event(self, event_name, data):
        if event_name == "endpoint":
            endpoint = validated_message_endpoint(self.origin, data.strip())
            with self.condition:
                self.message_endpoint = endpoint
                self.condition.notify_all()
            return
        if event_name not in {"message", ""} or not data.strip():
            return
        try:
            payload = json.loads(data)
        except json.JSONDecodeError:
            self.set_failure("Official MCP returned an invalid JSON-RPC message")
            return

        self.message_handler(payload)

    def write_message(self, payload):
        with self.output_lock:
            sys.stdout.write(json.dumps(payload, ensure_ascii=False, separators=(",", ":")) + "\n")
            sys.stdout.flush()

    def read_sse(self):
        request = Request(self.origin, headers={"Accept": "text/event-stream"})
        try:
            with self.opener(request, timeout=300) as response:
                event_name = ""
                data_lines = []
                event_bytes = 0
                partial_line = bytearray()
                while True:
                    raw_line = response.readline(READ_CHUNK_BYTES)
                    partial_line.extend(raw_line)
                    if event_bytes + len(partial_line) > self.max_event_bytes:
                        raise RuntimeError("Official MCP event exceeded the size limit")
                    if raw_line and not raw_line.endswith((b"\n", b"\r")):
                        continue

                    complete_line = bytes(partial_line)
                    partial_line.clear()
                    if not raw_line and not complete_line:
                        if data_lines:
                            self.dispatch_event(event_name, "\n".join(data_lines))
                        break

                    event_bytes += len(complete_line)
                    line = complete_line.decode("utf-8").rstrip("\r\n")
                    if not line:
                        if data_lines:
                            self.dispatch_event(event_name, "\n".join(data_lines))
                        event_name = ""
                        data_lines = []
                        event_bytes = 0
                    elif line.startswith("event:"):
                        event_name = line[6:].strip()
                    elif line.startswith("data:"):
                        data_lines.append(line[5:].lstrip())
                    if not raw_line:
                        if data_lines:
                            self.dispatch_event(event_name, "\n".join(data_lines))
                        break
        except HTTPError as error:
            self.set_failure(f"Official MCP rejected the connection with HTTP {error.code}")
        except (URLError, OSError):
            self.set_failure("Unable to connect to the official Ocean Engine MCP")
        except RuntimeError as error:
            self.set_failure(str(error))
        finally:
            if self.failure is None:
                self.set_failure("Official MCP connection closed")

    def wait_for_endpoint(self):
        with self.condition:
            self.condition.wait_for(
                lambda: self.message_endpoint is not None or self.failure is not None,
                timeout=30,
            )
            if self.message_endpoint is not None:
                return self.message_endpoint
            raise RuntimeError(self.failure or "Official MCP did not provide a message endpoint")

    def send(self, payload):
        endpoint = self.wait_for_endpoint()
        request = Request(
            endpoint,
            data=payload,
            method="POST",
            headers={
                "Accept": "application/json, text/event-stream",
                "Content-Type": "application/json",
            },
        )
        try:
            with self.opener(request, timeout=30) as response:
                read_bounded_response(response, self.max_response_bytes)
        except HTTPError as error:
            raise RuntimeError(
                f"Official MCP rejected a message with HTTP {error.code}"
            ) from error
        except (URLError, OSError) as error:
            raise RuntimeError("Unable to send a message to the official MCP") from error


def probe(app_id, developer_id, timeout=30):
    responses = {}
    condition = threading.Condition()

    def receive(payload):
        message_id = payload.get("id") if isinstance(payload, dict) else None
        if message_id is not None:
            with condition:
                responses[message_id] = payload
                condition.notify_all()

    def wait_for(message_id):
        with condition:
            condition.wait_for(lambda: message_id in responses, timeout=timeout)
            response = responses.get(message_id)
        if response is None:
            raise RuntimeError("Official MCP handshake timed out")
        if response.get("error"):
            raise RuntimeError("Official MCP rejected the configured developer credentials")
        return response.get("result") or {}

    bridge = LegacySseBridge(build_url(app_id, developer_id), message_handler=receive)
    threading.Thread(target=bridge.read_sse, daemon=True).start()
    initialize_id = f"ocean-watch-{uuid.uuid4().hex}"
    bridge.send(json.dumps({
        "jsonrpc": "2.0",
        "id": initialize_id,
        "method": "initialize",
        "params": {
            "protocolVersion": "2025-03-26",
            "capabilities": {},
            "clientInfo": {"name": "ocean-watch", "version": "0.2.0"},
        },
    }).encode("utf-8"))
    wait_for(initialize_id)
    bridge.send(json.dumps({
        "jsonrpc": "2.0",
        "method": "notifications/initialized",
        "params": {},
    }).encode("utf-8"))
    tools_id = f"ocean-watch-{uuid.uuid4().hex}"
    bridge.send(json.dumps({
        "jsonrpc": "2.0",
        "id": tools_id,
        "method": "tools/list",
        "params": {},
    }).encode("utf-8"))
    result = wait_for(tools_id)
    names = sorted(
        tool.get("name")
        for tool in (result.get("tools") or [])
        if isinstance(tool, dict) and isinstance(tool.get("name"), str)
    )
    if not names:
        raise RuntimeError("Official MCP returned no developer tools")
    return names


def main():
    credentials = {
        **authorization_store.read_app("marketing"),
        **{"developer_id": credential_store.read_credentials().get("developer_id")},
    }
    app_id = credentials.get("app_id")
    developer_id = credentials.get("developer_id")
    if credential_store.is_missing(app_id) or credential_store.is_missing(developer_id):
        print("Official MCP credentials are not configured", file=sys.stderr)
        return 1

    bridge = LegacySseBridge(build_url(app_id, developer_id))
    reader = threading.Thread(target=bridge.read_sse, daemon=True)
    reader.start()

    try:
        for line in sys.stdin.buffer:
            payload = line.strip()
            if payload:
                bridge.send(payload)
    except RuntimeError as error:
        print(str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
