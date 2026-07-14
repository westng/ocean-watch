import json
import urllib.error
import urllib.parse
import urllib.request

from ocean_watch.core.errors import ApiError


class OceanEngineClient:
    """Small official API client shared by all business domains."""

    def __init__(self, base_url, access_token=None, timeout=30, opener=None):
        self.base_url = str(base_url).rstrip("/")
        self.access_token = access_token
        self.timeout = timeout
        self.opener = opener or urllib.request.urlopen

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
                return json.loads(response.read().decode("utf-8"))
        except urllib.error.HTTPError as error:
            response_body = error.read().decode("utf-8", errors="replace")
            try:
                parsed = json.loads(response_body)
            except json.JSONDecodeError:
                parsed = {"message": response_body}
            return {"code": error.code, **parsed}
        except urllib.error.URLError as error:
            raise ApiError("Ocean Engine API request failed", {"reason": str(error.reason)}) from error


def request_json(base_url, access_token, method, path, params=None, payload=None):
    client = OceanEngineClient(base_url, access_token)
    return client.request(method, path, params=params, payload=payload)


def get_json(base_url, access_token, path, params):
    return request_json(base_url, access_token, "GET", path, params=params)


def post_json(base_url, access_token, path, payload):
    return request_json(base_url, access_token, "POST", path, payload=payload)
