package entities

// DesiredRepoSettings is the enforced policy for every `rios0rios0` repo
// (phase 2). Forks and private repos may have per-field carve-outs;
// AuditResult.ComputeIssues() encodes those. Exposed as a function so
// the policy stays immutable from call sites.
//
// The merge toggles encode a semi-linear history: a pull request is
// rebased onto the base branch first ("Update with rebase", which GitHub
// only offers while AllowRebaseMerge is on) and then landed with a merge
// commit, so `main` keeps one merge commit per pull request over an
// otherwise linear ancestry. AllowSquashMerge is off because squashing
// discards the branch's individual commits.
//
// AllowRebaseMerge stays on for one reason only: it is the toggle that
// surfaces "Update with rebase". It also surfaces "Rebase and merge",
// which fast-forwards without the merge commit and breaks the shape —
// GitHub gates both on the same flag, so the repo level cannot separate
// them. The fast-forward path is therefore blocked one level down, by
// DesiredAllowedMergeMethods() on the ruleset's pull_request rule, which
// narrows `main` to merge commits only while the repo-wide toggle stays
// on. Repo settings and the ruleset are two halves of one decision:
// changing either alone re-opens the fast-forward or kills the button.
//
// AllowUpdateBranch is "Always suggest updating pull request branches".
// Without it the "Update branch" control only appears when branch
// protection requires the branch be up to date before merging, which
// this policy does not require — so it is what makes the rebase-update
// button reliably present on every repo.
func DesiredRepoSettings() RepositorySettings {
	return RepositorySettings{
		DeleteBranchOnMerge: true,
		AllowAutoMerge:      true,
		AllowSquashMerge:    false,
		AllowRebaseMerge:    true,
		AllowMergeCommit:    true,
		AllowUpdateBranch:   true,
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

// DesiredForkActionsEnabled is the policy for the repository-level GitHub
// Actions switch on forks: off. A fork carries the upstream's workflows
// verbatim, so with Actions on, someone else's automation can run under
// this account — on its triggers, with this account's minutes and
// tokens. Nothing here needs a fork to run workflows: the compliance
// audit, the config-and-docs refresh, and the release reconciliation all
// skip forks. Non-forks are not subject to the rule (their workflows are
// our own); AuditResult.ComputeIssues() applies it and phase 3 flips the
// switch.
const DesiredForkActionsEnabled = false

// DesiredAllowedMergeMethods is the policy for the ruleset's
// pull_request rule: `main` accepts merge commits and nothing else.
// This is where "no fast-forward merges" is actually enforced —
// DesiredRepoSettings() deliberately leaves allow_rebase_merge on so the
// "Update with rebase" button survives, and a ruleset can only narrow
// what the repo already allows, so the two must be read together.
// Returns a fresh slice each call to keep the policy immutable at call
// sites. Values are GitHub's lowercase spelling (`merge`, `squash`,
// `rebase`) used by the pull_request rule — the merge_queue rule uses
// the uppercase spelling for the same concept, so do not share them.
func DesiredAllowedMergeMethods() []string {
	return []string{"merge"}
}

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
