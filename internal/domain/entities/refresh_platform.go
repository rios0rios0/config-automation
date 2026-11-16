package entities

// The functions in this file hold the only part of the refresh scope that is
// specific to the platform this workflow runs on.

// RefreshCreatablePaths returns the files the config-and-docs refresh may
// create when a repository lacks them.
func RefreshCreatablePaths() []string {
	return []string{AgentsFile, ".github/copilot-instructions.md", ".github/skills/code-review/SKILL.md"}
}

// refreshRootDocuments returns the single documents the refresh keeps
// accurate besides agent guidance.
func refreshRootDocuments() []string {
	return []string{
		"README.md", "CONTRIBUTING.md", ".github/CONTRIBUTING.md", "docs/CONTRIBUTING.md",
		".github/copilot-instructions.md",
	}
}
