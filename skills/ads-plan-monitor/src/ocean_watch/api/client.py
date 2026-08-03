import json
import math
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from email.utils import parsedate_to_datetime
from pathlib import Path
from urllib.parse import urlparse

from ocean_watch.core.config_store import atomic_write_json
from ocean_watch.core.errors import ApiError
from ocean_watch.core.process_lock import ProcessLock

OFFICIAL_HOST_SUFFIXES = (
    "oceanengine.com",
    "jinritemai.com",
)
DEFAULT_MAX_RESPONSE_BYTES = 8 * 1024 * 1024
RATE_LIMIT_CODES = {"40100"}
RATE_LIMIT_HTTP_STATUSES = {429}
DEFAULT_RATE_LIMIT_COOLDOWN_SECONDS = 5.0
DEFAULT_MAX_RETRY_AFTER_SECONDS = 30.0


class RequestBudget:
    def __init__(self, limit):
        self.limit = int(limit)
        if self.limit < 1:
            raise ValueError("request budget limit must be positive")
        self.lock = threading.Lock()
        self.used = 0

    def reserve(self):
        with self.lock:
            if self.used >= self.limit:
                raise ApiError(
                    "Ocean Engine API request budget exhausted",
                    {"code": "request_budget_exceeded", "limit": self.limit},
                )
            self.used += 1

    def snapshot(self):
        with self.lock:
            return {
                "limit": self.limit,
                "used": self.used,
                "remaining": self.limit - self.used,
            }


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


def retry_after_seconds(headers, *, now_fn=time.time):
    value = str(headers.get("Retry-After") if headers else "").strip()
    if not value:
        return None
    try:
        seconds = float(value)
    except ValueError:
        try:
            seconds = parsedate_to_datetime(value).timestamp() - now_fn()
        except (TypeError, ValueError, OverflowError):
            return None
    if not math.isfinite(seconds) or seconds < 0:
        return None
    return max(0.0, seconds)


class RequestThrottle:
    def __init__(
        self,
        *,
        minimum_interval=0.25,
        rate_limit_cooldown=DEFAULT_RATE_LIMIT_COOLDOWN_SECONDS,
        max_retry_after=DEFAULT_MAX_RETRY_AFTER_SECONDS,
        max_in_flight=1,
        state_path=None,
        monotonic_fn=time.monotonic,
        wall_time_fn=time.time,
        sleep_fn=time.sleep,
        process_lock_factory=ProcessLock,
    ):
        self.minimum_interval = max(0.0, float(minimum_interval))
        self.rate_limit_cooldown = max(0.0, float(rate_limit_cooldown))
        self.max_retry_after = max(0.0, float(max_retry_after))
        self.monotonic_fn = monotonic_fn
        self.wall_time_fn = wall_time_fn
        self.sleep_fn = sleep_fn
        self.state_path = None if state_path is None else Path(state_path)
        self.process_lock_factory = process_lock_factory
        self.lock = threading.Lock()
        self.local = threading.local()
        self.in_flight = threading.BoundedSemaphore(max(1, int(max_in_flight)))
        self.next_request_at = 0.0
        self.cooldown_until = 0.0

    def _ensure_shared_directory(self):
        directory = self.state_path.parent
        if directory.is_symlink():
            raise ApiError("Qianchuan request-control state is invalid")
        if directory.exists() and not directory.is_dir():
            raise ApiError("Qianchuan request-control state is invalid")
        directory.mkdir(parents=True, exist_ok=True)
        if directory.is_symlink() or not directory.is_dir():
            raise ApiError("Qianchuan request-control state is invalid")

    def _shared_state(self):
        if self.state_path.is_symlink():
            raise ApiError("Qianchuan request-control state is invalid")
        if not self.state_path.exists():
            return {"next_request_at": 0.0, "cooldown_until": 0.0}
        if not self.state_path.is_file():
            raise ApiError("Qianchuan request-control state is invalid")
        try:
            value = json.loads(self.state_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as error:
            raise ApiError("Qianchuan request-control state is invalid") from error
        if not isinstance(value, dict):
            raise ApiError("Qianchuan request-control state is invalid")
        if set(value) - {"next_request_at", "cooldown_until"}:
            raise ApiError("Qianchuan request-control state is invalid")
        for field in ("next_request_at", "cooldown_until"):
            raw = value.get(field, 0.0)
            if isinstance(raw, bool) or not isinstance(raw, (int, float)):
                raise ApiError("Qianchuan request-control state is invalid")
            parsed = float(raw)
            if not math.isfinite(parsed) or parsed < 0:
                raise ApiError("Qianchuan request-control state is invalid")
            value[field] = parsed
        return value

    def _shared_lock(self):
        lock_path = self.state_path.with_suffix(".lock")
        if lock_path.is_symlink() or (lock_path.exists() and not lock_path.is_file()):
            raise ApiError("Qianchuan request-control state is invalid")
        return self.process_lock_factory(lock_path)

    def acquire(self):
        self.in_flight.acquire()
        process_lock = None
        try:
            if self.state_path is not None:
                self._ensure_shared_directory()
                process_lock = self._shared_lock()
                process_lock.__enter__()
                self.local.process_lock = process_lock
            self._wait_for_turn()
        except Exception:
            if process_lock is not None:
                process_lock.__exit__(None, None, None)
                self.local.process_lock = None
            self.in_flight.release()
            raise

    def release(self):
        try:
            process_lock = getattr(self.local, "process_lock", None)
            if process_lock is not None:
                process_lock.__exit__(None, None, None)
                self.local.process_lock = None
        finally:
            self.in_flight.release()

    def wait(self):
        self.acquire()
        self.release()

    def _wait_for_turn(self):
        while True:
            if self.state_path is not None:
                state = self._shared_state()
                now = self.wall_time_fn()
                wait_seconds = max(
                    float(state.get("next_request_at") or 0),
                    float(state.get("cooldown_until") or 0),
                ) - now
                if wait_seconds <= 0:
                    state["next_request_at"] = now + self.minimum_interval
                    atomic_write_json(self.state_path, state, backup=False)
                    return
                self.sleep_fn(wait_seconds)
                continue
            with self.lock:
                now = self.monotonic_fn()
                wait_seconds = max(self.next_request_at, self.cooldown_until) - now
                if wait_seconds <= 0:
                    self.next_request_at = now + self.minimum_interval
                    return
            self.sleep_fn(wait_seconds)

    def observe(self, response, *, headers=None):
        code = str(response.get("code") or "") if isinstance(response, dict) else ""
        http_status = response.get("http_status") if isinstance(response, dict) else None
        if code not in RATE_LIMIT_CODES and http_status not in RATE_LIMIT_HTTP_STATUSES:
            return
        retry_after = retry_after_seconds(headers)
        if retry_after is not None:
            retry_after = min(retry_after, self.max_retry_after)
        cooldown = (
            self.rate_limit_cooldown
            if retry_after is None
            else max(self.rate_limit_cooldown, retry_after)
        )
        if self.state_path is not None:
            state = self._shared_state()
            state["cooldown_until"] = max(
                float(state.get("cooldown_until") or 0),
                self.wall_time_fn() + cooldown,
            )
            atomic_write_json(self.state_path, state, backup=False)
            return
        with self.lock:
            self.cooldown_until = max(
                self.cooldown_until,
                self.monotonic_fn() + cooldown,
            )


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
        request_throttle=None,
        request_budget=None,
    ):
        self.base_url = str(base_url).rstrip("/")
        if opener is None and not official_https_url(self.base_url):
            raise ApiError("Ocean Engine API base URL must use an official HTTPS host")
        self.access_token = access_token
        self.timeout = timeout
        self.opener = opener or default_opener
        self.request_throttle = request_throttle
        self.request_budget = request_budget
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

        if self.request_throttle is not None:
            self.request_throttle.acquire()
        try:
            if self.request_budget is not None:
                self.request_budget.reserve()
            with self.opener(request, timeout=self.timeout) as response:
                result = read_json_response(response, self.max_response_bytes)
                if self.request_throttle is not None:
                    self.request_throttle.observe(result, headers=response.headers)
                return result
        except urllib.error.HTTPError as error:
            parsed = read_error_response(
                error,
                self.max_response_bytes,
                self.access_token,
            )
            result = {
                **parsed,
                "code": parsed.get("code", error.code),
                "http_status": error.code,
            }
            if self.request_throttle is not None:
                self.request_throttle.observe(result, headers=error.headers)
            return result
        except urllib.error.URLError as error:
            raise ApiError("Ocean Engine API request failed", {"reason": str(error.reason)}) from error
        finally:
            if self.request_throttle is not None:
                self.request_throttle.release()


def request_json(base_url, access_token, method, path, params=None, payload=None):
    client = OceanEngineClient(base_url, access_token)
    return client.request(method, path, params=params, payload=payload)


def get_json(base_url, access_token, path, params):
    return request_json(base_url, access_token, "GET", path, params=params)


def post_json(base_url, access_token, path, payload):
    return request_json(base_url, access_token, "POST", path, payload=payload)
