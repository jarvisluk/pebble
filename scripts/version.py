#!/usr/bin/env python3
"""Single-source-of-truth version manager and Conventional Commits changelog generator."""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import plistlib
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

ROOT_DEFAULT = Path(__file__).resolve().parent.parent

SEMVER_RE = re.compile(
    r"^(?P<major>0|[1-9]\d*)\.(?P<minor>0|[1-9]\d*)\.(?P<patch>0|[1-9]\d*)"
    r"(?:-(?P<pre>[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$"
)
PRERELEASE_CHANNEL_RE = re.compile(r"^(alpha|beta|rc)\.([1-9]\d*|0)$")
CHANNEL_ORDER = {"alpha": 1, "beta": 2, "rc": 3}

CONVENTIONAL_HEADER_RE = re.compile(
    r"^(?P<type>[A-Za-z]+)(?:\((?P<scope>[^)]+)\))?(?P<bang>!)?:\s*(?P<desc>.+)$"
)
RELEASE_COMMIT_RE = re.compile(r"^chore\(release\):\s*v\d+\.\d+\.\d+")

SECTION_ORDER = [
    ("breaking", "Breaking Changes"),
    ("feat", "Features"),
    ("fix", "Bug Fixes"),
    ("perf", "Performance"),
    ("refactor", "Refactoring"),
    ("docs", "Documentation"),
    ("maint", "Maintenance"),
]

TYPE_TO_SECTION = {
    "feat": "feat",
    "fix": "fix",
    "perf": "perf",
    "refactor": "refactor",
    "docs": "docs",
    "chore": "maint",
    "build": "maint",
    "ci": "maint",
    "test": "maint",
    "style": "maint",
    "revert": "fix",
}


@dataclass(frozen=True)
class VersionState:
    version: str
    build: int
    major: int
    minor: int
    patch: int
    prerelease: str | None

    @property
    def base(self) -> str:
        return f"{self.major}.{self.minor}.{self.patch}"

    @property
    def tag(self) -> str:
        return f"v{self.version}"

    @property
    def is_prerelease(self) -> bool:
        return bool(self.prerelease)

    def to_dict(self) -> dict[str, object]:
        return {
            "version": self.version,
            "base": self.base,
            "build": self.build,
            "tag": self.tag,
            "prerelease": self.is_prerelease,
        }


def parse_semver(raw: str) -> tuple[int, int, int, str | None]:
    text = raw.strip()
    if text.startswith("v"):
        text = text[1:]
    m = SEMVER_RE.match(text)
    if not m:
        raise ValueError(f"Invalid SemVer version: {raw!r}")
    return (
        int(m.group("major")),
        int(m.group("minor")),
        int(m.group("patch")),
        m.group("pre"),
    )


def format_semver(major: int, minor: int, patch: int, pre: str | None = None) -> str:
    base = f"{major}.{minor}.{patch}"
    return f"{base}-{pre}" if pre else base


def load_version(root: Path) -> VersionState:
    vfile = root / "version.json"
    if not vfile.is_file():
        raise FileNotFoundError(f"Missing {vfile}")
    data = json.loads(vfile.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        raise ValueError("version.json must be a JSON object")
    raw_ver = data.get("version")
    raw_build = data.get("build")
    if not isinstance(raw_ver, str):
        raise ValueError("version.json 'version' must be a string")
    if isinstance(raw_build, bool) or not isinstance(raw_build, int) or raw_build < 1:
        raise ValueError("version.json 'build' must be an integer >= 1")
    major, minor, patch, pre = parse_semver(raw_ver)
    norm_ver = format_semver(major, minor, patch, pre)
    if norm_ver != raw_ver:
        raise ValueError(f"version.json 'version' must not have leading 'v' or whitespace: {raw_ver!r}")
    return VersionState(
        version=norm_ver,
        build=raw_build,
        major=major,
        minor=minor,
        patch=patch,
        prerelease=pre,
    )


def save_version(root: Path, version: str, build: int) -> VersionState:
    major, minor, patch, pre = parse_semver(version)
    norm = format_semver(major, minor, patch, pre)
    if build < 1:
        raise ValueError("build must be >= 1")
    vfile = root / "version.json"
    payload = {"version": norm, "build": build}
    vfile.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    return VersionState(
        version=norm,
        build=build,
        major=major,
        minor=minor,
        patch=patch,
        prerelease=pre,
    )


def compute_bump(
    state: VersionState,
    target: str,
    base_bump: str = "minor",
    explicit_build: int | None = None,
    no_build_inc: bool = False,
) -> tuple[str, int]:
    if explicit_build is not None:
        if explicit_build < 1:
            raise ValueError("--build must be >= 1")
        next_build = explicit_build
    elif no_build_inc:
        next_build = state.build
    else:
        next_build = state.build + 1

    major, minor, patch, pre = state.major, state.minor, state.patch, state.prerelease

    if target == "build":
        if explicit_build is None and no_build_inc:
            raise ValueError("Cannot combine 'build' bump with --no-build-inc")
        return state.version, next_build

    if target == "patch":
        if pre:
            return format_semver(major, minor, patch), next_build
        return format_semver(major, minor, patch + 1), next_build

    if target == "minor":
        if pre and patch == 0:
            return format_semver(major, minor, 0), next_build
        return format_semver(major, minor + 1, 0), next_build

    if target == "major":
        if pre and minor == 0 and patch == 0:
            return format_semver(major, 0, 0), next_build
        return format_semver(major + 1, 0, 0), next_build

    if target in CHANNEL_ORDER:
        if pre:
            m = PRERELEASE_CHANNEL_RE.match(pre)
            if m:
                cur_chan, cur_num = m.group(1), int(m.group(2))
                if cur_chan == target:
                    return format_semver(major, minor, patch, f"{target}.{cur_num + 1}"), next_build
                if CHANNEL_ORDER[target] > CHANNEL_ORDER[cur_chan]:
                    return format_semver(major, minor, patch, f"{target}.1"), next_build
                raise ValueError(
                    f"Cannot move backwards from prerelease '{pre}' to channel '{target}'"
                )
            return format_semver(major, minor, patch, f"{target}.1"), next_build

        # Currently stable -> bump base according to --base, then start <target>.1
        if base_bump == "patch":
            return format_semver(major, minor, patch + 1, f"{target}.1"), next_build
        if base_bump == "minor":
            return format_semver(major, minor + 1, 0, f"{target}.1"), next_build
        if base_bump == "major":
            return format_semver(major + 1, 0, 0, f"{target}.1"), next_build
        raise ValueError(f"Unsupported --base {base_bump!r}")

    # Explicit SemVer string
    emajor, eminor, epatch, epre = parse_semver(target)
    return format_semver(emajor, eminor, epatch, epre), next_build


def _write_if_changed(path: Path, new_text: str) -> bool:
    old_text = path.read_text(encoding="utf-8") if path.exists() else None
    if old_text == new_text:
        return False
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(new_text, encoding="utf-8")
    return True


def _render_apple_xcconfig(state: VersionState) -> str:
    return (
        "// Managed by scripts/version.py from root version.json. Do not edit manually.\n"
        f"MARKETING_VERSION = {state.base}\n"
        f"CURRENT_PROJECT_VERSION = {state.build}\n"
    )


def _render_android_properties(state: VersionState) -> str:
    return (
        "# Managed by scripts/version.py from root version.json. Do not edit manually.\n"
        f"VERSION_NAME={state.version}\n"
        f"VERSION_CODE={state.build}\n"
    )


def _update_plist_text(text: str, short_ver: str, bundle_ver: str) -> str:
    short_re = re.compile(
        r"(<key>CFBundleShortVersionString</key>\s*<string>)([^<]*)(</string>)"
    )
    build_re = re.compile(
        r"(<key>CFBundleVersion</key>\s*<string>)([^<]*)(</string>)"
    )
    if not short_re.search(text):
        raise ValueError("CFBundleShortVersionString not found in Info.plist")
    if not build_re.search(text):
        raise ValueError("CFBundleVersion not found in Info.plist")
    out = short_re.sub(rf"\g<1>{short_ver}\g<3>", text, count=1)
    out = build_re.sub(rf"\g<1>{bundle_ver}\g<3>", out, count=1)
    return out


def sync_all(root: Path, state: VersionState) -> list[str]:
    changed: list[str] = []

    # 1. internal/protocol/protocol.go
    proto_path = root / "internal/protocol/protocol.go"
    if proto_path.is_file():
        proto_text = proto_path.read_text(encoding="utf-8")
        app_ver_re = re.compile(r'^(var AppVersion = ")([^"]*)(")$', re.MULTILINE)
        if not app_ver_re.search(proto_text):
            raise ValueError(f"Could not locate 'var AppVersion = ...' in {proto_path}")
        new_proto = app_ver_re.sub(rf'\g<1>{state.version}\g<3>', proto_text, count=1)
        if _write_if_changed(proto_path, new_proto):
            changed.append("internal/protocol/protocol.go")

    # 2. web/package.json
    pkg_path = root / "web/package.json"
    if pkg_path.is_file():
        pkg_data = json.loads(pkg_path.read_text(encoding="utf-8"))
        if pkg_data.get("version") != state.version:
            pkg_data["version"] = state.version
            if _write_if_changed(pkg_path, json.dumps(pkg_data, indent=2, ensure_ascii=False) + "\n"):
                changed.append("web/package.json")

    # 3. web/package-lock.json
    lock_path = root / "web/package-lock.json"
    if lock_path.is_file():
        lock_data = json.loads(lock_path.read_text(encoding="utf-8"))
        dirty = False
        if lock_data.get("version") != state.version:
            lock_data["version"] = state.version
            dirty = True
        root_pkg = lock_data.get("packages", {}).get("")
        if isinstance(root_pkg, dict) and root_pkg.get("version") != state.version:
            root_pkg["version"] = state.version
            dirty = True
        if dirty and _write_if_changed(
            lock_path, json.dumps(lock_data, indent=2, ensure_ascii=False) + "\n"
        ):
            changed.append("web/package-lock.json")

    # 4. apps/apple/Config/Version.xcconfig
    if (root / "apps/apple").is_dir():
        xcconfig_path = root / "apps/apple/Config/Version.xcconfig"
        if _write_if_changed(xcconfig_path, _render_apple_xcconfig(state)):
            changed.append("apps/apple/Config/Version.xcconfig")

    # 5. apps/android/version.properties
    if (root / "apps/android").is_dir():
        props_path = root / "apps/android/version.properties"
        if _write_if_changed(props_path, _render_android_properties(state)):
            changed.append("apps/android/version.properties")

    # 6. build/darwin/Info.plist
    plist_path = root / "build/darwin/Info.plist"
    if plist_path.is_file():
        plist_text = plist_path.read_text(encoding="utf-8")
        new_plist = _update_plist_text(plist_text, state.base, str(state.build))
        if _write_if_changed(plist_path, new_plist):
            changed.append("build/darwin/Info.plist")

    return changed


def check_all(root: Path, state: VersionState, expect_tag: str | None = None) -> list[str]:
    errors: list[str] = []

    if expect_tag:
        norm_tag = expect_tag.strip()
        if norm_tag.startswith("refs/tags/"):
            norm_tag = norm_tag[len("refs/tags/") :]
        if norm_tag != state.tag:
            errors.append(f"tag mismatch: expected {norm_tag!r}, version.json has {state.tag!r}")

    # 1. internal/protocol/protocol.go
    proto_path = root / "internal/protocol/protocol.go"
    if proto_path.is_file():
        m = re.search(r'^var AppVersion = "([^"]*)"$', proto_path.read_text(encoding="utf-8"), re.MULTILINE)
        if not m or m.group(1) != state.version:
            got = m.group(1) if m else "<missing>"
            errors.append(f"internal/protocol/protocol.go: AppVersion={got!r}, want {state.version!r}")

    # 2. web/package.json
    pkg_path = root / "web/package.json"
    if pkg_path.is_file():
        pkg_data = json.loads(pkg_path.read_text(encoding="utf-8"))
        if pkg_data.get("version") != state.version:
            errors.append(f"web/package.json: version={pkg_data.get('version')!r}, want {state.version!r}")

    # 3. web/package-lock.json
    lock_path = root / "web/package-lock.json"
    if lock_path.is_file():
        lock_data = json.loads(lock_path.read_text(encoding="utf-8"))
        if lock_data.get("version") != state.version:
            errors.append(
                f"web/package-lock.json: version={lock_data.get('version')!r}, want {state.version!r}"
            )
        root_pkg_ver = lock_data.get("packages", {}).get("", {}).get("version")
        if root_pkg_ver != state.version:
            errors.append(
                f"web/package-lock.json: packages[''].version={root_pkg_ver!r}, want {state.version!r}"
            )

    # 4. apps/apple/Config/Version.xcconfig & project.pbxproj
    if (root / "apps/apple").is_dir():
        xcconfig_path = root / "apps/apple/Config/Version.xcconfig"
        if not xcconfig_path.is_file():
            errors.append("apps/apple/Config/Version.xcconfig: missing")
        else:
            xtxt = xcconfig_path.read_text(encoding="utf-8")
            m_ver = re.search(r"^MARKETING_VERSION\s*=\s*(.+)$", xtxt, re.MULTILINE)
            m_bld = re.search(r"^CURRENT_PROJECT_VERSION\s*=\s*(.+)$", xtxt, re.MULTILINE)
            got_ver = m_ver.group(1).strip() if m_ver else None
            got_bld = m_bld.group(1).strip() if m_bld else None
            if got_ver != state.base:
                errors.append(
                    f"apps/apple/Config/Version.xcconfig: MARKETING_VERSION={got_ver!r}, want {state.base!r}"
                )
            if got_bld != str(state.build):
                errors.append(
                    f"apps/apple/Config/Version.xcconfig: CURRENT_PROJECT_VERSION={got_bld!r}, want {str(state.build)!r}"
                )

        pbx_path = root / "apps/apple/Pebble.xcodeproj/project.pbxproj"
        if pbx_path.is_file():
            pbx_txt = pbx_path.read_text(encoding="utf-8")
            if re.search(r"^\s*(MARKETING_VERSION|CURRENT_PROJECT_VERSION)\s*=", pbx_txt, re.MULTILINE):
                errors.append(
                    "apps/apple/Pebble.xcodeproj/project.pbxproj: contains hardcoded MARKETING_VERSION or CURRENT_PROJECT_VERSION overrides"
                )

    # 5. apps/android/version.properties
    if (root / "apps/android").is_dir():
        props_path = root / "apps/android/version.properties"
        if not props_path.is_file():
            errors.append("apps/android/version.properties: missing")
        else:
            ptxt = props_path.read_text(encoding="utf-8")
            m_name = re.search(r"^VERSION_NAME=(.+)$", ptxt, re.MULTILINE)
            m_code = re.search(r"^VERSION_CODE=(.+)$", ptxt, re.MULTILINE)
            got_name = m_name.group(1).strip() if m_name else None
            got_code = m_code.group(1).strip() if m_code else None
            if got_name != state.version:
                errors.append(
                    f"apps/android/version.properties: VERSION_NAME={got_name!r}, want {state.version!r}"
                )
            if got_code != str(state.build):
                errors.append(
                    f"apps/android/version.properties: VERSION_CODE={got_code!r}, want {str(state.build)!r}"
                )

    # 6. build/darwin/Info.plist
    plist_path = root / "build/darwin/Info.plist"
    if plist_path.is_file():
        with plist_path.open("rb") as fh:
            plist_data = plistlib.load(fh)
        if plist_data.get("CFBundleShortVersionString") != state.base:
            errors.append(
                f"build/darwin/Info.plist: CFBundleShortVersionString={plist_data.get('CFBundleShortVersionString')!r}, want {state.base!r}"
            )
        if plist_data.get("CFBundleVersion") != str(state.build):
            errors.append(
                f"build/darwin/Info.plist: CFBundleVersion={plist_data.get('CFBundleVersion')!r}, want {str(state.build)!r}"
            )

    return errors


def _git(root: Path, *args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["git", *args],
        cwd=str(root),
        text=True,
        capture_output=True,
        check=False,
    )


def detect_previous_tag(root: Path, to_ref: str) -> str | None:
    res = _git(root, "describe", "--tags", "--abbrev=0", "--match", "v*", to_ref)
    if res.returncode != 0:
        return None
    tag = res.stdout.strip()
    if not tag:
        return None
    to_sha = _git(root, "rev-parse", "--verify", f"{to_ref}^{{commit}}")
    tag_sha = _git(root, "rev-parse", "--verify", f"{tag}^{{commit}}")
    if to_sha.returncode == 0 and tag_sha.returncode == 0 and to_sha.stdout.strip() == tag_sha.stdout.strip():
        parent = _git(root, "rev-parse", "--verify", f"{to_ref}^")
        if parent.returncode != 0:
            return None
        prev_res = _git(root, "describe", "--tags", "--abbrev=0", "--match", "v*", f"{to_ref}^")
        if prev_res.returncode != 0:
            return None
        prev_tag = prev_res.stdout.strip()
        return prev_tag or None
    return tag


def parse_commits(raw_log: str) -> dict[str, list[str]]:
    buckets: dict[str, list[str]] = {k: [] for k, _ in SECTION_ORDER}
    if not raw_log.strip():
        return buckets

    records = raw_log.split("\x1e")
    for rec in records:
        rec = rec.strip()
        if not rec:
            continue
        parts = rec.split("\x1f", 2)
        if len(parts) < 2:
            continue
        sha = parts[0].strip()
        subject = parts[1].strip()
        body = parts[2].strip() if len(parts) > 2 else ""
        if not subject or RELEASE_COMMIT_RE.match(subject):
            continue

        m = CONVENTIONAL_HEADER_RE.match(subject)
        breaking = False
        if m:
            ctype = m.group("type").lower()
            scope = m.group("scope")
            bang = bool(m.group("bang"))
            desc = m.group("desc").strip()
            line = f"- **{scope}**: {desc} (`{sha}`)" if scope else f"- {desc} (`{sha}`)"
            if bang or "BREAKING CHANGE:" in body or "BREAKING-CHANGE:" in body:
                breaking = True
            section = TYPE_TO_SECTION.get(ctype, "maint")
        else:
            line = f"- {subject} (`{sha}`)"
            if "BREAKING CHANGE:" in body or "BREAKING-CHANGE:" in body:
                breaking = True
            section = "maint"

        if breaking:
            buckets["breaking"].append(line)
        else:
            buckets[section].append(line)
    return buckets


def render_release_notes(
    root: Path,
    version: str,
    from_ref: str | None = None,
    to_ref: str = "HEAD",
    date_str: str | None = None,
    include_header: bool = True,
) -> str:
    if from_ref is None:
        from_ref = detect_previous_tag(root, to_ref)
    rev_range = f"{from_ref}..{to_ref}" if from_ref else to_ref
    res = _git(
        root,
        "log",
        "--no-merges",
        "--format=%h%x1f%s%x1f%b%x1e",
        rev_range,
    )
    if res.returncode != 0:
        raise RuntimeError(f"git log failed for {rev_range}: {res.stderr.strip()}")

    buckets = parse_commits(res.stdout)
    sections_md: list[str] = []
    for key, title in SECTION_ORDER:
        items = buckets[key]
        if items:
            sections_md.append(f"### {title}\n" + "\n".join(items))

    if not sections_md:
        sections_md.append("### Maintenance\n- Release maintenance and version bump.")

    body = "\n\n".join(sections_md)
    if not include_header:
        return body + "\n"

    ver_tag = version if version.startswith("v") else f"v{version}"
    date_val = date_str or dt.date.today().isoformat()
    return f"## [{ver_tag}] - {date_val}\n\n{body}\n"


def update_changelog_file(changelog_path: Path, section_md: str, version: str) -> None:
    ver_tag = version if version.startswith("v") else f"v{version}"
    if not changelog_path.is_file():
        initial = (
            "# Changelog\n\n"
            "All notable changes to Pebble will be documented in this file.\n\n"
            "The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),\n"
            "and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).\n\n"
            "## [Unreleased]\n"
        )
        changelog_path.write_text(initial, encoding="utf-8")

    content = changelog_path.read_text(encoding="utf-8")
    # Remove existing section for the same version for idempotency
    existing_re = re.compile(
        rf"^## \[{re.escape(ver_tag)}\][^\n]*\n(?:(?!^## \[).*\n?)*",
        re.MULTILINE,
    )
    content = existing_re.sub("", content)

    marker = "## [Unreleased]"
    idx = content.find(marker)
    if idx == -1:
        updated = content.rstrip() + "\n\n" + section_md.rstrip() + "\n"
    else:
        line_end = content.find("\n", idx)
        if line_end == -1:
            line_end = len(content)
            prefix = content + "\n"
            suffix = ""
        else:
            prefix = content[: line_end + 1]
            suffix = content[line_end + 1 :].lstrip("\n")
        if suffix:
            updated = prefix.rstrip() + "\n\n" + section_md.rstrip() + "\n\n" + suffix
        else:
            updated = prefix.rstrip() + "\n\n" + section_md.rstrip() + "\n"

    changelog_path.write_text(updated, encoding="utf-8")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Pebble version & changelog tool")
    parser.add_argument("--root", type=Path, default=ROOT_DEFAULT, help="Repository root path")
    sub = parser.add_subparsers(dest="cmd", required=True)

    p_show = sub.add_parser("show", help="Show current version metadata")
    p_show.add_argument("--json", action="store_true", help="Output JSON")
    p_show.add_argument(
        "--field",
        choices=["version", "base", "build", "tag", "prerelease"],
        help="Print a single field",
    )

    sub.add_parser("sync", help="Sync version.json to all project files")

    p_check = sub.add_parser("check", help="Verify all project files match version.json")
    p_check.add_argument("--expect-tag", help="Verify version.json tag matches this tag")

    p_bump = sub.add_parser("bump", help="Bump version.json and sync all files")
    p_bump.add_argument(
        "target",
        help="patch | minor | major | alpha | beta | rc | build | explicit SemVer",
    )
    p_bump.add_argument(
        "--base",
        choices=["patch", "minor", "major"],
        default="minor",
        help="Base bump when starting a prerelease from a stable version (default: minor)",
    )
    p_bump.add_argument("--build", type=int, help="Explicit build number (>= 1)")
    p_bump.add_argument("--no-build-inc", action="store_true", help="Do not increment build number")
    p_bump.add_argument("--dry-run", action="store_true", help="Print result without modifying files")

    p_log = sub.add_parser("changelog", help="Generate Conventional Commits release notes")
    p_log.add_argument("--from", dest="from_ref", help="Start git ref (exclusive)")
    p_log.add_argument("--to", dest="to_ref", default="HEAD", help="End git ref (inclusive)")
    p_log.add_argument("--version", help="Version header to render (defaults to version.json)")
    p_log.add_argument("--date", help="Release date YYYY-MM-DD (defaults to today)")
    p_log.add_argument("--write", type=Path, help="Update CHANGELOG.md file in place")
    p_log.add_argument("--no-header", action="store_true", help="Omit '## [vX.Y.Z] - YYYY-MM-DD' header")

    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    root: Path = args.root.resolve()

    try:
        if args.cmd == "show":
            state = load_version(root)
            if args.field:
                val = state.to_dict()[args.field]
                if isinstance(val, bool):
                    print("true" if val else "false")
                else:
                    print(val)
            elif args.json:
                print(json.dumps(state.to_dict(), indent=2))
            else:
                print(f"{state.version} (build {state.build}, tag {state.tag})")
            return 0

        if args.cmd == "sync":
            state = load_version(root)
            changed = sync_all(root, state)
            if changed:
                print(f"Synced {state.version} (build {state.build}) to: {', '.join(changed)}")
            else:
                print(f"All targets already match {state.version} (build {state.build})")
            return 0

        if args.cmd == "check":
            state = load_version(root)
            errors = check_all(root, state, expect_tag=args.expect_tag)
            if errors:
                for err in errors:
                    print(f"ERROR: {err}", file=sys.stderr)
                return 1
            print(f"OK: all targets match {state.version} (build {state.build}, tag {state.tag})")
            return 0

        if args.cmd == "bump":
            state = load_version(root)
            next_ver, next_build = compute_bump(
                state,
                args.target,
                base_bump=args.base,
                explicit_build=args.build,
                no_build_inc=args.no_build_inc,
            )
            if args.dry_run:
                print(
                    f"[dry-run] {state.version} (build {state.build}) -> {next_ver} (build {next_build})"
                )
                return 0
            new_state = save_version(root, next_ver, next_build)
            changed = sync_all(root, new_state)
            print(
                f"Bumped {state.version} (build {state.build}) -> {new_state.version} (build {new_state.build})"
            )
            if changed:
                print(f"Updated: {', '.join(changed)}")
            return 0

        if args.cmd == "changelog":
            ver = args.version or load_version(root).version
            if args.write:
                section = render_release_notes(
                    root,
                    version=ver,
                    from_ref=args.from_ref,
                    to_ref=args.to_ref,
                    date_str=args.date,
                    include_header=True,
                )
                target_path = args.write if args.write.is_absolute() else (root / args.write)
                update_changelog_file(target_path, section, ver)
                print(f"Updated {target_path} for {ver}")
            else:
                notes = render_release_notes(
                    root,
                    version=ver,
                    from_ref=args.from_ref,
                    to_ref=args.to_ref,
                    date_str=args.date,
                    include_header=not args.no_header,
                )
                sys.stdout.write(notes)
            return 0
    except Exception as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1

    return 1


if __name__ == "__main__":
    raise SystemExit(main())
