#!/usr/bin/env python3
"""Build a signed, reproducible Ocean Watch runtime release candidate."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import stat
import subprocess
import sys
import zipfile
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
GO_MODULE = ROOT / "prototype" / "ocean-watch-go"
BOOTSTRAP_MODULE = ROOT / "prototype" / "runtime-bootstrap"
GO_TOOLCHAIN = "go1.26.5"
SDK_VERSION = "v1.1.92"
TARGETS = (
    ("darwin", "arm64"),
    ("darwin", "amd64"),
    ("linux", "amd64"),
    ("linux", "arm64"),
    ("windows", "amd64"),
)
REPOSITORY = "https://github.com/westng/ocean-watch"
DEFAULT_RELEASE_BASE_URL = f"{REPOSITORY}/releases/download"
ZIP_TIMESTAMP = (1980, 1, 1, 0, 0, 0)
SIGNING_KEY_ENV = "OCEAN_WATCH_RELEASE_SIGNING_KEY"


class ReleaseBuildError(RuntimeError):
    pass


def canonical_json(value: object) -> bytes:
    return json.dumps(
        value,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")


def sha256_bytes(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def run(command, *, cwd=ROOT, env=None, text=True) -> subprocess.CompletedProcess:
    completed = subprocess.run(
        [str(value) for value in command],
        cwd=cwd,
        env=env,
        capture_output=True,
        text=text,
        check=False,
    )
    if completed.returncode != 0:
        stderr = completed.stderr if text else completed.stderr.decode("utf-8", errors="replace")
        raise ReleaseBuildError(f"command failed ({' '.join(command)}): {stderr.strip()}")
    return completed


def load_versions() -> tuple[str, str]:
    sys.path.insert(0, str(ROOT))
    from scripts.version_tag import validate_versions

    versions = validate_versions(ROOT)
    return versions["project"], versions["plugin"]


def git_identity() -> tuple[str, int, list[str]]:
    commit = run(["git", "rev-parse", "HEAD"]).stdout.strip()
    if len(commit) != 40:
        raise ReleaseBuildError("Git commit identity is malformed")
    timestamp = int(run(["git", "show", "-s", "--format=%ct", commit]).stdout.strip())
    status = run(["git", "status", "--porcelain", "--untracked-files=all"]).stdout.splitlines()
    return commit, timestamp, status


def release_sources() -> list[Path]:
    roots = (
        ROOT / ".codex-plugin",
        ROOT / ".agents" / "plugins",
        ROOT / "skills",
        ROOT / "scripts" / "runtime_launcher.py",
        ROOT / "scripts" / "release",
        ROOT / "prototype" / "ocean-watch-go",
        ROOT / "prototype" / "runtime-bootstrap",
        ROOT / "pyproject.toml",
        ROOT / "LICENSE",
    )
    result = []
    for root in roots:
        if root.is_file():
            result.append(root)
            continue
        for path in sorted(root.rglob("*")):
            if not path.is_file() or "__pycache__" in path.parts or path.suffix in {".pyc", ".tmp"}:
                continue
            result.append(path)
    return sorted(set(result))


def source_tree_digest() -> str:
    digest = hashlib.sha256()
    for path in release_sources():
        relative = path.relative_to(ROOT).as_posix().encode("utf-8")
        payload = path.read_bytes()
        digest.update(len(relative).to_bytes(4, "big"))
        digest.update(relative)
        digest.update(len(payload).to_bytes(8, "big"))
        digest.update(payload)
    return digest.hexdigest()


def prepare_output(path: Path) -> Path:
    resolved = path.resolve()
    if resolved in {ROOT, ROOT.parent, Path.home().resolve()} or len(resolved.parts) < 4:
        raise ReleaseBuildError("refusing unsafe release output path")
    if resolved.exists():
        shutil.rmtree(resolved)
    resolved.mkdir(parents=True, mode=0o700)
    return resolved


def build_environment(
    timestamp: int, goos: str | None = None, goarch: str | None = None
) -> dict:
    environment = {
        **os.environ,
        "CGO_ENABLED": "0",
        "GOTOOLCHAIN": GO_TOOLCHAIN,
        "SOURCE_DATE_EPOCH": str(timestamp),
        "TZ": "UTC",
        "LC_ALL": "C",
    }
    if goos:
        environment["GOOS"] = goos
    if goarch:
        environment["GOARCH"] = goarch
    return environment


def target_name(prefix: str, goos: str, goarch: str) -> str:
    suffix = ".exe" if goos == "windows" else ""
    return f"{prefix}_{goos}_{goarch}{suffix}"


def build_go_binary(module: Path, package: str, output: Path, environment: dict, ldflags: list[str]) -> None:
    run(
        [
            "go",
            "build",
            "-buildvcs=false",
            "-trimpath",
            "-ldflags",
            " ".join(["-s", "-w", "-buildid=", *ldflags]),
            "-o",
            output,
            package,
        ],
        cwd=module,
        env=environment,
    )
    output.chmod(0o700)


def release_tool(arguments: list[str], environment: dict) -> str:
    completed = run(
        ["go", "run", "./cmd/release-tool", *arguments],
        cwd=BOOTSTRAP_MODULE,
        env=environment,
    )
    return completed.stdout.strip()


def route_manifest(environment: dict) -> dict:
    completed = run(
        ["go", "run", "./cmd/route-manifest"],
        cwd=GO_MODULE,
        env=environment,
    )
    result = json.loads(completed.stdout)
    routes = result.get("routes")
    if not isinstance(routes, dict) or not routes or set(routes.values()) != {"python"}:
        raise ReleaseBuildError("production route manifest must keep every route on Python before G5")
    return result


def build_binaries(
    output: Path,
    product_version: str,
    plugin_version: str,
    commit: str,
    timestamp: int,
    public_key: str,
    release_base_url: str,
    allow_insecure_test_bootstrap: bool,
) -> tuple[list[Path], list[Path]]:
    runtimes = []
    bootstraps = []
    for goos, goarch in TARGETS:
        environment = build_environment(timestamp, goos, goarch)
        runtime = output / target_name("ocean-watch", goos, goarch)
        build_go_binary(
            GO_MODULE,
            "./cmd/ocean-watch",
            runtime,
            environment,
            [
                f"-X main.productVersion={product_version}",
                f"-X main.gitCommit={commit}",
                f"-X main.sdkVersion={SDK_VERSION}",
            ],
        )
        runtimes.append(runtime)
        bootstrap = output / target_name("ocean-watch-bootstrap", goos, goarch)
        build_go_binary(
            BOOTSTRAP_MODULE,
            "./cmd/ocean-watch-bootstrap",
            bootstrap,
            environment,
            [
                f"-X main.productVersion={product_version}",
                f"-X main.pluginVersion={plugin_version}",
                f"-X main.gitCommit={commit}",
                f"-X main.sdkVersion={SDK_VERSION}",
                f"-X main.trustedPublicKeyHex={public_key}",
                f"-X main.releaseBaseURL={release_base_url}",
                f"-X main.allowInsecureTests={'true' if allow_insecure_test_bootstrap else 'false'}",
            ],
        )
        bootstraps.append(bootstrap)
    return runtimes, bootstraps


def write_runtime_manifest(
    output: Path,
    product_version: str,
    plugin_version: str,
    commit: str,
    routes: dict,
    runtimes: list[Path],
    environment: dict,
) -> tuple[Path, Path]:
    assets = {}
    for path, (goos, goarch) in zip(runtimes, TARGETS, strict=True):
        assets[f"{goos}-{goarch}"] = {
            "name": path.name,
            "sha256": sha256_file(path),
            "size": path.stat().st_size,
        }
    manifest = output / "runtime-manifest.json"
    manifest.write_bytes(
        canonical_json(
            {
                "assets": assets,
                "git_commit": commit,
                "manifest_version": 1,
                "plugin_version": plugin_version,
                "product_version": product_version,
                "routes": routes,
                "sdk_version": SDK_VERSION,
                "tag": f"v{product_version}",
            }
        )
    )
    signature = output / "runtime-manifest.sig"
    release_tool(["sign", "--input", str(manifest), "--output", str(signature)], environment)
    return manifest, signature


def tracked_plugin_files() -> list[Path]:
    tracked = run(["git", "ls-files", "-z"], text=False).stdout.split(b"\0")
    result = []
    for raw in tracked:
        if not raw:
            continue
        relative = Path(os.fsdecode(raw))
        if (
            relative == Path(".codex-plugin/plugin.json")
            or relative == Path(".agents/plugins/marketplace.json")
            or relative in {Path("LICENSE"), Path("README.md"), Path("README.en-US.md")}
            or relative.parts[:1] == ("skills",)
        ):
            result.append(relative)
    for required in (
        Path(".codex-plugin/plugin.json"),
        Path("scripts/runtime_launcher.py"),
        Path("skills/ads-plan-monitor/run.py"),
        Path("skills/qc-plan-monitor/run.py"),
    ):
        if required not in result:
            result.append(required)
    return sorted(set(result))


def build_plugin_candidate(
    output: Path,
    product_version: str,
    plugin_version: str,
    routes: dict,
    bootstraps: list[Path],
) -> Path:
    staging = output / ".plugin-staging" / "ocean-watch"
    for relative in tracked_plugin_files():
        source = ROOT / relative
        if not source.is_file():
            raise ReleaseBuildError(f"Plugin source is missing: {relative}")
        destination = staging / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, destination)
    policy = {
        "schema_version": 1,
        "enabled": True,
        "product_version": product_version,
        "plugin_version": plugin_version,
        "commands": sorted(route for route in routes if route != "--version"),
    }
    policy_path = staging / ".codex-plugin" / "runtime-policy.json"
    policy_path.write_bytes(canonical_json(policy) + b"\n")
    bootstrap_root = staging / ".codex-plugin" / "runtime" / "bootstrap"
    bootstrap_root.mkdir(parents=True, exist_ok=True)
    for bootstrap in bootstraps:
        destination = bootstrap_root / bootstrap.name
        shutil.copyfile(bootstrap, destination)
        destination.chmod(0o700)
    archive = output / f"ocean-watch-plugin_v{product_version}.zip"
    with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as bundle:
        for path in sorted(staging.rglob("*")):
            if not path.is_file():
                continue
            relative = path.relative_to(staging.parent).as_posix()
            info = zipfile.ZipInfo(relative, ZIP_TIMESTAMP)
            info.compress_type = zipfile.ZIP_DEFLATED
            info.create_system = 3
            executable = path.name == "run.py" or "runtime/bootstrap" in relative
            mode = 0o755 if executable else 0o644
            info.external_attr = (stat.S_IFREG | mode) << 16
            bundle.writestr(info, path.read_bytes(), compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)
    shutil.rmtree(output / ".plugin-staging")
    return archive


def module_inventory(environment: dict) -> list[dict]:
    completed = run(["go", "list", "-m", "-json", "all"], cwd=GO_MODULE, env=environment)
    decoder = json.JSONDecoder()
    value = completed.stdout
    modules = []
    index = 0
    while index < len(value):
        while index < len(value) and value[index].isspace():
            index += 1
        if index >= len(value):
            break
        item, index = decoder.raw_decode(value, index)
        modules.append(item)
    return modules


def spdx_identifier(value: str) -> str:
    return "SPDXRef-" + "".join(character if character.isalnum() else "-" for character in value)


def write_sbom(
    output: Path,
    product_version: str,
    commit: str,
    timestamp: int,
    source_digest: str,
    environment: dict,
) -> Path:
    modules = module_inventory(environment)
    license_map = {
        "github.com/oceanengine/ad_open_sdk_go": "Apache-2.0",
        "golang.org/x/sys": "BSD-3-Clause",
    }
    packages = [
        {
            "SPDXID": "SPDXRef-Package-ocean-watch",
            "name": "ocean-watch",
            "versionInfo": product_version,
            "downloadLocation": f"git+{REPOSITORY}.git@{commit}",
            "filesAnalyzed": False,
            "licenseConcluded": "MIT",
            "licenseDeclared": "MIT",
            "supplier": "Organization: westng",
            "checksums": [{"algorithm": "SHA256", "checksumValue": source_digest}],
        }
    ]
    relationships = []
    for module in modules:
        if module.get("Main"):
            continue
        name = module["Path"]
        version = module.get("Version") or "unknown"
        identifier = spdx_identifier(name)
        package = {
            "SPDXID": identifier,
            "name": name,
            "versionInfo": version,
            "downloadLocation": f"https://proxy.golang.org/{name}/@v/{version}.zip",
            "filesAnalyzed": False,
            "licenseConcluded": license_map.get(name, "NOASSERTION"),
            "licenseDeclared": license_map.get(name, "NOASSERTION"),
            "supplier": "NOASSERTION",
            "externalRefs": [
                {
                    "referenceCategory": "PACKAGE-MANAGER",
                    "referenceType": "purl",
                    "referenceLocator": f"pkg:golang/{name}@{version}",
                }
            ],
        }
        if module.get("Sum"):
            package["comment"] = f"Go module sum: {module['Sum']}"
        packages.append(package)
        relationships.append(
            {
                "spdxElementId": "SPDXRef-Package-ocean-watch",
                "relationshipType": "DEPENDS_ON",
                "relatedSpdxElement": identifier,
            }
        )
    created = datetime.fromtimestamp(timestamp, timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    sbom = {
        "spdxVersion": "SPDX-2.3",
        "dataLicense": "CC0-1.0",
        "SPDXID": "SPDXRef-DOCUMENT",
        "name": f"ocean-watch-{product_version}",
        "documentNamespace": f"{REPOSITORY}/spdx/v{product_version}/{source_digest}",
        "creationInfo": {
            "created": created,
            "creators": ["Tool: ocean-watch-release-builder"],
        },
        "documentDescribes": ["SPDXRef-Package-ocean-watch"],
        "packages": packages,
        "relationships": relationships,
    }
    path = output / "ocean-watch.spdx.json"
    path.write_bytes(canonical_json(sbom) + b"\n")
    return path


def write_provenance(
    output: Path,
    product_version: str,
    plugin_version: str,
    commit: str,
    source_digest: str,
    dirty_paths: list[str],
    subjects: list[Path],
) -> Path:
    provenance = {
        "_type": "https://in-toto.io/Statement/v1",
        "subject": [
            {"name": path.name, "digest": {"sha256": sha256_file(path)}}
            for path in sorted(subjects)
        ],
        "predicateType": "https://slsa.dev/provenance/v1",
        "predicate": {
            "buildDefinition": {
                "buildType": f"{REPOSITORY}/buildtypes/go-runtime/v1",
                "externalParameters": {
                    "product_version": product_version,
                    "plugin_version": plugin_version,
                    "sdk_version": SDK_VERSION,
                    "targets": [f"{goos}-{goarch}" for goos, goarch in TARGETS],
                },
                "internalParameters": {
                    "cgo_enabled": False,
                    "go_toolchain": GO_TOOLCHAIN,
                    "trimpath": True,
                },
                "resolvedDependencies": [
                    {
                        "uri": f"git+{REPOSITORY}.git@{commit}",
                        "digest": {"gitCommit": commit, "sha256": source_digest},
                    },
                    {
                        "uri": "pkg:golang/github.com/oceanengine/ad_open_sdk_go@v1.1.92",
                        "digest": {"h1": "1yL4xoERfG94Cwm/2q9mm2iJfdpgn9fOecnaG49Qqn8="},
                    },
                ],
            },
            "runDetails": {
                "builder": {"id": f"{REPOSITORY}/.github/workflows/tag.yml"},
                "metadata": {
                    "invocationId": f"{commit}:{source_digest}",
                    "source_dirty": bool(dirty_paths),
                    "dirty_paths": dirty_paths,
                },
            },
        },
    }
    path = output / "provenance.intoto.jsonl"
    path.write_bytes(canonical_json(provenance) + b"\n")
    return path


def write_checksums(
    output: Path,
    product_version: str,
    plugin_version: str,
    commit: str,
    files: list[Path],
    environment: dict,
) -> tuple[Path, Path]:
    payload = {
        "schema_version": 1,
        "product_version": product_version,
        "plugin_version": plugin_version,
        "git_commit": commit,
        "files": [
            {"name": path.name, "size": path.stat().st_size, "sha256": sha256_file(path)}
            for path in sorted(files)
        ],
    }
    checksums = output / "checksums.json"
    checksums.write_bytes(canonical_json(payload))
    signature = output / "checksums.sig"
    release_tool(["sign", "--input", str(checksums), "--output", str(signature)], environment)
    return checksums, signature


def write_build_summary(
    output: Path,
    *,
    release: bool,
    product_version: str,
    plugin_version: str,
    commit: str,
    source_digest: str,
    dirty_paths: list[str],
    route_data: dict,
    runtime_count: int,
    bootstrap_count: int,
    signed_file_count: int,
) -> tuple[Path, dict]:
    result = {
        "schema_version": 1,
        "status": "release_ready" if release else "candidate_built",
        "release": release,
        "product_version": product_version,
        "plugin_version": plugin_version,
        "git_commit": commit,
        "source_tree_sha256": source_digest,
        "source_dirty": bool(dirty_paths),
        "route_manifest_version": route_data["route_manifest_version"],
        "production_go_routes": sum(
            1 for value in route_data["routes"].values() if value == "go"
        ),
        "runtime_assets": runtime_count,
        "bootstrap_assets": bootstrap_count,
        "signed_file_count": signed_file_count,
        "output": ".",
    }
    summary = output / "build-summary.json"
    summary.write_bytes(canonical_json(result) + b"\n")
    return summary, result


def verify_release(
    output: Path,
    public_key: str,
    manifest: Path,
    manifest_signature: Path,
    checksums: Path,
    checksums_signature: Path,
    environment: dict,
) -> None:
    for payload, signature in (
        (manifest, manifest_signature),
        (checksums, checksums_signature),
    ):
        release_tool(
            [
                "verify",
                "--input",
                str(payload),
                "--signature",
                str(signature),
                "--public-key-hex",
                public_key,
            ],
            environment,
        )
    declared = json.loads(checksums.read_text(encoding="utf-8"))["files"]
    for item in declared:
        path = output / item["name"]
        if not path.is_file() or path.stat().st_size != item["size"] or sha256_file(path) != item["sha256"]:
            raise ReleaseBuildError(f"release checksum verification failed: {item['name']}")


def build(args) -> dict:
    if not os.environ.get(SIGNING_KEY_ENV, "").strip():
        raise ReleaseBuildError(f"{SIGNING_KEY_ENV} is required")
    product_version, plugin_version = load_versions()
    commit, timestamp, dirty_paths = git_identity()
    if args.release and dirty_paths:
        raise ReleaseBuildError("release mode requires a clean working tree")
    if args.allow_insecure_test_bootstrap and not args.test_release_base_url:
        raise ReleaseBuildError("insecure test bootstrap requires --test-release-base-url")
    if args.test_release_base_url and args.release:
        raise ReleaseBuildError("release mode cannot override the fixed release origin")
    output = prepare_output(args.out_dir)
    source_digest = source_tree_digest()
    environment = build_environment(timestamp)
    public_key = release_tool(["public-key"], environment)
    if len(public_key) != 64:
        raise ReleaseBuildError("release public key is malformed")
    public_key_path = output / "release-public-key.txt"
    public_key_path.write_text(public_key + "\n", encoding="utf-8")
    release_base_url = args.test_release_base_url or DEFAULT_RELEASE_BASE_URL
    runtimes, bootstraps = build_binaries(
        output,
        product_version,
        plugin_version,
        commit,
        timestamp,
        public_key,
        release_base_url,
        args.allow_insecure_test_bootstrap,
    )
    route_data = route_manifest(environment)
    manifest, manifest_signature = write_runtime_manifest(
        output,
        product_version,
        plugin_version,
        commit,
        route_data["routes"],
        runtimes,
        environment,
    )
    plugin = build_plugin_candidate(
        output,
        product_version,
        plugin_version,
        route_data["routes"],
        bootstraps,
    )
    sbom = write_sbom(output, product_version, commit, timestamp, source_digest, environment)
    signed_file_count = len(runtimes) + len(bootstraps) + 7
    summary, result = write_build_summary(
        output,
        release=bool(args.release),
        product_version=product_version,
        plugin_version=plugin_version,
        commit=commit,
        source_digest=source_digest,
        dirty_paths=dirty_paths,
        route_data=route_data,
        runtime_count=len(runtimes),
        bootstrap_count=len(bootstraps),
        signed_file_count=signed_file_count,
    )
    provenance_subjects = [
        *runtimes,
        *bootstraps,
        manifest,
        manifest_signature,
        plugin,
        sbom,
        summary,
    ]
    provenance = write_provenance(
        output,
        product_version,
        plugin_version,
        commit,
        source_digest,
        dirty_paths,
        provenance_subjects,
    )
    signed_files = [*provenance_subjects, provenance, public_key_path]
    checksums, checksums_signature = write_checksums(
        output,
        product_version,
        plugin_version,
        commit,
        signed_files,
        environment,
    )
    verify_release(
        output,
        public_key,
        manifest,
        manifest_signature,
        checksums,
        checksums_signature,
        environment,
    )
    if len(signed_files) != signed_file_count:
        raise ReleaseBuildError("release signed file count is inconsistent")
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return result


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser()
    parser.add_argument("--out-dir", type=Path, required=True)
    parser.add_argument("--release", action="store_true")
    parser.add_argument("--test-release-base-url")
    parser.add_argument("--allow-insecure-test-bootstrap", action="store_true")
    return parser


def main() -> int:
    try:
        build(build_parser().parse_args())
    except (OSError, ValueError, ReleaseBuildError) as error:
        print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
