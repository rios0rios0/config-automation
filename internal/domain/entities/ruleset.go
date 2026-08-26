package entities

// Ruleset is the subset of GitHub's ruleset body that the compliance
// policy enforces: the `main-protection` ruleset with the
// `non_fast_forward` rule targeting `refs/heads/main`, a `pull_request`
// rule pinning the allowed merge methods, and a bypass actor for the
// repository admin role so the owner can still force-push.
//
// HasNonFastForward and AllowedMergeMethods sound like the same thing
// and are not. GitHub's `non_fast_forward` rule blocks FORCE PUSHES;
// it says nothing about how pull requests land. Restricting merges to
// merge commits — the actual "no fast-forward merge" policy — is
// AllowedMergeMethods on the pull_request rule. Conflating them leaves
// the fast-forward merge path wide open.
type Ruleset struct {
	ID          int64
	Name        string
	Enforcement string
	// HasNonFastForward reports the `non_fast_forward` rule: blocks force pushes.
	HasNonFastForward bool
	// AllowedMergeMethods is the pull_request rule's allowed_merge_methods,
	// in GitHub's lowercase spelling. Nil means the repo has no
	// pull_request rule at all, which is not the same as an empty list.
	AllowedMergeMethods []string
	TargetsMain         bool
	AdminBypass         bool
}

// HasAllowedMergeMethods reports whether this ruleset's pull_request
// rule restricts merges to exactly the policy's method set, order
// independent. A superset is not compliant: leaving `rebase` in the list
// is precisely the fast-forward path the policy exists to close.
func (r Ruleset) HasAllowedMergeMethods(want []string) bool {
	if len(r.AllowedMergeMethods) != len(want) {
		return false
	}
	seen := make(map[string]struct{}, len(r.AllowedMergeMethods))
	for _, method := range r.AllowedMergeMethods {
		seen[method] = struct{}{}
	}
	for _, method := range want {
		if _, ok := seen[method]; !ok {
			return false
		}
	}
	return true
}

// IsCompliant reports whether this ruleset matches the policy fully.
// A name-only match is not enough: the rule body and target ref must also
// be correct, otherwise phase 4 would leave the repo in a broken state.
func (r Ruleset) IsCompliant() bool {
	return r.HasNonFastForward &&
		r.HasAllowedMergeMethods(DesiredAllowedMergeMethods()) &&
		r.TargetsMain &&
		r.AdminBypass
}
