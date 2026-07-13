#!/usr/bin/env python3
import argparse
import csv
import json
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

import token_manager
import config_store
import config_paths
import credential_store


def read_city_names(path):
    with Path(path).open("r", encoding="utf-8-sig", newline="") as f:
        rows = list(csv.DictReader(f))
    names = []
    for row in rows:
        value = (row.get("city") or row.get("城市") or "").strip()
        if value:
            names.append(value)
    return names


def get_json(base_url, token, path, params):
    query = urllib.parse.urlencode({
        key: value if isinstance(value, str) else json.dumps(value, ensure_ascii=False)
        for key, value in params.items()
    })
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


def flatten_admin_nodes(value):
    nodes = []
    if isinstance(value, dict):
        if any(k in value for k in ("name", "code", "id")):
            nodes.append(value)
        for child_key in (
            "children",
            "child",
            "list",
            "districts",
            "regions",
            "sub_admin_info",
            "sub_districts",
        ):
            child = value.get(child_key)
            if child is not None:
                nodes.extend(flatten_admin_nodes(child))
    elif isinstance(value, list):
        for item in value:
            nodes.extend(flatten_admin_nodes(item))
    return nodes


def normalize_name(name):
    if not name:
        return ""
    suffixes = [
        "壮族自治区",
        "回族自治区",
        "维吾尔自治区",
        "自治区",
        "省",
        "市",
    ]
    result = str(name).strip()
    for suffix in suffixes:
        if result.endswith(suffix):
            result = result[: -len(suffix)]
            break
    return result


def node_name(node):
    for key in ("name", "region_name", "district_name", "cn_name"):
        if node.get(key):
            return str(node[key])
    return ""


def node_code(node):
    for key in ("code", "id", "region_id", "district_id", "geoname_id"):
        if node.get(key) is not None:
            return node[key]
    return None


def resolve(config, city_csv, country_codes):
    names = read_city_names(city_csv)
    base_url = config["api"]["base_url"]
    token = config["api"]["access_token"]
    advertiser_id = config["account"]["advertiser_id"]

    attempts = []
    for country_code in country_codes:
        rsp = get_json(
            base_url,
            token,
            "/2/tools/admin/info/",
            {
                "advertiser_id": advertiser_id,
                "codes": [country_code],
                "language": "ZH_CN_GOV",
                "sub_district": "ONE_LEVEL",
                "version": "V2_3_2",
            },
        )
        nodes = flatten_admin_nodes(rsp.get("data"))
        mapping = {}
        for node in nodes:
            name = node_name(node)
            code = node_code(node)
            if name and code is not None:
                mapping[name] = code
                mapping[normalize_name(name)] = code
        resolved = []
        missing = []
        for name in names:
            code = mapping.get(name) or mapping.get(normalize_name(name))
            if code is None:
                missing.append(name)
            else:
                resolved.append({"name": name, "code": code})
        attempts.append({
            "country_code": country_code,
            "response_code": rsp.get("code"),
            "response_message": rsp.get("message"),
            "node_count": len(nodes),
            "resolved": resolved,
            "missing": missing,
            "raw_response": rsp if len(nodes) == 0 else None,
        })
        if resolved and not missing:
            return attempts[-1], attempts
    return attempts[-1] if attempts else {}, attempts


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config")
    parser.add_argument("--city-csv", required=True)
    parser.add_argument("--country-code", action="append", default=["CN", "CHN", "156"])
    parser.add_argument("--write-config", action="store_true")
    parser.add_argument("--out")
    args = parser.parse_args()

    config_path = config_paths.resolve_config_path(args.config)
    config = json.loads(config_path.read_text(encoding="utf-8"))
    config = token_manager.ensure_access_token(config_path, config)
    best, attempts = resolve(config, args.city_csv, args.country_code)
    result = {
        "city_csv": args.city_csv,
        "best_country_code": best.get("country_code"),
        "resolved_count": len(best.get("resolved", [])),
        "missing": best.get("missing", []),
        "resolved": best.get("resolved", []),
        "attempts": attempts,
    }
    if args.write_config and result["resolved_count"] and not result["missing"]:
        config.setdefault("resolved_ids", {})["city_ids"] = [item["code"] for item in result["resolved"]]
        config.setdefault("resolved_ids", {})["city_names"] = [item["name"] for item in result["resolved"]]
        safe_config = credential_store.strip_sensitive_config(config)
        config_store.atomic_write_json(config_path, safe_config)
        result["config_updated"] = str(config_path)

    output = json.dumps(result, ensure_ascii=False, indent=2)
    if args.out:
        out_path = Path(args.out)
        out_path.parent.mkdir(parents=True, exist_ok=True)
        out_path.write_text(output + "\n", encoding="utf-8")
    print(output)
    return 0 if result["resolved_count"] and not result["missing"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
