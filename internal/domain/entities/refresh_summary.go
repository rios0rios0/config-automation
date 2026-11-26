package entities

import (
	"fmt"
	"strings"
)

// FailureReason says which step a repository failed at. Each one turns the
// batch red; skips (a usage limit, a safeguard refusal) are not failures.
type FailureReason string

const (
	// FailureClone means the repository could not be cloned.
	FailureClone FailureReason = "clone"
	// FailureCheckout means its default branch could not be checked out.
	FailureCheckout FailureReason = "checkout"
	// FailureLayout means the instruction-file layout pass failed.
	FailureLayout FailureReason = "layout"
	// FailureIgnoreRules means the shared ignore rules could not be written
	// into `.gitignore`, such as when its block's markers are malformed.
	FailureIgnoreRules FailureReason = "gitignore"
	// FailureScope means the refresh manifest could not be built.
	FailureScope FailureReason = "scope"
	// FailureClaude means a Claude phase failed for any other reason.
	FailureClaude FailureReason = "claude"
	// FailureClaudeQuota means a Claude usage limit stopped the batch here.
	FailureClaudeQuota FailureReason = "claude-quota"
	// FailureChangelogGuard means a hand-edit of a generated changelog could
	// not be reverted.
	FailureChangelogGuard FailureReason = "changelog-guard"
	// FailureFragment means a new chlog fragment was malformed or hand-typed.
	FailureFragment FailureReason = "fragment"
	// FailureDrift means the drift of the manifest could not be read.
	FailureDrift FailureReason = "drift"
	// FailureSecretShape means the diff carries a credential shape, or could
	// not be read to check for one.
	FailureSecretShape FailureReason = "secret-shape"
	// FailureStage means paths could not be staged.
	FailureStage FailureReason = "stage"
	// FailureBranch means the refresh branch could not be created.
	FailureBranch FailureReason = "branch"
	// FailureCommit means the refresh could not be committed.
	FailureCommit FailureReason = "commit"
	// FailurePush means the refresh branch could not be pushed.
	FailurePush FailureReason = "push"
	// FailurePullRequestList means the open pull request could not be looked
	// up, so none is created rather than risking a duplicate.
	FailurePullRequestList FailureReason = "pr-list"
	// FailurePullRequestCreate means the pull request could not be opened.
	FailurePullRequestCreate FailureReason = "pr-create"
	// FailureInternal means the refresh itself broke on this repository.
	FailureInternal FailureReason = "internal"
)

// RefreshFailure is one repository the batch could not refresh, and why.
type RefreshFailure struct {
	Reason FailureReason
	Target string
}

// RefreshSummary tallies one batch's outcomes in processing order. Processed
// counts every repository reached, skipped ones included.
type RefreshSummary struct {
	Total            int
	Processed        int
	NoDrift          []string
	PRsCreated       []string
	PRsUpdated       []string
	QuotaSkipped     []string
	SafeguardSkipped []string
	Failed           []RefreshFailure
}

// NewRefreshSummary returns an empty summary of a batch of total
// repositories.
func NewRefreshSummary(total int) RefreshSummary {
	return RefreshSummary{
		Total: total, NoDrift: []string{}, PRsCreated: []string{}, PRsUpdated: []string{},
		QuotaSkipped: []string{}, SafeguardSkipped: []string{}, Failed: []RefreshFailure{},
	}
}

// HasFailures reports whether any repository failed, which turns the leg red.
func (s RefreshSummary) HasFailures() bool {
	return len(s.Failed) > 0
}

// Lines renders the summary footer. Failures are one per line, because each
// is a "<reason>: <repository>" pair that a joined line would make
// unreadable.
func (s RefreshSummary) Lines() []string {
	lines := []string{
		"==== Batch summary ====",
		fmt.Sprintf("processed (%d/%d)", s.Processed, s.Total),
		summaryLine("no_drift", s.NoDrift),
		summaryLine("prs_created", s.PRsCreated),
		summaryLine("prs_updated", s.PRsUpdated),
		summaryLine("quota_skipped", s.QuotaSkipped),
		summaryLine("safeguard_skipped", s.SafeguardSkipped),
		fmt.Sprintf("failed (%d):", len(s.Failed)),
	}
	for _, failure := range s.Failed {
		lines = append(lines, fmt.Sprintf("  - %s: %s", failure.Reason, failure.Target))
	}
	return lines
}

// summaryLine renders one bucket as "name (count): members".
func summaryLine(name string, members []string) string {
	return fmt.Sprintf("%s (%d): %s", name, len(members), strings.Join(members, " "))
}
