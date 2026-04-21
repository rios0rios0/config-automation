package entities

// Ruleset is the subset of GitHub's ruleset body that the compliance
// policy enforces: the `main-protection` ruleset with the
// `non_fast_forward` rule targeting `refs/heads/main`, and a bypass actor
// for the repository admin role so the owner can still force-push.
type Ruleset struct {
	ID                int64
	Name              string
	Enforcement       string
	HasNonFastForward bool
	TargetsMain       bool
	AdminBypass       bool
}

// IsCompliant reports whether this ruleset matches the policy fully.
// A name-only match is not enough: the rule body and target ref must also
// be correct, otherwise phase 4 would leave the repo in a broken state.
func (r Ruleset) IsCompliant() bool {
	return r.HasNonFastForward && r.TargetsMain && r.AdminBypass
}
