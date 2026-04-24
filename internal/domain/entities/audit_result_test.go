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
