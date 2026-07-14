#!/usr/bin/env python3
import argparse
import copy
import datetime as dt
import hashlib
import json
import os
import threading
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
from types import SimpleNamespace

import ocean_watch.auth.authorization_store as authorization_store
import ocean_watch.auth.channels as channels
import ocean_watch.core.config_paths as config_paths
import ocean_watch.core.config_store as config_store
import ocean_watch.materials.creator_materials as creator_materials
import ocean_watch.plans.create_creator_plan as create_creator_plan
import ocean_watch.plans.create_plan as create_plan
import ocean_watch.templates.plan_templates as plan_templates
from ocean_watch.core.data import get_path

SCHEMA_VERSION = 1
DEFAULT_CONCURRENCY = 4
MAX_CONCURRENCY = 10
PRODUCT_MATCH_STATUSES = {"MATCHED", "USER_CONFIRMED"}


class CreatorBatchError(ValueError):
    def __init__(self, code, message, details=None):
        super().__init__(message)
        self.code = code
        self.details = details or {}


def build_parser():
    parser = argparse.ArgumentParser(
        description="Create creator-authorized projects and promotions concurrently."
    )
    parser.add_argument("--config")
    parser.add_argument("--jobs-file", required=True)
    parser.add_argument("--concurrency", type=int, default=DEFAULT_CONCURRENCY)
    parser.add_argument("--journal")
    parser.add_argument("--submit", action="store_true")
    parser.add_argument("--include-payloads", action="store_true")
    parser.add_argument("--out")
    parser.add_argument("--channel", choices=tuple(channels.CHANNELS))
    parser.add_argument("--auth-account-id")
    return parser


def decimal_id(value, field):
    if value is None or isinstance(value, bool):
        raise CreatorBatchError("invalid_batch_job", f"{field} must be a decimal string")
    normalized = str(value).strip()
    if not normalized.isdigit():
        raise CreatorBatchError("invalid_batch_job", f"{field} must be a decimal string")
    return normalized


def nonempty_string(value, field):
    normalized = str(value or "").strip()
    if not normalized:
        raise CreatorBatchError("invalid_batch_job", f"{field} is required")
    return normalized


def normalize_product_match(value, index):
    if not isinstance(value, dict):
        raise CreatorBatchError(
            "product_match_confirmation_required",
            f"jobs[{index}].product_match must record the product selection decision",
        )
    status = str(value.get("status") or "").strip().upper()
    evidence = str(value.get("evidence") or "").strip()
    if status not in PRODUCT_MATCH_STATUSES or not evidence:
        raise CreatorBatchError(
            "product_match_confirmation_required",
            f"jobs[{index}].product_match requires status MATCHED or USER_CONFIRMED and evidence",
        )
    return {"status": status, "evidence": evidence}


def inherited(job, manifest, field, default=None):
    return job[field] if field in job else manifest.get(field, default)


def normalize_job(job, manifest, index, channel):
    if not isinstance(job, dict):
        raise CreatorBatchError("invalid_batch_job", f"jobs[{index}] must be an object")
    advertiser_id = decimal_id(
        inherited(job, manifest, "advertiser_id"),
        f"jobs[{index}].advertiser_id",
    )
    plan_template = nonempty_string(
        inherited(job, manifest, "plan_template"),
        f"jobs[{index}].plan_template",
    )
    aweme_id = nonempty_string(job.get("aweme_id"), f"jobs[{index}].aweme_id")
    item_ids = [decimal_id(value, f"jobs[{index}].item_ids") for value in job.get("item_ids") or []]
    item_ids = list(dict.fromkeys(item_ids))
    if not item_ids:
        raise CreatorBatchError("invalid_batch_job", f"jobs[{index}].item_ids cannot be empty")
    product_match = normalize_product_match(job.get("product_match"), index)
    normalized = {
        "index": index,
        "channel": channel,
        "advertiser_id": advertiser_id,
        "plan_template": plan_template,
        "aweme_id": aweme_id,
        "item_ids": item_ids,
        "product_match": product_match,
    }
    for field in (
        "budget",
        "bid",
        "roi_goal",
        "material_date",
        "product_name",
        "product_id",
        "project_name",
        "promotion_name",
    ):
        value = inherited(job, manifest, field)
        if value is not None:
            normalized[field] = value
    return normalized


def job_key(job):
    material_key = ",".join(sorted(job["item_ids"]))
    return ":".join((
        job["channel"],
        job["advertiser_id"],
        job["plan_template"],
        job["aweme_id"],
        material_key,
    ))


def resolve_names(raw_config, job):
    runtime = channels.runtime_config(
        raw_config,
        channel=job["channel"],
        capability="create",
    )
    effective = plan_templates.apply(
        runtime,
        job["plan_template"],
        advertiser_id=job["advertiser_id"],
        channel=job["channel"],
    )
    if get_path(effective, "material_strategy.source_type") != creator_materials.SOURCE_TYPE:
        raise CreatorBatchError(
            "material_source_mismatch",
            f"template {job['plan_template']} is not a creator-authorized template",
        )
    defaults = effective["defaults"]
    material_date = str(job.get("material_date") or create_plan.material_date_for_yesterday())
    product_name = str(job.get("product_name") or defaults["product_name"])
    values = {
        "material_date": material_date,
        "product_name": product_name,
        "creator_id": job["aweme_id"],
        "aweme_id": job["aweme_id"],
        "group_index": job["index"] + 1,
        "index": job["index"] + 1,
        "suffix": f"{job['index'] + 1:02d}",
    }
    for field in ("project_name", "promotion_name"):
        if job.get(field):
            continue
        template_field = f"{field}_template"
        template = defaults[template_field]
        rendered = create_plan.render_template(template, values)
        if all(token not in template for token in ("{creator_id}", "{aweme_id}")):
            rendered = f"{rendered}_{job['aweme_id']}"
        job[field] = rendered
    job["material_date"] = material_date
    return job


def load_jobs(config_path, jobs_file, channel_override=None):
    jobs_path = Path(jobs_file).expanduser()
    manifest = json.loads(jobs_path.read_text(encoding="utf-8"))
    if not isinstance(manifest, dict):
        raise CreatorBatchError("invalid_batch_manifest", "jobs file must contain a JSON object")
    schema_version = int(manifest.get("schema_version") or SCHEMA_VERSION)
    if schema_version != SCHEMA_VERSION:
        raise CreatorBatchError("invalid_batch_manifest", "unsupported creator batch schema version")
    channel = channel_override or manifest.get("channel") or "marketing"
    channels.get(channel, capability="create")
    raw_config = json.loads(Path(config_path).read_text(encoding="utf-8"))
    rows = manifest.get("jobs")
    if not isinstance(rows, list) or not rows:
        raise CreatorBatchError("invalid_batch_manifest", "jobs file must contain at least one job")
    jobs = [
        resolve_names(raw_config, normalize_job(row, manifest, index, channel))
        for index, row in enumerate(rows)
    ]
    keys = [job_key(job) for job in jobs]
    if len(keys) != len(set(keys)):
        raise CreatorBatchError("duplicate_batch_job", "creator batch contains duplicate jobs")
    return manifest, jobs


def batch_fingerprint(jobs):
    payload = []
    for job in jobs:
        row = {
            key: copy.deepcopy(value)
            for key, value in job.items()
            if key not in {"index", "product_match"}
        }
        payload.append(row)
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(encoded.encode("utf-8")).hexdigest()


def default_journal_path(fingerprint):
    return authorization_store.state_root() / "runs" / f"creator-batch-{fingerprint[:16]}.json"


def compact_response(response):
    if not isinstance(response, dict):
        return None
    compact = {
        "code": response.get("code"),
        "message": response.get("message") or response.get("msg"),
        "request_id": response.get("request_id"),
    }
    for field in ("project_id", "promotion_id"):
        value = get_path(response, f"data.{field}")
        if value is not None:
            compact[field] = str(value)
    return {key: value for key, value in compact.items() if value is not None}


def new_journal(fingerprint, jobs):
    return {
        "schema_version": SCHEMA_VERSION,
        "fingerprint": fingerprint,
        "created_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "jobs": {
            job_key(job): {
                "status": "pending",
                "aweme_id": job["aweme_id"],
                "item_ids": copy.deepcopy(job["item_ids"]),
                "advertiser_id": job["advertiser_id"],
                "plan_template": job["plan_template"],
            }
            for job in jobs
        },
    }


def load_or_create_journal(path, fingerprint, jobs):
    path = Path(path)
    if path.exists():
        journal = json.loads(path.read_text(encoding="utf-8"))
        if journal.get("fingerprint") != fingerprint:
            raise CreatorBatchError(
                "batch_journal_mismatch",
                "existing journal belongs to a different creator batch",
            )
        return journal
    return new_journal(fingerprint, jobs)


def save_journal(path, journal):
    config_store.atomic_write_json(path, journal, backup=False)
    try:
        os.chmod(path, 0o600)
    except OSError:
        pass


def creator_args(config_path, job, args, *, submit=False, project_id=None):
    return SimpleNamespace(
        config=str(config_path),
        advertiser_id=job["advertiser_id"],
        plan_template=job["plan_template"],
        item_id=copy.deepcopy(job["item_ids"]),
        budget=job.get("budget"),
        bid=job.get("bid"),
        roi_goal=job.get("roi_goal"),
        material_date=job.get("material_date"),
        product_name=job.get("product_name"),
        product_id=job.get("product_id"),
        project_name=job.get("project_name"),
        promotion_name=job.get("promotion_name"),
        project_id=project_id,
        promotion_only=bool(project_id),
        submit=submit,
        out=None,
        channel=job["channel"],
        auth_account_id=args.auth_account_id,
    )


def compact_execution(job, result, exit_code, status=None, include_payloads=False):
    selected_creator = result.get("selected_creator") or {}
    row = {
        "job_key": job_key(job),
        "aweme_id": job["aweme_id"],
        "creator_name": selected_creator.get("aweme_name"),
        "item_ids": copy.deepcopy(job["item_ids"]),
        "product_match": copy.deepcopy(job["product_match"]),
        "status": status,
        "exit_code": exit_code,
        "project_name": get_path(result, "project_payload.name") or job.get("project_name"),
        "promotion_name": get_path(result, "promotion_payload.name") or job.get("promotion_name"),
        "missing_fields": copy.deepcopy(result.get("missing_fields") or result.get("blocking_fields") or []),
        "error_code": result.get("error_code"),
        "error": result.get("error"),
        "failure_stage": result.get("failure_stage"),
        "project_response": compact_response(result.get("project_response")),
        "promotion_response": compact_response(result.get("promotion_response")),
    }
    project_id = (
        get_path(result, "project_response.data.project_id")
        or result.get("project_id")
    )
    promotion_id = get_path(result, "promotion_response.data.promotion_id")
    if project_id is not None:
        row["project_id"] = str(project_id)
    if promotion_id is not None:
        row["promotion_id"] = str(promotion_id)
    if include_payloads:
        row["project_payload"] = result.get("project_payload")
        row["promotion_payload"] = result.get("promotion_payload")
    return {key: value for key, value in row.items() if value is not None}


def run_preflight(config_path, job, args):
    result, exit_code = create_creator_plan.execute(
        creator_args(config_path, job, args),
        config_path=config_path,
    )
    selected_aweme_id = get_path(result, "selected_creator.aweme_id")
    if exit_code == 0 and selected_aweme_id != job["aweme_id"]:
        result = {
            "error_code": "creator_identity_mismatch",
            "error": (
                f"selected materials belong to aweme_id {selected_aweme_id}, "
                f"not {job['aweme_id']}"
            ),
        }
        exit_code = 2
    status = "ready" if exit_code == 0 and not result.get("missing_fields") else "blocked"
    return compact_execution(job, result, exit_code, status, args.include_payloads)


def summarize(mode, fingerprint, journal_path, results):
    counts = {}
    for row in results:
        status = row.get("status") or "unknown"
        counts[status] = counts.get(status, 0) + 1
    return {
        "mode": mode,
        "batch_id": fingerprint[:16],
        "journal": str(journal_path) if journal_path else None,
        "counts": counts,
        "results": results,
    }


def run_batch(args):
    config_path = config_paths.resolve_config_path(args.config)
    concurrency = int(args.concurrency)
    if concurrency < 1 or concurrency > MAX_CONCURRENCY:
        raise CreatorBatchError(
            "invalid_concurrency",
            f"concurrency must be between 1 and {MAX_CONCURRENCY}",
        )
    _, jobs = load_jobs(config_path, args.jobs_file, args.channel)
    fingerprint = batch_fingerprint(jobs)

    journal_path = None
    journal = None
    if args.submit:
        journal_path = (
            Path(args.journal).expanduser()
            if args.journal
            else default_journal_path(fingerprint)
        )
        journal = load_or_create_journal(journal_path, fingerprint, jobs)
        save_journal(journal_path, journal)

    preflight_by_key = {}
    preflight_jobs = []
    for job in jobs:
        key = job_key(job)
        state = (journal or {}).get("jobs", {}).get(key, {})
        if args.submit and state.get("status") == "completed" and state.get("promotion_id"):
            preflight_by_key[key] = {
                "job_key": key,
                "aweme_id": job["aweme_id"],
                "item_ids": copy.deepcopy(job["item_ids"]),
                "product_match": copy.deepcopy(job["product_match"]),
                "status": "skipped_completed",
                "exit_code": 0,
                "project_id": state.get("project_id"),
                "promotion_id": state.get("promotion_id"),
            }
        else:
            preflight_jobs.append(job)
    with ThreadPoolExecutor(max_workers=min(concurrency, max(1, len(preflight_jobs)))) as executor:
        futures = {
            executor.submit(run_preflight, config_path, job, args): job
            for job in preflight_jobs
        }
        for future in as_completed(futures):
            job = futures[future]
            try:
                preflight_by_key[job_key(job)] = future.result()
            except Exception as exc:
                preflight_by_key[job_key(job)] = {
                    "job_key": job_key(job),
                    "aweme_id": job["aweme_id"],
                    "item_ids": copy.deepcopy(job["item_ids"]),
                    "status": "blocked",
                    "exit_code": 2,
                    "error_code": "creator_preflight_failed",
                    "error": str(exc),
                }
    preflight = [preflight_by_key[job_key(job)] for job in jobs]
    if not args.submit:
        summary = summarize("dry_run", fingerprint, None, preflight)
        return summary, 0 if all(row["status"] == "ready" for row in preflight) else 2

    journal_lock = threading.Lock()

    submitted_by_key = {
        row["job_key"]: copy.deepcopy(row)
        for row in preflight
        if row["status"] != "ready"
    }

    def persist_event(key, event):
        with journal_lock:
            entry = journal["jobs"][key]
            entry["status"] = event["status"]
            if event.get("project_id") is not None:
                entry["project_id"] = str(event["project_id"])
            if event.get("promotion_id") is not None:
                entry["promotion_id"] = str(event["promotion_id"])
            compact = compact_response(event.get("response"))
            if compact:
                entry["last_response"] = compact
            save_journal(journal_path, journal)

    def submit_job(job):
        key = job_key(job)
        state = journal["jobs"][key]
        if state.get("status") == "completed" and state.get("promotion_id"):
            return {
                "job_key": key,
                "aweme_id": job["aweme_id"],
                "item_ids": copy.deepcopy(job["item_ids"]),
                "product_match": copy.deepcopy(job["product_match"]),
                "status": "skipped_completed",
                "exit_code": 0,
                "project_id": state.get("project_id"),
                "promotion_id": state.get("promotion_id"),
            }
        project_id = state.get("project_id") if state.get("status") in {
            "project_created",
            "promotion_failed",
        } else None
        persist_event(key, {
            "status": "promotion_retrying" if project_id else "submitting",
            "project_id": project_id,
        })
        execution, exit_code = create_creator_plan.execute(
            creator_args(
                config_path,
                job,
                args,
                submit=True,
                project_id=project_id,
            ),
            config_path=config_path,
            progress_callback=lambda event: persist_event(key, event),
        )
        state = journal["jobs"][key]
        if exit_code == 0:
            status = "created"
        elif state.get("project_id"):
            status = "promotion_failed"
        else:
            status = "project_failed"
        if state.get("status") not in {"completed", status}:
            persist_event(key, {
                "status": status,
                "project_id": state.get("project_id"),
            })
        return compact_execution(
            job,
            execution,
            exit_code,
            status,
            args.include_payloads,
        )

    ready_jobs = [
        job for job in jobs
        if preflight_by_key[job_key(job)]["status"] == "ready"
    ]
    with ThreadPoolExecutor(max_workers=min(concurrency, max(1, len(ready_jobs)))) as executor:
        futures = {executor.submit(submit_job, job): job for job in ready_jobs}
        for future in as_completed(futures):
            job = futures[future]
            key = job_key(job)
            try:
                submitted_by_key[key] = future.result()
            except Exception as exc:
                state = journal["jobs"][key]
                failure_status = "promotion_failed" if state.get("project_id") else "project_failed"
                persist_event(key, {
                    "status": failure_status,
                    "project_id": state.get("project_id"),
                })
                submitted_by_key[key] = {
                    "job_key": key,
                    "aweme_id": job["aweme_id"],
                    "item_ids": copy.deepcopy(job["item_ids"]),
                    "status": failure_status,
                    "exit_code": 1,
                    "error_code": "creator_submit_failed",
                    "error": str(exc),
                    **({"project_id": state["project_id"]} if state.get("project_id") else {}),
                }
    submitted = [submitted_by_key[job_key(job)] for job in jobs]
    summary = summarize("submit", fingerprint, journal_path, submitted)
    failed = any(row["status"] in {"blocked", "project_failed", "promotion_failed"} for row in submitted)
    return summary, 1 if failed else 0


def write_output(result, out_path=None):
    rendered = json.dumps(result, ensure_ascii=False, indent=2)
    if out_path:
        path = Path(out_path)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(rendered + "\n", encoding="utf-8")
    print(rendered)


def main(argv=None):
    args = build_parser().parse_args(argv)
    try:
        result, exit_code = run_batch(args)
    except (ValueError, channels.ChannelError) as exc:
        result = {
            "mode": "submit" if args.submit else "dry_run",
            "error_code": getattr(exc, "code", "creator_batch_invalid"),
            "error": str(exc),
            "details": getattr(exc, "details", {}),
        }
        exit_code = 2
    write_output(result, args.out)
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
