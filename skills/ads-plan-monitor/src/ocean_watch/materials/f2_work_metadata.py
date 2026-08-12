import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path

from ocean_watch.materials.douyin_work_links import WorkLinkError

DEFAULT_CLI_TIMEOUT = 25
MAX_RESPONSE_BYTES = 1024 * 1024
EXPECTED_F2_VERSION = "0.0.1.7"
SAFE_ERROR_CODES = {
    "f2_batch_deadline_exceeded",
    "f2_metadata_query_failed",
    "f2_work_timeout",
}
PERFORMANCE_INTEGER_FIELDS = {
    "requested_count",
    "success_count",
    "failure_count",
    "concurrency",
    "retry_count",
    "timed_out_count",
}
PERFORMANCE_NUMBER_FIELDS = {
    "first_pass_seconds",
    "retry_seconds",
    "total_seconds",
    "deadline_seconds",
    "slowest_seconds",
}


class F2WorkMetadataCliResolver:
    def __init__(self, *, runner=None, timeout=None, command=None):
        self.runner = runner or subprocess.run
        self.timeout = timeout
        self.command = list(command or [
            sys.executable,
            "-m",
            "ocean_watch.materials.f2_work_metadata_cli",
        ])

    def resolve_many(self, work_ids, *, concurrency=8):
        work_ids = list(dict.fromkeys(
            str(value or "").strip() for value in work_ids or []
        ))
        if not work_ids:
            return {"results": {}, "errors": {}}
        command = [*self.command, "--concurrency", str(concurrency)]
        for work_id in work_ids:
            if not work_id.isdigit():
                raise WorkLinkError("invalid_f2_work_id", "F2 作品 ID 必须为数字")
            command.extend(("--work-id", work_id))
        try:
            environment = os.environ.copy()
            source_root = str(Path(__file__).resolve().parents[2])
            existing_pythonpath = environment.get("PYTHONPATH")
            environment["PYTHONPATH"] = os.pathsep.join(
                value for value in (source_root, existing_pythonpath) if value
            )
            timeout = self.timeout
            if timeout is None:
                timeout = DEFAULT_CLI_TIMEOUT
            with tempfile.TemporaryDirectory(prefix="ocean-watch-f2-run-") as directory:
                completed = self.runner(
                    command,
                    check=False,
                    stdin=subprocess.DEVNULL,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.PIPE,
                    cwd=directory,
                    env=environment,
                    timeout=timeout,
                )
        except subprocess.TimeoutExpired as error:
            raise WorkLinkError("f2_cli_timeout", "F2 作品解析超时") from error
        except OSError as error:
            raise WorkLinkError("f2_cli_unavailable", "F2 作品解析命令不可用") from error
        payload = completed.stdout or b""
        if isinstance(payload, str):
            payload = payload.encode("utf-8")
        if len(payload) > MAX_RESPONSE_BYTES:
            raise WorkLinkError("f2_response_too_large", "F2 作品解析响应超过大小限制")
        try:
            result = json.loads(payload.decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError) as error:
            raise WorkLinkError("invalid_f2_response", "F2 作品解析未返回有效 JSON") from error
        child_error = result.get("error") if isinstance(result, dict) else None
        if isinstance(child_error, dict) and child_error.get("code") == "f2_cli_failed":
            raise WorkLinkError(
                "f2_runtime_unavailable",
                f"当前 Python 解释器需要安装固定版本 F2 {EXPECTED_F2_VERSION}",
            )
        rows = result.get("results") if isinstance(result, dict) else None
        errors = result.get("errors") if isinstance(result, dict) else None
        if not isinstance(rows, dict) or not isinstance(errors, dict):
            raise WorkLinkError("invalid_f2_response", "F2 作品解析响应结构无效")
        validated = {}
        for work_id, metadata in rows.items():
            if work_id not in work_ids or not isinstance(metadata, dict):
                continue
            data = metadata.get("data")
            author = data.get("author") if isinstance(data, dict) else None
            product = data.get("product") if isinstance(data, dict) else None
            video = data.get("video") if isinstance(data, dict) else None
            item_id = str((video or {}).get("video_info_id") or "")
            creator_id = str((author or {}).get("uid") or "")
            visible_id = str((author or {}).get("unique_id") or "").strip()
            if (
                metadata.get("code") != 200
                or item_id != work_id
                or not creator_id.isdigit()
                or not visible_id
            ):
                continue
            creator_name = str((author or {}).get("nickname") or "").strip()
            product_id = str((product or {}).get("product_info_id") or "").strip()
            validated[work_id] = {
                "aweme_item_id": work_id,
                "creator_name_hint": creator_name or None,
                "owner_hint": {
                    "aweme_id": creator_id,
                    "aweme_show_id": visible_id,
                    "source": "f2_cli",
                },
                "product_hint": (
                    {
                        "product_id": product_id,
                        "product_name": str(
                            (product or {}).get("product_info_name") or ""
                        ).strip() or None,
                        "source": "f2_cli",
                    }
                    if product_id.isdigit()
                    else None
                ),
                "metadata": {
                    "code": 200,
                    "message": str(metadata.get("message") or "数据获取成功"),
                    "data": data,
                },
            }
        compact_errors = {}
        for work_id in work_ids:
            if work_id in validated:
                continue
            child_row = errors.get(work_id) if isinstance(errors, dict) else None
            child_code = child_row.get("code") if isinstance(child_row, dict) else None
            compact_errors[work_id] = {
                "code": (
                    child_code
                    if child_code in SAFE_ERROR_CODES
                    else "f2_metadata_query_failed"
                ),
                "message": "F2 未返回可用的公开作品元数据",
            }
        return {
            "results": validated,
            "errors": compact_errors,
            "performance": compact_performance(result.get("performance")),
        }


def compact_performance(value):
    if not isinstance(value, dict):
        return {}
    result = {}
    for field in PERFORMANCE_INTEGER_FIELDS:
        field_value = value.get(field)
        if isinstance(field_value, int) and not isinstance(field_value, bool) and field_value >= 0:
            result[field] = field_value
    for field in PERFORMANCE_NUMBER_FIELDS:
        field_value = value.get(field)
        if (
            isinstance(field_value, (int, float))
            and not isinstance(field_value, bool)
            and field_value >= 0
        ):
            result[field] = field_value
    slowest_work_id = str(value.get("slowest_work_id") or "")
    if slowest_work_id.isdigit():
        result["slowest_work_id"] = slowest_work_id
    return result
