package entities

import (
	"regexp"
	"slices"
	"strings"
)

// HasCredentialShape reports whether a line a unified diff adds carries a
// credential shape. What the refresh writes is prose about secret hygiene, and
// secret scanners match shapes rather than values: several rules fire on a
// vendor prefix or a key banner alone, and a finding committed to a default
// branch outlives any later edit. Only added lines are read, so text a
// repository already had never fails a refresh that did not write it.
//
// Each pattern is written so it cannot match itself: every prefix is followed
// by a bracket expression rather than by body characters. Two shapes need a
// full-length body instead of a prefix, because what they catch is a token
// itself: a fine-grained GitHub token and an Anthropic token.
func HasCredentialShape(diff string) bool {
	shapes := regexp.MustCompile(
		`xox[abprs]-|BEGIN[ A-Z]*PRIVATE KEY|gh[opsur]_[0-9A-Za-z]{36}|github_pat_[0-9A-Za-z_]{82}|` +
			`sk-ant-[0-9A-Za-z_-]{32,}|AKIA[0-9A-Z]{16}|ey[0-9A-Za-z]{17,}\.ey[0-9A-Za-z]`,
	)
	return slices.ContainsFunc(AddedLines(diff), shapes.MatchString)
}

// AddedLines returns the lines a unified diff adds, without their "+" marker.
// A "+++" line is a file header only before a file's first hunk; inside a
// hunk it is an added line whose own text starts with "++".
func AddedLines(diff string) []string {
	added := []string{}
	inHunk := false
	for line := range strings.SplitSeq(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			inHunk = false
		case strings.HasPrefix(line, "@@"):
			inHunk = true
		case inHunk && strings.HasPrefix(line, "+"):
			added = append(added, line[1:])
		}
	}
	return added
}
