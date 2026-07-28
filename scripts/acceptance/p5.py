#!/usr/bin/env python3
"""Execute fail-closed P5 release, launcher, rollback, and canary checks."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
import platform
import re
import shlex
import shutil
import stat
import subprocess
import sys
import tempfile
import time
import zipfile
from pathlib import Path

try:
    from .candidate_identity import (
        CandidateIdentityError,
        build_candidate_identity,
        candidate_identity_sha256,
        compare_candidate_identities,
        sha256_file,
    )
except ImportError:
    from candidate_identity import (  # type: ignore[no-redef]
        CandidateIdentityError,
        build_candidate_identity,
        candidate_identity_sha256,
        compare_candidate_identities,
        sha256_file,
    )

ROOT = Path(__file__).resolve().parents[2]
BOOTSTRAP_MODULE = ROOT / "prototype" / "runtime-bootstrap"
ACCOUNT_FIXTURE = ROOT / "testdata" / "contracts" / "python" / "account-book" / "config.json"
TEMPLATE_FIXTURE = ROOT / "testdata" / "contracts" / "python" / "templates" / "config.json"
TARGETS = (
    ("darwin", "arm64"),
    ("darwin", "amd64"),
    ("linux", "amd64"),
    ("linux", "arm64"),
    ("windows", "amd64"),
)
SDK_VERSION = "v1.1.92"
HEX_40 = re.compile(r"^[a-f0-9]{40}$")
HEX_64 = re.compile(r"^[a-f0-9]{64}$")
REQUIRED_CANARY_ROLES = {"MT", "SO"}
APPROVAL_ID = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$")


class AcceptanceError(RuntimeError):
    pass


def canonical_json(value: object) -> bytes:
    return (
        json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")) + "\n"
    ).encode("utf-8")


def write_evidence(path: Path, value: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(canonical_json(value))


def load_json(path: Path) -> dict:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as error:
        raise AcceptanceError(f"invalid JSON file: {path}") from error
    if not isinstance(value, dict):
        raise AcceptanceError(f"JSON file must contain an object: {path}")
    return value


def run(
    command: list[str],
    *,
    cwd: Path = ROOT,
    env: dict[str, str] | None = None,
    input_text: str | None = None,
    timeout: int = 120,
) -> subprocess.CompletedProcess:
    try:
        return subprocess.run(
            command,
            cwd=cwd,
            env=env,
            input=input_text,
            capture_output=True,
            text=True,
            check=False,
            timeout=timeout,
        )
    except (OSError, subprocess.TimeoutExpired) as error:
        raise AcceptanceError(f"command could not complete: {' '.join(command)}") from error


def target_name(prefix: str, goos: str, goarch: str) -> str:
    suffix = ".exe" if goos == "windows" else ""
    return f"{prefix}_{goos}_{goarch}{suffix}"


def current_target() -> tuple[str, str]:
    if sys.platform == "darwin":
        goos = "darwin"
    elif sys.platform.startswith("linux"):
        goos = "linux"
    elif sys.platform in {"win32", "cygwin"}:
        goos = "windows"
    else:
        raise AcceptanceError(f"unsupported native operating system: {sys.platform}")
    machine = platform.machine().lower()
    if machine in {"x86_64", "amd64"}:
        goarch = "amd64"
    elif machine in {"arm64", "aarch64"}:
        goarch = "arm64"
    else:
        raise AcceptanceError(f"unsupported native architecture: {machine}")
    if (goos, goarch) not in TARGETS:
        raise AcceptanceError(f"unsupported native platform: {goos}-{goarch}")
    return goos, goarch


def current_go_target() -> tuple[str, str]:
    completed = run(
        ["go", "env", "GOOS", "GOARCH"],
        env={**os.environ, "GOTOOLCHAIN": "go1.26.5"},
    )
    if completed.returncode != 0:
        raise AcceptanceError("go env could not identify the native runner")
    values = completed.stdout.splitlines()
    if len(values) != 2:
        raise AcceptanceError("go env returned a malformed native platform")
    target = (values[0].strip(), values[1].strip())
    if target not in TARGETS:
        raise AcceptanceError(f"unsupported Go native platform: {'-'.join(target)}")
    return target


def verify_detached_signature(candidate: Path, payload: str, signature: str, public_key: str) -> None:
    environment = {**os.environ, "GOTOOLCHAIN": "go1.26.5", "CGO_ENABLED": "0"}
    completed = run(
        [
            "go",
            "run",
            "./cmd/release-tool",
            "verify",
            "--input",
            str((candidate / payload).resolve()),
            "--signature",
            str((candidate / signature).resolve()),
            "--public-key-hex",
            public_key,
        ],
        cwd=BOOTSTRAP_MODULE,
        env=environment,
        timeout=300,
    )
    if completed.returncode != 0 or completed.stdout.strip() != "signature verified":
        raise AcceptanceError(f"detached signature verification failed: {payload}")


def expected_signed_files(product_version: str) -> set[str]:
    names = {
        "build-summary.json",
        "runtime-manifest.json",
        "runtime-manifest.sig",
        "ocean-watch.spdx.json",
        "provenance.intoto.jsonl",
        "release-public-key.txt",
        f"ocean-watch-plugin_v{product_version}.zip",
    }
    for goos, goarch in TARGETS:
        names.add(target_name("ocean-watch", goos, goarch))
        names.add(target_name("ocean-watch-bootstrap", goos, goarch))
    return names


def verify_zip(candidate: Path, product_version: str, plugin_version: str) -> dict:
    archive = candidate / f"ocean-watch-plugin_v{product_version}.zip"
    prefix = "ocean-watch/"
    required = {
        prefix + ".codex-plugin/plugin.json",
        prefix + ".codex-plugin/runtime-policy.json",
        prefix + "scripts/runtime_launcher.py",
        prefix + "skills/ads-plan-monitor/run.py",
        prefix + "skills/qc-plan-monitor/run.py",
    }
    with zipfile.ZipFile(archive) as bundle:
        members = bundle.infolist()
        names = {member.filename for member in members}
        missing = sorted(required - names)
        if missing:
            raise AcceptanceError(f"Plugin ZIP is incomplete: {missing}")
        for member in members:
            path = Path(member.filename)
            if path.is_absolute() or ".." in path.parts or not member.filename.startswith(prefix):
                raise AcceptanceError(f"Plugin ZIP contains an unsafe path: {member.filename}")
            mode = member.external_attr >> 16
            if stat.S_ISLNK(mode):
                raise AcceptanceError(f"Plugin ZIP contains a symlink: {member.filename}")
        plugin = json.loads(bundle.read(prefix + ".codex-plugin/plugin.json"))
        policy = json.loads(bundle.read(prefix + ".codex-plugin/runtime-policy.json"))
        if plugin.get("version") != plugin_version:
            raise AcceptanceError("Plugin ZIP version does not match release identity")
        if policy.get("schema_version") != 1 or policy.get("enabled") is not True:
            raise AcceptanceError("Plugin ZIP runtime policy is not enabled")
        if policy.get("product_version") != product_version or policy.get("plugin_version") != plugin_version:
            raise AcceptanceError("Plugin ZIP runtime policy identity does not match")
        commands = policy.get("commands")
        if not isinstance(commands, list) or not commands or not all(isinstance(item, str) for item in commands):
            raise AcceptanceError("Plugin ZIP runtime policy commands are malformed")
        bootstrap_names = []
        for goos, goarch in TARGETS:
            name = target_name("ocean-watch-bootstrap", goos, goarch)
            member_name = prefix + ".codex-plugin/runtime/bootstrap/" + name
            if member_name not in names:
                raise AcceptanceError(f"Plugin ZIP is missing bootstrap: {name}")
            if hashlib.sha256(bundle.read(member_name)).hexdigest() != sha256_file(candidate / name):
                raise AcceptanceError(f"Plugin ZIP bootstrap differs from release asset: {name}")
            bootstrap_names.append(name)
    return {
        "archive": archive.name,
        "members": len(members),
        "bootstrap_assets": bootstrap_names,
        "policy_enabled": True,
        "commands": len(commands),
    }


def verify_candidate(
    candidate: Path,
    *,
    verify_signatures: bool = True,
    require_release: bool = False,
    expected_commit: str | None = None,
) -> dict:
    candidate = candidate.resolve()
    summary = load_json(candidate / "build-summary.json")
    manifest_path = candidate / "runtime-manifest.json"
    manifest = load_json(manifest_path)
    canonical_manifest = json.dumps(
        manifest, ensure_ascii=False, sort_keys=True, separators=(",", ":")
    ).encode("utf-8")
    if manifest_path.read_bytes() != canonical_manifest:
        raise AcceptanceError("runtime manifest is not canonical JSON")
    checksums = load_json(candidate / "checksums.json")
    product_version = str(manifest.get("product_version") or "")
    plugin_version = str(manifest.get("plugin_version") or "")
    commit = str(manifest.get("git_commit") or "")
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", product_version):
        raise AcceptanceError("runtime manifest product version is malformed")
    if not plugin_version.startswith(product_version + "+codex."):
        raise AcceptanceError("runtime manifest Plugin version is malformed")
    if not HEX_40.fullmatch(commit):
        raise AcceptanceError("runtime manifest commit is malformed")
    if manifest.get("manifest_version") != 1 or manifest.get("sdk_version") != SDK_VERSION:
        raise AcceptanceError("runtime manifest schema or SDK identity is invalid")
    if manifest.get("tag") != "v" + product_version:
        raise AcceptanceError("runtime manifest Tag identity is invalid")
    for source in (summary, checksums):
        if source.get("product_version") != product_version:
            raise AcceptanceError("release product identities disagree")
        if source.get("plugin_version") != plugin_version:
            raise AcceptanceError("release Plugin identities disagree")
        if source.get("git_commit") != commit:
            raise AcceptanceError("release commit identities disagree")
    source_digest = str(summary.get("source_tree_sha256") or "")
    if not HEX_64.fullmatch(source_digest):
        raise AcceptanceError("build summary source digest is malformed")
    if summary.get("schema_version") != 1:
        raise AcceptanceError("build summary schema is invalid")
    if summary.get("output") != ".":
        raise AcceptanceError("build summary output identity is not reproducible")
    if expected_commit is not None and commit != expected_commit:
        raise AcceptanceError("release candidate does not match the expected commit")
    routes = manifest.get("routes")
    if not isinstance(routes, dict) or not routes:
        raise AcceptanceError("runtime route manifest is empty")
    if any(value not in {"python", "go"} for value in routes.values()):
        raise AcceptanceError("runtime route manifest contains an invalid runtime")
    go_routes = sum(value == "go" for value in routes.values())
    if summary.get("production_go_routes") != go_routes:
        raise AcceptanceError("build summary route count does not match manifest")
    if require_release:
        if (
            summary.get("status") != "release_ready"
            or summary.get("release") is not True
            or summary.get("source_dirty") is not False
        ):
            raise AcceptanceError("release publication requires a clean release-mode candidate")
        if go_routes != 0:
            raise AcceptanceError("release publication cannot switch production routes before G5")
    assets = manifest.get("assets")
    expected_platforms = {f"{goos}-{goarch}" for goos, goarch in TARGETS}
    if not isinstance(assets, dict) or set(assets) != expected_platforms:
        raise AcceptanceError("runtime manifest platform matrix is incomplete")
    for goos, goarch in TARGETS:
        item = assets[f"{goos}-{goarch}"]
        name = target_name("ocean-watch", goos, goarch)
        path = candidate / name
        if item.get("name") != name or item.get("size") != path.stat().st_size:
            raise AcceptanceError(f"runtime asset identity mismatch: {name}")
        if item.get("sha256") != sha256_file(path):
            raise AcceptanceError(f"runtime asset digest mismatch: {name}")
    declared = checksums.get("files")
    if not isinstance(declared, list):
        raise AcceptanceError("checksums.json files must be a list")
    by_name = {item.get("name"): item for item in declared if isinstance(item, dict)}
    expected = expected_signed_files(product_version)
    if set(by_name) != expected or len(by_name) != len(declared):
        raise AcceptanceError("checksums.json does not cover the exact release asset set")
    expected_entries = expected | {"checksums.json", "checksums.sig"}
    entries = list(candidate.iterdir())
    if any(path.is_symlink() or not path.is_file() for path in entries):
        raise AcceptanceError("release candidate directory must contain regular files only")
    actual_entries = {path.name for path in entries}
    if actual_entries != expected_entries or len(entries) != len(expected_entries):
        raise AcceptanceError("release candidate directory contains unsigned or missing assets")
    if summary.get("signed_file_count") != len(declared):
        raise AcceptanceError("build summary signed file count is inconsistent")
    for name, item in by_name.items():
        path = candidate / name
        if not path.is_file() or path.stat().st_size != item.get("size"):
            raise AcceptanceError(f"release asset size mismatch: {name}")
        if sha256_file(path) != item.get("sha256"):
            raise AcceptanceError(f"release asset digest mismatch: {name}")
    public_key = (candidate / "release-public-key.txt").read_text(encoding="utf-8").strip()
    if not HEX_64.fullmatch(public_key):
        raise AcceptanceError("release public key is malformed")
    if verify_signatures:
        verify_detached_signature(candidate, "runtime-manifest.json", "runtime-manifest.sig", public_key)
        verify_detached_signature(candidate, "checksums.json", "checksums.sig", public_key)
    sbom = load_json(candidate / "ocean-watch.spdx.json")
    if sbom.get("spdxVersion") != "SPDX-2.3":
        raise AcceptanceError("release SBOM is not SPDX 2.3")
    sdk_packages = [
        package
        for package in sbom.get("packages", [])
        if package.get("name") == "github.com/oceanengine/ad_open_sdk_go"
    ]
    if len(sdk_packages) != 1 or sdk_packages[0].get("versionInfo") != SDK_VERSION:
        raise AcceptanceError("release SBOM does not contain the pinned official SDK")
    if sdk_packages[0].get("licenseDeclared") != "Apache-2.0":
        raise AcceptanceError("official SDK license is not declared in the SBOM")
    for package in sbom.get("packages", []):
        if package.get("licenseDeclared") in {None, "", "NOASSERTION"}:
            raise AcceptanceError(f"SBOM package license is unresolved: {package.get('name')}")
        if package.get("licenseConcluded") in {None, "", "NOASSERTION"}:
            raise AcceptanceError(f"SBOM package license conclusion is unresolved: {package.get('name')}")
    provenance = load_json(candidate / "provenance.intoto.jsonl")
    if provenance.get("predicateType") != "https://slsa.dev/provenance/v1":
        raise AcceptanceError("release provenance predicate is invalid")
    subjects = provenance.get("subject")
    if not isinstance(subjects, list) or not subjects:
        raise AcceptanceError("release provenance has no subjects")
    for subject in subjects:
        name = subject.get("name")
        digest = (subject.get("digest") or {}).get("sha256")
        if not isinstance(name, str) or name not in by_name or digest != sha256_file(candidate / name):
            raise AcceptanceError(f"release provenance subject is invalid: {name}")
    zip_result = verify_zip(candidate, product_version, plugin_version)
    try:
        candidate_identity = build_candidate_identity(
            git_sha=commit,
            product_version=product_version,
            plugin_version=plugin_version,
            sdk_version=SDK_VERSION,
            source_tree_sha256=source_digest,
            candidate_checksums_sha256=sha256_file(candidate / "checksums.json"),
            release_public_key_hex=public_key,
            release=summary.get("release"),
        )
    except CandidateIdentityError as error:
        raise AcceptanceError(f"release candidate identity is invalid: {error}") from error
    return {
        "candidate": str(candidate),
        "product_version": product_version,
        "plugin_version": plugin_version,
        "git_commit": commit,
        "source_tree_sha256": source_digest,
        "source_dirty": summary.get("source_dirty"),
        "runtime_assets": len(assets),
        "bootstrap_assets": len(TARGETS),
        "signed_files": len(declared),
        "production_go_routes": go_routes,
        "signatures_verified": verify_signatures,
        "candidate_identity": candidate_identity,
        "candidate_identity_sha256": candidate_identity_sha256(candidate_identity),
        "zip": zip_result,
    }


def extract_plugin(candidate: Path, destination: Path, product_version: str) -> Path:
    archive = candidate / f"ocean-watch-plugin_v{product_version}.zip"
    with zipfile.ZipFile(archive) as bundle:
        for member in bundle.infolist():
            relative = Path(member.filename)
            if relative.is_absolute() or ".." in relative.parts:
                raise AcceptanceError(f"unsafe Plugin archive path: {member.filename}")
            target = destination / relative
            if member.is_dir():
                target.mkdir(parents=True, exist_ok=True)
                continue
            mode = member.external_attr >> 16
            if stat.S_ISLNK(mode):
                raise AcceptanceError(f"Plugin archive symlink rejected: {member.filename}")
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(bundle.read(member))
            target.chmod(stat.S_IMODE(mode) or 0o644)
    return destination / "ocean-watch"


def prime_cache(candidate: Path, codex_home: Path, product_version: str, target: tuple[str, str]) -> Path:
    goos, goarch = target
    cache = codex_home / "ocean-watch" / "runtime" / product_version / f"{goos}-{goarch}"
    cache.mkdir(parents=True, mode=0o700)
    for name in ("runtime-manifest.json", "runtime-manifest.sig"):
        destination = cache / name
        shutil.copyfile(candidate / name, destination)
        destination.chmod(0o600)
    return cache


def invoke_skill(
    run_path: Path,
    arguments: list[str],
    codex_home: Path,
    *,
    timeout: int = 60,
) -> subprocess.CompletedProcess:
    environment = {
        **os.environ,
        "CODEX_HOME": str(codex_home),
        "PATH": "",
        "PYTHONDONTWRITEBYTECODE": "1",
        "PYTHONNOUSERSITE": "1",
    }
    return run(
        [sys.executable, "-I", str(run_path), *arguments],
        cwd=run_path.parents[2],
        env=environment,
        timeout=timeout,
    )


def require_success(completed: subprocess.CompletedProcess, label: str) -> None:
    if completed.returncode != 0:
        raise AcceptanceError(f"{label} failed: {completed.stderr.strip()}")


def parse_success_json(completed: subprocess.CompletedProcess, label: str) -> dict:
    require_success(completed, label)
    try:
        value = json.loads(completed.stdout)
    except json.JSONDecodeError as error:
        raise AcceptanceError(f"{label} did not return JSON") from error
    if not isinstance(value, dict) or value.get("ok") is False:
        raise AcceptanceError(f"{label} returned a failure envelope")
    return value


def launcher_acceptance(
    candidate: Path,
    out: Path,
    expected_platform: str | None,
    *,
    require_release: bool = False,
    expected_commit: str | None = None,
) -> dict:
    verified = verify_candidate(
        candidate,
        verify_signatures=False,
        require_release=require_release,
        expected_commit=expected_commit,
    )
    target = current_target()
    go_target = current_go_target()
    platform_key = "-".join(target)
    if go_target != target:
        raise AcceptanceError(
            f"native Go platform mismatch: Python identified {platform_key}, "
            f"go env identified {'-'.join(go_target)}"
        )
    if expected_platform and expected_platform != platform_key:
        raise AcceptanceError(
            f"native runner platform mismatch: expected {expected_platform}, got {platform_key}"
        )
    with tempfile.TemporaryDirectory(prefix="ocean-watch-launcher-") as directory:
        workspace = Path(directory)
        plugin = extract_plugin(candidate, workspace / "install", verified["product_version"])
        codex_home = workspace / "codex-home"
        prime_cache(candidate, codex_home, verified["product_version"], target)
        runs = {}
        for skill in ("ads-plan-monitor", "qc-plan-monitor"):
            run_path = plugin / "skills" / skill / "run.py"
            version = invoke_skill(run_path, ["--version"], codex_home)
            require_success(version, f"{skill} --version")
            if version.stdout.strip() != f"ocean-watch {verified['product_version']}":
                raise AcceptanceError(f"{skill} returned the wrong version")
            accounts = parse_success_json(
                invoke_skill(
                    run_path,
                    ["accounts", "list", "--config", str(ACCOUNT_FIXTURE)],
                    codex_home,
                ),
                f"{skill} accounts list",
            )
            if len(accounts.get("accounts") or []) != 2:
                raise AcceptanceError(f"{skill} accounts list lost synthetic records")
            runs[skill] = {"version": version.stdout.strip(), "account_count": 2}
        tampered_home = workspace / "tampered-home"
        tampered_cache = prime_cache(
            candidate, tampered_home, verified["product_version"], target
        )
        (tampered_cache / "runtime-manifest.sig").write_text("invalid\n", encoding="utf-8")
        (tampered_cache / "runtime-manifest.sig").chmod(0o600)
        rejected = invoke_skill(
            plugin / "skills" / "ads-plan-monitor" / "run.py",
            ["accounts", "list", "--config", str(ACCOUNT_FIXTURE)],
            tampered_home,
        )
        if rejected.returncode == 0 or "runtime_bootstrap_failed" not in rejected.stderr:
            raise AcceptanceError("launcher did not reject the tampered runtime signature")
        if '"ok": true' in rejected.stdout.lower():
            raise AcceptanceError("tampered bootstrap path executed a business command")
    result = {
        "schema_version": 1,
        "acceptance": "AC-124",
        "status": "passed",
        "platform": platform_key,
        "git_commit": verified["git_commit"],
        "product_version": verified["product_version"],
        "candidate_identity": verified["candidate_identity"],
        "candidate_identity_sha256": verified["candidate_identity_sha256"],
        "runs": runs,
        "system_go_required_by_plugin": False,
        "installed_ocean_watch_package_required": False,
        "tampered_signature_rejected": True,
        "invalid_asset_execution_count": 0,
        "offline_verified_cache": True,
        "go_env_verified": True,
    }
    write_evidence(out, result)
    return result


def fixture_hashes(accounts: Path, templates: Path) -> dict[str, str]:
    return {"accounts": sha256_file(accounts), "templates": sha256_file(templates)}


def copy_fixtures(workspace: Path) -> tuple[Path, Path]:
    accounts = workspace / "accounts.json"
    templates = workspace / "templates.json"
    shutil.copyfile(ACCOUNT_FIXTURE, accounts)
    shutil.copyfile(TEMPLATE_FIXTURE, templates)
    return accounts, templates


def direct_command(binary: Path, arguments: list[str], codex_home: Path) -> dict:
    environment = {**os.environ, "CODEX_HOME": str(codex_home)}
    return parse_success_json(run([str(binary), *arguments], env=environment), binary.name)


def upgrade_rollback_acceptance(
    candidate: Path,
    out: Path,
    *,
    require_release: bool = False,
    expected_commit: str | None = None,
) -> dict:
    started = time.monotonic()
    verified = verify_candidate(
        candidate,
        verify_signatures=False,
        require_release=require_release,
        expected_commit=expected_commit,
    )
    target = current_target()
    runtime = candidate / target_name("ocean-watch", *target)
    with tempfile.TemporaryDirectory(prefix="ocean-watch-rollback-") as directory:
        workspace = Path(directory)
        accounts, templates = copy_fixtures(workspace)
        original = fixture_hashes(accounts, templates)
        codex_home = workspace / "codex-home"
        steps = []
        python_runner = ROOT / "skills" / "ads-plan-monitor" / "run.py"
        parse_success_json(
            invoke_skill(
                python_runner,
                ["accounts", "list", "--config", str(accounts)],
                codex_home,
            ),
            "Python-only accounts list",
        )
        parse_success_json(
            invoke_skill(
                python_runner,
                ["templates", "list", "--config", str(templates)],
                codex_home,
            ),
            "Python-only templates list",
        )
        steps.append({"runtime": "python_only", "state": fixture_hashes(accounts, templates)})
        direct_command(runtime, ["accounts", "list", "--config", str(accounts)], codex_home)
        direct_command(runtime, ["templates", "list", "--config", str(templates)], codex_home)
        steps.append({"runtime": "current_go_direct", "state": fixture_hashes(accounts, templates)})
        plugin = extract_plugin(candidate, workspace / "install", verified["product_version"])
        cache = prime_cache(candidate, codex_home, verified["product_version"], target)
        candidate_runner = plugin / "skills" / "ads-plan-monitor" / "run.py"
        parse_success_json(
            invoke_skill(
                candidate_runner,
                ["accounts", "list", "--config", str(accounts)],
                codex_home,
            ),
            "candidate launcher accounts list",
        )
        steps.append({"runtime": "signed_launcher", "state": fixture_hashes(accounts, templates)})
        signature = cache / "runtime-manifest.sig"
        good_signature = signature.read_bytes()
        signature.write_text("damaged\n", encoding="utf-8")
        signature.chmod(0o600)
        rejected = invoke_skill(
            candidate_runner,
            ["accounts", "list", "--config", str(accounts)],
            codex_home,
        )
        if rejected.returncode == 0:
            raise AcceptanceError("damaged cache was accepted during rollback drill")
        if fixture_hashes(accounts, templates) != original:
            raise AcceptanceError("damaged cache changed synthetic user state")
        signature.write_bytes(good_signature)
        signature.chmod(0o600)
        parse_success_json(
            invoke_skill(
                python_runner,
                ["accounts", "list", "--config", str(accounts)],
                codex_home,
            ),
            "Python rollback accounts list",
        )
        steps.append({"runtime": "python_rollback", "state": fixture_hashes(accounts, templates)})
        if any(step["state"] != original for step in steps):
            raise AcceptanceError("upgrade or rollback changed synthetic user state")
        if (codex_home / "ads-plan-monitor").exists():
            raise AcceptanceError("read-only rollback drill created authorization or credential state")
    duration = round(time.monotonic() - started, 3)
    result = {
        "schema_version": 1,
        "acceptance": "AC-126",
        "status": "passed",
        "git_commit": verified["git_commit"],
        "product_version": verified["product_version"],
        "candidate_identity": verified["candidate_identity"],
        "candidate_identity_sha256": verified["candidate_identity_sha256"],
        "platform": "-".join(target),
        "steps": steps,
        "state_hash_unchanged": True,
        "reauthorization_count": 0,
        "damaged_cache_execution_count": 0,
        "duration_seconds": duration,
        "rollback_under_15_minutes": duration < 900,
        "external_remaining": [
            "repeat the drill with the previous signed Go release on every native platform",
            "obtain Release, Auth Platform, and Quality Owner approval",
        ],
    }
    write_evidence(out, result)
    return result


def user_journey_acceptance(
    candidate: Path,
    out: Path,
    *,
    require_release: bool = False,
    expected_commit: str | None = None,
) -> dict:
    verified = verify_candidate(
        candidate,
        verify_signatures=False,
        require_release=require_release,
        expected_commit=expected_commit,
    )
    target = current_target()
    with tempfile.TemporaryDirectory(prefix="ocean-watch-journey-") as directory:
        workspace = Path(directory)
        accounts, templates = copy_fixtures(workspace)
        original = fixture_hashes(accounts, templates)
        plugin = extract_plugin(candidate, workspace / "install", verified["product_version"])
        codex_home = workspace / "codex-home"
        prime_cache(candidate, codex_home, verified["product_version"], target)
        marketing = plugin / "skills" / "ads-plan-monitor" / "run.py"
        qianchuan = plugin / "skills" / "qc-plan-monitor" / "run.py"
        account_result = parse_success_json(
            invoke_skill(
                marketing,
                ["accounts", "list", "--config", str(accounts)],
                codex_home,
            ),
            "user journey accounts list",
        )
        presentation = account_result.get("presentation") or {}
        labels = [column.get("label") for column in presentation.get("columns", [])]
        if labels != ["渠道", "账户名称", "广告主 ID", "启用状态"]:
            raise AcceptanceError("responsible-account Presentation columns changed")
        template_result = parse_success_json(
            invoke_skill(
                qianchuan,
                ["templates", "list", "--config", str(templates)],
                codex_home,
            ),
            "user journey templates list",
        )
        if set((template_result.get("channels") or {}).keys()) != {"marketing", "qianchuan"}:
            raise AcceptanceError("template journey lost a channel")
        dry_run = parse_success_json(
            invoke_skill(
                marketing,
                [
                    "templates",
                    "delete",
                    "--config",
                    str(templates),
                    "--channel",
                    "marketing",
                    "--template",
                    "巨量营销-1000000000000001-示例商品-3001-混剪素材",
                ],
                codex_home,
            ),
            "user journey protected write preview",
        )
        if dry_run.get("mode") != "dry_run" or dry_run.get("changed") is not False:
            raise AcceptanceError("protected user journey did not remain a dry-run")
        batch_golden = (
            ROOT / "contracts" / "presentation" / "qianchuan-batch-empty.md"
        ).read_text(encoding="utf-8")
        if "| 计划ID | 达人昵称 | 商品ID | 素材ID | 素材标题 |" not in batch_golden:
            raise AcceptanceError("Qianchuan batch Presentation contract changed")
        if fixture_hashes(accounts, templates) != original:
            raise AcceptanceError("synthetic user journey changed fixture state")
    result = {
        "schema_version": 1,
        "acceptance": "AC-128",
        "status": "passed",
        "git_commit": verified["git_commit"],
        "product_version": verified["product_version"],
        "candidate_identity": verified["candidate_identity"],
        "candidate_identity_sha256": verified["candidate_identity_sha256"],
        "platform": "-".join(target),
        "journeys": {
            "responsible_account_membership": "passed",
            "cross_channel_template_list": "passed",
            "protected_write_preview": "passed",
            "mandatory_qianchuan_batch_columns": "passed",
        },
        "network_write_calls": 0,
        "state_hash_unchanged": True,
        "external_remaining": [
            "run the approved model semantic suite for three trials per blocking case",
            "validate installation through the final Marketplace distribution path",
            "repeat first-run and upgrade journeys on all five native platforms",
        ],
    }
    write_evidence(out, result)
    return result


def supply_chain_acceptance(
    first: Path,
    second: Path,
    out: Path,
    *,
    require_release: bool = False,
    expected_commit: str | None = None,
) -> dict:
    first_result = verify_candidate(
        first,
        verify_signatures=True,
        require_release=require_release,
        expected_commit=expected_commit,
    )
    second_result = verify_candidate(
        second,
        verify_signatures=True,
        require_release=require_release,
        expected_commit=expected_commit,
    )
    identity_errors = compare_candidate_identities(
        second_result["candidate_identity"], first_result["candidate_identity"]
    )
    if identity_errors:
        raise AcceptanceError("candidate builds do not have the same identity: " + "; ".join(identity_errors))
    first_checksums = (first / "checksums.json").read_bytes()
    second_checksums = (second / "checksums.json").read_bytes()
    if first_checksums != second_checksums:
        raise AcceptanceError("candidate builds are not byte reproducible")
    declared = load_json(first / "checksums.json")["files"]
    differences = []
    for item in declared:
        name = item["name"]
        if (first / name).read_bytes() != (second / name).read_bytes():
            differences.append(name)
    if differences:
        raise AcceptanceError(f"candidate assets differ across builds: {differences}")
    result = {
        "schema_version": 1,
        "acceptance": "AC-125",
        "status": "passed",
        "git_commit": first_result["git_commit"],
        "product_version": first_result["product_version"],
        "source_tree_sha256": first_result["source_tree_sha256"],
        "candidate_identity": first_result["candidate_identity"],
        "candidate_identity_sha256": first_result["candidate_identity_sha256"],
        "signed_files": first_result["signed_files"],
        "signatures_verified": 4,
        "reproducible_asset_differences": differences,
        "sbom": "SPDX-2.3",
        "provenance": "SLSA-v1-compatible",
        "official_sdk": SDK_VERSION,
        "release_ready": require_release,
    }
    write_evidence(out, result)
    return result


def parse_rfc3339(value: object) -> dt.datetime | None:
    if not isinstance(value, str) or not value:
        return None
    try:
        parsed = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    return parsed if parsed.tzinfo is not None else None


def verify_canary_approval(
    approval: dict,
    commit: str,
    product_version: str,
    candidate_identity_digest: str,
) -> list[str]:
    errors = []
    now = dt.datetime.now(dt.timezone.utc)
    if approval.get("schema_version") != 1 or approval.get("kind") != "ac127_write_canary":
        errors.append("approval identity must be ac127_write_canary schema 1")
    approval_id = str(approval.get("approval_id") or "")
    if not APPROVAL_ID.fullmatch(approval_id):
        errors.append("approval_id is missing or malformed")
    if approval.get("git_commit") != commit:
        errors.append("approval git_commit does not match the candidate")
    if approval.get("product_version") != product_version:
        errors.append("approval product_version does not match the candidate")
    if approval.get("candidate_identity_sha256") != candidate_identity_digest:
        errors.append("approval candidate_identity_sha256 does not match the candidate")
    expires_at = parse_rfc3339(approval.get("expires_at"))
    if expires_at is None or expires_at <= now:
        errors.append("approval is missing an unexpired RFC3339 expires_at")
    advertiser_hash = str(approval.get("advertiser_hash") or "")
    if not HEX_64.fullmatch(advertiser_hash) or advertiser_hash == "0" * 64:
        errors.append("approval advertiser_hash must be a non-placeholder SHA-256")
    max_objects = approval.get("max_objects")
    if not isinstance(max_objects, int) or isinstance(max_objects, bool) or max_objects != 1:
        errors.append("approval max_objects must be exactly 1")
    max_spend = approval.get("max_spend")
    if (
        not isinstance(max_spend, (int, float))
        or isinstance(max_spend, bool)
        or max_spend != 0
    ):
        errors.append("approval max_spend must be exactly 0")
    for field in ("commands", "endpoints"):
        values = approval.get(field)
        if not isinstance(values, list) or not values or not all(
            isinstance(value, str) and value for value in values
        ):
            errors.append(f"approval {field} must be a non-empty string list")
        elif len(values) != len(set(values)):
            errors.append(f"approval {field} must not contain duplicates")
    if not str(approval.get("stop_owner") or "").strip():
        errors.append("approval stop_owner is required")
    approvals = approval.get("approvals")
    if not isinstance(approvals, list):
        return errors + ["approval approvals must be a list"]
    by_role = {}
    identities = set()
    for item in approvals:
        if not isinstance(item, dict):
            errors.append("approval entry must be an object")
            continue
        role = item.get("role")
        if role not in REQUIRED_CANARY_ROLES:
            errors.append(f"unknown canary approval role: {role}")
            continue
        if role in by_role:
            errors.append(f"duplicate canary approval role: {role}")
        by_role[role] = item
        identity = str(item.get("approver") or "").strip()
        if identity:
            identities.add(identity)
        if item.get("decision") != "approved":
            errors.append(f"{role} has not approved the canary")
        if not identity:
            errors.append(f"{role} approver is missing")
        approved_at = parse_rfc3339(item.get("approved_at"))
        if approved_at is None:
            errors.append(f"{role} approved_at must be RFC3339 with timezone")
        elif approved_at > now:
            errors.append(f"{role} approved_at must not be in the future")
        elif expires_at is not None and approved_at >= expires_at:
            errors.append(f"{role} approved_at must precede expires_at")
    missing = REQUIRED_CANARY_ROLES - set(by_role)
    if missing:
        errors.append(f"missing canary approval roles: {sorted(missing)}")
    if len(identities) < 2:
        errors.append("canary requires two distinct approvers")
    return errors


def blocked_canary(
    out: Path,
    commit: str,
    product_version: str,
    errors: list[str],
    *,
    candidate_identity: dict | None = None,
    candidate_identity_digest: str | None = None,
    approval_valid: bool = False,
    approval_id: str | None = None,
    driver_started: bool = False,
    reported_write_calls: object = None,
) -> dict:
    result = {
        "schema_version": 1,
        "acceptance": "AC-127",
        "status": "blocked",
        "git_commit": commit,
        "product_version": product_version,
        "candidate_identity": candidate_identity,
        "candidate_identity_sha256": candidate_identity_digest,
        "approval_valid": approval_valid,
        "approval_id": approval_id,
        "blockers": errors,
        "driver_started": driver_started,
        "write_state": "unknown" if driver_started else "not_started",
        "write_calls": None if driver_started else 0,
        "reported_write_calls": (
            reported_write_calls
            if isinstance(reported_write_calls, int)
            and not isinstance(reported_write_calls, bool)
            else None
        ),
        "duplicate_objects": None if driver_started else 0,
        "wrong_account_writes": None if driver_started else 0,
        "object_ids_recorded": None if driver_started else 0,
    }
    write_evidence(out, result)
    return result


def canary_acceptance(
    candidate: Path,
    approval_file: Path | None,
    driver_command: str | None,
    out: Path,
) -> tuple[dict, int]:
    verified = verify_candidate(candidate, verify_signatures=True)
    if approval_file is None or not approval_file.is_file():
        return (
            blocked_canary(
                out,
                verified["git_commit"],
                verified["product_version"],
                ["a commit-bound MT and SO approval file is required"],
                candidate_identity=verified["candidate_identity"],
                candidate_identity_digest=verified["candidate_identity_sha256"],
            ),
            2,
        )
    approval = load_json(approval_file)
    errors = verify_canary_approval(
        approval,
        verified["git_commit"],
        verified["product_version"],
        verified["candidate_identity_sha256"],
    )
    if errors:
        return (
            blocked_canary(
                out,
                verified["git_commit"],
                verified["product_version"],
                errors,
                candidate_identity=verified["candidate_identity"],
                candidate_identity_digest=verified["candidate_identity_sha256"],
            ),
            2,
        )
    if not driver_command:
        return (
            blocked_canary(
                out,
                verified["git_commit"],
                verified["product_version"],
                ["approval is valid but no explicit canary driver command was provided"],
                candidate_identity=verified["candidate_identity"],
                candidate_identity_digest=verified["candidate_identity_sha256"],
                approval_valid=True,
                approval_id=approval["approval_id"],
            ),
            2,
        )
    try:
        driver_arguments = shlex.split(driver_command)
    except ValueError:
        driver_arguments = []
    if not driver_arguments:
        return (
            blocked_canary(
                out,
                verified["git_commit"],
                verified["product_version"],
                ["canary driver command is malformed"],
                candidate_identity=verified["candidate_identity"],
                candidate_identity_digest=verified["candidate_identity_sha256"],
                approval_valid=True,
                approval_id=approval["approval_id"],
            ),
            2,
        )
    try:
        completed = run(
            driver_arguments,
            input_text=json.dumps(
                {
                    "schema_version": 1,
                    "mode": "ac127_canary",
                    "candidate": str(candidate.resolve()),
                    "candidate_identity": verified["candidate_identity"],
                    "candidate_identity_sha256": verified[
                        "candidate_identity_sha256"
                    ],
                    "approval": approval,
                },
                ensure_ascii=False,
            ),
            timeout=900,
        )
    except AcceptanceError:
        return (
            blocked_canary(
                out,
                verified["git_commit"],
                verified["product_version"],
                ["canary driver state is unknown after an execution failure"],
                candidate_identity=verified["candidate_identity"],
                candidate_identity_digest=verified["candidate_identity_sha256"],
                approval_valid=True,
                approval_id=approval["approval_id"],
                driver_started=True,
            ),
            2,
        )
    if completed.returncode != 0:
        return (
            blocked_canary(
                out,
                verified["git_commit"],
                verified["product_version"],
                ["canary driver state is unknown after a nonzero exit"],
                candidate_identity=verified["candidate_identity"],
                candidate_identity_digest=verified["candidate_identity_sha256"],
                approval_valid=True,
                approval_id=approval["approval_id"],
                driver_started=True,
            ),
            2,
        )
    try:
        driver = json.loads(completed.stdout)
    except json.JSONDecodeError:
        driver = None
    if not isinstance(driver, dict):
        return (
            blocked_canary(
                out,
                verified["git_commit"],
                verified["product_version"],
                ["canary driver returned malformed evidence; write state is unknown"],
                candidate_identity=verified["candidate_identity"],
                candidate_identity_digest=verified["candidate_identity_sha256"],
                approval_valid=True,
                approval_id=approval["approval_id"],
                driver_started=True,
            ),
            2,
        )
    write_calls = driver.get("write_calls")
    endpoints = driver.get("endpoints")
    object_hashes = driver.get("object_hashes")
    command = driver.get("command")
    spend = driver.get("spend")
    approval_still_valid = not verify_canary_approval(
        approval,
        verified["git_commit"],
        verified["product_version"],
        verified["candidate_identity_sha256"],
    )
    accepted = (
        approval_still_valid
        and driver.get("approval_id") == approval["approval_id"]
        and driver.get("git_commit") == verified["git_commit"]
        and driver.get("product_version") == verified["product_version"]
        and driver.get("candidate_identity_sha256")
        == verified["candidate_identity_sha256"]
        and command in approval["commands"]
        and driver.get("advertiser_hash") == approval["advertiser_hash"]
        and isinstance(write_calls, int)
        and not isinstance(write_calls, bool)
        and write_calls == 1
        and isinstance(spend, (int, float))
        and not isinstance(spend, bool)
        and spend == 0
        and isinstance(endpoints, list)
        and endpoints
        and all(isinstance(value, str) and value for value in endpoints)
        and len(endpoints) == len(set(endpoints))
        and set(endpoints).issubset(set(approval["endpoints"]))
        and driver.get("duplicate_objects") == 0
        and driver.get("wrong_account_writes") == 0
        and driver.get("reconciled") is True
        and driver.get("paused_or_cleaned") is True
        and isinstance(object_hashes, list)
        and object_hashes
        and len(object_hashes) <= approval["max_objects"]
        and len(object_hashes) == len(set(object_hashes))
        and all(HEX_64.fullmatch(str(value)) for value in object_hashes)
        and all(value != "0" * 64 for value in object_hashes)
    )
    if not accepted:
        return (
            blocked_canary(
                out,
                verified["git_commit"],
                verified["product_version"],
                ["canary evidence violated the approved safety envelope"],
                candidate_identity=verified["candidate_identity"],
                candidate_identity_digest=verified["candidate_identity_sha256"],
                approval_valid=approval_still_valid,
                approval_id=approval["approval_id"],
                driver_started=True,
                reported_write_calls=write_calls,
            ),
            2,
        )
    result = {
        "schema_version": 1,
        "acceptance": "AC-127",
        "status": "passed",
        "git_commit": verified["git_commit"],
        "product_version": verified["product_version"],
        "candidate_identity": verified["candidate_identity"],
        "candidate_identity_sha256": verified["candidate_identity_sha256"],
        "approval_valid": True,
        "approval_id": approval["approval_id"],
        "driver_started": True,
        "write_state": "reconciled",
        "command": command,
        "advertiser_hash": approval["advertiser_hash"],
        "write_calls": write_calls,
        "spend": spend,
        "duplicate_objects": 0,
        "wrong_account_writes": 0,
        "object_ids_recorded": len(object_hashes),
        "reconciled": True,
        "paused_or_cleaned": True,
        "endpoints": endpoints,
    }
    write_evidence(out, result)
    return result, 0


def default_out(name: str) -> Path:
    return ROOT / "artifacts" / "go-sdk-acceptance" / "p5" / name


def canary_out(args: argparse.Namespace) -> Path:
    if args.out is not None and args.evidence_dir is not None:
        raise AcceptanceError("canary accepts only one of --out or --evidence-dir")
    if args.out is not None:
        return args.out
    if args.evidence_dir is not None:
        return args.evidence_dir / "canary" / "ac-127-summary.json"
    return default_out("canary/ac-127-summary.json")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser()
    commands = parser.add_subparsers(dest="command", required=True)
    verify = commands.add_parser("verify-candidate")
    verify.add_argument("--candidate-dir", type=Path, required=True)
    verify.add_argument("--require-release", action="store_true")
    verify.add_argument("--expected-commit")
    verify.add_argument("--out", type=Path, default=default_out("candidate.json"))
    launcher = commands.add_parser("launcher")
    launcher.add_argument("--candidate-dir", type=Path, required=True)
    launcher.add_argument("--expected-platform")
    launcher.add_argument("--expected-commit")
    launcher.add_argument("--require-release", action="store_true")
    launcher.add_argument("--out", type=Path, default=default_out("release/ac-124-platform.json"))
    supply = commands.add_parser("supply-chain")
    supply.add_argument("--first-dir", type=Path, required=True)
    supply.add_argument("--second-dir", type=Path, required=True)
    supply.add_argument("--require-release", action="store_true")
    supply.add_argument("--expected-commit")
    supply.add_argument("--out", type=Path, default=default_out("security/ac-125-supply-chain.json"))
    rollback = commands.add_parser("upgrade-rollback")
    rollback.add_argument("--candidate-dir", type=Path, required=True)
    rollback.add_argument("--expected-commit")
    rollback.add_argument("--require-release", action="store_true")
    rollback.add_argument("--out", type=Path, default=default_out("release/ac-126-upgrade-rollback.json"))
    journey = commands.add_parser("user-journey")
    journey.add_argument("--candidate-dir", type=Path, required=True)
    journey.add_argument("--expected-commit")
    journey.add_argument("--require-release", action="store_true")
    journey.add_argument("--out", type=Path, default=default_out("contracts/ac-128-user-journeys.json"))
    canary = commands.add_parser("canary")
    canary.add_argument("--candidate-dir", type=Path, required=True)
    canary.add_argument("--approval-file", type=Path)
    canary.add_argument("--driver-command")
    canary.add_argument("--out", type=Path)
    canary.add_argument("--evidence-dir", type=Path)
    return parser


def main() -> int:
    args = build_parser().parse_args()
    try:
        if args.command == "verify-candidate":
            result = verify_candidate(
                args.candidate_dir,
                verify_signatures=True,
                require_release=args.require_release,
                expected_commit=args.expected_commit,
            )
            result.update({"schema_version": 1, "status": "passed"})
            write_evidence(args.out, result)
            exit_code = 0
        elif args.command == "launcher":
            result = launcher_acceptance(
                args.candidate_dir,
                args.out,
                args.expected_platform,
                require_release=args.require_release,
                expected_commit=args.expected_commit,
            )
            exit_code = 0
        elif args.command == "supply-chain":
            result = supply_chain_acceptance(
                args.first_dir,
                args.second_dir,
                args.out,
                require_release=args.require_release,
                expected_commit=args.expected_commit,
            )
            exit_code = 0
        elif args.command == "upgrade-rollback":
            result = upgrade_rollback_acceptance(
                args.candidate_dir,
                args.out,
                require_release=args.require_release,
                expected_commit=args.expected_commit,
            )
            exit_code = 0
        elif args.command == "user-journey":
            result = user_journey_acceptance(
                args.candidate_dir,
                args.out,
                require_release=args.require_release,
                expected_commit=args.expected_commit,
            )
            exit_code = 0
        elif args.command == "canary":
            result, exit_code = canary_acceptance(
                args.candidate_dir,
                args.approval_file,
                args.driver_command,
                canary_out(args),
            )
        else:
            raise AssertionError("unreachable")
    except (AcceptanceError, OSError, ValueError, zipfile.BadZipFile) as error:
        print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        return 2
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
