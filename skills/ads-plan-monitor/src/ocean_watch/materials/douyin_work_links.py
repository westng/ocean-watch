import json
import re
import urllib.parse
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed

DEFAULT_TIMEOUT = 15
DEFAULT_CONCURRENCY = 4
MAX_CONCURRENCY = 10
MAX_REDIRECTS = 5
MAX_METADATA_RESPONSE_BYTES = 1024 * 1024
WORK_PATH_PATTERN = re.compile(r"(?:^|/)video/(\d+)(?:/|$)")
URL_PATTERN = re.compile(r"https?://[^\s]+", re.IGNORECASE)
TRUSTED_DOUYIN_DOMAINS = ("douyin.com", "iesdouyin.com")


class WorkLinkError(ValueError):
    def __init__(self, code, message):
        super().__init__(message)
        self.code = code


def is_douyin_host(hostname):
    hostname = str(hostname or "").rstrip(".").lower()
    return any(
        hostname == domain or hostname.endswith(f".{domain}")
        for domain in TRUSTED_DOUYIN_DOMAINS
    )


def normalize_input_url(value):
    text = str(value or "").strip()
    match = URL_PATTERN.search(text)
    if match:
        text = match.group(0).rstrip(".,;:)]}>，。；：）】》")
    parsed = urllib.parse.urlsplit(text)
    if parsed.scheme != "https" or not parsed.hostname:
        raise WorkLinkError("invalid_url", "作品链接必须是有效的 HTTPS 地址")
    if parsed.username or parsed.password:
        raise WorkLinkError("invalid_url", "作品链接不能包含用户凭据")
    if not is_douyin_host(parsed.hostname):
        raise WorkLinkError("untrusted_host", "作品链接不属于受信任的抖音域名")
    try:
        port = parsed.port
    except ValueError as error:
        raise WorkLinkError("invalid_url", "作品链接端口无效") from error
    if port not in {None, 443}:
        raise WorkLinkError("untrusted_port", "作品链接使用了不允许的端口")
    return urllib.parse.urlunsplit(parsed)


def work_id_from_url(value):
    parsed = urllib.parse.urlsplit(value)
    match = WORK_PATH_PATTERN.search(parsed.path)
    return match.group(1) if match else None


def canonical_work_url(aweme_item_id):
    return f"https://www.douyin.com/video/{aweme_item_id}"


class SafeDouyinRedirectHandler(urllib.request.HTTPRedirectHandler):
    max_redirections = MAX_REDIRECTS
    max_repeats = MAX_REDIRECTS

    def redirect_request(self, request, file_pointer, code, message, headers, new_url):
        normalize_input_url(new_url)
        return super().redirect_request(
            request,
            file_pointer,
            code,
            message,
            headers,
            new_url,
        )


class NoMetadataRedirectHandler(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, file_pointer, code, message, headers, new_url):
        return None


class DouyinWorkMetadataResolver:
    def __init__(self, endpoint, opener=None, timeout=DEFAULT_TIMEOUT):
        self.endpoint = str(endpoint)
        self.opener = opener or urllib.request.build_opener(NoMetadataRedirectHandler())
        self.timeout = timeout

    def resolve(self, input_url):
        parsed = urllib.parse.urlsplit(self.endpoint)
        if (
            parsed.scheme != "https"
            or not parsed.hostname
            or parsed.username
            or parsed.password
            or parsed.fragment
        ):
            raise WorkLinkError(
                "invalid_metadata_endpoint",
                "作品解析服务必须使用无凭据的 HTTPS 地址",
            )
        query = urllib.parse.parse_qsl(parsed.query, keep_blank_values=True)
        query.append(("url", input_url))
        request_url = urllib.parse.urlunsplit(parsed._replace(
            query=urllib.parse.urlencode(query),
        ))
        request = urllib.request.Request(
            request_url,
            headers={"Accept": "application/json", "User-Agent": "ocean-watch/0.9"},
            method="GET",
        )
        try:
            with self.opener.open(request, timeout=self.timeout) as response:
                payload = response.read(MAX_METADATA_RESPONSE_BYTES + 1)
        except Exception as error:
            raise WorkLinkError(
                "metadata_query_failed",
                f"作品解析服务请求失败: {error}",
            ) from error
        if len(payload) > MAX_METADATA_RESPONSE_BYTES:
            raise WorkLinkError(
                "metadata_response_too_large",
                "作品解析服务响应超过大小限制",
            )
        try:
            result = json.loads(payload.decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError) as error:
            raise WorkLinkError(
                "invalid_metadata_response",
                "作品解析服务返回了无效 JSON",
            ) from error
        code = result.get("code") if isinstance(result, dict) else None
        data = result.get("data") if isinstance(result, dict) else None
        author = data.get("author") if isinstance(data, dict) else None
        product = data.get("product") if isinstance(data, dict) else None
        video = data.get("video") if isinstance(data, dict) else None
        aweme_item_id = str((video or {}).get("video_info_id") or "")
        aweme_id = str((author or {}).get("uid") or "")
        aweme_show_id = str((author or {}).get("unique_id") or "").strip()
        creator_name = str((author or {}).get("nickname") or "").strip()
        if code != 200 or not aweme_item_id.isdigit():
            raise WorkLinkError(
                "invalid_metadata_response",
                "作品解析服务未返回有效作品 ID",
            )
        owner_hint = None
        if aweme_id.isdigit() and aweme_show_id:
            owner_hint = {
                "aweme_id": aweme_id,
                "aweme_show_id": aweme_show_id,
                "source": "configured_metadata_api",
            }
        product_id = str((product or {}).get("product_info_id") or "").strip()
        product_hint = None
        if product_id.isdigit():
            product_hint = {
                "product_id": product_id,
                "product_name": str(
                    (product or {}).get("product_info_name") or ""
                ).strip() or None,
                "source": "configured_metadata_api",
            }
        return {
            "aweme_item_id": aweme_item_id,
            "creator_name_hint": creator_name or None,
            "owner_hint": owner_hint,
            "product_hint": product_hint,
        }


class DouyinWorkLinkResolver:
    def __init__(self, opener=None, timeout=DEFAULT_TIMEOUT, metadata_resolver=None):
        self.opener = opener or urllib.request.build_opener(SafeDouyinRedirectHandler())
        self.timeout = timeout
        self.metadata_resolver = metadata_resolver

    def resolve(self, value):
        input_url = normalize_input_url(value)
        aweme_item_id = work_id_from_url(input_url)
        resolved_url = input_url
        owner_hint = None
        product_hint = None
        creator_name_hint = None
        hint_warning = None
        if self.metadata_resolver is not None:
            try:
                metadata = self.metadata_resolver.resolve(input_url)
                metadata_item_id = metadata["aweme_item_id"]
                if aweme_item_id is not None and metadata_item_id != aweme_item_id:
                    raise WorkLinkError(
                        "metadata_work_mismatch",
                        "作品解析服务返回的作品 ID 与输入链接不一致",
                    )
                aweme_item_id = metadata_item_id
                owner_hint = metadata.get("owner_hint")
                product_hint = metadata.get("product_hint")
                creator_name_hint = metadata.get("creator_name_hint")
                resolved_url = canonical_work_url(aweme_item_id)
            except Exception as error:
                hint_warning = {
                    "code": getattr(error, "code", "metadata_query_failed"),
                    "message": str(error),
                }
        if aweme_item_id is None:
            request = urllib.request.Request(
                input_url,
                headers={
                    "Accept": "text/html,application/xhtml+xml",
                    "Range": "bytes=0-0",
                    "User-Agent": (
                        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "
                        "AppleWebKit/537.36 Chrome/126 Safari/537.36"
                    ),
                },
                method="GET",
            )
            try:
                with self.opener.open(request, timeout=self.timeout) as response:
                    resolved_url = normalize_input_url(response.geturl())
            except WorkLinkError:
                raise
            except Exception as error:
                raise WorkLinkError(
                    "redirect_failed",
                    f"作品短链解析失败: {error}",
                ) from error
            aweme_item_id = work_id_from_url(resolved_url)
        if aweme_item_id is None:
            raise WorkLinkError(
                "missing_work_id",
                "作品链接跳转后未包含 /video/{作品ID}",
            )
        result = {
            "input_url": input_url,
            "resolved_url": resolved_url,
            "canonical_url": canonical_work_url(aweme_item_id),
            "aweme_item_id": aweme_item_id,
        }
        if owner_hint:
            result["owner_hint"] = owner_hint
        if product_hint:
            result["product_hint"] = product_hint
        if creator_name_hint:
            result["creator_name_hint"] = creator_name_hint
        if hint_warning:
            result["hint_warning"] = hint_warning
        return result


def resolve_work_links(values, *, resolver=None, concurrency=DEFAULT_CONCURRENCY):
    concurrency = int(concurrency)
    if concurrency < 1 or concurrency > MAX_CONCURRENCY:
        raise ValueError(f"concurrency must be between 1 and {MAX_CONCURRENCY}")
    resolver = resolver or DouyinWorkLinkResolver()
    indexed = [(index, value) for index, value in enumerate(values or [])]
    resolved_by_index = {}
    with ThreadPoolExecutor(max_workers=min(concurrency, max(1, len(indexed)))) as pool:
        futures = {
            pool.submit(resolver.resolve, value): (index, value)
            for index, value in indexed
        }
        for future in as_completed(futures):
            index, value = futures[future]
            try:
                resolved_by_index[index] = {
                    "input_index": index,
                    **future.result(),
                }
            except Exception as error:
                resolved_by_index[index] = {
                    "input_index": index,
                    "input_url": str(value),
                    "status": "skipped",
                    "reason": getattr(error, "code", "link_resolution_failed"),
                    "message": str(error),
                }

    resolved = []
    skipped = []
    seen_item_ids = set()
    for index, _ in indexed:
        row = resolved_by_index[index]
        item_id = row.get("aweme_item_id")
        if item_id is None:
            skipped.append(row)
        elif item_id in seen_item_ids:
            skipped.append({
                **row,
                "status": "skipped",
                "reason": "duplicate_input",
                "message": "同一作品在本批次中重复出现",
            })
        else:
            seen_item_ids.add(item_id)
            resolved.append(row)
    return {"resolved": resolved, "skipped": skipped}
