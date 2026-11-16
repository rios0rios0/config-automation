package entities

import "regexp"

// AgentFailureKind says what a failed Claude run means for the rest of the
// batch.
type AgentFailureKind int

const (
	// AgentFailureOther is a failure local to one repository: a transient API
	// error, a turn limit, a crash. The repository fails; the batch goes on.
	AgentFailureOther AgentFailureKind = iota
	// AgentFailureUsageLimit is an exhausted Claude cap. Every later call in
	// the batch would hit the same wall, so the batch stops calling Claude.
	AgentFailureUsageLimit
	// AgentFailureSafeguard is a safeguard refusal of this repository's
	// content. It is deterministic for that repository and says nothing about
	// the others, so the repository is skipped, not failed.
	AgentFailureSafeguard
)

// ClassifyAgentFailure reads a failed run's output. A usage limit is matched
// by wording and by the `usage_limit_reached` code the audit's JSON result
// carries: the CLI rewords its messages (the 5-hour and the organization spend
// caps each named a wording no older phrase matched), and the code has held
// for every limit seen so far, while the refresh phase prints text only.
func ClassifyAgentFailure(output string) AgentFailureKind {
	usageLimit := regexp.MustCompile(
		`(?i)monthly (usage|spend) limit|hit your (weekly|5-hour) limit|usage_limit_reached`,
	)
	if usageLimit.MatchString(output) {
		return AgentFailureUsageLimit
	}
	if regexp.MustCompile(`(?i)safeguards flagged|cyber-use-case`).MatchString(output) {
		return AgentFailureSafeguard
	}
	return AgentFailureOther
}
