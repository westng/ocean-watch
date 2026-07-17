#!/usr/bin/env python3
import argparse

from ocean_watch.core import config_paths, config_store
from ocean_watch.core.errors import ConfigurationError
from ocean_watch.core.output import write_json
from ocean_watch.core.validation import positive_integer
from ocean_watch.integrations import qianchuan_work_metadata
from ocean_watch.materials.douyin_work_links import (
    DEFAULT_CONCURRENCY,
    MAX_CONCURRENCY,
    DouyinWorkLinkResolver,
    DouyinWorkMetadataResolver,
    resolve_work_links,
)


def inspect(config, work_urls, *, concurrency=DEFAULT_CONCURRENCY):
    concurrency = positive_integer(
        concurrency,
        "concurrency",
        maximum=MAX_CONCURRENCY,
    )
    endpoint = qianchuan_work_metadata.endpoint_from_config(config)
    metadata_resolver = DouyinWorkMetadataResolver(endpoint) if endpoint else None
    result = resolve_work_links(
        work_urls,
        resolver=DouyinWorkLinkResolver(metadata_resolver=metadata_resolver),
        concurrency=concurrency,
    )
    if endpoint:
        for row in [*result["resolved"], *result["skipped"]]:
            warning = row.get("hint_warning")
            if isinstance(warning, dict) and isinstance(warning.get("message"), str):
                warning["message"] = warning["message"].replace(
                    endpoint,
                    "<configured locally>",
                )
            if isinstance(row.get("message"), str):
                row["message"] = row["message"].replace(
                    endpoint,
                    "<configured locally>",
                )
    return {
        "ok": not result["skipped"],
        "mode": "qianchuan_work_inspection",
        "metadata_integration": "configured" if endpoint else "not_configured",
        "input_count": len(work_urls),
        "resolved_count": len(result["resolved"]),
        "skipped_count": len(result["skipped"]),
        **result,
    }


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Inspect public Douyin work links without creating a Qianchuan plan."
    )
    parser.add_argument("--config")
    parser.add_argument("--work-url", action="append", required=True)
    parser.add_argument("--concurrency", type=int, default=DEFAULT_CONCURRENCY)
    parser.add_argument("--out")
    args = parser.parse_args(argv)

    config_path = config_paths.resolve_config_path(args.config)
    if not config_path.is_file():
        raise ConfigurationError("local config does not exist", {"config": str(config_path)})
    result = inspect(
        config_store.load_json(config_path),
        args.work_url,
        concurrency=args.concurrency,
    )
    result["config"] = str(config_path)
    write_json(result, destination=args.out)
    return 0 if result["ok"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
