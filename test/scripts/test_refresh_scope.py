"""Discover scope from real Git indexes, including nested and excluded files."""

import importlib.util
from pathlib import Path
import subprocess
import tempfile
import unittest


SOURCE = Path(__file__).resolve().parents[2] / "scripts/refresh_scope.py"
SPEC = importlib.util.spec_from_file_location("refresh_scope", SOURCE)
SCOPE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SCOPE)

SUPPORTED = (
    "README.md", "CONTRIBUTING.md", ".github/CONTRIBUTING.md", "docs/CONTRIBUTING.md",
    "AGENTS.md", "CLAUDE.md", "packages/api/AGENTS.md", "packages/api/CLAUDE.md",
    ".claude/CLAUDE.md", ".claude/rules/go/testing.md",
    ".github/instructions/go.instructions.md", ".github/copilot-instructions.md",
    ".claude/skills/release/SKILL.md", ".claude/skills/release/references/usage.md",
    ".github/skills/review/SKILL.md", ".github/skills/review/references/examples.md",
    ".claude/agents/reviewer.md", ".github/agents/reviewer.agent.md",
    ".claude/commands/release/check.md", ".github/prompts/check.prompt.md",
    "packages/web/.claude/rules/layout.md", "some folder/AGENTS.md",
)
EXCLUDED = (
    "vendor/package/AGENTS.md", "node_modules/package/CLAUDE.md",
    "packages/web/dist/CLAUDE.md", ".venv/CLAUDE.md", "build/AGENTS.md",
    "docs/generated/CLAUDE.md", ".claude/settings.json", ".mcp.json",
    ".claude/skills/release/scripts/run.sh", ".github/workflows/default.yaml",
    "CHANGELOG.md", "SECURITY.md", "LICENSE", "docs/adr/0001-decision.md",
    "src/main.go", ".github/instructions/notes.md", ".github/prompts/notes.md",
)


class RefreshScopeTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        subprocess.run(["git", "init", "--quiet", str(self.root)], check=True)

    def track(self, paths):
        contents = paths if isinstance(paths, dict) else dict.fromkeys(paths, "# Documentation\n")
        for path, content in contents.items():
            target = self.root / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(content)
        # The fixture explicitly represents tracked files, even when a user's
        # global ignore rules would normally hide a .claude directory.
        subprocess.run(["git", "-C", str(self.root), "add", "--force", "--", "."], check=True)

    def test_discovers_all_categories_and_exact_permissions(self):
        self.track(SUPPORTED + EXCLUDED)
        scope = SCOPE.discover(self.root)
        self.assertEqual(set(scope["existing"]), set(SUPPORTED))
        self.assertEqual(set(scope["paths"]), set(SUPPORTED) | set(SCOPE.CREATABLE))
        self.assertEqual(scope["allowed_tools"][3:], [f"Edit(/{p})" for p in scope["paths"]])
        self.assertTrue(scope["has_guidance"])

    def test_only_three_targets_are_created_while_claude_md_stays_host_managed(self):
        scope = SCOPE.discover(self.root)
        self.assertEqual(set(scope["creatable"]), set(SCOPE.CREATABLE))
        self.assertIn("AGENTS.md", scope["creatable"])
        self.assertEqual(scope["managed"], ["CLAUDE.md"])
        self.assertEqual(set(scope["paths"]), set(SCOPE.CREATABLE) | {"CLAUDE.md"})
        self.assertNotIn("Edit(/CLAUDE.md)", scope["allowed_tools"])
        self.assertEqual(scope["existing"], [])
        self.assertFalse(scope["has_guidance"])

    def test_should_never_grant_pointers_when_claude_md_only_imports_agents_md(self):
        # given
        self.track({"AGENTS.md": "# Guidance\n", "CLAUDE.md": "@AGENTS.md\n",
                    "packages/api/AGENTS.md": "# API\n", "packages/api/CLAUDE.md": "@./AGENTS.md\n",
                    "tools/CLAUDE.md": "@AGENTS.md\n", ".claude/CLAUDE.md": "@AGENTS.md\n"})

        # when
        scope = SCOPE.discover(self.root)

        # then
        self.assertEqual(scope["managed"], ["CLAUDE.md", "packages/api/CLAUDE.md", "tools/CLAUDE.md"])
        self.assertIn("tools/AGENTS.md", scope["creatable"])
        self.assertNotIn("AGENTS.md", scope["creatable"])
        self.assertIn(".claude/CLAUDE.md", scope["existing"])
        for path in scope["managed"]:
            self.assertIn(path, scope["paths"])
            self.assertNotIn(f"Edit(/{path})", scope["allowed_tools"])
        self.assertIn("Edit(/tools/AGENTS.md)", scope["allowed_tools"])
        self.assertTrue(scope["has_guidance"])

    def test_should_not_count_a_pointer_as_guidance_when_agents_md_is_missing(self):
        # given
        self.track({"README.md": "# Read me\n", "CLAUDE.md": "@AGENTS.md\n"})

        # when
        scope = SCOPE.discover(self.root)

        # then
        self.assertFalse(scope["has_guidance"])
        self.assertIn("AGENTS.md", scope["creatable"])

    def test_should_not_offer_agents_md_when_the_repository_ignores_it(self):
        # given
        self.track({"README.md": "# Read me\n", ".gitignore": "AGENTS.md\n"})

        # when
        scope = SCOPE.discover(self.root)

        # then
        self.assertNotIn("AGENTS.md", scope["creatable"])
        self.assertNotIn("Edit(/AGENTS.md)", scope["allowed_tools"])

    def test_readme_does_not_count_as_bootstrap_guidance(self):
        self.track(["README.md", "CONTRIBUTING.md"])
        self.assertFalse(SCOPE.discover(self.root)["has_guidance"])

    def test_untracked_new_categories_are_not_granted(self):
        self.track(["README.md"])
        (self.root / ".claude/rules").mkdir(parents=True)
        (self.root / ".claude/rules/new.md").write_text("untracked instructions")
        self.assertNotIn(".claude/rules/new.md", SCOPE.discover(self.root)["paths"])

    def test_symlinks_and_symlinked_creation_parents_are_excluded(self):
        self.track(["README.md"])
        (self.root / "CLAUDE.md").symlink_to("README.md")
        (self.root / "outside").mkdir()
        (self.root / ".github").symlink_to("outside", target_is_directory=True)
        subprocess.run(["git", "-C", str(self.root), "add", "."], check=True)
        scope = SCOPE.discover(self.root)
        self.assertEqual(scope["paths"], ["AGENTS.md", "README.md"])
        self.assertIn("CLAUDE.md", scope["skipped"])

    def test_permission_pattern_and_delimiter_characters_are_not_granted(self):
        paths = ["comma,name/AGENTS.md", "glob*/CLAUDE.md", "array[0]/AGENTS.md",
                 "newline\nname/CLAUDE.md"]
        self.track(paths)
        scope = SCOPE.discover(self.root)
        self.assertEqual(set(scope["skipped"]), set(paths))
        self.assertEqual(set(scope["paths"]), set(SCOPE.CREATABLE) | {"CLAUDE.md"})


if __name__ == "__main__":
    unittest.main()
