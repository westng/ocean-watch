#!/usr/bin/env python3
import argparse
import ast
import json
import re
import sys
from pathlib import Path

try:
    import tomllib
except ModuleNotFoundError:  # pragma: no cover - exercised by the Python 3.9 CI job
    import tomli as tomllib


TAG_PATTERN = re.compile(r"v(?P<version>[0-9]+\.[0-9]+\.[0-9]+)\Z")


class VersionTagError(RuntimeError):
    pass


def project_version(root):
    data = tomllib.loads((Path(root) / "pyproject.toml").read_text(encoding="utf-8"))
    value = ((data.get("project") or {}).get("version") or "").strip()
    if not value:
        raise VersionTagError("pyproject.toml does not define project.version")
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
    raise VersionTagError("ocean_watch.__version__ is missing or not a string literal")


def plugin_version(root):
    path = Path(root) / ".codex-plugin/plugin.json"
    data = json.loads(path.read_text(encoding="utf-8"))
    value = str(data.get("version") or "").strip()
    if not value:
        raise VersionTagError("plugin.json does not define version")
    return value


def validate_tag_version(value):
    if TAG_PATTERN.fullmatch(f"v{value}") is None:
        raise VersionTagError("project version must use MAJOR.MINOR.PATCH")
    return value


def validate_tag_changelog(changelog, version):
    release_heading = re.compile(
        rf"^## {re.escape(version)}\s+-\s+\d{{4}}-\d{{2}}-\d{{2}}\s*$",
        re.MULTILINE,
    )
    release_match = release_heading.search(changelog)
    if release_match is None:
        raise VersionTagError(f"CHANGELOG.md has no dated version heading for {version}")
    unreleased_heading = re.search(r"^## 未发布\s*$", changelog, re.MULTILINE)
    if unreleased_heading is None:
        raise VersionTagError("CHANGELOG.md must retain a 未发布 heading")
    next_heading = re.search(
        r"^## (?!未发布\s*$).+$",
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
        raise VersionTagError("CHANGELOG.md 未发布段落在创建版本 Tag 前必须为空")
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
        raise VersionTagError(f"CHANGELOG.md version section for {version} is empty")
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
        raise VersionTagError(f"version tag metadata does not match: {versions}")
    validate_tag_version(versions["project"])
    if tag:
        match = TAG_PATTERN.fullmatch(tag)
        if match is None:
            raise VersionTagError("version tag must use vMAJOR.MINOR.PATCH")
        if match.group("version") != versions["project"]:
            raise VersionTagError(f"tag {tag} does not match project version {versions['project']}")
        changelog = (root / "CHANGELOG.md").read_text(encoding="utf-8")
        validate_tag_changelog(changelog, versions["project"])
    return {**versions, "plugin_base": plugin_base, "tag": tag}


def derive_version_tag(root):
    return f"v{validate_versions(root)['project']}"


def release_notes(root, tag):
    versions = validate_versions(root, tag=tag)
    changelog = (Path(root) / "CHANGELOG.md").read_text(encoding="utf-8")
    return validate_tag_changelog(changelog, versions["project"])


def repository_root():
    return Path(__file__).resolve().parents[1]


def build_parser():
    parser = argparse.ArgumentParser(description="Validate Ocean Watch version tags.")
    parser.add_argument("--root", default=str(repository_root()))
    commands = parser.add_subparsers(dest="command", required=True)

    check = commands.add_parser("check", help="Validate version tag consistency.")
    check.add_argument("--tag")

    commands.add_parser("tag", help="Derive the version tag from project.version.")

    notes = commands.add_parser("notes", help="Write release notes from CHANGELOG.md.")
    notes.add_argument("--tag", required=True)
    notes.add_argument("--out", type=Path, required=True)

    return parser


def main(argv=None):
    parser = build_parser()
    args = parser.parse_args(argv)
    root = Path(args.root).resolve()
    try:
        if args.command == "check":
            result = validate_versions(root, tag=args.tag)
        elif args.command == "tag":
            result = {"tag": derive_version_tag(root)}
        elif args.command == "notes":
            notes = release_notes(root, args.tag)
            args.out.parent.mkdir(parents=True, exist_ok=True)
            args.out.write_text(notes, encoding="utf-8")
            result = {"tag": args.tag, "out": str(args.out), "bytes": len(notes.encode("utf-8"))}
        else:
            raise AssertionError("unreachable")
    except VersionTagError as error:
        print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=False, indent=2))
        return 1
    print(json.dumps({"ok": True, **result}, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
