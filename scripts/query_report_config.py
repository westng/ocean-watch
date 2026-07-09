#!/usr/bin/env python3
import argparse
import json
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

import token_manager


DEFAULT_DATA_TOPICS = ["MATERIAL_DATA"]


def get_path(data, dotted, default=None):
    current = data
    for part in dotted.split("."):
        if not isinstance(current, dict) or part not in current:
            return default
        current = current[part]
    return current


def split_csv(values):
    result = []
    for value in values or []:
        result.extend(part.strip() for part in str(value).split(",") if part.strip())
    return result


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


def compact_topic(topic):
    return {
        "data_topic": topic.get("data_topic"),
        "dimension_count": len(topic.get("dimensions") or []),
        "metric_count": len(topic.get("metrics") or []),
        "dimensions": [
            {
                "field": item.get("field"),
                "name": item.get("name"),
                "sort_able": item.get("sort_able"),
            }
            for item in topic.get("dimensions") or []
        ],
        "metrics": [
            {
                "field": item.get("field"),
                "name": item.get("name"),
            }
            for item in topic.get("metrics") or []
        ],
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", default="config/ads-plan-monitor/config.json")
    parser.add_argument("--out", help="Optional JSON output path. No file is written by default.")
    parser.add_argument(
        "--data-topic",
        action="append",
        help="Data topic or comma-separated topics, such as MATERIAL_DATA,BASIC_DATA.",
    )
    parser.add_argument("--full", action="store_true", help="Print the full API response.")
    args = parser.parse_args()

    config_path = Path(args.config)
    config = json.loads(config_path.read_text(encoding="utf-8"))
    config = token_manager.ensure_access_token(config_path, config)
    data_topics = split_csv(args.data_topic) or DEFAULT_DATA_TOPICS
    params = {
        "advertiser_id": get_path(config, "account.advertiser_id"),
        "data_topics": data_topics,
    }
    response = get_json(
        get_path(config, "api.base_url"),
        get_path(config, "api.access_token"),
        "/v3.0/report/custom/config/get/",
        params,
    )
    topics = get_path(response, "data.list", []) or []
    result = {
        "endpoint": "/v3.0/report/custom/config/get/",
        "params": params,
        "response_code": response.get("code"),
        "response_message": response.get("message"),
        "request_id": response.get("request_id"),
        "topics": topics if args.full else [compact_topic(topic) for topic in topics],
    }
    if args.full:
        result["response"] = response

    output = json.dumps(result, ensure_ascii=False, indent=2)
    if args.out:
        out_path = Path(args.out)
        out_path.parent.mkdir(parents=True, exist_ok=True)
        out_path.write_text(output + "\n", encoding="utf-8")
    print(output)
    return 0 if response.get("code") == 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())
