package entities

import "strings"

const (
	// readOnlyToolList is what the audit may use, and what both phases read
	// with.
	readOnlyToolList = "Read,Grep,Glob"
	// editingToolList adds the file-editing tools the refresh's grants cover.
	editingToolList = readOnlyToolList + ",Edit,Write"
)

// AgentRun is one Claude phase against one working tree. Tools is the set of
// tools the session has at all; AllowedTools is what it may use without
// asking, which, under dontAsk, is all it may use.
type AgentRun struct {
	Directory    string
	Prompt       string
	Model        string
	Effort       string
	MaxTurns     int
	Tools        string
	AllowedTools string
}

// AgentResult is what a phase printed, kept whole so a failure can be
// classified, and for the audit the report it returned.
type AgentResult struct {
	Output string
	Report string
}

// AuditTools is the tool list of the read-only audit.
func AuditTools() string {
	return readOnlyToolList
}

// RefreshTools is the tool list of the refresh. The default tool set on Linux
// leaves out Grep and Glob, which checking documented claims against the code
// needs, so the list is explicit; Bash is offered only where `chlog new` is the
// granted changelog command.
func RefreshTools(changelogGrant string) string {
	if strings.HasPrefix(changelogGrant, "Bash(") {
		return editingToolList + ",Bash"
	}
	return editingToolList
}

// RefreshAllowedTools is the refresh's allowlist: the manifest's exact
// grants plus the one changelog grant the repository's convention calls for.
// Every file grant is an Edit rule, which covers every file-editing tool,
// creating a missing file included.
func RefreshAllowedTools(scope RefreshScope, changelogGrant string) string {
	tools := strings.Join(scope.AllowedTools, ",")
	if changelogGrant != "" {
		tools += "," + changelogGrant
	}
	return tools
}
