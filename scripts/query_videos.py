#!/usr/bin/env python3
import argparse
import datetime as dt
import json
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

import token_manager


def get_path(data, dotted, default=None):
    current = data
    for part in dotted.split("."):
        if not isinstance(current, dict) or part not in current:
            return default
        current = current[part]
    return current


def get_json(base_url, token, path, params):
    query = urllib.parse.urlencode(
        {
            key: value if isinstance(value, str) else json.dumps(value, ensure_ascii=False)
            for key, value in params.items()
            if value is not None
        }
    )
    request = urllib.request.Request(
        base_url.rstrip("/") + path + "?" + query,
        headers={"Access-Token": token},
        method="GET",
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        return {"code": exc.code, "message": body}


def split_csv(values):
    result = []
    for value in values or []:
        result.extend(part.strip() for part in str(value).split(",") if part.strip())
    return result


def int_values(values):
    result = []
    for value in values:
        result.append(int(value))
    return result


def today():
    return dt.date.today().isoformat()


def resolve_date(value):
    if not value:
        return None
    lowered = value.lower()
    if lowered == "today":
        return dt.date.today()
    if lowered == "yesterday":
        return dt.date.today() - dt.timedelta(days=1)
    return dt.date.fromisoformat(value)


def day_window(value):
    date_value = resolve_date(value)
    if not date_value:
        return None, None
    return date_value.isoformat(), date_value.isoformat()


def normalize_time(value, which):
    if not value:
        return None
    return value[:10] if len(value) > 10 else value


def get_page_info(response):
    return get_path(response, "data.page_info") or {}


def total_pages(response):
    page_info = get_page_info(response)
    try:
        return int(page_info.get("total_page") or 0)
    except (TypeError, ValueError):
        return 0


def request_ids(responses):
    return [response.get("request_id") for response in responses if response.get("request_id")]


def fetch_paged_list(base_url, token, path, params, list_dotted_path, fetch_all=False):
    first_response = get_json(base_url, token, path, params)
    rows = list(get_path(first_response, list_dotted_path, []) or [])
    responses = [first_response]
    if not fetch_all or first_response.get("code") != 0:
        return rows, responses

    page_count = total_pages(first_response)
    current_page = int(params.get("page") or 1)
    for page in range(current_page + 1, page_count + 1):
        page_params = dict(params)
        page_params["page"] = page
        response = get_json(base_url, token, path, page_params)
        responses.append(response)
        if response.get("code") != 0:
            break
        rows.extend(get_path(response, list_dotted_path, []) or [])
    return rows, responses


def compact_video(item):
    return {
        "video_id": item.get("id") or item.get("video_id"),
        "material_id": item.get("material_id"),
        "filename": item.get("filename"),
        "create_time": item.get("create_time"),
        "width": item.get("width"),
        "height": item.get("height"),
        "duration": item.get("duration"),
        "format": item.get("format"),
        "source": item.get("source"),
        "signature": item.get("signature"),
        "poster_url": item.get("poster_url"),
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", default="config/ads-plan-monitor/config.json")
    parser.add_argument("--advertiser-id", type=int, help="Override account.advertiser_id from config.")
    parser.add_argument("--out")
    parser.add_argument("--video-id", action="append")
    parser.add_argument("--material-id", action="append")
    parser.add_argument("--signature", action="append")
    parser.add_argument("--filename")
    parser.add_argument("--date", help="Shortcut for upload day: today, yesterday, or yyyy-mm-dd.")
    parser.add_argument("--start-time", help="Upload start date, yyyy-mm-dd")
    parser.add_argument("--end-time", help="Upload end date, yyyy-mm-dd")
    parser.add_argument("--page", type=int, default=1)
    parser.add_argument("--page-size", type=int, default=20)
    parser.add_argument("--fetch-all", action="store_true", help="Fetch all returned pages for library-get.")
    parser.add_argument(
        "--mode",
        choices=["ad-get", "library-get", "cover-suggest"],
        default="library-get",
        help="library-get searches the video library; ad-get validates promotion-usable video IDs; cover-suggest gets recommended video covers.",
    )
    args = parser.parse_args()

    config_path = Path(args.config)
    config = json.loads(config_path.read_text(encoding="utf-8"))
    config = token_manager.ensure_access_token(config_path, config)
    video_ids = split_csv(args.video_id)
    if not video_ids and args.mode in {"ad-get", "cover-suggest"}:
        video_ids = [str(video_id) for video_id in get_path(config, "materials.video_ids", [])]
    material_ids = split_csv(args.material_id)
    signatures = split_csv(args.signature)
    base_url = get_path(config, "api.base_url")
    token = get_path(config, "api.access_token")
    advertiser_id = args.advertiser_id or get_path(config, "account.advertiser_id")

    if args.mode == "ad-get":
        path = "/2/file/video/ad/get/"
        params = {
            "advertiser_id": advertiser_id,
            "video_ids": video_ids,
        }
    elif args.mode == "cover-suggest":
        path = "/2/tools/video_cover/suggest/"
        params = {
            "advertiser_id": advertiser_id,
            "video_id": video_ids[0] if video_ids else None,
        }
    else:
        path = "/2/file/video/get/"
        filtering = {}
        filter_count = len([values for values in (material_ids, video_ids, signatures) if values])
        if filter_count > 1:
            raise SystemExit("Use only one of --material-id, --video-id, or --signature for library-get filtering.")
        if material_ids:
            filtering["material_ids"] = int_values(material_ids)
        elif video_ids:
            filtering["video_ids"] = video_ids
        elif signatures:
            filtering["signatures"] = signatures
        date_start, date_end = day_window(args.date) if args.date else (None, None)
        start_time = normalize_time(args.start_time, "start") or date_start
        end_time = normalize_time(args.end_time, "end") or date_end
        if start_time:
            filtering["start_time"] = start_time
        if end_time:
            filtering["end_time"] = end_time
        params = {
            "advertiser_id": advertiser_id,
            "filtering": filtering or None,
            "page": args.page,
            "page_size": args.page_size,
        }

    if args.mode == "library-get":
        data_list, responses = fetch_paged_list(
            base_url,
            token,
            path,
            params,
            "data.list",
            fetch_all=args.fetch_all,
        )
        response = responses[-1]
    else:
        response = get_json(base_url, token, path, params)
        responses = [response]
        data_list = get_path(response, "data.list", [])
    if args.filename and isinstance(data_list, list):
        data_list = [
            item
            for item in data_list
            if args.filename.lower() in str(item.get("filename", "")).lower()
        ]
    selected_cover_id = None
    if args.mode == "cover-suggest" and isinstance(data_list, list) and data_list:
        selected_cover_id = data_list[0].get("id")
    selected_videos = [compact_video(item) for item in data_list] if args.mode == "library-get" and isinstance(data_list, list) else []

    result = {
        "endpoint": path,
        "params": params,
        "response_code": response.get("code"),
        "response_message": response.get("message"),
        "request_id": response.get("request_id"),
        "request_ids": request_ids(responses),
        "status": get_path(response, "data.status"),
        "page_info": get_page_info(response),
        "matched_count": len(data_list) if isinstance(data_list, list) else None,
        "selected_videos": selected_videos,
        "matched_list": data_list,
        "selected_cover_id": selected_cover_id,
        "response": response,
    }
    output = json.dumps(result, ensure_ascii=False, indent=2)
    if args.out:
        out_path = Path(args.out)
        out_path.parent.mkdir(parents=True, exist_ok=True)
        out_path.write_text(output + "\n", encoding="utf-8")
    print(output)
    return 0 if response.get("code") == 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())
