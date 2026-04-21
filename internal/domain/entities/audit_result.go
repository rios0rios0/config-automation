package entities

import "fmt"

// AuditResult is the snapshot of one repository's current state plus the
// compliance issues computed from the policy. Phase 1 emits a slice of
// these; phases 2-4 consume them to decide what to mutate; phase 5
// compares a before/after pair.
type AuditResult struct {
	Repository       Repository
	Security         SecuritySettings
	BranchProtection BranchProtection
	Ruleset          *Ruleset
	AuditError       string
}

// HasForcePushRuleset reports whether the `main-protection` ruleset
// currently exists on the repo (regardless of whether its body is
// compliant).
func (a AuditResult) HasForcePushRuleset() bool {
	return a.Ruleset != nil
}

// ComputeIssues returns the list of non-compliance strings for this
// audit. Fork and private carve-outs mirror the Python original:
//
//   - forks skip Dependabot and secret scanning (upstream syncs wipe them).
//   - private repos on GitHub Free skip allow_auto_merge (silent noop),
//     secret scanning, branch protection, and the ruleset.
//   - the wiki setting is skipped for repos in DesiredWikiAllowlist.
func (a AuditResult) ComputeIssues() []string {
	if a.AuditError != "" {
		return []string{fmt.Sprintf("audit_error: %s", a.AuditError)}
	}

	issues := a.repoSettingsIssues()

	if !a.Repository.Fork {
		issues = append(issues, a.dependabotIssues()...)
	}

	// Public-only enforcement: secret scanning, branch protection, ruleset.
	if a.Repository.Private || !a.BranchProtection.Available {
		return issues
	}

	if !a.Repository.Fork {
		issues = append(issues, a.secretScanningIssues()...)
	}

	issues = append(issues, a.branchProtectionIssues()...)
	issues = append(issues, a.rulesetIssues()...)
	return issues
}

func (a AuditResult) repoSettingsIssues() []string {
	settings := a.Repository.Settings
	policy := DesiredRepoSettings()
	issues := []string{}

	issues = append(
		issues,
		checkSetting("delete_branch_on_merge", settings.DeleteBranchOnMerge, policy.DeleteBranchOnMerge, false)...)
	// allow_auto_merge is a Team feature; GitHub Free silently ignores the
	// PATCH on private repos, so only skip that specific unfixable case.
	skipAutoMerge := a.Repository.Private && policy.AllowAutoMerge && !settings.AllowAutoMerge
	if !skipAutoMerge {
		issues = append(
			issues,
			checkSetting("allow_auto_merge", settings.AllowAutoMerge, policy.AllowAutoMerge, false)...)
	}
	issues = append(
		issues,
		checkSetting("allow_squash_merge", settings.AllowSquashMerge, policy.AllowSquashMerge, false)...)
	issues = append(
		issues,
		checkSetting("allow_rebase_merge", settings.AllowRebaseMerge, policy.AllowRebaseMerge, false)...)
	issues = append(
		issues,
		checkSetting("allow_merge_commit", settings.AllowMergeCommit, policy.AllowMergeCommit, false)...)

	if _, wikiAllowed := DesiredWikiAllowlist()[a.Repository.Name]; !wikiAllowed {
		issues = append(issues, checkSetting("has_wiki", settings.HasWiki, policy.HasWiki, false)...)
	}
	issues = append(issues, checkSetting("has_projects", settings.HasProjects, policy.HasProjects, false)...)
	return issues
}

func (a AuditResult) dependabotIssues() []string {
	issues := []string{}
	switch a.Security.DependabotAlertsState() {
	case SecurityStateUnknown:
		issues = append(issues, "dependabot_alerts=unknown")
	case SecurityStateDisabled:
		issues = append(issues, "dependabot_alerts=off")
	}
	if !a.Security.DependabotUpdates {
		issues = append(issues, "dependabot_updates=off")
	}
	return issues
}

func (a AuditResult) secretScanningIssues() []string {
	issues := []string{}
	if !a.Security.IsSecretScanningEnabled() {
		issues = append(issues, fmt.Sprintf("secret_scanning=%s", stateOrEmpty(a.Security.SecretScanning)))
	}
	if !a.Security.IsPushProtectionEnabled() {
		issues = append(issues, fmt.Sprintf("push_protection=%s", stateOrEmpty(a.Security.PushProtection)))
	}
	return issues
}

func (a AuditResult) branchProtectionIssues() []string {
	if !a.BranchProtection.Enabled {
		return []string{"branch_protection=off"}
	}
	issues := []string{}
	if a.BranchProtection.ReviewCount != DesiredReviewCount {
		issues = append(issues, fmt.Sprintf("prot_review_count=%d", a.BranchProtection.ReviewCount))
	}
	if !a.BranchProtection.DismissStaleReviews {
		issues = append(issues, "prot_dismiss_stale=off")
	}
	if !a.BranchProtection.ConversationResolution {
		issues = append(issues, "prot_conversation_resolution=off")
	}
	if a.BranchProtection.Signatures == nil || !*a.BranchProtection.Signatures {
		issues = append(issues, "prot_signatures=off")
	}
	return issues
}

func (a AuditResult) rulesetIssues() []string {
	if a.Ruleset == nil {
		return []string{"ruleset_non_fast_forward=missing"}
	}
	issues := []string{}
	if !a.Ruleset.HasNonFastForward {
		issues = append(issues, "ruleset_non_fast_forward=rule_missing")
	}
	if !a.Ruleset.TargetsMain {
		issues = append(issues, "ruleset_targets_main=missing")
	}
	if !a.Ruleset.AdminBypass {
		issues = append(issues, "ruleset_admin_bypass=missing")
	}
	return issues
}

// IsCompliant reports whether this audit has zero issues.
func (a AuditResult) IsCompliant() bool {
	return len(a.ComputeIssues()) == 0
}

func checkSetting(field string, current, want, _ bool) []string {
	if current == want {
		return nil
	}
	return []string{fmt.Sprintf("%s=%t(want %t)", field, current, want)}
}

func stateOrEmpty(s string) string {
	if s == "" {
		return "N/A"
	}
	return s
}
