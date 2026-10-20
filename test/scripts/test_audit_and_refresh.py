"""Exercise the real shell helper with a deterministic Claude CLI double."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parents[2] / "scripts/audit-and-refresh.sh"
CLAUDE_DOUBLE = '''#!/usr/bin/env python3
import json
import os
import sys

args = sys.argv[1:]
with open(os.environ["CALLS"], "a") as calls:
    calls.write(json.dumps({"args": args, "stdin": sys.stdin.read()}) + "\\n")
if "--output-format" in args:
    sys.stdout.write(os.environ["AUDIT_RESULT"])
    sys.exit(int(os.environ.get("AUDIT_EXIT", "0")))
print("Refresh completed")
sys.exit(int(os.environ.get("REFRESH_EXIT", "0")))
'''


class AuditAndRefreshTest(unittest.TestCase):
    def run_helper(self, audit_result=None, changelog_tools="", **overrides):
        if audit_result is None:
            audit_result = json.dumps({
                "type": "result", "subtype": "success", "is_error": False,
                "result": "AGENTS.md:4: stale command; proposed diff: replace it",
            })
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary = root / "claude"
            binary.write_text(CLAUDE_DOUBLE)
            binary.chmod(0o755)
            scope = root / "scope.json"
            scope.write_text(json.dumps({
                "existing": ["README.md", "packages/api/AGENTS.md", ".claude/skills/check/references/usage.md"],
                "creatable": ["AGENTS.md"],
                "managed": ["CLAUDE.md", "packages/api/CLAUDE.md"],
                "allowed_tools": ["Read", "Grep", "Glob", "Edit(/AGENTS.md)",
                                  "Edit(/README.md)", "Edit(/packages/api/AGENTS.md)",
                                  "Edit(/.claude/skills/check/references/usage.md)"],
            }))
            calls = root / "calls.jsonl"
            scratch = root / "scratch"
            scratch.mkdir()
            # An effort set in the caller's shell must not leak into the defaults.
            env = {key: value for key, value in os.environ.items() if key != "CLAUDE_EFFORT"}
            env.update(PATH=f"{root}{os.pathsep}{os.environ['PATH']}",
                       TMPDIR=str(scratch), CALLS=str(calls),
                       AUDIT_RESULT=audit_result, **overrides)
            result = subprocess.run(
                ["bash", str(SCRIPT), "Refresh only supported guidance.",
                 "claude-opus-5-5", "7", changelog_tools, str(scope)],
                cwd=root, env=env, input="next repository\n", text=True,
                capture_output=True, check=False,
            )
            invocations = [json.loads(line) for line in calls.read_text().splitlines()]
            self.assertEqual(list(scratch.iterdir()), [], "audit temp file leaked")
            return result, invocations

    def test_success_runs_read_only_audit_then_scoped_refresh(self):
        result, calls = self.run_helper()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(calls), 2)
        audit, refresh = [call["args"] for call in calls]
        self.assertTrue(audit[1].startswith("/checkup prompt-audit .\n"))
        self.assertIn("only the import line `@AGENTS.md`", audit[1])
        self.assertIn("under 200 lines", audit[1])
        self.assertEqual(audit[audit.index("--tools") + 1], "Read,Grep,Glob")
        self.assertEqual(audit[audit.index("--allowedTools") + 1], "Read,Grep,Glob")
        self.assertEqual(refresh[refresh.index("--tools") + 1], "Read,Grep,Glob,Edit,Write")
        self.assertIn("AGENTS.md:4: stale command", refresh[1])
        self.assertIn("review data", refresh[1])
        self.assertIn("## Allowed files for this repository", refresh[1])
        self.assertIn("packages/api/AGENTS.md", refresh[1])
        self.assertNotIn("packages/api/CLAUDE.md", refresh[1], "host-managed pointer offered for edits")
        for call in calls:
            args = call["args"]
            self.assertEqual(args[args.index("--model") + 1], "claude-opus-5-5")
            self.assertEqual(args[args.index("--effort") + 1], "high")
            self.assertEqual(args[args.index("--max-turns") + 1], "7")
            # Neither phase may run the target repository's hooks or MCP servers.
            self.assertEqual(args[args.index("--setting-sources") + 1], "")
            self.assertIn("--strict-mcp-config", args)
            self.assertEqual(args[args.index("--permission-mode") + 1], "dontAsk")
            self.assertEqual(call["stdin"], "", "CLI drained the batch's stdin")

    def test_should_run_both_phases_at_the_configured_effort_when_it_is_overridden(self):
        # given
        effort = {"CLAUDE_EFFORT": "xhigh"}

        # when
        result, calls = self.run_helper(**effort)

        # then
        self.assertEqual(result.returncode, 0, result.stderr)
        for call in calls:
            self.assertEqual(call["args"][call["args"].index("--effort") + 1], "xhigh")

    def test_changelog_grants_remain_mutually_exclusive(self):
        for extra in ("", ",Bash(chlog new:*)", ",Edit(/CHANGELOG.md)"):
            with self.subTest(extra=extra):
                result, calls = self.run_helper(changelog_tools=extra)
                self.assertEqual(result.returncode, 0, result.stderr)
                args = calls[1]["args"]
                self.assertEqual(args[args.index("--allowedTools") + 1],
                                 "Read,Grep,Glob,Edit(/AGENTS.md),Edit(/README.md),"
                                 "Edit(/packages/api/AGENTS.md),"
                                 "Edit(/.claude/skills/check/references/usage.md)" + extra)
                # Bash exists only where `chlog new` is the granted command.
                self.assertEqual(args[args.index("--tools") + 1].endswith(",Bash"), "Bash(" in extra)

    def test_failed_audit_preserves_diagnostics_and_does_not_refresh(self):
        for diagnostic in ("monthly usage limit", "safeguards flagged", "network error"):
            with self.subTest(diagnostic=diagnostic):
                result, calls = self.run_helper(diagnostic, AUDIT_EXIT="23")
                self.assertEqual(result.returncode, 23)
                self.assertEqual(len(calls), 1)
                self.assertIn(diagnostic, result.stdout)

    def test_invalid_or_unsuccessful_audit_does_not_refresh(self):
        for output in ("", "not json", "{}", "null", "[]",
                       '{"type":"result","subtype":"error_max_turns",'
                       '"is_error":true,"result":"Reached max turns"}',
                       '{"type":"result","subtype":"success",'
                       '"is_error":true,"result":"monthly usage limit"}',
                       '{"type":"result","subtype":"success",'
                       '"is_error":false,"result":"   "}'):
            with self.subTest(output=output):
                result, calls = self.run_helper(output)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(len(calls), 1)
                self.assertIn("did not return a successful", result.stderr)

    def test_clean_audit_still_allows_existing_factual_refresh(self):
        report = json.dumps({"type": "result", "subtype": "success",
                             "is_error": False, "result": "No findings. Proposed diff: empty."})
        result, calls = self.run_helper(report)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(calls), 2)

    def test_refresh_failure_propagates_to_batch_handler(self):
        result, calls = self.run_helper(REFRESH_EXIT="19")
        self.assertEqual(result.returncode, 19)
        self.assertEqual(len(calls), 2)


if __name__ == "__main__":
    unittest.main()
