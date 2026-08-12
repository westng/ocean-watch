#!/usr/bin/env python3
import argparse
import asyncio
import importlib.metadata
import json
import logging
import os
import tempfile
import time
from pathlib import Path

DEFAULT_CONCURRENCY = 8
MAX_CONCURRENCY = 10
DEFAULT_REQUEST_TIMEOUT = 5
PER_WORK_TIMEOUT = 8
BATCH_TIMEOUT = 20
EXPECTED_F2_VERSION = "0.0.1.7"
DOUYIN_COOKIE_ENV = "OCEAN_WATCH_F2_DOUYIN_COOKIE"
USER_AGENT = (
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "
    "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"
)


def validate_work_ids(values):
    result = []
    seen = set()
    for value in values or []:
        work_id = str(value or "").strip()
        if not work_id.isdigit():
            raise ValueError("F2 metadata work IDs must be numeric")
        if work_id not in seen:
            seen.add(work_id)
            result.append(work_id)
    if not result:
        raise ValueError("at least one F2 metadata work ID is required")
    return result


def validate_f2_runtime(version_factory=None):
    version_factory = version_factory or importlib.metadata.version
    try:
        version = str(version_factory("f2") or "").strip()
    except importlib.metadata.PackageNotFoundError as error:
        raise RuntimeError("F2 0.0.1.7 is not installed in the current Python runtime") from error
    if version != EXPECTED_F2_VERSION:
        raise RuntimeError(
            f"F2 {EXPECTED_F2_VERSION} is required in the current Python runtime"
        )
    return version


def first_value(value, *path):
    current = value
    for key in path:
        if isinstance(key, int):
            if not isinstance(current, list) or key >= len(current):
                return None
            current = current[key]
        elif isinstance(current, dict):
            current = current.get(key)
        else:
            return None
    return current


def parse_product(detail):
    product = {
        "product_info_id": "",
        "product_info_img": "",
        "product_info_name": "",
    }
    extra = first_value(detail, "anchor_info", "extra")
    if not extra:
        return product
    try:
        products = json.loads(extra) if isinstance(extra, str) else extra
    except json.JSONDecodeError:
        return product
    if not isinstance(products, list) or not products or not isinstance(products[0], dict):
        return product
    item = products[0]
    return {
        "product_info_id": str(item.get("product_id") or ""),
        "product_info_img": str(first_value(item, "elastic_images", 0, "uri") or ""),
        "product_info_name": str(item.get("title") or ""),
    }


def map_video_response(video, expected_work_id):
    raw = video._to_raw()
    detail = raw.get("aweme_detail") if isinstance(raw, dict) else None
    if not isinstance(detail, dict):
        raise ValueError("F2 returned invalid work metadata")
    work_id = str(detail.get("aweme_id") or "").strip()
    if work_id != expected_work_id:
        raise ValueError("F2 returned mismatched work metadata")
    author = detail.get("author") if isinstance(detail.get("author"), dict) else {}
    author_result = {
        "nickname": str(author.get("nickname") or ""),
        "unique_id": str(author.get("unique_id") or author.get("short_id") or ""),
        "uid": str(author.get("uid") or ""),
        "avatar": str(first_value(author, "avatar_thumb", "url_list", 0) or ""),
    }
    video_result = {
        "video_info_cover": str(first_value(detail, "video", "cover", "url_list", 0) or ""),
        "video_info_id": work_id,
        "video_info_title": str(detail.get("preview_title") or ""),
        "video_info_url": "",
        "play_url": str(first_value(detail, "music", "play_url", "uri") or ""),
    }
    if detail.get("aweme_type") == 0:
        video_result["video_info_url"] = str(
            first_value(detail, "video", "bit_rate", 0, "play_addr", "url_list", 0)
            or ""
        )
    return {
        "code": 200,
        "message": "数据获取成功",
        "data": {
            "author": author_result,
            "product": parse_product(detail),
            "video": video_result,
        },
    }


def proxy_config():
    proxy = (
        os.environ.get("HTTPS_PROXY")
        or os.environ.get("https_proxy")
        or os.environ.get("HTTP_PROXY")
        or os.environ.get("http_proxy")
    )
    return {"http://": proxy, "https://": proxy}


def configure_f2_logging():
    f2_logger = logging.getLogger("f2")
    f2_logger.handlers.clear()
    f2_logger.addHandler(logging.NullHandler())
    f2_logger.propagate = False
    f2_logger.disabled = True


def load_handler_factory():
    configure_f2_logging()
    from f2.apps.douyin.handler import DouyinHandler

    return DouyinHandler


def load_shared_crawler_types():
    configure_f2_logging()
    from f2.apps.douyin.crawler import DouyinCrawler
    from f2.apps.douyin.filter import PostDetailFilter
    from f2.apps.douyin.model import PostDetail

    return DouyinCrawler, PostDetail, PostDetailFilter


def load_ttwid_factory():
    from f2.apps.douyin.utils import TokenManager

    return TokenManager.gen_ttwid


def resolve_cookie(ttwid_factory=None):
    cookie = str(os.environ.get(DOUYIN_COOKIE_ENV) or "").strip()
    if cookie:
        return cookie
    ttwid_factory = ttwid_factory or load_ttwid_factory()
    ttwid = str(ttwid_factory() or "").strip()
    if not ttwid:
        raise ValueError("F2 did not generate a visitor cookie")
    return f"ttwid={ttwid};"


async def fetch_many(
    work_ids,
    *,
    concurrency=DEFAULT_CONCURRENCY,
    handler_factory=None,
    ttwid_factory=None,
    per_work_timeout=PER_WORK_TIMEOUT,
    batch_timeout=BATCH_TIMEOUT,
):
    semaphore = asyncio.Semaphore(concurrency)
    started_at = time.monotonic()
    deadline = started_at + batch_timeout
    kwargs = {
        "headers": {"User-Agent": USER_AGENT, "Referer": "https://www.douyin.com/"},
        "cookie": resolve_cookie(ttwid_factory),
        "proxies": proxy_config(),
        "max_connections": concurrency,
        "max_tasks": concurrency,
        "max_retries": 1,
        "timeout": DEFAULT_REQUEST_TIMEOUT,
    }

    crawler = None
    if handler_factory is None:
        crawler_type, request_type, filter_type = load_shared_crawler_types()
        crawler = crawler_type(kwargs)

        async def fetch_video(work_id):
            response = await crawler.fetch_post_detail(request_type(aweme_id=work_id))
            video = filter_type(response)
            if video.nickname is None:
                raise ValueError("F2 returned incomplete work metadata")
            return video
    else:

        async def fetch_video(work_id):
            return await handler_factory(kwargs).fetch_one_video(work_id)

    async def fetch(work_id, attempts):
        attempt_started_at = time.monotonic()
        async with semaphore:
            try:
                video = await asyncio.wait_for(
                    fetch_video(work_id),
                    timeout=per_work_timeout,
                )
                error = None
                metadata = map_video_response(video, work_id)
            except asyncio.TimeoutError:
                metadata = None
                error = {
                    "code": "f2_work_timeout",
                    "message": "F2 work metadata query timed out",
                }
            except Exception:
                metadata = None
                error = {
                    "code": "f2_metadata_query_failed",
                    "message": "F2 did not return usable public work metadata",
                }
        elapsed = time.monotonic() - attempt_started_at
        attempts.setdefault(work_id, []).append(elapsed)
        return work_id, metadata, error

    async def run_round(round_work_ids, attempts):
        tasks = {
            asyncio.create_task(fetch(work_id, attempts)): work_id
            for work_id in round_work_ids
        }
        remaining = max(0.0, deadline - time.monotonic())
        done, pending = await asyncio.wait(tasks, timeout=remaining)
        rows = []
        for task in done:
            rows.append(task.result())
        for task in pending:
            task.cancel()
        if pending:
            await asyncio.gather(*pending, return_exceptions=True)
            rows.extend(
                (
                    tasks[task],
                    None,
                    {
                        "code": "f2_batch_deadline_exceeded",
                        "message": "F2 batch metadata deadline was exceeded",
                    },
                )
                for task in pending
            )
        return rows

    try:
        attempts = {}
        first_started_at = time.monotonic()
        first_rows = await run_round(work_ids, attempts)
        first_finished_at = time.monotonic()
        results = {}
        errors = {}
        for work_id, metadata, error in first_rows:
            if metadata is not None:
                results[work_id] = metadata
            else:
                errors[work_id] = error
        retry_ids = [
            work_id
            for work_id in work_ids
            if work_id in errors and time.monotonic() < deadline
        ]
        retry_started_at = time.monotonic()
        if retry_ids:
            retry_rows = await run_round(retry_ids, attempts)
            for work_id, metadata, error in retry_rows:
                if metadata is not None:
                    results[work_id] = metadata
                    errors.pop(work_id, None)
                else:
                    errors[work_id] = error
        retry_finished_at = time.monotonic()
    finally:
        if crawler is not None:
            await crawler.close()
    total_seconds = time.monotonic() - started_at
    slowest_work_id = None
    slowest_seconds = 0.0
    for work_id, durations in attempts.items():
        elapsed = sum(durations)
        if elapsed > slowest_seconds:
            slowest_work_id = work_id
            slowest_seconds = elapsed
    return {
        "ok": not errors,
        "mode": "f2_work_metadata",
        "results": results,
        "errors": errors,
        "performance": {
            "requested_count": len(work_ids),
            "success_count": len(results),
            "failure_count": len(errors),
            "concurrency": concurrency,
            "first_pass_seconds": round(first_finished_at - first_started_at, 3),
            "retry_count": len(retry_ids),
            "retry_seconds": round(retry_finished_at - retry_started_at, 3),
            "total_seconds": round(total_seconds, 3),
            "deadline_seconds": batch_timeout,
            "timed_out_count": sum(
                1
                for error in errors.values()
                if error.get("code") in {"f2_work_timeout", "f2_batch_deadline_exceeded"}
            ),
            "slowest_work_id": slowest_work_id,
            "slowest_seconds": round(slowest_seconds, 3),
        },
    }


def execute(work_ids, *, concurrency=DEFAULT_CONCURRENCY):
    concurrency = int(concurrency)
    if concurrency < 1 or concurrency > MAX_CONCURRENCY:
        raise ValueError(f"concurrency must be between 1 and {MAX_CONCURRENCY}")
    work_ids = validate_work_ids(work_ids)
    validate_f2_runtime()
    original_directory = Path.cwd()
    with tempfile.TemporaryDirectory(prefix="ocean-watch-f2-") as directory:
        os.chdir(directory)
        try:
            return asyncio.run(fetch_many(work_ids, concurrency=concurrency))
        finally:
            logging.shutdown()
            os.chdir(original_directory)


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Resolve public Douyin work metadata through F2 without downloading media."
    )
    parser.add_argument("--work-id", action="append", required=True)
    parser.add_argument("--concurrency", type=int, default=DEFAULT_CONCURRENCY)
    args = parser.parse_args(argv)
    try:
        result = execute(args.work_id, concurrency=args.concurrency)
    except Exception:
        result = {
            "ok": False,
            "mode": "f2_work_metadata",
            "error": {
                "code": "f2_cli_failed",
                "message": "F2 metadata CLI could not start",
            },
        }
        exit_code = 2
    else:
        exit_code = 0 if result["ok"] else 1
    print(json.dumps(result, ensure_ascii=False))
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
