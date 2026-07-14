#!/usr/bin/env python3
import argparse
import copy
import datetime as dt
import json
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
from types import SimpleNamespace

import ocean_watch.auth.channels as channels
import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths
import ocean_watch.materials.query_videos as query_videos
import ocean_watch.plans.create_plan as create_plan
import ocean_watch.templates.plan_templates as plan_templates
from ocean_watch.core.data import get_path, is_missing, split_csv
from ocean_watch.plans.executor import PlanExecutionRequest, PlanExecutor

PROJECT_CREATE_PATH = "/v3.0/project/create/"
PROMOTION_CREATE_PATH = "/v3.0/promotion/create/"
VIDEO_LIBRARY_PATH = "/2/file/video/get/"
VIDEO_AD_GET_PATH = "/2/file/video/ad/get/"
VIDEO_COVER_PATH = "/2/tools/video_cover/suggest/"


def unique_values(values):
    seen = set()
    result = []
    for value in values:
        key = str(value)
        if key in seen:
            continue
        seen.add(key)
        result.append(value)
    return result


def resolve_accounts(config, accounts_arg):
    requested = split_csv(accounts_arg)
    if requested:
        return unique_values(requested)

    accounts = []
    account_id = get_path(config, "account.advertiser_id")
    if not is_missing(account_id):
        accounts.append(str(account_id))

    for item in config.get("accounts") or []:
        if isinstance(item, dict):
            item_id = item.get("advertiser_id")
        else:
            item_id = item
        if not is_missing(item_id):
            accounts.append(str(item_id))

    return unique_values(accounts)


def parse_account_template_mappings(values):
    mappings = {}
    for item in values or []:
        if "=" not in item:
            raise ValueError("account template mapping must use ADVERTISER_ID=TEMPLATE_NAME")
        advertiser_id, template_name = item.split("=", 1)
        advertiser_id = advertiser_id.strip()
        template_name = template_name.strip()
        if not advertiser_id or not template_name:
            raise ValueError("account template mapping must include both advertiser ID and template name")
        if advertiser_id in mappings and mappings[advertiser_id] != template_name:
            raise ValueError(f"advertiser {advertiser_id} has multiple template mappings")
        mappings[advertiser_id] = template_name
    return mappings


def templates_by_advertiser(config):
    result = {}
    for name, raw_template in (config.get("plan_templates") or {}).items():
        template = plan_templates.normalize_template(config, name, raw_template)
        advertiser_id = str(template["bindings"].get("advertiser_id"))
        result.setdefault(advertiser_id, []).append(name)
    return result


def resolve_account_jobs(config, accounts_arg, mapping_args, fallback_template=None):
    explicit_mappings = parse_account_template_mappings(mapping_args)
    accounts = resolve_accounts(config, accounts_arg)
    if explicit_mappings:
        if accounts:
            missing = [account for account in accounts if account not in explicit_mappings]
            extra = [account for account in explicit_mappings if account not in accounts]
            if missing or extra:
                raise ValueError(
                    f"account template mappings must exactly match accounts; missing={missing}, extra={extra}"
                )
        else:
            accounts = list(explicit_mappings)

    if not accounts:
        raise ValueError("No advertiser account found. Set account.advertiser_id or pass --accounts.")

    bound_templates = templates_by_advertiser(config)
    jobs = []
    for advertiser_id in accounts:
        template_name = explicit_mappings.get(advertiser_id)
        if not template_name and fallback_template:
            if len(accounts) > 1:
                raise ValueError("--plan-template can only be used for one account; use --account-template for multiple accounts")
            template_name = fallback_template
        if not template_name:
            candidates = bound_templates.get(str(advertiser_id), [])
            if len(candidates) != 1:
                raise ValueError(
                    f"advertiser {advertiser_id} needs an explicit template mapping; candidates={candidates}"
                )
            template_name = candidates[0]
        effective = plan_templates.apply(
            config,
            template_name,
            advertiser_id=advertiser_id,
        )
        if get_path(effective, "material_strategy.source_type") == "CREATOR_AUTHORIZED":
            raise ValueError(
                f"plan template {template_name} uses creator-authorized materials; "
                "plans batch-upload only supports account uploads"
            )
        jobs.append({"advertiser_id": str(advertiser_id), "plan_template": template_name})
    return jobs


def parse_day(value):
    if not value:
        return dt.date.today()
    lowered = value.lower()
    if lowered == "today":
        return dt.date.today()
    if lowered == "yesterday":
        return dt.date.today() - dt.timedelta(days=1)
    return dt.date.fromisoformat(value)


def day_label(value):
    day = parse_day(value)
    return f"{day.month}.{day.day}"


def chunked(items, size):
    return [items[index:index + size] for index in range(0, len(items), size)]


def compact_response(response):
    if not isinstance(response, dict):
        return {"raw": str(response)}
    result = {
        "code": response.get("code"),
        "message": response.get("message"),
        "request_id": response.get("request_id"),
    }
    project_id = get_path(response, "data.project_id")
    promotion_id = get_path(response, "data.promotion_id")
    if project_id:
        result["project_id"] = project_id
    if promotion_id:
        result["promotion_id"] = promotion_id
    return {key: value for key, value in result.items() if value is not None}


def request_ids(responses):
    return [response.get("request_id") for response in responses if response.get("request_id")]


def response_code(response):
    return response.get("code") if isinstance(response, dict) else None


def fetch_library_videos(base_url, token, advertiser_id, args):
    start_time, end_time = query_videos.day_window(args.date)
    filtering = {}
    if start_time:
        filtering["start_time"] = start_time
    if end_time:
        filtering["end_time"] = end_time

    params = {
        "advertiser_id": advertiser_id,
        "filtering": filtering or None,
        "page": 1,
        "page_size": args.page_size,
    }
    rows, responses = query_videos.fetch_paged_list(
        base_url,
        token,
        VIDEO_LIBRARY_PATH,
        params,
        "data.list",
        fetch_all=True,
    )
    if args.filename:
        filename = args.filename.lower()
        rows = [
            row
            for row in rows
            if filename in str(row.get("filename", "")).lower()
        ]
    last_response = responses[-1] if responses else {}
    return {
        "params": params,
        "rows": rows,
        "responses": responses,
        "response_code": response_code(last_response),
        "response_message": last_response.get("message"),
        "page_info": query_videos.get_page_info(last_response),
        "request_ids": request_ids(responses),
    }


def compact_and_dedupe_videos(rows):
    videos = []
    skipped = []
    seen = set()
    for row in rows:
        video = query_videos.compact_video(row)
        video_id = video.get("video_id")
        if is_missing(video_id):
            skipped.append({
                "reason": "missing_video_id",
                "material_id": video.get("material_id"),
                "filename": video.get("filename"),
            })
            continue
        video_id = str(video_id)
        video["video_id"] = video_id
        if video_id in seen:
            skipped.append({
                "reason": "duplicate_video_id",
                "video_id": video_id,
                "material_id": video.get("material_id"),
                "filename": video.get("filename"),
            })
            continue
        seen.add(video_id)
        videos.append(video)
    return videos, skipped


def validate_ad_get(base_url, token, advertiser_id, videos, args):
    if not args.validate_ad_get or not videos:
        return videos, [], []

    accepted_ids = set()
    responses = []
    for video_group in chunked([video["video_id"] for video in videos], args.ad_get_batch_size):
        response = query_videos.get_json(
            base_url,
            token,
            VIDEO_AD_GET_PATH,
            {
                "advertiser_id": advertiser_id,
                "video_ids": video_group,
            },
        )
        responses.append(response)
        if response.get("code") != 0:
            raise RuntimeError(json.dumps(compact_response(response), ensure_ascii=False))
        for item in get_path(response, "data.list", []) or []:
            item_id = item.get("id") or item.get("video_id")
            if item_id:
                accepted_ids.add(str(item_id))

    accepted = []
    skipped = []
    for video in videos:
        if video["video_id"] in accepted_ids:
            accepted.append(video)
        else:
            skipped.append({
                "reason": "not_returned_by_ad_get",
                "video_id": video.get("video_id"),
                "material_id": video.get("material_id"),
                "filename": video.get("filename"),
            })
    return accepted, skipped, responses


def fetch_cover(base_url, token, advertiser_id, video, attempts, wait_sec):
    video_id = video["video_id"]
    last_response = {}
    for attempt in range(1, attempts + 1):
        response = query_videos.get_json(
            base_url,
            token,
            VIDEO_COVER_PATH,
            {
                "advertiser_id": advertiser_id,
                "video_id": video_id,
            },
        )
        last_response = response
        data_list = get_path(response, "data.list", []) or []
        if response.get("code") == 0 and data_list:
            cover_id = data_list[0].get("id")
            if not is_missing(cover_id):
                return {
                    "ok": True,
                    "video_id": video_id,
                    "video_cover_id": cover_id,
                    "attempts": attempt,
                    "status": get_path(response, "data.status"),
                    "request_id": response.get("request_id"),
                }
        if attempt < attempts:
            time.sleep(wait_sec)

    return {
        "ok": False,
        "video_id": video_id,
        "attempts": attempts,
        "status": get_path(last_response, "data.status"),
        "response": compact_response(last_response),
    }


def attach_covers(base_url, token, advertiser_id, videos, args):
    if not videos:
        return [], []

    workers = max(1, min(args.cover_concurrency, len(videos)))
    cover_results = {}
    with ThreadPoolExecutor(max_workers=workers) as executor:
        future_map = {
            executor.submit(
                fetch_cover,
                base_url,
                token,
                advertiser_id,
                video,
                args.cover_attempts,
                args.cover_wait_sec,
            ): video
            for video in videos
        }
        for future in as_completed(future_map):
            video = future_map[future]
            try:
                cover_results[video["video_id"]] = future.result()
            except Exception as exc:
                cover_results[video["video_id"]] = {
                    "ok": False,
                    "video_id": video["video_id"],
                    "error": str(exc),
                }

    ready = []
    skipped = []
    for video in videos:
        result = cover_results.get(video["video_id"], {})
        if result.get("ok"):
            enriched = copy.deepcopy(video)
            enriched["video_cover_id"] = result["video_cover_id"]
            enriched["cover_attempts"] = result.get("attempts")
            ready.append(enriched)
        else:
            skipped.append({
                "reason": "missing_video_cover",
                "video_id": video.get("video_id"),
                "material_id": video.get("material_id"),
                "filename": video.get("filename"),
                "cover_status": result.get("status"),
                "cover_response": result.get("response"),
            })
    return ready, skipped


def apply_overrides(config, args):
    effective = copy.deepcopy(config)
    defaults = effective.setdefault("defaults", {})
    if args.roi_goal is not None:
        defaults["roi_goal"] = args.roi_goal
    return effective


def group_names(config, args, group_number):
    defaults = config["defaults"]
    material_date = args.material_date or day_label(args.date)
    product_name = args.product_name or defaults["product_name"]
    suffix_number = args.start_index + group_number - 1
    suffix = f"{suffix_number:02d}"
    values = {
        "material_date": material_date,
        "product_name": product_name,
        "group_index": suffix_number,
        "index": suffix_number,
        "suffix": suffix,
    }

    project_name = create_plan.render_template(defaults["project_name_template"], values)
    promotion_name = create_plan.render_template(defaults["promotion_name_template"], values)
    if all(token not in defaults["project_name_template"] for token in ("{group_index}", "{index}", "{suffix}")):
        project_name = f"{project_name}_{suffix}"
    if all(token not in defaults["promotion_name_template"] for token in ("{group_index}", "{index}", "{suffix}")):
        promotion_name = f"{promotion_name}_{suffix}"
    return project_name, promotion_name


def build_group_payloads(base_config, advertiser_id, videos, args, group_number):
    config = copy.deepcopy(base_config)
    config.setdefault("materials", {})
    config["materials"]["video_ids"] = [video["video_id"] for video in videos]
    config["materials"]["video_cover_ids"] = {
        str(video["video_id"]): video["video_cover_id"]
        for video in videos
        if not is_missing(video.get("video_cover_id"))
    }

    project_name, promotion_name = group_names(config, args, group_number)
    payload_args = SimpleNamespace(
        advertiser_id=int(advertiser_id),
        material_date=args.material_date or day_label(args.date),
        product_name=args.product_name,
        product_id=args.product_id,
        budget=args.budget,
        bid=args.bid,
        video_id=None,
        project_name=project_name,
        promotion_name=promotion_name,
        project_id=None,
    )
    project_payload, promotion_payload = create_plan.build_payloads(config, payload_args)
    return config, project_payload, promotion_payload


def preflight_summary(project_payload, promotion_payload):
    delivery_setting = project_payload["delivery_setting"]
    summary = {
        "advertiser_id": project_payload["advertiser_id"],
        "project_name": project_payload["name"],
        "promotion_name": promotion_payload["name"],
        "budget": delivery_setting.get("budget"),
        "cpa_bid": delivery_setting.get("cpa_bid"),
        "city_count": len(project_payload["audience"].get("city") or []),
        "video_count": len(promotion_payload["promotion_materials"].get("video_material_list") or []),
        "operation": project_payload["operation"],
    }
    if delivery_setting.get("roi_goal") is not None:
        summary["roi_goal"] = delivery_setting["roi_goal"]
    return summary


def video_summary(videos):
    return [
        {
            "video_id": video.get("video_id"),
            "video_cover_id": video.get("video_cover_id"),
            "material_id": video.get("material_id"),
            "filename": video.get("filename"),
            "create_time": video.get("create_time"),
        }
        for video in videos
    ]


def process_group(base_config, base_url, token, advertiser_id, videos, group_number, args):
    config, project_payload, promotion_payload = build_group_payloads(
        base_config,
        advertiser_id,
        videos,
        args,
        group_number,
    )
    missing = create_plan.missing_fields(config, project_payload, promotion_payload, args.submit)
    result = {
        "group_index": group_number,
        "status": "blocked" if missing else ("submit_pending" if args.submit else "planned"),
        "videos": video_summary(videos),
        "missing_fields": missing,
        "preflight_summary": preflight_summary(project_payload, promotion_payload),
    }
    if args.include_payloads:
        result["project_payload"] = project_payload
        result["promotion_payload"] = promotion_payload
    if missing:
        return result
    if not args.submit:
        return result

    execution = PlanExecutor.from_credentials(base_url, token).execute(PlanExecutionRequest(
        project_payload=project_payload,
        promotion_payload=promotion_payload,
        submit=True,
    ))
    result.update({
        key: value
        for key, value in execution.items()
        if key not in {"project_payload", "promotion_payload"}
    })
    if result.get("project_response"):
        result["project_response"] = compact_response(result["project_response"])
    if result.get("promotion_response"):
        result["promotion_response"] = compact_response(result["promotion_response"])
    if execution.get("promotion_id"):
        result["status"] = "created"
    elif execution.get("failure_stage") == "project_create":
        result["status"] = "project_failed"
    else:
        result["status"] = "promotion_failed"
    return result


def process_account(raw_config, config_path, advertiser_id, template_name, args):
    plan_templates.apply(
        raw_config,
        template_name,
        advertiser_id=advertiser_id,
        channel=args.channel,
    )
    raw_config = token_manager.ensure_access_token(
        config_path,
        raw_config,
        channel=args.channel,
        advertiser_id=advertiser_id,
        auth_account_id=args.auth_account_id,
    )
    base_config = plan_templates.apply(
        raw_config,
        template_name,
        advertiser_id=advertiser_id,
        channel=args.channel,
    )
    base_config = apply_overrides(base_config, args)
    base_url = get_path(base_config, "api.base_url")
    token = get_path(base_config, "api.access_token")
    result = {
        "advertiser_id": str(advertiser_id),
        "plan_template": base_config.get("_selected_plan_template"),
        "status": "running",
        "groups": [],
        "skipped_videos": [],
    }

    if is_missing(base_url) or is_missing(token):
        result["status"] = "blocked"
        result["error"] = "missing api.base_url or api.access_token"
        return result

    try:
        library = fetch_library_videos(base_url, token, advertiser_id, args)
        result["video_query"] = {
            "endpoint": VIDEO_LIBRARY_PATH,
            "params": library["params"],
            "response_code": library["response_code"],
            "response_message": library["response_message"],
            "page_info": library["page_info"],
            "request_ids": library["request_ids"],
        }
        if library["response_code"] != 0:
            result["status"] = "query_failed"
            return result

        videos, skipped = compact_and_dedupe_videos(library["rows"])
        result["library_video_count"] = len(library["rows"])
        result["deduped_video_count"] = len(videos)
        result["skipped_videos"].extend(skipped)

        if args.max_videos is not None:
            videos = videos[:args.max_videos]
            result["limited_to_video_count"] = len(videos)

        videos, skipped, ad_get_responses = validate_ad_get(base_url, token, advertiser_id, videos, args)
        result["ad_get_request_ids"] = request_ids(ad_get_responses)
        result["validated_video_count"] = len(videos)
        result["skipped_videos"].extend(skipped)

        videos, skipped = attach_covers(base_url, token, advertiser_id, videos, args)
        result["cover_ready_video_count"] = len(videos)
        result["skipped_videos"].extend(skipped)
        if skipped and not args.skip_missing_cover:
            result["status"] = "blocked"
            result["error"] = "some videos have no suggested cover"
            return result

        groups = chunked(videos, args.videos_per_unit)
        result["group_count"] = len(groups)
        if not groups:
            result["status"] = "no_qualified_videos"
            return result

        workers = max(1, min(args.group_concurrency, len(groups)))
        group_results = []
        with ThreadPoolExecutor(max_workers=workers) as executor:
            future_map = {
                executor.submit(
                    process_group,
                    base_config,
                    base_url,
                    token,
                    advertiser_id,
                    group,
                    index,
                    args,
                ): index
                for index, group in enumerate(groups, start=1)
            }
            for future in as_completed(future_map):
                index = future_map[future]
                try:
                    group_results.append(future.result())
                except Exception as exc:
                    group_results.append({
                        "group_index": index,
                        "status": "failed",
                        "error": str(exc),
                    })

        result["groups"] = sorted(group_results, key=lambda item: item.get("group_index", 0))
        statuses = [group.get("status") for group in result["groups"]]
        result["created_project_count"] = len([group for group in result["groups"] if group.get("project_id")])
        result["created_promotion_count"] = len([group for group in result["groups"] if group.get("promotion_id")])
        result["blocked_group_count"] = len([status for status in statuses if status == "blocked"])
        result["failed_group_count"] = len([
            status
            for status in statuses
            if status in {"failed", "project_failed", "promotion_failed"}
        ])
        if args.submit:
            result["status"] = "completed" if result["failed_group_count"] == 0 and result["blocked_group_count"] == 0 else "completed_with_errors"
        else:
            result["status"] = "planned" if result["blocked_group_count"] == 0 else "planned_with_blocks"
        return result
    except Exception as exc:
        result["status"] = "failed"
        result["error"] = str(exc)
        return result


def totals(accounts):
    group_count = sum(account.get("group_count") or 0 for account in accounts)
    return {
        "account_count": len(accounts),
        "qualified_video_count": sum(account.get("cover_ready_video_count") or 0 for account in accounts),
        "skipped_video_count": sum(len(account.get("skipped_videos") or []) for account in accounts),
        "group_count": group_count,
        "created_project_count": sum(account.get("created_project_count") or 0 for account in accounts),
        "created_promotion_count": sum(account.get("created_promotion_count") or 0 for account in accounts),
        "failed_group_count": sum(account.get("failed_group_count") or 0 for account in accounts),
        "blocked_group_count": sum(account.get("blocked_group_count") or 0 for account in accounts),
    }


def positive_int(value):
    parsed = int(value)
    if parsed < 1:
        raise argparse.ArgumentTypeError("must be >= 1")
    return parsed


def batch_exit_code(accounts):
    bad_statuses = {
        "blocked",
        "query_failed",
        "failed",
        "completed_with_errors",
        "planned_with_blocks",
        "no_qualified_videos",
    }
    return 1 if any(account.get("status") in bad_statuses for account in accounts) else 0


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument("--config")
    parser.add_argument("--accounts", action="append", help="Comma-separated advertiser IDs. Defaults to config account.")
    parser.add_argument("--plan-template")
    parser.add_argument(
        "--account-template",
        action="append",
        help="Advertiser-to-template mapping as ADVERTISER_ID=TEMPLATE_NAME; repeat for multiple accounts.",
    )
    parser.add_argument("--date", default="today", help="today, yesterday, or yyyy-mm-dd.")
    parser.add_argument("--filename")
    parser.add_argument("--material-date", help="Name date label such as 7.8. Defaults to --date.")
    parser.add_argument("--product-name")
    parser.add_argument("--product-id")
    parser.add_argument("--budget", type=float)
    parser.add_argument("--bid", type=float, help="CPA bid override when the template uses cpa_bid.")
    parser.add_argument("--roi-goal", type=float, help="ROI goal override for ROI deep bid templates.")
    parser.add_argument("--videos-per-unit", type=positive_int)
    parser.add_argument("--max-videos", type=positive_int)
    parser.add_argument("--start-index", type=positive_int, default=1)
    parser.add_argument("--account-concurrency", type=positive_int, default=2)
    parser.add_argument("--group-concurrency", type=positive_int, default=2)
    parser.add_argument("--cover-concurrency", type=positive_int, default=4)
    parser.add_argument("--cover-attempts", type=positive_int, default=8)
    parser.add_argument("--cover-wait-sec", type=float, default=2.0)
    parser.add_argument("--page-size", type=positive_int, default=100)
    parser.add_argument("--ad-get-batch-size", type=positive_int, default=50)
    parser.add_argument("--validate-ad-get", action=argparse.BooleanOptionalAction, default=True)
    parser.add_argument("--skip-missing-cover", action=argparse.BooleanOptionalAction, default=True)
    parser.add_argument("--include-payloads", action="store_true")
    parser.add_argument("--submit", action="store_true")
    parser.add_argument("--out")
    token_manager.add_authorization_arguments(parser)
    args = parser.parse_args(argv)

    config_path = config_paths.resolve_config_path(args.config)
    raw_config = json.loads(config_path.read_text(encoding="utf-8"))
    raw_config = channels.runtime_config(raw_config, channel=args.channel, capability="create")
    jobs = resolve_account_jobs(
        raw_config,
        args.accounts,
        args.account_template,
        fallback_template=args.plan_template,
    )
    preview_config = create_plan.apply_plan_template(
        raw_config,
        jobs[0]["plan_template"],
        channel=args.channel,
    )
    if args.videos_per_unit is None:
        args.videos_per_unit = int(
            get_path(preview_config, "material_strategy.max_materials_per_unit")
            or get_path(preview_config, "defaults.max_videos_per_project", 5)
            or 5
        )
    if args.videos_per_unit > 5:
        raise SystemExit("videos-per-unit must be <= 5 for the current promotion material rule.")

    workers = max(1, min(args.account_concurrency, len(jobs)))
    account_results = []
    with ThreadPoolExecutor(max_workers=workers) as executor:
        future_map = {
            executor.submit(
                process_account,
                raw_config,
                config_path,
                job["advertiser_id"],
                job["plan_template"],
                args,
            ): job
            for job in jobs
        }
        for future in as_completed(future_map):
            job = future_map[future]
            account_id = job["advertiser_id"]
            try:
                account_results.append(future.result())
            except Exception as exc:
                account_results.append({
                    "advertiser_id": str(account_id),
                    "plan_template": {
                        "name": job["plan_template"],
                    },
                    "status": "failed",
                    "error": str(exc),
                    "groups": [],
                    "skipped_videos": [],
                })

    job_order = [job["advertiser_id"] for job in jobs]
    ordered_accounts = sorted(account_results, key=lambda item: job_order.index(item["advertiser_id"]))
    result = {
        "mode": "submit" if args.submit else "dry_run",
        "generated_at": dt.datetime.now().isoformat(timespec="seconds"),
        "config": str(config_path),
        "account_templates": jobs,
        "settings": {
            "date": args.date,
            "material_date": args.material_date or day_label(args.date),
            "videos_per_unit": args.videos_per_unit,
            "budget": args.budget,
            "bid": args.bid,
            "roi_goal": args.roi_goal,
            "account_concurrency": args.account_concurrency,
            "group_concurrency": args.group_concurrency,
            "cover_concurrency": args.cover_concurrency,
            "cover_attempts": args.cover_attempts,
            "cover_wait_sec": args.cover_wait_sec,
            "validate_ad_get": args.validate_ad_get,
            "skip_missing_cover": args.skip_missing_cover,
        },
        "accounts": ordered_accounts,
        "totals": totals(ordered_accounts),
    }

    output = json.dumps(result, ensure_ascii=False, indent=2)
    if args.out:
        out_path = Path(args.out)
        out_path.parent.mkdir(parents=True, exist_ok=True)
        out_path.write_text(output + "\n", encoding="utf-8")
    print(output)

    return batch_exit_code(ordered_accounts)


if __name__ == "__main__":
    raise SystemExit(main())
