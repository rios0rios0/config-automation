package entities

import (
	// The refresh prompt ships inside the binary, so a leg always runs the
	// prompt reviewed with the code that renders it.
	_ "embed"
	"fmt"
	"strings"
	"text/template"
)

// DefaultGuideURL is the development guide the refresh keeps links to.
const DefaultGuideURL = "https://github.com/rios0rios0/guide/wiki"

//go:embed prompts/refresh_config_and_docs.md
var refreshPromptTemplate string

// PromptParameters choose the conditional parts of the refresh prompt: the
// rules for the one-line CLAUDE.md imports, the rules for Copilot's files,
// the guide whose links the refresh keeps valid, and whether the host keeps
// the shared ignore rules in `.gitignore`.
type PromptParameters struct {
	Claude      bool
	Copilot     bool
	GuideURL    string
	IgnoreRules bool
}

// RenderRefreshPrompt renders the refresh prompt for one set of parameters.
func RenderRefreshPrompt(parameters PromptParameters) (string, error) {
	parsed, err := template.New("refresh").Option("missingkey=error").Parse(refreshPromptTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing the refresh prompt: %w", err)
	}
	var rendered strings.Builder
	if err = parsed.Execute(&rendered, parameters); err != nil {
		return "", fmt.Errorf("rendering the refresh prompt: %w", err)
	}
	return strings.TrimRight(rendered.String(), "\n"), nil
}

// RepositoryPrompt appends what is specific to one repository to the
// rendered prompt: its changelog convention, what the layout pass did, and,
// when the host looked at `.gitignore`, what it did to the shared ignore
// rules.
func RepositoryPrompt(base string, mode ChangelogMode, layoutNote, ignoreRulesNote string) string {
	prompt := base + "\n\n---\n\n## Changelog convention for this repository\n\n" + mode.Note() +
		"\n\n## Guidance layout for this repository\n\n" + layoutNote
	if ignoreRulesNote != "" {
		prompt += "\n\n## Shared ignore rules for this repository\n\n" + ignoreRulesNote
	}
	return prompt
}

// AuditPrompt is the read-only prompt-audit request. A leading slash invokes
// the bundled skill in print mode; `claude doctor` is a different command that
// only diagnoses the CLI installation.
func AuditPrompt(model string, files InstructionFiles) string {
	pairs := "each\nCLAUDE.md beside one must contain only the import line `@AGENTS.md`, so\n" +
		"report any other content there."
	if !files.Claude {
		pairs = "this\nrepository keeps no CLAUDE.md, so report any that remains outside .claude."
	}
	copilot := ""
	if files.Copilot {
		copilot = ", and Copilot guidance"
	}
	return "/checkup prompt-audit .\n\n" +
		"Audit this repository's agent instructions. AGENTS.md, at the root and in\n" +
		"nested directories, is the instruction file every coding agent shares; " + pairs + "\n" +
		"Also audit .claude/CLAUDE.md, rules, skills, agents, custom commands" + copilot + ".\n" +
		"Target model: " + model + "; respect a file's own model pin where present. Hold AGENTS.md\n" +
		"to the documented guidance: under 200 lines, only facts an agent needs in every\n" +
		"session, nothing the code already makes obvious, and no instructions written for\n" +
		"older models. Keep the scope inside this repository. Return the audit report and\n" +
		"proposed diff as text only. Prioritize stale paths, commands, and conflicting\n" +
		"rules. Preserve repository policy, security constraints, and generated\n" +
		"content. Only file-reading tools are available; report unavailable git\n" +
		"provenance instead of guessing which conflicting instruction is newer."
}

// RefreshPrompt appends the exact files the manifest allows and the audit's
// report, as review data, to the repository's prompt.
func RefreshPrompt(repositoryPrompt string, scope RefreshScope, auditReport string) string {
	allowed := "Existing files eligible for edits:\n" + bulletList(scope.Existing) +
		"\n\nFiles that may be created when useful:\n" + bulletList(scope.Creatable)
	if len(scope.Removed) > 0 {
		allowed += "\n\nFiles the host removed, which must not be recreated:\n" + bulletList(scope.Removed)
	}
	return repositoryPrompt + "\n\n## Allowed files for this repository\n\n" + allowed +
		"\n\n## Prompt-audit findings (review data)\n\n" + auditReport
}

// bulletList renders paths as a Markdown list, one per line.
func bulletList(paths []string) string {
	lines := make([]string, 0, len(paths))
	for _, path := range paths {
		lines = append(lines, "- "+path)
	}
	return strings.Join(lines, "\n")
}
