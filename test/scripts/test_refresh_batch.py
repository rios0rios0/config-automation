"""Run the workflow's batch shell against local repos and stub only remote CLIs."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import textwrap
import unittest

from test_refresh_scope import SUPPORTED, EXCLUDED


PROJECT = Path(__file__).resolve().parents[2]


class RefreshBatchTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.env = dict(os.environ,
                        GIT_CONFIG_GLOBAL=str(self.root / "gitconfig"),
                        GIT_CONFIG_NOSYSTEM="1", GIT_TERMINAL_PROMPT="0",
                        TMPDIR=str(self.root), GITHUB_WORKSPACE=str(PROJECT),
                        REAL_GIT=shutil.which("git"), FIXTURE_ROOT=str(self.root),
                        PATH=f"{self.bin}{os.pathsep}{os.environ['PATH']}",
                        GH_TOKEN="fixture-token-placeholder", CLAUDE_MODEL="claude-opus-5-5",
                        MAX_TURNS="7", STALE_AFTER_DAYS="0", REPO_INPUT="",
                        REFRESH_BRANCH="chore/config-and-docs-refresh", REFRESH_WRITES="{}")
        self.repos = []
        self.stub("git", '''
            import os, subprocess, sys
            from pathlib import Path
            from urllib.parse import urlparse
            args = sys.argv[1:]
            if (args[:1] == ["-C"] and len(args) > 3
                    and Path(args[1]).name == os.environ.get("FAIL_STAGE_REPO")
                    and args[2:4] == ["add", "--"]):
                sys.exit(22)
            if args[:2] == ["clone", "--quiet"]:
                name = Path(urlparse(args[2]).path).name
                args[2] = str(Path(os.environ["FIXTURE_ROOT"]) / name)
            sys.exit(subprocess.call([os.environ["REAL_GIT"], *args]))
        ''')
        self.stub("gh", '''
            import os, sys
            from pathlib import Path
            if sys.argv[1:3] == ["pr", "create"]:
                with open(Path(os.environ["FIXTURE_ROOT"]) / "prs", "a") as out:
                    out.write("created\\n")
                print("https://github.com/fixture/repository/pull/1")
        ''')
        self.stub("claude", '''
            import json, os, sys
            from pathlib import Path
            root = Path(os.environ["FIXTURE_ROOT"])
            with open(root / "claude-calls", "a") as out:
                out.write(Path.cwd().name + "\\n")
            assert sys.stdin.read() == "", "batch stdin leaked"
            if "--output-format" in sys.argv:
                if Path.cwd().name == os.environ.get("FAIL_AUDIT_REPO"):
                    print("network error")
                    sys.exit(23)
                print(json.dumps({"type": "result", "subtype": "success",
                                  "is_error": False, "result": "Audit completed; proposed corrections."}))
            else:
                for path, content in json.loads(os.environ["REFRESH_WRITES"]).items():
                    target = Path(path)
                    if content is None:
                        target.unlink()
                    else:
                        target.parent.mkdir(parents=True, exist_ok=True)
                        target.write_text(content)
                print("Refresh completed")
        ''')

    def stub(self, name, body):
        path = self.bin / name
        path.write_text("#!/usr/bin/env python3\n" + textwrap.dedent(body))
        path.chmod(0o755)

    def git(self, *args, cwd=None, **env):
        return subprocess.check_output(
            [self.env["REAL_GIT"], *args], cwd=cwd, env=dict(self.env, **env),
            stderr=subprocess.PIPE, text=True,
        ).strip()

    def repository(self, name, files, old=False):
        work = self.root / f"source-{name}"
        work.mkdir()
        self.git("init", "-b", "main", cwd=work)
        self.git("config", "user.name", "fixture", cwd=work)
        self.git("config", "user.email", "fixture@example.invalid", cwd=work)
        for path in files:
            target = work / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text("Original documentation\n")
        (work / "source.txt").write_text("Original implementation\n")
        self.git("add", ".", cwd=work)
        date = {"GIT_AUTHOR_DATE": "2020-01-01T00:00:00Z",
                "GIT_COMMITTER_DATE": "2020-01-01T00:00:00Z"} if old else {}
        self.git("commit", "-m", "Initial fixture", cwd=work, **date)
        remote = self.root / f"{name}.git"
        self.git("clone", "--bare", str(work), str(remote))
        self.repos.append({"owner": "fixture", "name": name, "default_branch": "main"})
        return remote, work

    def run_batch(self, writes=None, **overrides):
        workflow = (PROJECT / ".github/workflows/config-and-docs-refresh.yaml").read_text()
        step = workflow.split("      - name: 'Process batch", 1)[1]
        block = step.split("        run: |\n", 1)[1]
        lines = []
        for line in block.splitlines():
            if line.strip() and not line.startswith("          "):
                break
            lines.append(line[10:] if line.strip() else "")
        result = subprocess.run(
            ["bash", "-c", "\n".join(lines)], cwd=self.root,
            env=dict(self.env, REPOS=json.dumps(self.repos),
                     REFRESH_WRITES=json.dumps(writes or {}), **overrides),
            text=True, capture_output=True, timeout=30, check=False,
        )
        return result

    def test_all_categories_reach_one_pr_and_unrelated_files_are_not_staged(self):
        # CHANGELOG.md is tested separately by the helper's convention tests.
        files = SUPPORTED + tuple(p for p in EXCLUDED if p != "CHANGELOG.md")
        remote, _ = self.repository("complete", files)
        writes = {path: "Updated documentation\n" for path in SUPPORTED}
        writes["source.txt"] = "Unexpected out-of-scope change\n"
        result = self.run_batch(writes)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        changed = self.git("diff", "--name-only", "main", self.env["REFRESH_BRANCH"], cwd=remote)
        self.assertEqual(set(changed.splitlines()), set(SUPPORTED))
        self.assertEqual((self.root / "prs").read_text(), "created\n")

    def test_audit_failure_keeps_processing_the_next_repository(self):
        self.repository("first", ["README.md"])
        remote, _ = self.repository("second", ["README.md"])
        result = self.run_batch({"README.md": "Updated\n"}, FAIL_AUDIT_REPO="fixture__first")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("claude: fixture/first", result.stdout)
        self.assertIn("processed (2/2)", result.stdout)
        self.assertEqual(self.git("show", f"{self.env['REFRESH_BRANCH']}:README.md", cwd=remote), "Updated")

    def test_noop_does_not_create_a_pr(self):
        self.repository("quiet", ["AGENTS.md", "README.md"])
        result = self.run_batch()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("no_drift (1)", result.stdout)
        self.assertFalse((self.root / "prs").exists())

    def test_changelog_only_edit_does_not_create_a_pr(self):
        self.repository("changelog", ["AGENTS.md", "CHANGELOG.md"])
        result = self.run_batch({"CHANGELOG.md": "Stray changelog entry\n"})
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("no_drift (1)", result.stdout)
        self.assertFalse((self.root / "prs").exists())

    def test_whitespace_only_edit_does_not_create_a_pr(self):
        self.repository("whitespace", ["AGENTS.md", "README.md"])
        result = self.run_batch({"README.md": "Original   documentation   \n"})
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("no_drift (1)", result.stdout)
        self.assertFalse((self.root / "prs").exists())

    def test_staging_failure_keeps_processing_the_next_repository(self):
        self.repository("first", ["README.md"])
        remote, _ = self.repository("second", ["README.md"])
        result = self.run_batch({"README.md": "Updated\n"}, FAIL_STAGE_REPO="fixture__first")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("stage: fixture/first", result.stdout)
        self.assertIn("processed (2/2)", result.stdout)
        self.assertEqual(self.git("show", f"{self.env['REFRESH_BRANCH']}:README.md", cwd=remote), "Updated")

    def test_documentation_only_commit_does_not_reactivate_a_quiet_repository(self):
        remote, work = self.repository("quiet", SUPPORTED, old=True)
        for path in SUPPORTED:
            (work / path).write_text("Recent refresh\n")
        self.git("commit", "-am", "Refreshed documentation", cwd=work)
        self.git("push", str(remote), "main", cwd=work)
        result = self.run_batch(STALE_AFTER_DAYS="8")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("unchanged (1)", result.stdout)
        self.assertFalse((self.root / "claude-calls").exists())

    def test_readme_only_repository_still_gets_initial_guidance(self):
        remote, _ = self.repository("bootstrap", ["README.md"], old=True)
        result = self.run_batch({"CLAUDE.md": "New useful guidance\n"}, STALE_AFTER_DAYS="8")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.git("show", f"{self.env['REFRESH_BRANCH']}:CLAUDE.md", cwd=remote),
                         "New useful guidance")

    def test_tracked_deletion_is_detected_and_staged(self):
        remote, _ = self.repository("deletion", ["AGENTS.md", "README.md"])
        result = self.run_batch({"AGENTS.md": None})
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.git("diff", "--name-status", "main", self.env["REFRESH_BRANCH"], cwd=remote),
                         "D\tAGENTS.md")


if __name__ == "__main__":
    unittest.main()
