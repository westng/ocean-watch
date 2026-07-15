import re
import urllib.parse
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed

DEFAULT_TIMEOUT = 15
DEFAULT_CONCURRENCY = 4
MAX_CONCURRENCY = 10
MAX_REDIRECTS = 5
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


class DouyinWorkLinkResolver:
    def __init__(self, opener=None, timeout=DEFAULT_TIMEOUT):
        self.opener = opener or urllib.request.build_opener(SafeDouyinRedirectHandler())
        self.timeout = timeout

    def resolve(self, value):
        input_url = normalize_input_url(value)
        aweme_item_id = work_id_from_url(input_url)
        resolved_url = input_url
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
        return {
            "input_url": input_url,
            "resolved_url": resolved_url,
            "canonical_url": canonical_work_url(aweme_item_id),
            "aweme_item_id": aweme_item_id,
        }


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
