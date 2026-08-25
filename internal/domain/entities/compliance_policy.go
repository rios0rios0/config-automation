package entities

// DesiredRepoSettings is the enforced policy for every `rios0rios0` repo
// (phase 2). Forks and private repos may have per-field carve-outs;
// AuditResult.ComputeIssues() encodes those. Exposed as a function so
// the policy stays immutable from call sites.
//
// The three merge toggles encode a semi-linear history: a pull request is
// rebased onto the base branch first ("Update with rebase", which GitHub
// only offers while AllowRebaseMerge is on) and then landed with a merge
// commit, so `main` keeps one merge commit per pull request over an
// otherwise linear ancestry. AllowSquashMerge is off because squashing
// discards the branch's individual commits, and GitHub's own "Rebase and
// merge" button fast-forwards without the merge commit — neither produces
// that shape. GitHub has no single "semi-linear merge" option, so the
// policy removes the buttons that break it rather than selecting one.
func DesiredRepoSettings() RepositorySettings {
	return RepositorySettings{
		DeleteBranchOnMerge: true,
		AllowAutoMerge:      true,
		AllowSquashMerge:    false,
		AllowRebaseMerge:    true,
		AllowMergeCommit:    true,
		HasWiki:             false,
		HasProjects:         false,
	}
}

// DesiredWikiAllowlist lists repos that legitimately use the wiki
// feature and should keep has_wiki=true. Verified with
// `git ls-remote <repo>.wiki.git`: the entries here have actual wiki
// content; every other repo's wiki is empty noise. Returns a fresh map
// each call to keep the allowlist immutable from call sites.
func DesiredWikiAllowlist() map[string]struct{} {
	return map[string]struct{}{
		"guide": {},
	}
}

// DesiredReviewCount is the policy for classic branch protection. The
// ruleset handles force-push protection separately.
const DesiredReviewCount = 1

// DesiredRulesetName is the stable name the compliance ruleset must use.
// Phase 4 creates or updates a ruleset with this exact name so repeated
// runs are idempotent.
const DesiredRulesetName = "main-protection"

// DesiredDefaultBranch is the branch the ruleset targets and branch
// protection is applied to.
const DesiredDefaultBranch = "main"

// RepositoryAdminActorType / RepositoryAdminActorID identify the
// Repository Admin role for ruleset bypass. Keeping the owner in the
// bypass list means they can still force-push when needed.
const (
	RepositoryAdminActorType = "RepositoryRole"
	RepositoryAdminActorID   = 5
)
