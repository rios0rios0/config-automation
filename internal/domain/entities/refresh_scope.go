package entities

import (
	"slices"
	"strings"
)

const (
	// AgentsFile is the instruction file every coding agent reads.
	AgentsFile = "AGENTS.md"
	// ClaudeFile is Claude Code's instruction file. Beside an AGENTS.md it
	// holds only AgentsImport.
	ClaudeFile = "CLAUDE.md"
	// AgentsImport is the whole content of a CLAUDE.md that defers to the
	// AGENTS.md beside it: the import Claude Code documents for sharing one
	// instruction file with other coding agents.
	AgentsImport = "@AGENTS.md\n"
)

const (
	// claudeDirectory is Claude Code's project directory. A CLAUDE.md inside
	// it is an alternative project location, not one side of a pair.
	claudeDirectory = ".claude"
	// unsafePathCharacters delimit or widen a permission rule, so a path
	// holding one never becomes an Edit grant.
	unsafePathCharacters = ",\\*?[]"
	// firstPrintableCharacter ends the ASCII control range.
	firstPrintableCharacter = ' '
	// scopedTail is how many trailing parts of a path can never open a
	// scoped section: a section needs its own name and a file below it.
	scopedTail = 2
	// markdownSuffix ends every maintained guidance file.
	markdownSuffix = ".md"
)

// RefreshScope is the manifest one repository's refresh works from: the
// exact paths it may stage, which of them exist, which it may create, which
// CLAUDE.md imports only the host writes, and the tools granted to Claude.
// The CLI maps it to the JSON the refresh workflows read with jq.
type RefreshScope struct {
	Paths        []string
	Existing     []string
	Creatable    []string
	Managed      []string
	HasGuidance  bool
	AllowedTools []string
	Skipped      []string
}

// IndexEntry is one path of a Git index with its file mode and merge stage.
type IndexEntry struct {
	Mode  string
	Stage int
	Path  string
}

// PathKind classifies a working-tree path without following a symlink.
type PathKind int

const (
	// PathAbsent means nothing exists at the path.
	PathAbsent PathKind = iota
	// PathFile is a regular file.
	PathFile
	// PathDirectory is a directory.
	PathDirectory
	// PathSymlink is a symbolic link, whatever it points at.
	PathSymlink
	// PathOther is any other file type, such as a socket or a device.
	PathOther
)

// ReadOnlyTools returns the tools every refresh phase may use.
func ReadOnlyTools() []string {
	return []string{"Read", "Grep", "Glob"}
}

// RegularFileModes returns the index modes of the files a refresh may edit.
func RegularFileModes() []string {
	return []string{"100644", "100755"}
}

// IsRefreshPath reports whether path belongs to a category the refresh
// maintains: an AGENTS.md or CLAUDE.md, a root document, or Markdown under
// a scoped rules, skills, agents, commands, instructions, or prompts section.
func IsRefreshPath(path string) bool {
	parts := strings.Split(path, "/")
	name := parts[len(parts)-1]
	if slices.ContainsFunc(parts[:len(parts)-1], isExcludedDirectory) {
		return false
	}
	if name == ClaudeFile || name == AgentsFile || slices.Contains(refreshRootDocuments(), path) {
		return true
	}
	suffixes := scopedGuidanceSuffixes()
	for index := range len(parts) - scopedTail {
		if suffix, found := suffixes[parts[index]+"/"+parts[index+1]]; found {
			return strings.HasSuffix(path, suffix)
		}
	}
	return false
}

// IsPairPath reports whether path is an AGENTS.md or CLAUDE.md outside a
// .claude directory, one side of an instruction-file pair.
func IsPairPath(path string) bool {
	parts := strings.Split(path, "/")
	name := parts[len(parts)-1]
	return (name == ClaudeFile || name == AgentsFile) && !slices.Contains(parts[:len(parts)-1], claudeDirectory)
}

// IsAgentsImport reports whether text, ignoring surrounding whitespace, only
// imports the AGENTS.md beside it.
func IsAgentsImport(text string) bool {
	trimmed := strings.TrimSpace(text)
	return trimmed == "@AGENTS.md" || trimmed == "@./AGENTS.md"
}

// HasSafePathCharacters reports whether path can become a permission rule:
// it holds no control character and none of the rule delimiters or
// wildcards, so it cannot inject a second rule or widen a grant.
func HasSafePathCharacters(path string) bool {
	isControl := func(char rune) bool { return char < firstPrintableCharacter }
	return !strings.ContainsFunc(path, isControl) && !strings.ContainsAny(path, unsafePathCharacters)
}

// IsHumanDocument reports whether path is written for people rather than
// agents, so that alone it does not count as existing guidance.
func IsHumanDocument(path string) bool {
	return slices.Contains(humanDocuments(), path)
}

// isExcludedDirectory reports whether a directory holds dependencies, a
// virtual environment, or build or generated output, never maintained
// guidance.
func isExcludedDirectory(name string) bool {
	excluded := []string{
		".git", ".venv", "venv", "node_modules", "vendor", "third_party",
		"third-party", "dist", "build", "target", "generated", "__pycache__",
	}
	return slices.Contains(excluded, name)
}

// scopedGuidanceSuffixes maps each scoped section to the file suffix it
// maintains.
func scopedGuidanceSuffixes() map[string]string {
	return map[string]string{
		".claude/rules":        markdownSuffix,
		".claude/skills":       markdownSuffix,
		".claude/agents":       markdownSuffix,
		".claude/commands":     markdownSuffix,
		".github/skills":       markdownSuffix,
		".github/agents":       markdownSuffix,
		".github/instructions": ".instructions" + markdownSuffix,
		".github/prompts":      ".prompt" + markdownSuffix,
	}
}
