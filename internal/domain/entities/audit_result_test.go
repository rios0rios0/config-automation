//go:build unit

package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/test/domain/builders"
)

func TestAuditResultComputeIssues(t *testing.T) {
	t.Parallel()

	t.Run("should return no issues for a fully compliant public repo", func(t *testing.T) {
		t.Parallel()
		// given
		audit := builders.NewAuditResultBuilder().Build()

		// when
		issues := audit.ComputeIssues()

		// then
		assert.Empty(t, issues)
	})

	t.Run("should flag has_wiki drift unless the repo is allowlisted", func(t *testing.T) {
		t.Parallel()
		// given
		settings := entities.DesiredRepoSettings()
		settings.HasWiki = true
		repo := builders.NewRepositoryBuilder().WithName("not-in-allowlist").WithSettings(settings).Build()
		audit := builders.NewAuditResultBuilder().WithRepository(repo).Build()

		// when
		issues := audit.ComputeIssues()

		// then
		assert.Contains(t, issues, "has_wiki=true(want false)")
	})

	t.Run("should skip has_wiki for allowlisted repos", func(t *testing.T) {
		t.Parallel()
		// given
		settings := entities.DesiredRepoSettings()
		settings.HasWiki = true
		repo := builders.NewRepositoryBuilder().WithName("guide").WithSettings(settings).Build()
		audit := builders.NewAuditResultBuilder().WithRepository(repo).Build()

		// when
		issues := audit.ComputeIssues()

		// then
		assert.Empty(t, issues)
	})

	t.Run("should skip secret scanning on private repos", func(t *testing.T) {
		t.Parallel()
		// given
		privateRepo := builders.NewRepositoryBuilder().WithName("secret").AsPrivate().WithSettings(entities.DesiredRepoSettings()).Build()
		disabled := true
		audit := builders.NewAuditResultBuilder().
			WithRepository(privateRepo).
			WithSecurity(entities.SecuritySettings{
				SecretScanning:    "",
				PushProtection:    "",
				DependabotAlerts:  &disabled,
				DependabotUpdates: true,
			}).
			WithBranchProtection(entities.BranchProtection{Available: true, Enabled: true, ReviewCount: entities.DesiredReviewCount, DismissStaleReviews: true, ConversationResolution: true}).
			Build()

		// when
		issues := audit.ComputeIssues()

		// then
		for _, i := range issues {
			assert.NotContains(t, i, "secret_scanning")
			assert.NotContains(t, i, "push_protection")
		}
	})

	t.Run("should skip Dependabot on forks", func(t *testing.T) {
		t.Parallel()
		// given
		forkRepo := builders.NewRepositoryBuilder().WithName("forked").AsFork().WithSettings(entities.DesiredRepoSettings()).Build()
		audit := builders.NewAuditResultBuilder().
			WithRepository(forkRepo).
			WithSecurity(entities.SecuritySettings{DependabotUpdates: false}). // alerts unknown, updates off
			Build()

		// when
		issues := audit.ComputeIssues()

		// then
		for _, i := range issues {
			assert.NotContains(t, i, "dependabot_alerts")
			assert.NotContains(t, i, "dependabot_updates")
		}
	})

	t.Run("should flag actions_enabled when a fork still runs GitHub Actions", func(t *testing.T) {
		t.Parallel()
		// given — a fork compliant in every other way, with the
		// repository-level Actions switch left on.
		forkRepo := builders.NewRepositoryBuilder().WithName("forked").AsFork().WithCompliantSettings().Build()
		audit := builders.NewAuditResultBuilder().WithRepository(forkRepo).WithActionsEnabled(true).Build()

		// when
		issues := audit.ComputeIssues()

		// then
		assert.Equal(t, []string{"actions_enabled=true(want false)"}, issues)
	})

	t.Run("should return no issues when a fork has GitHub Actions disabled", func(t *testing.T) {
		t.Parallel()
		// given
		forkRepo := builders.NewRepositoryBuilder().WithName("forked").AsFork().WithCompliantSettings().Build()
		audit := builders.NewAuditResultBuilder().WithRepository(forkRepo).WithActionsEnabled(false).Build()

		// when
		issues := audit.ComputeIssues()

		// then
		assert.Empty(t, issues)
	})

	t.Run("should not check actions_enabled when the repo is not a fork", func(t *testing.T) {
		t.Parallel()
		// given — a repo of our own runs its workflows; Actions on is the
		// expected state there, not drift.
		audit := builders.NewAuditResultBuilder().WithActionsEnabled(true).Build()

		// when
		issues := audit.ComputeIssues()

		// then
		assert.Empty(t, issues)
	})

	t.Run("should report actions_enabled=unknown when a fork's Actions state could not be read", func(t *testing.T) {
		t.Parallel()
		// given — nil mirrors dependabot_alerts: an API failure is drift to
		// look at, not compliance.
		forkRepo := builders.NewRepositoryBuilder().WithName("forked").AsFork().WithCompliantSettings().Build()
		audit := builders.NewAuditResultBuilder().WithRepository(forkRepo).WithActionsUnknown().Build()

		// when
		issues := audit.ComputeIssues()

		// then
		assert.Equal(t, []string{"actions_enabled=unknown"}, issues)
	})

	t.Run("should flag allow_update_branch when the rebase-update button is off", func(t *testing.T) {
		t.Parallel()
		// given — a repo compliant in every other way, with GitHub's default
		// "Always suggest updating pull request branches" left off.
		settings := entities.DesiredRepoSettings()
		settings.AllowUpdateBranch = false
		repo := builders.NewRepositoryBuilder().WithSettings(settings).Build()
		audit := builders.NewAuditResultBuilder().WithRepository(repo).Build()

		// when
		issues := audit.ComputeIssues()

		// then
		assert.Contains(t, issues, "allow_update_branch=false(want true)")
	})

	t.Run("should flag a ruleset whose merge methods still allow rebase", func(t *testing.T) {
		t.Parallel()
		// given — non_fast_forward is present, so only the merge-method check
		// can catch the open fast-forward merge path.
		audit := builders.NewAuditResultBuilder().
			WithRuleset(&entities.Ruleset{
				ID:                  1,
				Name:                entities.DesiredRulesetName,
				Enforcement:         "active",
				HasNonFastForward:   true,
				AllowedMergeMethods: []string{"merge", "rebase"},
				TargetsMain:         true,
				AdminBypass:         true,
			}).
			Build()

		// when
		issues := audit.ComputeIssues()

		// then
		assert.Contains(t, issues, "ruleset_allowed_merge_methods=merge+rebase(want merge)")
	})

	t.Run("should report rule_missing when the ruleset has no pull_request rule", func(t *testing.T) {
		t.Parallel()
		// given
		audit := builders.NewAuditResultBuilder().
			WithRuleset(&entities.Ruleset{
				ID:                1,
				Name:              entities.DesiredRulesetName,
				Enforcement:       "active",
				HasNonFastForward: true,
				TargetsMain:       true,
				AdminBypass:       true,
			}).
			Build()

		// when
		issues := audit.ComputeIssues()

		// then
		assert.Contains(t, issues, "ruleset_allowed_merge_methods=rule_missing(want merge)")
	})

	t.Run("should skip ruleset merge methods on private repos", func(t *testing.T) {
		t.Parallel()
		// given — rulesets need GitHub Pro on private repos, so the whole
		// ruleset block is carved out there.
		privateRepo := builders.NewRepositoryBuilder().
			WithName("closed").
			AsPrivate().
			WithSettings(entities.DesiredRepoSettings()).
			Build()
		audit := builders.NewAuditResultBuilder().
			WithRepository(privateRepo).
			WithoutRuleset().
			Build()

		// when
		issues := audit.ComputeIssues()

		// then
		for _, i := range issues {
			assert.NotContains(t, i, "ruleset_")
		}
	})

	t.Run("should distinguish dependabot_alerts=unknown from =off", func(t *testing.T) {
		t.Parallel()
		// given — public repo with alerts=nil (API failure).
		publicRepo := builders.NewRepositoryBuilder().WithSettings(entities.DesiredRepoSettings()).Build()
		audit := builders.NewAuditResultBuilder().
			WithRepository(publicRepo).
			WithSecurity(entities.SecuritySettings{
				SecretScanning:    "enabled",
				PushProtection:    "enabled",
				DependabotUpdates: true,
			}).
			Build()

		// when
		issues := audit.ComputeIssues()

		// then
		assert.Contains(t, issues, "dependabot_alerts=unknown")
	})
}
