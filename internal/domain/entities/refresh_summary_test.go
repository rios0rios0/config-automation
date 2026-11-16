package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

func TestRefreshSummary(t *testing.T) {
	t.Parallel()

	t.Run("should print every bucket, with one failure per line", func(t *testing.T) {
		t.Parallel()
		// given
		summary := entities.NewRefreshSummary(4)
		summary.Processed = 3
		summary.NoDrift = append(summary.NoDrift, "fixture-owner/quiet")
		summary.PRsCreated = append(summary.PRsCreated, "fixture-owner/new (https://example.invalid/1)")
		summary.QuotaSkipped = append(summary.QuotaSkipped, "fixture-owner/late")
		summary.Failed = append(summary.Failed,
			entities.RefreshFailure{Reason: entities.FailureSecretShape, Target: "fixture-owner/leak"})

		// when
		lines := summary.Lines()

		// then
		assert.Equal(t, []string{
			"==== Batch summary ====",
			"processed (3/4)",
			"no_drift (1): fixture-owner/quiet",
			"prs_created (1): fixture-owner/new (https://example.invalid/1)",
			"prs_updated (0): ",
			"quota_skipped (1): fixture-owner/late",
			"safeguard_skipped (0): ",
			"failed (1):",
			"  - secret-shape: fixture-owner/leak",
		}, lines)
		assert.True(t, summary.HasFailures())
	})

	t.Run("should not count a skip as a failure", func(t *testing.T) {
		t.Parallel()
		// given
		summary := entities.NewRefreshSummary(2)
		summary.QuotaSkipped = append(summary.QuotaSkipped, "fixture-owner/late")
		summary.SafeguardSkipped = append(summary.SafeguardSkipped, "fixture-owner/refused")

		// when / then
		assert.False(t, summary.HasFailures())
	})
}
