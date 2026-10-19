"""Normalize AGENTS.md/CLAUDE.md pairs in real Git repositories."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parents[2] / "scripts/agents_layout.py"
LEGACY = ("# CLAUDE.md\n\nThis file provides guidance to Claude Code (claude.ai/code) "
          "when working with code in this repository.\n")
POINTER = "@AGENTS.md\n"


class AgentsLayoutTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "repository"
        config = Path(self.temp.name) / "gitconfig"
        config.write_text("[user]\n\tname = fixture\n\temail = fixture@example.invalid\n")
        self.env = dict(os.environ, GIT_CONFIG_GLOBAL=str(config), GIT_CONFIG_NOSYSTEM="1")
        self.git("init", "--quiet", "-b", "main", str(self.root), cwd=self.temp.name)

    def git(self, *args, cwd=None):
        return subprocess.run(["git", *args], cwd=cwd or self.root, env=self.env, check=True,
                              capture_output=True, text=True).stdout.strip()

    def commit(self, files, links=None):
        for path, content in files.items():
            target = self.root / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(content)
        for path, destination in (links or {}).items():
            (self.root / path).parent.mkdir(parents=True, exist_ok=True)
            (self.root / path).symlink_to(destination)
        self.git("add", "--force", "--", ".")
        self.git("commit", "--quiet", "-m", "Initial fixture")

    def normalize(self):
        result = subprocess.run(["python3", str(SCRIPT), str(self.root)], env=self.env,
                                check=True, capture_output=True, text=True)
        return json.loads(result.stdout)

    def read(self, path):
        return (self.root / path).read_text()

    def test_should_rename_legacy_claude_md_in_its_own_commit_when_agents_md_is_absent(self):
        # given
        self.commit({"CLAUDE.md": LEGACY + "\n## Build\n\nmake test\n"})

        # when
        report = self.normalize()

        # then
        self.assertEqual(report["renamed"], ["CLAUDE.md"])
        self.assertTrue(report["changed"])
        self.assertEqual(self.git("log", "-1", "--format=%s"),
                         "chore(refresh): renamed `CLAUDE.md` to `AGENTS.md`")
        self.assertEqual(self.git("log", "--follow", "--format=%s", "--", "AGENTS.md").splitlines(),
                         ["chore(refresh): renamed `CLAUDE.md` to `AGENTS.md`", "Initial fixture"])
        self.assertTrue(self.read("AGENTS.md").startswith("# AGENTS.md\n\nThis file provides guidance to AI coding agents"))
        self.assertTrue(self.read("AGENTS.md").endswith("## Build\n\nmake test\n"))
        self.assertEqual(self.read("CLAUDE.md"), POINTER)
        self.assertEqual(self.git("diff", "--cached", "--name-status").splitlines(),
                         ["M\tAGENTS.md", "A\tCLAUDE.md"])

    def test_should_merge_only_the_claude_specific_text_when_both_files_exist(self):
        # given
        self.commit({"packages/api/AGENTS.md": "# API\n\nShared facts.\n",
                     "packages/api/CLAUDE.md": "@AGENTS.md\n\nUse the release skill.\n"})

        # when
        report = self.normalize()

        # then
        self.assertEqual(report["merged"], ["packages/api/CLAUDE.md"])
        self.assertEqual(self.read("packages/api/AGENTS.md"),
                         "# API\n\nShared facts.\n\n## Migrated from CLAUDE.md\n\nUse the release skill.\n")
        self.assertEqual(self.read("packages/api/CLAUDE.md"), POINTER)
        self.assertEqual(self.git("log", "--format=%s"), "Initial fixture")

    def test_should_not_append_a_copy_when_claude_md_repeats_agents_md(self):
        # given
        self.commit({"AGENTS.md": "# Guidance\n\nRun make test.\n", "CLAUDE.md": "# Guidance\n\nRun make test.\n"})

        # when
        self.normalize()

        # then
        self.assertEqual(self.read("AGENTS.md"), "# Guidance\n\nRun make test.\n")
        self.assertEqual(self.read("CLAUDE.md"), POINTER)

    def test_should_add_the_import_when_only_agents_md_exists(self):
        # given
        self.commit({"AGENTS.md": "# Guidance\n", "tools/AGENTS.md": "# Tools\n"})

        # when
        report = self.normalize()

        # then
        self.assertEqual(report["pointed"], ["CLAUDE.md", "tools/CLAUDE.md"])
        self.assertEqual(self.read("CLAUDE.md"), POINTER)
        self.assertEqual(self.git("diff", "--cached", "--name-only").splitlines(),
                         ["CLAUDE.md", "tools/CLAUDE.md"])

    def test_should_replace_sibling_symlinks_with_a_regular_import(self):
        # given
        self.commit({"AGENTS.md": "# Linked\n", "legacy/CLAUDE.md": "# Legacy\n"},
                    links={"CLAUDE.md": "AGENTS.md", "legacy/AGENTS.md": "CLAUDE.md"})

        # when
        report = self.normalize()

        # then
        self.assertEqual(report["pointed"], ["CLAUDE.md"])
        self.assertEqual(report["renamed"], ["legacy/CLAUDE.md"])
        for path in ("CLAUDE.md", "legacy/CLAUDE.md", "legacy/AGENTS.md"):
            self.assertFalse((self.root / path).is_symlink(), path)
        self.assertEqual(self.read("CLAUDE.md"), POINTER)
        self.assertEqual(self.read("legacy/AGENTS.md"), "# Legacy\n")
        self.assertEqual(self.read("legacy/CLAUDE.md"), POINTER)

    def test_should_change_nothing_when_the_layout_already_complies(self):
        # given
        self.commit({"CLAUDE.md": LEGACY + "\nGuidance.\n"})
        self.normalize()
        commits = self.git("rev-list", "--count", "HEAD")

        # when
        report = self.normalize()

        # then
        self.assertFalse(report["changed"])
        self.assertEqual(self.git("rev-list", "--count", "HEAD"), commits)
        self.assertIn("No changes", report["note"])

    def test_should_report_a_pointer_whose_agents_md_is_missing(self):
        # given
        self.commit({"tools/CLAUDE.md": POINTER})

        # when
        report = self.normalize()

        # then
        self.assertEqual(report["dangling"], ["tools/CLAUDE.md"])
        self.assertFalse(report["changed"])
        self.assertIn("create it if that directory needs guidance", report["note"])

    def test_should_skip_pairs_it_cannot_migrate_safely(self):
        # given
        self.commit({"README.md": "# Read me\n", ".gitignore": "AGENTS.md\n",
                     "ignored/CLAUDE.md": "# Ignored destination\n"},
                    links={"CLAUDE.md": "README.md"})

        # when
        report = self.normalize()

        # then
        self.assertEqual(report["skipped"], ["CLAUDE.md", "ignored/CLAUDE.md"])
        self.assertFalse(report["changed"])
        self.assertTrue((self.root / "CLAUDE.md").is_symlink())
        self.assertEqual(self.git("status", "--porcelain"), "")

    def test_should_ignore_claude_directory_and_dependency_paths(self):
        # given
        files = {".claude/CLAUDE.md": "# Claude only\n", "node_modules/package/CLAUDE.md": "# Vendored\n",
                 "vendor/package/AGENTS.md": "# Vendored\n"}
        self.commit(files)

        # when
        report = self.normalize()

        # then
        self.assertFalse(report["changed"])
        for path, content in files.items():
            self.assertEqual(self.read(path), content)


if __name__ == "__main__":
    unittest.main()
