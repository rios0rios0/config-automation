package entities

import (
	"path"
	"strings"
)

const (
	// CopilotInstructionsFile is GitHub Copilot's repository-wide instruction
	// file.
	CopilotInstructionsFile = ".github/copilot-instructions.md"
	// CopilotReviewSkillFile is the Copilot agent skill that teaches how a
	// change in the repository is judged.
	CopilotReviewSkillFile = ".github/skills/code-review/SKILL.md"
	// copilotReviewSkillDirectory holds the review skill and its references.
	copilotReviewSkillDirectory = ".github/skills/code-review"
)

// InstructionFiles chooses which kinds of instruction file a refresh keeps:
// whether Claude may create a root AGENTS.md, whether every CLAUDE.md is the
// one-line import of the AGENTS.md beside it, and whether the repository has
// GitHub Copilot's instructions and code-review skill. A kind that is off is
// removed from the repository by the host, never by the model; AGENTS.md
// itself is never removed, because it is where the guidance lives.
type InstructionFiles struct {
	Agents  bool
	Claude  bool
	Copilot bool
}

// CreatablePaths returns the files the refresh may create when a repository
// lacks them.
func (f InstructionFiles) CreatablePaths() []string {
	paths := []string{}
	if f.Agents {
		paths = append(paths, AgentsFile)
	}
	if f.Copilot {
		paths = append(paths, CopilotInstructionsFile, CopilotReviewSkillFile)
	}
	return paths
}

// IsRemovedPath reports whether a refresh path is an instruction file of a
// kind this repository does not keep, which the host removes before the
// audit: Copilot's instructions and review skill when Copilot is off, and
// every CLAUDE.md outside .claude when CLAUDE.md is off.
func (f InstructionFiles) IsRemovedPath(file string) bool {
	if !f.Copilot && (file == CopilotInstructionsFile || strings.HasPrefix(file, copilotReviewSkillDirectory+"/")) {
		return true
	}
	return !f.Claude && IsPairPath(file) && path.Base(file) == ClaudeFile
}

// refreshRootDocuments returns the single documents the refresh keeps
// accurate besides agent guidance, wherever the platform keeps them.
func refreshRootDocuments() []string {
	return []string{
		"README.md", "CONTRIBUTING.md", ".github/CONTRIBUTING.md", "docs/CONTRIBUTING.md",
		".azuredevops/CONTRIBUTING.md", CopilotInstructionsFile,
	}
}
