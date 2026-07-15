import json
import urllib.error
import urllib.parse
import urllib.request
from urllib.parse import urlparse

from ocean_watch.core.errors import ApiError

OFFICIAL_HOST_SUFFIXES = (
    "oceanengine.com",
    "jinritemai.com",
)
DEFAULT_MAX_RESPONSE_BYTES = 8 * 1024 * 1024


class RejectRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, _req, _fp, _code, _msg, _headers, _newurl):
        return None


def official_https_url(value):
    parsed = urlparse(str(value))
    host = (parsed.hostname or "").lower()
    return parsed.scheme == "https" and any(
        host == suffix or host.endswith(f".{suffix}")
        for suffix in OFFICIAL_HOST_SUFFIXES
    )


def default_opener(request, timeout):
    return urllib.request.build_opener(RejectRedirects()).open(request, timeout=timeout)


def read_json_response(response, max_response_bytes):
    content_length = response.headers.get("Content-Length") if response.headers else None
    if content_length is not None:
        try:
            if int(content_length) > max_response_bytes:
                raise ApiError("Ocean Engine API response exceeded the size limit")
        except ValueError:
            pass
    body = response.read(max_response_bytes + 1)
    if len(body) > max_response_bytes:
        raise ApiError("Ocean Engine API response exceeded the size limit")
    try:
        return json.loads(body.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ApiError("Ocean Engine API returned invalid JSON") from exc


def read_error_response(error, max_response_bytes, access_token=None):
    body = error.read(max_response_bytes + 1)
    if len(body) > max_response_bytes:
        return {"message": "Ocean Engine API error response exceeded the size limit"}
    text = body.decode("utf-8", errors="replace")
    if access_token:
        text = text.replace(str(access_token), "[REDACTED]")
    try:
        parsed = json.loads(text)
    except json.JSONDecodeError:
        return {"message": text[:1000]}
    if not isinstance(parsed, dict):
        return {"message": "Ocean Engine API returned an invalid error payload"}
    rendered = json.dumps(parsed, ensure_ascii=False)
    if access_token and str(access_token) in rendered:
        return json.loads(rendered.replace(str(access_token), "[REDACTED]"))
    return parsed


class OceanEngineClient:
    """Small official API client shared by all business domains."""

    def __init__(
        self,
        base_url,
        access_token=None,
        timeout=30,
        opener=None,
        max_response_bytes=DEFAULT_MAX_RESPONSE_BYTES,
    ):
        self.base_url = str(base_url).rstrip("/")
        if opener is None and not official_https_url(self.base_url):
            raise ApiError("Ocean Engine API base URL must use an official HTTPS host")
        self.access_token = access_token
        self.timeout = timeout
        self.opener = opener or default_opener
        self.max_response_bytes = int(max_response_bytes)
        if self.max_response_bytes <= 0:
            raise ValueError("max_response_bytes must be positive")

    def get(self, path, params=None):
        return self.request("GET", path, params=params)

    def post(self, path, payload=None, params=None):
        return self.request("POST", path, params=params, payload=payload)

    def request(self, method, path, params=None, payload=None):
        url = self.base_url + path
        if params:
            encoded = {
                key: value if isinstance(value, str) else json.dumps(value, ensure_ascii=False)
                for key, value in params.items()
                if value is not None
            }
            url += "?" + urllib.parse.urlencode(encoded)

        headers = {"Content-Type": "application/json"}
        if self.access_token:
            headers["Access-Token"] = self.access_token
        body = None if payload is None else json.dumps(payload, ensure_ascii=False).encode("utf-8")
        request = urllib.request.Request(url, data=body, headers=headers, method=method)

        try:
            with self.opener(request, timeout=self.timeout) as response:
                return read_json_response(response, self.max_response_bytes)
        except urllib.error.HTTPError as error:
            parsed = read_error_response(
                error,
                self.max_response_bytes,
                self.access_token,
            )
            return {
                **parsed,
                "code": parsed.get("code", error.code),
                "http_status": error.code,
            }
        except urllib.error.URLError as error:
            raise ApiError("Ocean Engine API request failed", {"reason": str(error.reason)}) from error


def request_json(base_url, access_token, method, path, params=None, payload=None):
    client = OceanEngineClient(base_url, access_token)
    return client.request(method, path, params=params, payload=payload)


def get_json(base_url, access_token, path, params):
    return request_json(base_url, access_token, "GET", path, params=params)


def post_json(base_url, access_token, path, payload):
    return request_json(base_url, access_token, "POST", path, payload=payload)
