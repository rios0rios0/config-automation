package commands

import "github.com/rios0rios0/config-automation/internal/domain/entities"

// ReportComplianceChangesCommand runs phase 5: given a "before" audit
// and an "after" audit, it lists the per-field diffs so the operator
// can see what the previous mutation phases actually changed.
type ReportComplianceChangesCommand struct{}

// NewReportComplianceChangesCommand is the Dig-injectable constructor.
func NewReportComplianceChangesCommand() *ReportComplianceChangesCommand {
	return &ReportComplianceChangesCommand{}
}

// ReportComplianceChangesInput takes two snapshots of the same repos.
type ReportComplianceChangesInput struct {
	Before []entities.AuditResult
	After  []entities.AuditResult
}

// ComplianceDiff is one changed field on one repo.
type ComplianceDiff struct {
	Repository string
	Field      string
	Before     string
	After      string
}

// ReportComplianceChangesListeners receives the computed diffs.
type ReportComplianceChangesListeners struct {
	OnSuccess func(diffs []ComplianceDiff, reposChanged int)
}

// Execute computes per-repo field diffs across the tracked attributes.
func (c ReportComplianceChangesCommand) Execute(
	input ReportComplianceChangesInput,
	listeners ReportComplianceChangesListeners,
) {
	beforeByName := make(map[string]entities.AuditResult, len(input.Before))
	for _, a := range input.Before {
		beforeByName[a.Repository.Name] = a
	}

	diffs := make([]ComplianceDiff, 0)
	reposChanged := map[string]struct{}{}

	for _, after := range input.After {
		before, ok := beforeByName[after.Repository.Name]
		if !ok {
			continue
		}
		repoDiffs := diffAudits(before, after)
		if len(repoDiffs) == 0 {
			continue
		}
		reposChanged[after.Repository.Name] = struct{}{}
		diffs = append(diffs, repoDiffs...)
	}

	listeners.OnSuccess(diffs, len(reposChanged))
}

func diffAudits(before, after entities.AuditResult) []ComplianceDiff {
	diffs := make([]ComplianceDiff, 0)
	diffs = append(diffs, diffRepoSettings(before, after)...)
	diffs = append(diffs, diffSecurity(before, after)...)
	diffs = append(diffs, diffBranchProtection(before, after)...)
	diffs = append(diffs, diffRuleset(before, after)...)
	return diffs
}

func diffRepoSettings(before, after entities.AuditResult) []ComplianceDiff {
	name := after.Repository.Name
	diffs := make([]ComplianceDiff, 0)
	diffs = appendBoolDiff(diffs, name, "delete_branch_on_merge",
		before.Repository.Settings.DeleteBranchOnMerge, after.Repository.Settings.DeleteBranchOnMerge)
	diffs = appendBoolDiff(diffs, name, "allow_auto_merge",
		before.Repository.Settings.AllowAutoMerge, after.Repository.Settings.AllowAutoMerge)
	diffs = appendBoolDiff(diffs, name, "has_wiki",
		before.Repository.Settings.HasWiki, after.Repository.Settings.HasWiki)
	diffs = appendBoolDiff(diffs, name, "has_projects",
		before.Repository.Settings.HasProjects, after.Repository.Settings.HasProjects)
	return diffs
}

func diffSecurity(before, after entities.AuditResult) []ComplianceDiff {
	name := after.Repository.Name
	diffs := make([]ComplianceDiff, 0)
	diffs = appendStringDiff(diffs, name, "secret_scanning",
		before.Security.SecretScanning, after.Security.SecretScanning)
	diffs = appendStringDiff(diffs, name, "push_protection",
		before.Security.PushProtection, after.Security.PushProtection)
	diffs = appendStringDiff(diffs, name, "dependabot_alerts",
		before.Security.DependabotAlertsState(), after.Security.DependabotAlertsState())
	diffs = appendBoolDiff(diffs, name, "dependabot_updates",
		before.Security.DependabotUpdates, after.Security.DependabotUpdates)
	return diffs
}

func diffBranchProtection(before, after entities.AuditResult) []ComplianceDiff {
	name := after.Repository.Name
	diffs := make([]ComplianceDiff, 0)
	diffs = appendBoolDiff(diffs, name, "protection_enabled",
		before.BranchProtection.Enabled, after.BranchProtection.Enabled)
	diffs = appendBoolDiff(diffs, name, "prot_dismiss_stale",
		before.BranchProtection.DismissStaleReviews, after.BranchProtection.DismissStaleReviews)
	diffs = appendBoolDiff(diffs, name, "prot_conversation_resolution",
		before.BranchProtection.ConversationResolution, after.BranchProtection.ConversationResolution)
	diffs = appendStringDiff(diffs, name, "prot_signatures",
		before.BranchProtection.SignaturesState(), after.BranchProtection.SignaturesState())
	return diffs
}

func diffRuleset(before, after entities.AuditResult) []ComplianceDiff {
	name := after.Repository.Name
	diffs := make([]ComplianceDiff, 0)
	diffs = appendBoolDiff(diffs, name, "has_force_push_ruleset",
		before.HasForcePushRuleset(), after.HasForcePushRuleset())
	if before.Ruleset != nil && after.Ruleset != nil {
		diffs = appendBoolDiff(diffs, name, "ruleset_admin_bypass",
			before.Ruleset.AdminBypass, after.Ruleset.AdminBypass)
	}
	return diffs
}

func appendBoolDiff(diffs []ComplianceDiff, repo, field string, before, after bool) []ComplianceDiff {
	if before == after {
		return diffs
	}
	return append(diffs, ComplianceDiff{
		Repository: repo, Field: field,
		Before: boolStr(before), After: boolStr(after),
	})
}

func appendStringDiff(diffs []ComplianceDiff, repo, field, before, after string) []ComplianceDiff {
	if before == after {
		return diffs
	}
	return append(diffs, ComplianceDiff{
		Repository: repo, Field: field,
		Before: before, After: after,
	})
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
