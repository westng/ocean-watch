#!/usr/bin/env python3
import argparse
import ast
import hashlib
import json
import re
import stat
import subprocess
import sys
import zipfile
from pathlib import Path, PurePosixPath

try:
    import tomllib
except ModuleNotFoundError:  # pragma: no cover - exercised by the Python 3.9 CI job
    import tomli as tomllib


PROJECT_NAME = "ocean-watch"
ARCHIVE_TIMESTAMP = (1980, 1, 1, 0, 0, 0)
RELEASE_MANIFEST = "RELEASE-MANIFEST.json"
PLUGIN_ROOT_FILES = (
    ".agents/plugins/marketplace.json",
    ".codex-plugin/plugin.json",
    "CHANGELOG.md",
    "CONTRIBUTING.md",
    "LICENSE",
    "MANIFEST.in",
    "README.en-US.md",
    "README.md",
    "SECURITY.md",
    "pyproject.toml",
)
PLUGIN_TREES = ("docs", "skills")
REQUIRED_PLUGIN_FILES = {
    ".agents/plugins/marketplace.json",
    ".codex-plugin/plugin.json",
    "LICENSE",
    "README.md",
    "skills/ads-plan-monitor/SKILL.md",
    "skills/ads-plan-monitor/run.py",
    "skills/qc-plan-monitor/SKILL.md",
    "skills/qc-plan-monitor/run.py",
}
FORBIDDEN_PATH_PARTS = {
    ".git",
    ".venv",
    "__pycache__",
    "build",
    "config",
    "dist",
    "runs",
}
TAG_PATTERN = re.compile(r"v(?P<version>[0-9]+\.[0-9]+\.[0-9]+)\Z")


class ReleaseError(RuntimeError):
    pass


def sha256_bytes(value):
    return hashlib.sha256(value).hexdigest()


def sha256_file(path):
    digest = hashlib.sha256()
    with Path(path).open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def project_version(root):
    data = tomllib.loads((Path(root) / "pyproject.toml").read_text(encoding="utf-8"))
    value = ((data.get("project") or {}).get("version") or "").strip()
    if not value:
        raise ReleaseError("pyproject.toml does not define project.version")
    return value


def package_version(root):
    path = Path(root) / "skills/ads-plan-monitor/src/ocean_watch/__init__.py"
    module = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
    for node in module.body:
        if not isinstance(node, ast.Assign):
            continue
        if any(isinstance(target, ast.Name) and target.id == "__version__" for target in node.targets):
            if isinstance(node.value, ast.Constant) and isinstance(node.value.value, str):
                return node.value.value
    raise ReleaseError("ocean_watch.__version__ is missing or not a string literal")


def plugin_version(root):
    path = Path(root) / ".codex-plugin/plugin.json"
    data = json.loads(path.read_text(encoding="utf-8"))
    value = str(data.get("version") or "").strip()
    if not value:
        raise ReleaseError("plugin.json does not define version")
    return value


def validate_release_version(value):
    if TAG_PATTERN.fullmatch(f"v{value}") is None:
        raise ReleaseError("project version must use MAJOR.MINOR.PATCH")
    return value


def validate_release_changelog(changelog, version):
    release_heading = re.compile(
        rf"^## {re.escape(version)}\s+-\s+\d{{4}}-\d{{2}}-\d{{2}}\s*$",
        re.MULTILINE,
    )
    release_match = release_heading.search(changelog)
    if release_match is None:
        raise ReleaseError(
            f"CHANGELOG.md has no dated release heading for {version}"
        )
    unreleased_heading = re.search(r"^## Unreleased\s*$", changelog, re.MULTILINE)
    if unreleased_heading is None:
        raise ReleaseError("CHANGELOG.md must retain an Unreleased heading")
    next_heading = re.search(
        r"^## (?!Unreleased\s*$).+$",
        changelog[unreleased_heading.end():],
        re.MULTILINE,
    )
    section_end = (
        unreleased_heading.end() + next_heading.start()
        if next_heading is not None
        else len(changelog)
    )
    section = changelog[unreleased_heading.end():section_end]
    substantive_lines = [
        line.strip()
        for line in section.splitlines()
        if line.strip() and not line.strip().startswith("### ")
    ]
    if substantive_lines:
        raise ReleaseError(
            "CHANGELOG.md Unreleased section must be empty before creating a release"
        )
    next_release = re.search(
        r"^## .+$",
        changelog[release_match.end():],
        re.MULTILINE,
    )
    release_end = (
        release_match.end() + next_release.start()
        if next_release is not None
        else len(changelog)
    )
    release_section = changelog[release_match.start():release_end].strip()
    release_body = changelog[release_match.end():release_end]
    release_lines = [
        line.strip()
        for line in release_body.splitlines()
        if line.strip() and not line.strip().startswith("### ")
    ]
    if not release_lines:
        raise ReleaseError(f"CHANGELOG.md release section for {version} is empty")
    return release_section + "\n"


def validate_versions(root, tag=None):
    root = Path(root)
    versions = {
        "project": project_version(root),
        "package": package_version(root),
        "plugin": plugin_version(root),
    }
    plugin_base = versions["plugin"].split("+", 1)[0]
    if versions["project"] != versions["package"] or versions["project"] != plugin_base:
        raise ReleaseError(f"release versions do not match: {versions}")
    validate_release_version(versions["project"])
    if tag:
        match = TAG_PATTERN.fullmatch(tag)
        if match is None:
            raise ReleaseError("release tag must use vMAJOR.MINOR.PATCH")
        if match.group("version") != versions["project"]:
            raise ReleaseError(
                f"tag {tag} does not match project version {versions['project']}"
            )
        changelog = (root / "CHANGELOG.md").read_text(encoding="utf-8")
        validate_release_changelog(changelog, versions["project"])
    return {**versions, "plugin_base": plugin_base, "tag": tag}


def derive_release_tag(root):
    return f"v{validate_versions(root)['project']}"


def write_release_notes(root, tag, output_path):
    root = Path(root).resolve()
    versions = validate_versions(root, tag=tag)
    changelog = (root / "CHANGELOG.md").read_text(encoding="utf-8")
    notes = validate_release_changelog(changelog, versions["project"])
    output_path = Path(output_path)
    if not output_path.is_absolute():
        output_path = root / output_path
    if output_path.is_symlink():
        raise ReleaseError(f"refusing to replace symlinked release notes: {output_path}")
    try:
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(notes, encoding="utf-8")
    except OSError as error:
        raise ReleaseError(f"unable to write release notes: {output_path}") from error
    return {
        "notes_file": str(output_path.resolve()),
        "version": versions["project"],
        "character_count": len(notes),
    }


def is_forbidden_path(path):
    parts = set(path.parts)
    return bool(parts.intersection(FORBIDDEN_PATH_PARTS)) or any(
        part.endswith((".egg-info", ".pyc")) for part in path.parts
    )


def tracked_plugin_files(root):
    root = Path(root).resolve()
    pathspecs = [*PLUGIN_ROOT_FILES, *PLUGIN_TREES]
    try:
        completed = subprocess.run(
            ["git", "ls-files", "-z", "--", *pathspecs],
            cwd=root,
            check=True,
            capture_output=True,
        )
    except (OSError, subprocess.CalledProcessError) as error:
        raise ReleaseError("unable to enumerate tracked Plugin files") from error
    relative_paths = sorted(
        Path(value.decode("utf-8"))
        for value in completed.stdout.split(b"\0")
        if value
    )
    if not relative_paths:
        raise ReleaseError("the Plugin release allowlist matched no tracked files")
    for relative_path in relative_paths:
        if relative_path.is_absolute() or ".." in relative_path.parts:
            raise ReleaseError(f"unsafe tracked path: {relative_path}")
        if is_forbidden_path(relative_path):
            raise ReleaseError(f"forbidden path entered Plugin release: {relative_path}")
        source = root / relative_path
        if source.is_symlink() or not source.is_file():
            raise ReleaseError(f"Plugin releases require regular files: {relative_path}")
    available = {path.as_posix() for path in relative_paths}
    missing = sorted(REQUIRED_PLUGIN_FILES - available)
    if missing:
        raise ReleaseError(f"Plugin release is missing required files: {missing}")
    return relative_paths


def archive_info(name, executable=False):
    info = zipfile.ZipInfo(name, ARCHIVE_TIMESTAMP)
    info.create_system = 3
    info.compress_type = zipfile.ZIP_DEFLATED
    mode = 0o755 if executable else 0o644
    info.external_attr = (stat.S_IFREG | mode) << 16
    return info


def build_plugin_archive(root, output_dir):
    root = Path(root).resolve()
    output_dir = Path(output_dir).resolve()
    output_dir.mkdir(parents=True, exist_ok=True)
    versions = validate_versions(root)
    version = versions["project"]
    relative_paths = tracked_plugin_files(root)
    archive_root = f"{PROJECT_NAME}-{version}"
    output_path = output_dir / f"{PROJECT_NAME}-plugin-{version}.zip"
    if output_path.is_symlink():
        raise ReleaseError(f"refusing to replace symlinked release artifact: {output_path}")
    files = []
    for relative_path in relative_paths:
        content = (root / relative_path).read_bytes()
        files.append({
            "path": relative_path.as_posix(),
            "sha256": sha256_bytes(content),
            "size": len(content),
        })
    manifest = {
        "schema_version": 1,
        "name": PROJECT_NAME,
        "version": version,
        "plugin_version": versions["plugin"],
        "file_count": len(files),
        "files": files,
    }
    manifest_bytes = (
        json.dumps(manifest, ensure_ascii=True, indent=2, sort_keys=True) + "\n"
    ).encode("utf-8")
    with zipfile.ZipFile(output_path, "w") as archive:
        for relative_path in relative_paths:
            content = (root / relative_path).read_bytes()
            archive.writestr(
                archive_info(
                    f"{archive_root}/{relative_path.as_posix()}",
                    executable=relative_path.name == "run.py",
                ),
                content,
                compresslevel=9,
            )
        archive.writestr(
            archive_info(f"{archive_root}/{RELEASE_MANIFEST}"),
            manifest_bytes,
            compresslevel=9,
        )
    verify_plugin_archive(output_path)
    return {
        "archive": str(output_path),
        "sha256": sha256_file(output_path),
        "version": version,
        "plugin_version": versions["plugin"],
        "file_count": len(files),
    }


def verify_plugin_archive(archive_path):
    archive_path = Path(archive_path)
    try:
        with zipfile.ZipFile(archive_path) as archive:
            infos = archive.infolist()
            if not infos:
                raise ReleaseError("Plugin archive is empty")
            names = [info.filename for info in infos]
            if len(names) != len(set(names)):
                raise ReleaseError("Plugin archive contains duplicate entries")
            roots = set()
            relative_infos = {}
            for info in infos:
                raw_parts = info.filename.split("/")
                if (
                    info.is_dir()
                    or info.filename.startswith("/")
                    or "\\" in info.filename
                    or any(part in {"", ".", ".."} for part in raw_parts)
                    or len(raw_parts) < 2
                ):
                    raise ReleaseError(f"unsafe Plugin archive entry: {info.filename}")
                mode = info.external_attr >> 16
                if info.create_system == 3 and mode and not stat.S_ISREG(mode):
                    raise ReleaseError(f"non-regular Plugin archive entry: {info.filename}")
                roots.add(raw_parts[0])
                relative = PurePosixPath(*raw_parts[1:])
                relative_infos[relative.as_posix()] = info
            if len(roots) != 1:
                raise ReleaseError("Plugin archive must contain one root directory")
            archive_root = next(iter(roots))
            if RELEASE_MANIFEST not in relative_infos:
                raise ReleaseError("Plugin archive has no release manifest")
            relative_names = set(relative_infos) - {RELEASE_MANIFEST}
            for relative_name in relative_names:
                relative = PurePosixPath(relative_name)
                if is_forbidden_path(Path(*relative.parts)):
                    raise ReleaseError(f"forbidden Plugin archive entry: {relative}")
            missing = sorted(REQUIRED_PLUGIN_FILES - relative_names)
            if missing:
                raise ReleaseError(f"Plugin archive is missing required files: {missing}")
            manifest_name = f"{archive_root}/{RELEASE_MANIFEST}"
            manifest = json.loads(archive.read(manifest_name).decode("utf-8"))
            if not isinstance(manifest, dict):
                raise ReleaseError("Plugin release manifest must be an object")
            version = manifest.get("version")
            plugin_version_value = manifest.get("plugin_version")
            if (
                manifest.get("schema_version") != 1
                or manifest.get("name") != PROJECT_NAME
                or not isinstance(version, str)
                or TAG_PATTERN.fullmatch(f"v{version}") is None
                or not isinstance(plugin_version_value, str)
                or plugin_version_value.split("+", 1)[0] != version
                or archive_root != f"{PROJECT_NAME}-{version}"
            ):
                raise ReleaseError("Plugin release manifest metadata is invalid")
            manifest_files = manifest.get("files")
            if not isinstance(manifest_files, list):
                raise ReleaseError("Plugin release manifest files must be an array")
            declared = {}
            for row in manifest_files:
                if not isinstance(row, dict):
                    raise ReleaseError("Plugin release manifest contains an invalid file entry")
                path = row.get("path")
                size = row.get("size")
                digest = row.get("sha256")
                if (
                    not isinstance(path, str)
                    or path in declared
                    or not isinstance(size, int)
                    or isinstance(size, bool)
                    or size < 0
                    or not isinstance(digest, str)
                    or re.fullmatch(r"[0-9a-f]{64}", digest) is None
                ):
                    raise ReleaseError("Plugin release manifest contains an invalid file entry")
                declared[path] = row
            declared_names = set(declared)
            if declared_names != relative_names:
                raise ReleaseError("Plugin release manifest does not match archive entries")
            if manifest.get("file_count") != len(manifest_files):
                raise ReleaseError("Plugin release manifest file count is invalid")
            for row in declared.values():
                content = archive.read(f"{archive_root}/{row['path']}")
                if len(content) != row.get("size") or sha256_bytes(content) != row.get("sha256"):
                    raise ReleaseError(f"Plugin release manifest mismatch: {row['path']}")
    except ReleaseError:
        raise
    except (KeyError, OSError, UnicodeError, ValueError, zipfile.BadZipFile) as error:
        raise ReleaseError("Plugin archive is unreadable or malformed") from error
    return {
        "archive": str(archive_path),
        "sha256": sha256_file(archive_path),
        "file_count": len(relative_names),
    }


def artifact_files(directory, checksum_name="SHA256SUMS"):
    directory = Path(directory)
    if not directory.is_dir():
        raise ReleaseError(f"release artifact directory does not exist: {directory}")
    try:
        files = sorted(
            path
            for path in directory.iterdir()
            if path.is_file() and path.name != checksum_name
        )
    except OSError as error:
        raise ReleaseError(f"unable to enumerate release artifacts: {directory}") from error
    if not files:
        raise ReleaseError("no release artifacts were found")
    if any(path.is_symlink() for path in files):
        raise ReleaseError("release artifacts must be regular files")
    return files


def write_checksums(directory, checksum_name="SHA256SUMS"):
    directory = Path(directory).resolve()
    output_path = directory / checksum_name
    if output_path.is_symlink():
        raise ReleaseError(f"refusing to replace symlinked checksum file: {output_path}")
    lines = [f"{sha256_file(path)}  {path.name}" for path in artifact_files(directory, checksum_name)]
    try:
        output_path.write_text("\n".join(lines) + "\n", encoding="ascii")
    except OSError as error:
        raise ReleaseError(f"unable to write checksum file: {output_path}") from error
    verify_checksums(output_path)
    return {"checksum_file": str(output_path), "artifact_count": len(lines)}


def verify_checksums(checksum_path):
    checksum_path = Path(checksum_path)
    if checksum_path.is_symlink() or not checksum_path.is_file():
        raise ReleaseError(f"checksum file does not exist or is not regular: {checksum_path}")
    checksum_path = checksum_path.resolve()
    directory = checksum_path.parent
    expected_files = {path.name for path in artifact_files(directory, checksum_path.name)}
    declared = {}
    try:
        checksum_lines = checksum_path.read_text(encoding="ascii").splitlines()
    except (OSError, UnicodeError) as error:
        raise ReleaseError(f"unable to read checksum file: {checksum_path}") from error
    for line in checksum_lines:
        match = re.fullmatch(r"([0-9a-f]{64})  ([^/\\]+)", line)
        if match is None or match.group(2) in declared:
            raise ReleaseError("SHA256SUMS contains an invalid or duplicate entry")
        declared[match.group(2)] = match.group(1)
    if set(declared) != expected_files:
        raise ReleaseError("SHA256SUMS does not cover exactly the release artifacts")
    for name, expected in declared.items():
        if sha256_file(directory / name) != expected:
            raise ReleaseError(f"checksum mismatch: {name}")
    return {"checksum_file": str(checksum_path), "artifact_count": len(declared)}


def repository_root():
    return Path(__file__).resolve().parents[1]


def build_parser():
    parser = argparse.ArgumentParser(description="Build and verify Ocean Watch releases.")
    parser.add_argument("--root", default=str(repository_root()))
    commands = parser.add_subparsers(dest="command", required=True)

    check = commands.add_parser("check", help="Validate release version consistency.")
    check.add_argument("--tag")

    commands.add_parser("tag", help="Derive the release tag from project.version.")

    notes = commands.add_parser("notes", help="Write release notes from CHANGELOG.md.")
    notes.add_argument("--tag", required=True)
    notes.add_argument("--output", default="release-notes/RELEASE_NOTES.md")

    plugin = commands.add_parser("plugin", help="Build the deterministic Plugin archive.")
    plugin.add_argument("--output-dir", default="dist")

    verify_plugin = commands.add_parser("verify-plugin", help="Verify one Plugin archive.")
    verify_plugin.add_argument("--archive", required=True)

    checksums = commands.add_parser("checksums", help="Write SHA256SUMS for a directory.")
    checksums.add_argument("--directory", default="dist")

    verify = commands.add_parser("verify-checksums", help="Verify SHA256SUMS.")
    verify.add_argument("--file", default="dist/SHA256SUMS")
    return parser


def main(argv=None):
    parser = build_parser()
    args = parser.parse_args(argv)
    root = Path(args.root).resolve()
    try:
        if args.command == "check":
            result = validate_versions(root, tag=args.tag)
        elif args.command == "tag":
            result = {"tag": derive_release_tag(root)}
        elif args.command == "notes":
            result = write_release_notes(root, args.tag, args.output)
        elif args.command == "plugin":
            output_dir = Path(args.output_dir)
            if not output_dir.is_absolute():
                output_dir = root / output_dir
            result = build_plugin_archive(root, output_dir)
        elif args.command == "verify-plugin":
            result = verify_plugin_archive(args.archive)
        elif args.command == "checksums":
            result = write_checksums(args.directory)
        else:
            result = verify_checksums(args.file)
    except ReleaseError as error:
        print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=False, indent=2))
        return 1
    print(json.dumps({"ok": True, **result}, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
