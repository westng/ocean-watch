import argparse
import json
import sys

from ocean_watch import __version__
from ocean_watch.auth import (
    credential_store,
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
    query_videos,
)
from ocean_watch.onboarding import first_run, validate_config
from ocean_watch.plans import (
    batch_create_creator_plans,
    batch_create_from_today_videos,
    create_creator_plan,
    create_plan,
)
from ocean_watch.reports import (
    query_active_materials_report,
    query_custom_report,
    query_report_config,
)
from ocean_watch.templates import manage_plan_templates

COMMANDS = {
    ("setup", "init"): (first_run.main, (), "Initialize local configuration"),
    ("setup", "validate"): (validate_config.main, (), "Validate configuration readiness"),
    ("auth", "set-app"): (credential_store.main, ("--set-app",), "Store app credentials"),
    ("auth", "authorize"): (oauth_local_authorize.main, (), "Run local OAuth authorization"),
    ("auth", "status"): (token_manager.main, ("--status",), "Show redacted token status"),
    ("auth", "refresh"): (token_manager.main, ("--refresh",), "Refresh an access token"),
    ("auth", "sync-accounts"): (token_manager.main, ("--sync-accounts",), "Sync advertisers"),
    ("auth", "migrate"): (migrate_channels.main, (), "Migrate legacy channel state"),
    ("templates", "list"): (manage_plan_templates.main, ("list",), "List plan templates"),
    ("templates", "create"): (manage_plan_templates.main, ("create-wizard",), "Create a template"),
    ("templates", "migrate"): (manage_plan_templates.main, ("migrate",), "Migrate templates"),
    ("templates", "set-copy"): (manage_plan_templates.main, ("set-copy",), "Set copy materials"),
    ("materials", "videos"): (query_videos.main, (), "Query uploaded videos"),
    ("materials", "creator"): (query_creator_materials.main, (), "Query creator videos"),
    ("materials", "images"): (query_images.main, (), "Query image assets"),
    ("materials", "products"): (query_products.main, (), "Query product assets"),
    ("plans", "create"): (create_plan.main, (), "Create from uploaded materials"),
    ("plans", "create-creator"): (create_creator_plan.main, (), "Create from creator materials"),
    ("plans", "batch-upload"): (batch_create_from_today_videos.main, (), "Batch uploaded materials"),
    ("plans", "batch-creator"): (batch_create_creator_plans.main, (), "Batch creator materials"),
    ("reports", "materials"): (query_active_materials_report.main, (), "Material performance"),
    ("reports", "schema"): (query_report_config.main, (), "Available report fields"),
    ("reports", "custom"): (query_custom_report.main, (), "Custom report"),
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
        payload = error.as_dict() if hasattr(error, "as_dict") else {
            "ok": False,
            "error": {"code": "unexpected_error", "message": str(error), "details": {}},
        }
        print(json.dumps(payload, ensure_ascii=False, indent=2))
        return int(getattr(error, "exit_code", 1))


if __name__ == "__main__":
    sys.exit(main())
