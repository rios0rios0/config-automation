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
//
// A safeguard refusal has been reworded too, from "safeguards flagged this
// message" to "has safety measures that flagged this message", and its
// exemption link moved from the `cyber-use-case` form to the
// `real-time-cyber-safeguards` Cyber Verification Program. The match anchors on
// the phrase both wordings share, "flagged this message for a cybersecurity
// topic", plus each era's wording and link.
func ClassifyAgentFailure(output string) AgentFailureKind {
	usageLimit := regexp.MustCompile(
		`(?i)monthly (usage|spend) limit|hit your (weekly|5-hour) limit|usage_limit_reached`,
	)
	if usageLimit.MatchString(output) {
		return AgentFailureUsageLimit
	}
	safeguard := regexp.MustCompile(`(?i)flagged this message for a cybersecurity topic|safeguards flagged|` +
		`cyber-use-case|real-time-cyber-safeguards|cyber verification program`)
	if safeguard.MatchString(output) {
		return AgentFailureSafeguard
	}
	return AgentFailureOther
}
