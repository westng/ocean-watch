import argparse
import json
import sys

from ocean_watch import __version__
from ocean_watch.accounts import manage_accounts
from ocean_watch.auth import (
    migrate_channels,
    oauth_local_authorize,
    token_manager,
)
from ocean_watch.discovery import (
    query_deep_bid_types,
    query_dpa,
    query_event_assets,
    query_optimized_goals,
    query_projects,
    query_promotions,
    resolve_city_ids,
)
from ocean_watch.integrations import configure_official_mcp
from ocean_watch.materials import (
    query_creator_materials,
    query_images,
    query_products,
    query_qianchuan_creator_videos,
    query_videos,
)
from ocean_watch.onboarding import environment_check, first_run, validate_config
from ocean_watch.plans import (
    batch_create_creator_plans,
    batch_create_from_today_videos,
    batch_qianchuan_work_plans,
    create_creator_plan,
    create_plan,
    create_qianchuan_plan,
    remove_qianchuan_work_materials,
)
from ocean_watch.reports import (
    query_active_materials_report,
    query_custom_report,
    query_managed_accounts_report,
    query_qianchuan_plan_report,
    query_report_config,
)
from ocean_watch.templates import manage_plan_templates, manage_qianchuan_templates

COMMANDS = {
    ("setup", "doctor"): (environment_check.main, (), "Check local runtime requirements"),
    ("setup", "init"): (first_run.main, (), "Initialize local configuration"),
    ("setup", "validate"): (validate_config.main, (), "Validate configuration readiness"),
    ("auth", "set-app"): (oauth_local_authorize.main, ("--configure-app-only",), "Store app credentials"),
    ("auth", "authorize"): (oauth_local_authorize.main, (), "Run local OAuth authorization"),
    ("auth", "status"): (token_manager.main, ("--status",), "Show redacted token status"),
    ("auth", "refresh"): (token_manager.main, ("--refresh",), "Refresh an access token"),
    ("auth", "sync-accounts"): (token_manager.main, ("--sync-accounts",), "Sync advertisers"),
    ("auth", "migrate"): (migrate_channels.main, (), "Migrate legacy channel state"),
    ("accounts", "list"): (manage_accounts.main, ("list",), "List responsible accounts"),
    ("accounts", "add"): (manage_accounts.main, ("add",), "Add or update a responsible account"),
    ("accounts", "remove"): (manage_accounts.main, ("remove",), "Remove a responsible account"),
    ("accounts", "enable"): (manage_accounts.main, ("enable",), "Enable a responsible account"),
    ("accounts", "disable"): (manage_accounts.main, ("disable",), "Disable a responsible account"),
    ("accounts", "report"): (
        query_managed_accounts_report.main,
        (),
        "Query spend for responsible accounts",
    ),
    ("templates", "list"): (manage_plan_templates.main, ("list",), "List plan templates"),
    ("templates", "create"): (manage_plan_templates.main, ("create-wizard",), "Create a template"),
    ("templates", "migrate"): (manage_plan_templates.main, ("migrate",), "Migrate templates"),
    ("templates", "set-copy"): (manage_plan_templates.main, ("set-copy",), "Set copy materials"),
    ("qc-templates", "list"): (
        manage_qianchuan_templates.main,
        ("list",),
        "List Qianchuan product templates",
    ),
    ("qc-templates", "create"): (
        manage_qianchuan_templates.main,
        ("create-wizard",),
        "Create a Qianchuan product template",
    ),
    ("qc-templates", "migrate"): (
        manage_qianchuan_templates.main,
        ("migrate",),
        "Migrate Qianchuan product templates",
    ),
    ("materials", "videos"): (query_videos.main, (), "Query uploaded videos"),
    ("materials", "creator"): (query_creator_materials.main, (), "Query creator videos"),
    ("materials", "images"): (query_images.main, (), "Query image assets"),
    ("materials", "products"): (query_products.main, (), "Query product assets"),
    ("qc-materials", "creator-videos"): (
        query_qianchuan_creator_videos.main,
        (),
        "Query Qianchuan creator videos",
    ),
    ("plans", "create"): (create_plan.main, (), "Create from uploaded materials"),
    ("plans", "create-creator"): (create_creator_plan.main, (), "Create from creator materials"),
    ("plans", "create-qianchuan"): (create_qianchuan_plan.main, (), "Create a Qianchuan all-domain plan"),
    ("plans", "batch-qianchuan-works"): (
        batch_qianchuan_work_plans.main,
        (),
        "Create or append Qianchuan plans from Douyin work links",
    ),
    ("plans", "remove-qianchuan-work"): (
        remove_qianchuan_work_materials.main,
        (),
        "Remove Qianchuan plan materials by Douyin work link",
    ),
    ("plans", "batch-upload"): (batch_create_from_today_videos.main, (), "Batch uploaded materials"),
    ("plans", "batch-creator"): (batch_create_creator_plans.main, (), "Batch creator materials"),
    ("reports", "materials"): (query_active_materials_report.main, (), "Material performance"),
    ("reports", "schema"): (query_report_config.main, (), "Available report fields"),
    ("reports", "custom"): (query_custom_report.main, (), "Custom report"),
    ("qc-reports", "plans"): (
        query_qianchuan_plan_report.main,
        (),
        "Qianchuan all-domain plan performance through the official MCP",
    ),
    ("discover", "projects"): (query_projects.main, (), "Find projects"),
    ("discover", "promotions"): (query_promotions.main, (), "Find promotions"),
    ("discover", "dpa"): (query_dpa.main, (), "Find DPA assets"),
    ("discover", "events"): (query_event_assets.main, (), "Find event assets"),
    ("discover", "deep-bids"): (query_deep_bid_types.main, (), "Find deep bid types"),
    ("discover", "goals"): (query_optimized_goals.main, (), "Find optimization goals"),
    ("discover", "cities"): (resolve_city_ids.main, (), "Resolve city identifiers"),
    ("mcp", "configure"): (configure_official_mcp.main, (), "Configure official docs MCP"),
    ("mcp", "status"): (configure_official_mcp.main, ("--status",), "Check MCP status"),
}


def build_parser():
    parser = argparse.ArgumentParser(prog="ocean-watch", description="Ocean Engine operations for Codex")
    parser.add_argument("--version", action="version", version=f"%(prog)s {__version__}")
    domains = parser.add_subparsers(dest="domain", required=True)
    grouped = {}
    for (domain, action), (_, _, description) in COMMANDS.items():
        grouped.setdefault(domain, []).append((action, description))
    for domain, actions in grouped.items():
        domain_parser = domains.add_parser(domain)
        action_parsers = domain_parser.add_subparsers(dest="action", required=True)
        for action, description in actions:
            action_parsers.add_parser(action, help=description, add_help=False)
    return parser


def main(argv=None):
    arguments, command_arguments = build_parser().parse_known_args(argv)
    handler, prefix, _ = COMMANDS[(arguments.domain, arguments.action)]
    try:
        previous_program = sys.argv[0]
        sys.argv[0] = f"ocean-watch {arguments.domain} {arguments.action}"
        try:
            result = handler([*prefix, *command_arguments])
        finally:
            sys.argv[0] = previous_program
        return int(result or 0)
    except KeyboardInterrupt:
        print(json.dumps({"ok": False, "error": {"code": "interrupted", "message": "operation interrupted", "details": {}}}, ensure_ascii=False, indent=2))
        return 130
    except Exception as error:
        as_dict = getattr(error, "as_dict", None)
        payload = as_dict() if callable(as_dict) else {
            "ok": False,
            "error": {"code": "unexpected_error", "message": str(error), "details": {}},
        }
        print(json.dumps(payload, ensure_ascii=False, indent=2))
        return int(getattr(error, "exit_code", 1))


if __name__ == "__main__":
    sys.exit(main())
