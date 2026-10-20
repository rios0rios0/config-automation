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

// humanDocuments returns the documents written for people, which alone do
// not count as agent guidance.
func humanDocuments() []string {
	return []string{"README.md", "CONTRIBUTING.md", ".github/CONTRIBUTING.md", "docs/CONTRIBUTING.md"}
}
