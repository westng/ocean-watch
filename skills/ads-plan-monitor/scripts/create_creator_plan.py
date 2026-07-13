#!/usr/bin/env python3
import argparse
import copy
import json
from pathlib import Path

import channels
import config_paths
import create_plan
import creator_materials
import plan_templates
import query_videos
import token_manager


PROJECT_CREATE_PATH = "/v3.0/project/create/"
PROMOTION_CREATE_PATH = "/v3.0/promotion/create/"


def build_parser():
    parser = argparse.ArgumentParser(
        description="Create one project and native promotion from creator-authorized videos."
    )
    parser.add_argument("--config")
    parser.add_argument("--advertiser-id")
    parser.add_argument("--plan-template")
    parser.add_argument("--item-id", action="append")
    parser.add_argument("--budget", type=float)
    parser.add_argument("--bid", type=float)
    parser.add_argument("--roi-goal", type=float)
    parser.add_argument("--material-date")
    parser.add_argument("--product-name")
    parser.add_argument("--product-id")
    parser.add_argument("--project-name")
    parser.add_argument("--promotion-name")
    parser.add_argument("--project-id")
    parser.add_argument("--promotion-only", action="store_true")
    parser.add_argument("--submit", action="store_true")
    parser.add_argument("--out")
    token_manager.add_authorization_arguments(parser)
    return parser


def material_strategy(config):
    return config.get("material_strategy") or {}


def creator_payloads(config, args, selected):
    runtime = copy.deepcopy(config)
    runtime_materials = runtime.setdefault("materials", {})
    runtime_materials["video_ids"] = [candidate["video_id"] for candidate in selected]
    runtime_materials["video_cover_ids"] = {
        candidate["video_id"]: candidate["video_cover_id"]
        for candidate in selected
    }
    payload_args = argparse.Namespace(
        advertiser_id=args.advertiser_id,
        budget=args.budget,
        bid=args.bid,
        roi_goal=args.roi_goal,
        video_id=None,
        material_date=args.material_date,
        product_name=args.product_name,
        product_id=args.product_id,
        project_name=args.project_name,
        promotion_name=args.promotion_name,
        project_id=args.project_id,
    )
    project, promotion = create_plan.build_payloads(runtime, payload_args)
    promotion = creator_materials.apply_to_promotion_payload(promotion, selected)
    return runtime, project, promotion


def write_output(result, out_path=None):
    rendered = json.dumps(result, ensure_ascii=False, indent=2)
    if out_path:
        path = Path(out_path)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(rendered + "\n", encoding="utf-8")
    print(rendered)


def main(argv=None):
    args = build_parser().parse_args(argv)
    config_path = config_paths.resolve_config_path(args.config)
    raw_config = json.loads(config_path.read_text(encoding="utf-8"))
    raw_config = channels.runtime_config(raw_config, channel=args.channel, capability="create")

    try:
        config = plan_templates.apply(
            raw_config,
            args.plan_template,
            advertiser_id=args.advertiser_id,
            channel=args.channel,
        )
        strategy = material_strategy(config)
        if strategy.get("source_type") != creator_materials.SOURCE_TYPE:
            raise creator_materials.CreatorMaterialError(
                "material_source_mismatch",
                "selected plan template is not a creator-authorized material template",
                {"source_type": strategy.get("source_type")},
            )

        advertiser_id = args.advertiser_id or create_plan.get_path(config, "account.advertiser_id")
        config = token_manager.ensure_access_token(
            config_path,
            config,
            channel=args.channel,
            advertiser_id=advertiser_id,
            auth_account_id=args.auth_account_id,
        )
        query = creator_materials.fetch_candidates(
            query_videos.get_json,
            create_plan.get_path(config, "api.base_url"),
            create_plan.get_path(config, "api.access_token"),
            advertiser_id,
            auth_types=create_plan.get_path(strategy, "creator_filters.auth_types"),
            aweme_ids=create_plan.get_path(strategy, "creator_filters.creator_ids"),
            item_ids=args.item_id,
            minimum_remaining_days=create_plan.get_path(
                strategy,
                "creator_filters.minimum_remaining_days",
                1,
            ),
        )

        if not args.item_id and strategy.get("selection_mode") != "LATEST":
            write_output({
                "mode": "selection_required",
                "source_type": creator_materials.SOURCE_TYPE,
                "selected_plan_template": config.get("_selected_plan_template"),
                "candidate_count": len(query["candidates"]),
                "candidates": query["candidates"],
            }, args.out)
            return 0

        if args.item_id:
            selected = creator_materials.select_candidates(
                query["candidates"],
                args.item_id,
                max_materials=strategy.get("max_materials_per_unit", 5),
            )
        else:
            selected = creator_materials.select_latest_candidates(
                query["candidates"],
                max_materials=strategy.get("max_materials_per_unit", 5),
            )
        runtime, project_payload, promotion_payload = creator_payloads(config, args, selected)
        missing = create_plan.missing_fields(
            runtime,
            project_payload,
            promotion_payload,
            args.submit,
        )
    except (ValueError, creator_materials.CreatorMaterialError) as exc:
        write_output({
            "mode": "submit" if args.submit else "dry_run",
            "error_code": getattr(exc, "code", "creator_plan_invalid"),
            "error": str(exc),
            "details": getattr(exc, "details", {}),
        }, args.out)
        return 2

    result = {
        "mode": "submit" if args.submit else "dry_run",
        "source_type": creator_materials.SOURCE_TYPE,
        "selected_plan_template": config.get("_selected_plan_template"),
        "selected_creator": {
            "aweme_id": selected[0]["creator_id"],
            "aweme_name": selected[0].get("creator_name"),
        },
        "selected_materials": [
            {
                "item_id": candidate["item_id"],
                "video_id": candidate["video_id"],
                "authorization_expires_at": candidate["authorization_expires_at"],
            }
            for candidate in selected
        ],
        "project_endpoint": PROJECT_CREATE_PATH,
        "promotion_endpoint": PROMOTION_CREATE_PATH,
        "project_payload": project_payload,
        "promotion_payload": promotion_payload,
        "missing_fields": missing,
    }

    submit_failed = False
    if args.submit:
        if missing:
            result["submit_blocked"] = True
            result["blocking_fields"] = missing
        else:
            base_url = create_plan.get_path(config, "api.base_url")
            access_token = create_plan.get_path(config, "api.access_token")
            project_id = args.project_id
            if args.promotion_only:
                if not project_id or project_id == "{{project_id}}":
                    result["submit_blocked"] = True
                    result["blocking_fields"] = ["project_id"]
                    project_id = None
            else:
                project_response = create_plan.post_json(
                    base_url,
                    access_token,
                    PROJECT_CREATE_PATH,
                    project_payload,
                )
                result["project_response"] = project_response
                project_id = create_plan.get_path(project_response, "data.project_id")
                if not project_id:
                    result["submit_failed"] = True
                    result["failure_stage"] = "project_create"
                    submit_failed = True
            if project_id:
                promotion_payload["project_id"] = project_id
                promotion_response = create_plan.post_json(
                    base_url,
                    access_token,
                    PROMOTION_CREATE_PATH,
                    promotion_payload,
                )
                result["promotion_payload"] = promotion_payload
                result["promotion_response"] = promotion_response
                if not create_plan.get_path(promotion_response, "data.promotion_id"):
                    result["submit_failed"] = True
                    result["failure_stage"] = "promotion_create"
                    result["project_id"] = project_id
                    submit_failed = True

    write_output(result, args.out)
    return 1 if args.submit and (result.get("submit_blocked") or submit_failed) else 0


if __name__ == "__main__":
    raise SystemExit(main())
