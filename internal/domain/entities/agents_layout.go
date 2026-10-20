package entities

import (
	"slices"
	"strings"
	"unicode"
)

const (
	// LegacyBanner opens a CLAUDE.md that Claude Code's /init wrote.
	LegacyBanner = "# CLAUDE.md\n\nThis file provides guidance to Claude Code (claude.ai/code) " +
		"when working with code in this repository.\n"
	// AgentsBanner opens an AGENTS.md written for every coding agent.
	AgentsBanner = "# AGENTS.md\n\nThis file provides guidance to AI coding agents (Claude Code, Codex, " +
		"Cursor, GitHub Copilot) when working with code in this repository.\n"
	// MigratedHeading introduces the text a CLAUDE.md added beside an
	// existing AGENTS.md, for the refresh to integrate.
	MigratedHeading = "## Migrated from CLAUDE.md"
	// RenameMessage is the commit that moves a CLAUDE.md into AGENTS.md on
	// its own, so `git log --follow AGENTS.md` reaches its history.
	RenameMessage = "chore(refresh): renamed `CLAUDE.md` to `AGENTS.md`"
)

const (
	// noLayoutChanges is the note of a repository already in the layout.
	noLayoutChanges = "- No changes: every `CLAUDE.md` already imports the `AGENTS.md` beside it."
	// maxFenceIndent is the most spaces a code fence may be indented by.
	maxFenceIndent = 3
	// minFenceLength is the shortest backtick or tilde run that is a fence.
	minFenceLength = 3
)

// LayoutOutcome is what the layout pass did to one CLAUDE.md.
type LayoutOutcome string

const (
	// OutcomeKept means the pair already complies and nothing is reported.
	OutcomeKept LayoutOutcome = ""
	// OutcomeRenamed means CLAUDE.md moved into AGENTS.md.
	OutcomeRenamed LayoutOutcome = "renamed"
	// OutcomeMerged means CLAUDE.md text was appended to AGENTS.md.
	OutcomeMerged LayoutOutcome = "merged"
	// OutcomePointed means CLAUDE.md was written as the import.
	OutcomePointed LayoutOutcome = "pointed"
	// OutcomeRebannered means AGENTS.md lost the legacy CLAUDE.md banner.
	OutcomeRebannered LayoutOutcome = "rebannered"
	// OutcomeDangling means CLAUDE.md imports an AGENTS.md that is missing.
	OutcomeDangling LayoutOutcome = "dangling"
	// OutcomeSkipped means the pair was left alone for a human to review.
	OutcomeSkipped LayoutOutcome = "skipped"
)

// PairSide classifies one file of an AGENTS.md and CLAUDE.md pair.
type PairSide string

const (
	// SideUnsafe is a symlink to another file, a non-file, non-UTF-8 text,
	// or a path that cannot become a permission rule.
	SideUnsafe PairSide = "unsafe"
	// SideIgnored is a path the repository's ignore rules match.
	SideIgnored PairSide = "ignored"
	// SideLink is a symlink to the other file of its pair.
	SideLink PairSide = "link"
	// SideAbsent means the file does not exist.
	SideAbsent PairSide = "absent"
	// SidePointer is a CLAUDE.md that only imports AGENTS.md.
	SidePointer PairSide = "pointer"
	// SideFile is any other regular UTF-8 file.
	SideFile PairSide = "file"
)

// LayoutPairState is the classification of both files of a pair.
type LayoutPairState struct {
	Claude PairSide
	Agents PairSide
}

// LayoutPair is one directory's CLAUDE.md and AGENTS.md with their state.
type LayoutPair struct {
	Claude string
	Agents string
	State  LayoutPairState
}

// AgentsLayoutReport records what the layout pass did to one repository.
// The CLI maps it to the JSON the refresh workflows read with jq.
type AgentsLayoutReport struct {
	Renamed    []string
	Merged     []string
	Pointed    []string
	Rebannered []string
	Dangling   []string
	Skipped    []string
	Changed    bool
	Note       string
}

// layoutSection ties one outcome to its list in a report and to the line
// that describes it in the refresh prompt.
type layoutSection struct {
	outcome  LayoutOutcome
	paths    *[]string
	template string
}

// NewAgentsLayoutReport returns an empty report whose lists encode as
// empty arrays rather than null.
func NewAgentsLayoutReport() AgentsLayoutReport {
	return AgentsLayoutReport{
		Renamed: []string{}, Merged: []string{}, Pointed: []string{},
		Rebannered: []string{}, Dangling: []string{}, Skipped: []string{},
	}
}

// Record lists path under outcome. OutcomeKept records nothing.
func (r *AgentsLayoutReport) Record(outcome LayoutOutcome, path string) {
	for _, section := range r.sections() {
		if section.outcome == outcome {
			*section.paths = append(*section.paths, path)
		}
	}
}

// DescribeAgentsLayout renders a report for the refresh prompt: one line per
// path, grouped by outcome in reporting order.
func DescribeAgentsLayout(report AgentsLayoutReport) string {
	lines := []string{}
	for _, section := range report.sections() {
		for _, path := range *section.paths {
			lines = append(
				lines,
				strings.NewReplacer("{path}", path, "{heading}", MigratedHeading).Replace(section.template),
			)
		}
	}
	if len(lines) == 0 {
		return noLayoutChanges
	}
	return strings.Join(lines, "\n")
}

// sections lists the report's outcomes in reporting order.
func (r *AgentsLayoutReport) sections() []layoutSection {
	return []layoutSection{
		{OutcomeRenamed, &r.Renamed, "- Renamed `{path}` to `AGENTS.md` beside it in a separate commit, " +
			"so history follows the file, and recreated it as the import line."},
		{OutcomeMerged, &r.Merged, "- Appended the former `{path}` text to the `AGENTS.md` beside it " +
			"under `{heading}`, then reduced `{path}` to the import line."},
		{OutcomePointed, &r.Pointed, "- Added `{path}` importing the existing `AGENTS.md` beside it."},
		{OutcomeRebannered, &r.Rebannered, "- Replaced the legacy `# CLAUDE.md` banner of `{path}`."},
		{OutcomeDangling, &r.Dangling, "- `{path}` imports an `AGENTS.md` that does not exist; " +
			"create it if that directory needs guidance."},
		{OutcomeSkipped, &r.Skipped, "- Left `{path}` and its `AGENTS.md` unchanged (a symlink to another " +
			"file, an ignored path, or not UTF-8 text); report it for human review."},
	}
}

// WithoutAgentsImports drops the lines that import AGENTS.md, except inside
// fenced code blocks: Claude Code does not evaluate an import there, so a
// line in one is an example to keep.
func WithoutAgentsImports(text string) string {
	kept, fence := []string{}, ""
	for _, line := range splitLines(text) {
		marker := fenceMarker(line)
		switch {
		case marker != "" && fence == "":
			fence = marker
		case marker != "" && strings.TrimSpace(line) == marker && strings.HasPrefix(marker, fence):
			fence = ""
		case fence == "" && IsAgentsImport(line):
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// ContainsLines reports whether every non-blank line of extra appears,
// whole and in order, as one run of non-blank lines of text. Unlike a
// substring test, it does not find "run make test." inside "Do not run make
// test.", so conflicting guidance is never mistaken for a copy.
func ContainsLines(text, extra string) bool {
	haystack, needle := contentLines(text), contentLines(extra)
	for index := range len(haystack) - len(needle) + 1 {
		if slices.Equal(haystack[index:index+len(needle)], needle) {
			return true
		}
	}
	return false
}

// MergedAgentsGuidance returns agents with what claude adds appended under
// MigratedHeading, leaving out the legacy banner and the import itself.
// Nothing is appended when agents already holds that text line for line.
// It reports whether agents changed.
func MergedAgentsGuidance(agents, claude string) (string, bool) {
	extra := strings.TrimSpace(WithoutAgentsImports(strings.TrimPrefix(claude, LegacyBanner)))
	if extra == "" || ContainsLines(agents, extra) {
		return agents, false
	}
	return strings.TrimRightFunc(agents, unicode.IsSpace) + "\n\n" + MigratedHeading + "\n\n" + extra + "\n", true
}

// WithAgentsBanner swaps the legacy CLAUDE.md banner at the start of text
// for the AGENTS.md one, reporting whether text started with it.
func WithAgentsBanner(text string) (string, bool) {
	rest, found := strings.CutPrefix(text, LegacyBanner)
	if !found {
		return text, false
	}
	return AgentsBanner + rest, true
}

// SameLinkTarget reports whether a symlink target names sibling. The paths
// are compared part by part as written: a "./" prefix and repeated slashes
// do not matter, but ".." is never resolved, so a target that reaches the
// file another way does not match.
func SameLinkTarget(target, sibling string) bool {
	return slices.Equal(lexicalParts(target), lexicalParts(sibling))
}

// fenceMarker returns the backtick or tilde run that opens or closes a
// fenced code block on line, or "" when the line is not a fence.
func fenceMarker(line string) string {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > maxFenceIndent || trimmed == "" || (trimmed[0] != '`' && trimmed[0] != '~') {
		return ""
	}
	run := len(trimmed) - len(strings.TrimLeft(trimmed, trimmed[:1]))
	if run < minFenceLength {
		return ""
	}
	return trimmed[:run]
}

// splitLines splits text into lines without their line endings, treating
// "\r\n" as one ending and adding no empty line after a final newline.
func splitLines(text string) []string {
	if text == "" {
		return []string{}
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for index, line := range lines {
		lines[index] = strings.TrimSuffix(line, "\r")
	}
	return lines
}

// contentLines returns the non-blank lines of text without trailing
// whitespace.
func contentLines(text string) []string {
	lines := []string{}
	for _, line := range splitLines(text) {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimRightFunc(line, unicode.IsSpace))
		}
	}
	return lines
}

// lexicalParts splits a slash-separated path into its parts, dropping empty
// and "." parts and marking an absolute path with a leading "/".
func lexicalParts(path string) []string {
	parts := []string{}
	if strings.HasPrefix(path, "/") {
		parts = append(parts, "/")
	}
	for part := range strings.SplitSeq(path, "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	return parts
}
