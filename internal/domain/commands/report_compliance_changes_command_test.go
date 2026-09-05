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

// auditForOwnedRepo builds a compliant audit for `owner/name` with only
// has_wiki varied, so cross-owner tests can differ in exactly one field.
func auditForOwnedRepo(owner, name string, hasWiki bool) entities.AuditResult {
	settings := entities.DesiredRepoSettings()
	settings.HasWiki = hasWiki
	return builders.NewAuditResultBuilder().
		WithRepository(builders.NewRepositoryBuilder().
			WithOwner(owner).
			WithName(name).
			WithSettings(settings).
			Build()).
		Build()
}

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

	t.Run("should emit no diffs when two owners share a repo name and neither changed", func(t *testing.T) {
		t.Parallel()
		// given
		// Same name under two owners, differing only in has_wiki, and
		// neither snapshot moved between before and after. Keyed on the
		// bare name the two collapse into one entry, so one owner's repo
		// gets diffed against the other's and a phantom has_wiki change is
		// reported for a fleet that did not drift at all.
		mine := auditForOwnedRepo("rios0rios0", "guide", false)
		theirs := auditForOwnedRepo("prefy", "guide", true)
		command := commands.NewReportComplianceChangesCommand()

		var diffs []commands.ComplianceDiff
		var reposChanged int

		// when
		command.Execute(commands.ReportComplianceChangesInput{
			Before: []entities.AuditResult{mine, theirs},
			After:  []entities.AuditResult{mine, theirs},
		}, commands.ReportComplianceChangesListeners{
			OnSuccess: func(d []commands.ComplianceDiff, r int) { diffs = d; reposChanged = r },
		})

		// then
		assert.Empty(t, diffs)
		assert.Equal(t, 0, reposChanged)
	})

	t.Run("should attribute the diff to the owner that changed when two owners share a repo name", func(t *testing.T) {
		t.Parallel()
		// given
		mine := auditForOwnedRepo("rios0rios0", "guide", false)
		theirsBefore := auditForOwnedRepo("prefy", "guide", false)
		theirsAfter := auditForOwnedRepo("prefy", "guide", true)
		command := commands.NewReportComplianceChangesCommand()

		var diffs []commands.ComplianceDiff
		var reposChanged int

		// when
		command.Execute(commands.ReportComplianceChangesInput{
			Before: []entities.AuditResult{mine, theirsBefore},
			After:  []entities.AuditResult{mine, theirsAfter},
		}, commands.ReportComplianceChangesListeners{
			OnSuccess: func(d []commands.ComplianceDiff, r int) { diffs = d; reposChanged = r },
		})

		// then
		require.Len(t, diffs, 1)
		assert.Equal(t, "prefy/guide", diffs[0].Repository)
		assert.Equal(t, "has_wiki", diffs[0].Field)
		assert.Equal(t, 1, reposChanged)
	})

	t.Run("should emit an actions_enabled diff when a fork's Actions were switched off between snapshots", func(t *testing.T) {
		t.Parallel()
		// given
		forkRepo := builders.NewRepositoryBuilder().WithName("forked").AsFork().WithCompliantSettings().Build()
		before := builders.NewAuditResultBuilder().WithRepository(forkRepo).WithActionsEnabled(true).Build()
		after := builders.NewAuditResultBuilder().WithRepository(forkRepo).WithActionsEnabled(false).Build()
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
		assert.Equal(t, "rios0rios0/forked", diffs[0].Repository)
		assert.Equal(t, "actions_enabled", diffs[0].Field)
		assert.Equal(t, entities.SecurityStateEnabled, diffs[0].Before)
		assert.Equal(t, entities.SecurityStateDisabled, diffs[0].After)
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
