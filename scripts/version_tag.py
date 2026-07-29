#!/usr/bin/env python3
import argparse
import ast
import datetime as dt
import json
import re
import sys
from pathlib import Path

try:
    import tomllib
except ModuleNotFoundError:  # pragma: no cover - exercised by the Python 3.9 CI job
    import tomli as tomllib


TAG_PATTERN = re.compile(r"v(?P<version>[0-9]+\.[0-9]+\.[0-9]+)\Z")
VERSION_PATTERN = re.compile(r"(?P<major>[0-9]+)\.(?P<minor>[0-9]+)\.(?P<patch>[0-9]+)\Z")
CACHEBUSTER_PATTERN = re.compile(r"[A-Za-z0-9][A-Za-z0-9.-]*\Z")


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


def version_tuple(value):
    match = VERSION_PATTERN.fullmatch(value)
    if match is None:
        raise VersionTagError("version must use MAJOR.MINOR.PATCH")
    return tuple(int(match.group(name)) for name in ("major", "minor", "patch"))


def next_patch_version(value):
    major, minor, patch = version_tuple(value)
    return f"{major}.{minor}.{patch + 1}"


def tag_version(tag):
    match = TAG_PATTERN.fullmatch(tag)
    if match is None:
        raise VersionTagError("latest release tag must use vMAJOR.MINOR.PATCH")
    return match.group("version")


def changelog_sections(changelog):
    unreleased = re.search(r"^## 未发布[ \t]*$", changelog, re.MULTILINE)
    if unreleased is None:
        raise VersionTagError("CHANGELOG.md must retain a 未发布 heading")
    next_heading = re.search(
        r"^## (?!未发布[ \t]*$).+$",
        changelog[unreleased.end():],
        re.MULTILINE,
    )
    section_end = (
        unreleased.end() + next_heading.start()
        if next_heading is not None
        else len(changelog)
    )
    section = changelog[unreleased.end():section_end]
    substantive = [
        line.strip()
        for line in section.splitlines()
        if line.strip() and not line.strip().startswith("### ")
    ]
    return unreleased, section_end, section, substantive


def validate_tag_changelog(changelog, version):
    release_heading = re.compile(
        rf"^## {re.escape(version)}[ \t]+-[ \t]+\d{{4}}-\d{{2}}-\d{{2}}[ \t]*$",
        re.MULTILINE,
    )
    release_match = release_heading.search(changelog)
    if release_match is None:
        raise VersionTagError(f"CHANGELOG.md has no dated version heading for {version}")
    _, _, _, substantive_lines = changelog_sections(changelog)
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


def replace_once(value, pattern, replacement, label):
    result, count = re.subn(pattern, replacement, value, count=1, flags=re.MULTILINE)
    if count != 1:
        raise VersionTagError(f"unable to update {label}")
    return result


def prepare_release(root, latest_tag, release_date, cachebuster):
    root = Path(root)
    versions = validate_versions(root)
    latest_version = tag_version(latest_tag)
    try:
        normalized_date = dt.date.fromisoformat(release_date).isoformat()
    except ValueError as error:
        raise VersionTagError("release date must use YYYY-MM-DD") from error
    if not CACHEBUSTER_PATTERN.fullmatch(cachebuster):
        raise VersionTagError("cachebuster contains unsupported characters")

    changelog_path = root / "CHANGELOG.md"
    changelog = changelog_path.read_text(encoding="utf-8")
    unreleased, section_end, section, substantive = changelog_sections(changelog)
    current_version = versions["project"]
    expected_version = next_patch_version(latest_version)

    if not substantive:
        if version_tuple(current_version) >= version_tuple(latest_version):
            validate_tag_changelog(changelog, current_version)
            return {
                "version": current_version,
                "tag": f"v{current_version}",
                "already_prepared": True,
            }
        raise VersionTagError("CHANGELOG.md 未发布段落没有可发布内容")

    if current_version == latest_version:
        target_version = expected_version
    elif current_version == expected_version:
        target_version = current_version
    else:
        raise VersionTagError(
            f"current version {current_version} cannot follow latest release {latest_tag}"
        )
    if re.search(rf"^## {re.escape(target_version)}[ \t]+-", changelog, re.MULTILINE):
        raise VersionTagError(f"CHANGELOG.md already contains release {target_version}")

    release_body = section.strip()
    prepared_changelog = (
        changelog[:unreleased.end()]
        + f"\n\n## {target_version} - {normalized_date}\n\n{release_body}\n\n"
        + changelog[section_end:].lstrip("\n")
    )
    changelog_path.write_text(prepared_changelog, encoding="utf-8")

    pyproject_path = root / "pyproject.toml"
    pyproject_path.write_text(
        replace_once(
            pyproject_path.read_text(encoding="utf-8"),
            r'^version = "[^"]+"$',
            f'version = "{target_version}"',
            "pyproject.toml project.version",
        ),
        encoding="utf-8",
    )
    package_path = root / "skills/ads-plan-monitor/src/ocean_watch/__init__.py"
    package_path.write_text(
        replace_once(
            package_path.read_text(encoding="utf-8"),
            r'^__version__ = "[^"]+"$',
            f'__version__ = "{target_version}"',
            "ocean_watch.__version__",
        ),
        encoding="utf-8",
    )
    plugin_path = root / ".codex-plugin/plugin.json"
    plugin_data = json.loads(plugin_path.read_text(encoding="utf-8"))
    if not isinstance(plugin_data, dict):
        raise VersionTagError("plugin.json must contain an object")
    plugin_data["version"] = f"{target_version}+codex.{cachebuster}"
    plugin_path.write_text(
        json.dumps(plugin_data, ensure_ascii=True, indent=2) + "\n",
        encoding="utf-8",
    )
    validate_versions(root, tag=f"v{target_version}")
    return {
        "version": target_version,
        "tag": f"v{target_version}",
        "already_prepared": False,
    }


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

    prepare = commands.add_parser("prepare", help="Prepare the next patch release in place.")
    prepare.add_argument("--latest-tag", required=True)
    prepare.add_argument("--date", required=True)
    prepare.add_argument("--cachebuster", required=True)

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
        elif args.command == "prepare":
            result = prepare_release(
                root,
                latest_tag=args.latest_tag,
                release_date=args.date,
                cachebuster=args.cachebuster,
            )
        else:
            raise AssertionError("unreachable")
    except VersionTagError as error:
        print(json.dumps({"ok": False, "error": str(error)}, ensure_ascii=False, indent=2))
        return 1
    print(json.dumps({"ok": True, **result}, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
