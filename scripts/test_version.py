#!/usr/bin/env python3
"""Unit tests for scripts/version.py."""

from __future__ import annotations

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import version as vm  # noqa: E402


PLIST_SAMPLE = """<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>Pebble</string>
  <key>CFBundleShortVersionString</key><string>0.0.1</string>
  <key>CFBundleVersion</key><string>0</string>
</dict>
</plist>
"""


class VersionManagerTests(unittest.TestCase):
    def _make_repo_fixture(self, tmp: Path, ver: str = "0.1.0", build: int = 1) -> None:
        (tmp / "version.json").write_text(
            json.dumps({"version": ver, "build": build}, indent=2) + "\n",
            encoding="utf-8",
        )
        (tmp / "internal/protocol").mkdir(parents=True)
        (tmp / "internal/protocol/protocol.go").write_text(
            'package protocol\n\nvar AppVersion = "0.0.0"\n',
            encoding="utf-8",
        )
        (tmp / "web").mkdir(parents=True)
        (tmp / "web/package.json").write_text(
            json.dumps({"name": "bot-hub-web", "version": "0.0.0"}, indent=2) + "\n",
            encoding="utf-8",
        )
        (tmp / "web/package-lock.json").write_text(
            json.dumps(
                {
                    "name": "bot-hub-web",
                    "version": "0.0.0",
                    "packages": {"": {"name": "bot-hub-web", "version": "0.0.0"}},
                },
                indent=2,
            )
            + "\n",
            encoding="utf-8",
        )
        (tmp / "apps/apple/Config").mkdir(parents=True)
        (tmp / "apps/apple/Pebble.xcodeproj").mkdir(parents=True)
        (tmp / "apps/apple/Pebble.xcodeproj/project.pbxproj").write_text(
            "// clean project without hardcoded versions\n",
            encoding="utf-8",
        )
        (tmp / "apps/android").mkdir(parents=True)
        (tmp / "build/darwin").mkdir(parents=True)
        (tmp / "build/darwin/Info.plist").write_text(PLIST_SAMPLE, encoding="utf-8")

    def test_bump_transitions(self) -> None:
        s0 = vm.VersionState("0.1.0", 1, 0, 1, 0, None)
        self.assertEqual(vm.compute_bump(s0, "patch"), ("0.1.1", 2))
        self.assertEqual(vm.compute_bump(s0, "minor"), ("0.2.0", 2))
        self.assertEqual(vm.compute_bump(s0, "major"), ("1.0.0", 2))
        self.assertEqual(vm.compute_bump(s0, "build"), ("0.1.0", 2))
        self.assertEqual(vm.compute_bump(s0, "alpha"), ("0.2.0-alpha.1", 2))
        self.assertEqual(vm.compute_bump(s0, "rc", base_bump="patch"), ("0.1.1-rc.1", 2))
        self.assertEqual(vm.compute_bump(s0, "v0.3.0-beta.2", explicit_build=10), ("0.3.0-beta.2", 10))

        s_alpha = vm.VersionState("0.2.0-alpha.1", 2, 0, 2, 0, "alpha.1")
        self.assertEqual(vm.compute_bump(s_alpha, "alpha"), ("0.2.0-alpha.2", 3))
        self.assertEqual(vm.compute_bump(s_alpha, "beta"), ("0.2.0-beta.1", 3))
        self.assertEqual(vm.compute_bump(s_alpha, "rc"), ("0.2.0-rc.1", 3))

        s_rc = vm.VersionState("0.2.0-rc.2", 4, 0, 2, 0, "rc.2")
        self.assertEqual(vm.compute_bump(s_rc, "patch"), ("0.2.0", 5))
        self.assertEqual(vm.compute_bump(s_rc, "minor"), ("0.2.0", 5))
        with self.assertRaises(ValueError):
            vm.compute_bump(s_rc, "beta")

        s_major_rc = vm.VersionState("1.0.0-rc.1", 8, 1, 0, 0, "rc.1")
        self.assertEqual(vm.compute_bump(s_major_rc, "major"), ("1.0.0", 9))

        with self.assertRaises(ValueError):
            vm.compute_bump(s0, "not-a-version")

    def test_sync_and_check_drift_detection(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            self._make_repo_fixture(root, ver="0.2.0-rc.1", build=7)
            state = vm.load_version(root)
            self.assertEqual(state.base, "0.2.0")
            self.assertTrue(state.is_prerelease)

            changed = vm.sync_all(root, state)
            self.assertGreaterEqual(len(changed), 5)
            self.assertEqual(vm.check_all(root, state, expect_tag="refs/tags/v0.2.0-rc.1"), [])

            # Apple MARKETING_VERSION must use base (without -rc.1)
            xcconfig = (root / "apps/apple/Config/Version.xcconfig").read_text(encoding="utf-8")
            self.assertIn("MARKETING_VERSION = 0.2.0", xcconfig)
            self.assertIn("CURRENT_PROJECT_VERSION = 7", xcconfig)

            # Android keeps full SemVer
            aprops = (root / "apps/android/version.properties").read_text(encoding="utf-8")
            self.assertIn("VERSION_NAME=0.2.0-rc.1", aprops)
            self.assertIn("VERSION_CODE=7", aprops)

            # Drift in pbxproj or protocol.go is caught
            (root / "apps/apple/Pebble.xcodeproj/project.pbxproj").write_text(
                "\t\tMARKETING_VERSION = 1.0;\n",
                encoding="utf-8",
            )
            (root / "internal/protocol/protocol.go").write_text(
                'package protocol\n\nvar AppVersion = "0.0.1"\n',
                encoding="utf-8",
            )
            errs = vm.check_all(root, state)
            self.assertEqual(len(errs), 2)

    def test_changelog_grouping_and_idempotent_write(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            subprocess.run(["git", "init"], cwd=root, check=True, capture_output=True)
            subprocess.run(["git", "config", "user.name", "Tester"], cwd=root, check=True)
            subprocess.run(["git", "config", "user.email", "test@example.com"], cwd=root, check=True)

            def commit(msg: str, filename: str) -> None:
                (root / filename).write_text(msg, encoding="utf-8")
                subprocess.run(["git", "add", filename], cwd=root, check=True, capture_output=True)
                subprocess.run(["git", "commit", "-m", msg], cwd=root, check=True, capture_output=True)

            commit("feat(cli): add version subcommand", "a.txt")
            commit("fix(android): send X-Pebble-Version header", "b.txt")
            commit("feat(api)!: drop legacy v0 endpoint", "c.txt")
            commit("chore(release): v0.1.0", "d.txt")

            notes = vm.render_release_notes(root, version="0.1.0", date_str="2026-11-26")
            self.assertIn("## [v0.1.0] - 2026-11-26", notes)
            self.assertIn("### Breaking Changes", notes)
            self.assertIn("### Features", notes)
            self.assertIn("### Bug Fixes", notes)
            self.assertNotIn("chore(release)", notes)

            cl = root / "CHANGELOG.md"
            vm.update_changelog_file(cl, notes, "0.1.0")
            vm.update_changelog_file(cl, notes, "0.1.0")
            text = cl.read_text(encoding="utf-8")
            self.assertEqual(text.count("## [v0.1.0]"), 1)


if __name__ == "__main__":
    unittest.main()
