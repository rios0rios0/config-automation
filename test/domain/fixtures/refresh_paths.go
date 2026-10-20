// Package fixtures holds test data shared by more than one test package.
package fixtures

// SupportedRefreshPaths returns one path of every category the refresh
// maintains, nested and with spaces included.
func SupportedRefreshPaths() []string {
	return []string{
		"README.md", "CONTRIBUTING.md", ".github/CONTRIBUTING.md", "docs/CONTRIBUTING.md",
		"AGENTS.md", "CLAUDE.md", "packages/api/AGENTS.md", "packages/api/CLAUDE.md",
		".claude/CLAUDE.md", ".claude/rules/go/testing.md",
		".github/instructions/go.instructions.md", ".github/copilot-instructions.md",
		".claude/skills/release/SKILL.md", ".claude/skills/release/references/usage.md",
		".github/skills/review/SKILL.md", ".github/skills/review/references/examples.md",
		".claude/agents/reviewer.md", ".github/agents/reviewer.agent.md",
		".claude/commands/release/check.md", ".github/prompts/check.prompt.md",
		"packages/web/.claude/rules/layout.md", "some folder/AGENTS.md",
	}
}

// ExcludedRefreshPaths returns paths the refresh never touches: dependency
// and build output, settings, scripts, workflows, and unmaintained
// documents.
func ExcludedRefreshPaths() []string {
	return []string{
		"vendor/package/AGENTS.md", "node_modules/package/CLAUDE.md",
		"packages/web/dist/CLAUDE.md", ".venv/CLAUDE.md", "build/AGENTS.md",
		"docs/generated/CLAUDE.md", ".claude/settings.json", ".mcp.json",
		".claude/skills/release/scripts/run.sh", ".github/workflows/default.yaml",
		"CHANGELOG.md", "SECURITY.md", "LICENSE", "docs/adr/0001-decision.md",
		"src/main.go", ".github/instructions/notes.md", ".github/prompts/notes.md",
	}
}

// PairedImports returns the CLAUDE.md files among SupportedRefreshPaths that
// sit beside an AGENTS.md, which the layout reduces to the import.
func PairedImports() []string {
	return []string{"CLAUDE.md", "packages/api/CLAUDE.md", "some folder/CLAUDE.md"}
}
