#!/usr/bin/env python3
import argparse

from ocean_watch.core import config_paths, config_store
from ocean_watch.core.errors import ConfigurationError
from ocean_watch.core.output import write_json
from ocean_watch.core.validation import positive_integer
from ocean_watch.materials.douyin_work_links import (
    DEFAULT_CONCURRENCY,
    MAX_CONCURRENCY,
    resolve_work_links,
)
from ocean_watch.materials.f2_work_metadata import F2WorkMetadataCliResolver


def inspect(config, work_urls, *, concurrency=DEFAULT_CONCURRENCY):
    concurrency = positive_integer(
        concurrency,
        "concurrency",
        maximum=MAX_CONCURRENCY,
    )
    result = resolve_work_links(
        work_urls,
        concurrency=concurrency,
        metadata_resolver=F2WorkMetadataCliResolver(),
    )
    return {
        "ok": not result["skipped"] and result.get("metadata_error") is None,
        "mode": "qianchuan_work_inspection",
        "metadata_integration": "f2_cli",
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
