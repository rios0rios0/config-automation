//go:build unit

package commands_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/commands"
	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/test/domain/builders"
)

func TestReportComplianceChangesCommand(t *testing.T) {
	t.Parallel()

	t.Run("should emit diffs when fields change between snapshots", func(t *testing.T) {
		t.Parallel()
		// given
		before := builders.NewAuditResultBuilder().
			WithRepository(builders.NewRepositoryBuilder().WithName("alpha").Build()).
			Build()
		// The default "before" builder repo has compliant settings; the
		// "after" repo changes two fields.
		afterSettings := before.Repository.Settings
		afterSettings.HasWiki = !afterSettings.HasWiki
		afterRepo := before.Repository
		afterRepo.Settings = afterSettings
		after := builders.NewAuditResultBuilder().
			WithRepository(afterRepo).
			Build()
		command := commands.NewReportComplianceChangesCommand()

		var diffs []commands.ComplianceDiff
		var reposChanged int

		// when
		command.Execute(commands.ReportComplianceChangesInput{
			Before: []entities.AuditResult{before},
			After:  []entities.AuditResult{after},
		}, commands.ReportComplianceChangesListeners{
			OnSuccess: func(d []commands.ComplianceDiff, r int) { diffs = d; reposChanged = r },
		})

		// then
		require.Len(t, diffs, 1)
		assert.Equal(t, "has_wiki", diffs[0].Field)
		assert.Equal(t, 1, reposChanged)
	})

	t.Run("should emit no diffs when snapshots are identical", func(t *testing.T) {
		t.Parallel()
		// given
		snapshot := builders.NewAuditResultBuilder().Build()
		command := commands.NewReportComplianceChangesCommand()

		var diffs []commands.ComplianceDiff
		var reposChanged int

		// when
		command.Execute(commands.ReportComplianceChangesInput{
			Before: []entities.AuditResult{snapshot},
			After:  []entities.AuditResult{snapshot},
		}, commands.ReportComplianceChangesListeners{
			OnSuccess: func(d []commands.ComplianceDiff, r int) { diffs = d; reposChanged = r },
		})

		// then
		assert.Empty(t, diffs)
		assert.Equal(t, 0, reposChanged)
	})
}
