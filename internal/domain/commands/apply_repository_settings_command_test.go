//go:build unit

package commands_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/fleet-maintenance/internal/domain/commands"
	"github.com/rios0rios0/fleet-maintenance/internal/domain/entities"
	"github.com/rios0rios0/fleet-maintenance/test/domain/builders"
	doubles "github.com/rios0rios0/fleet-maintenance/test/domain/doubles/repositories"
)

func TestApplyRepositorySettingsCommand(t *testing.T) {
	t.Parallel()

	t.Run("should call Save when a repo's settings drift from the policy", func(t *testing.T) {
		t.Parallel()
		// given
		drift := entities.RepositorySettings{
			DeleteBranchOnMerge: false,
			AllowAutoMerge:      false,
			AllowSquashMerge:    true,
			AllowRebaseMerge:    true,
			AllowMergeCommit:    true,
			HasWiki:             true,
			HasProjects:         true,
		}
		audit := builders.NewAuditResultBuilder().
			WithRepository(builders.NewRepositoryBuilder().WithName("drifted").WithSettings(drift).Build()).
			Build()
		reposRepo := doubles.NewInMemoryRepositoriesRepository().WithRepos(audit.Repository)
		command := commands.NewApplyRepositorySettingsCommand(reposRepo)

		var changes []commands.ApplyRepositorySettingsChange
		var changed, compliant int

		// when
		command.Execute(context.TODO(), commands.ApplyRepositorySettingsInput{
			Owner:  "rios0rios0",
			Audits: []entities.AuditResult{audit},
		}, commands.ApplyRepositorySettingsListeners{
			OnChange:  func(c commands.ApplyRepositorySettingsChange) { changes = append(changes, c) },
			OnSuccess: func(c, cp int) { changed = c; compliant = cp },
			OnError:   func(name string, err error) { t.Fatalf("unexpected error on %s: %v", name, err) },
		})

		// then
		require.Len(t, reposRepo.Saves, 1)
		assert.Equal(t, entities.DesiredRepoSettings, reposRepo.Saves[0].Settings)
		require.Len(t, changes, 1)
		assert.True(t, changes[0].Applied)
		assert.Equal(t, 1, changed)
		assert.Equal(t, 0, compliant)
	})

	t.Run("should skip Save when a repo is already compliant", func(t *testing.T) {
		t.Parallel()
		// given
		audit := builders.NewAuditResultBuilder().Build()
		reposRepo := doubles.NewInMemoryRepositoriesRepository().WithRepos(audit.Repository)
		command := commands.NewApplyRepositorySettingsCommand(reposRepo)

		// when
		var changed, compliant int
		command.Execute(context.TODO(), commands.ApplyRepositorySettingsInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}}, commands.ApplyRepositorySettingsListeners{
			OnSuccess: func(c, cp int) { changed = c; compliant = cp },
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Empty(t, reposRepo.Saves)
		assert.Equal(t, 0, changed)
		assert.Equal(t, 1, compliant)
	})

	t.Run("should keep current HasWiki for repos in the allowlist", func(t *testing.T) {
		t.Parallel()
		// given — the allowlist currently holds "guide"
		settings := entities.DesiredRepoSettings
		settings.HasWiki = true
		audit := builders.NewAuditResultBuilder().
			WithRepository(builders.NewRepositoryBuilder().WithName("guide").WithSettings(settings).Build()).
			Build()
		reposRepo := doubles.NewInMemoryRepositoriesRepository().WithRepos(audit.Repository)
		command := commands.NewApplyRepositorySettingsCommand(reposRepo)

		// when
		command.Execute(context.TODO(), commands.ApplyRepositorySettingsInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}}, commands.ApplyRepositorySettingsListeners{
			OnSuccess: func(_, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Empty(t, reposRepo.Saves, "wiki allowlist should prevent any PATCH")
	})

	t.Run("should keep current AllowAutoMerge for private repos when target is true", func(t *testing.T) {
		t.Parallel()
		// given — private repo with AutoMerge=false; policy says true, but
		// Free plan silently ignores the PATCH so we leave it alone.
		settings := entities.DesiredRepoSettings
		settings.AllowAutoMerge = false
		repo := builders.NewRepositoryBuilder().WithName("secret").AsPrivate().WithSettings(settings).Build()
		audit := builders.NewAuditResultBuilder().WithRepository(repo).Build()
		reposRepo := doubles.NewInMemoryRepositoriesRepository().WithRepos(audit.Repository)
		command := commands.NewApplyRepositorySettingsCommand(reposRepo)

		// when
		command.Execute(context.TODO(), commands.ApplyRepositorySettingsInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}}, commands.ApplyRepositorySettingsListeners{
			OnSuccess: func(_, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Empty(t, reposRepo.Saves)
	})

	t.Run("should skip Save when DryRun is set", func(t *testing.T) {
		t.Parallel()
		// given
		drift := entities.RepositorySettings{HasWiki: true}
		audit := builders.NewAuditResultBuilder().
			WithRepository(builders.NewRepositoryBuilder().WithName("alpha").WithSettings(drift).Build()).
			Build()
		reposRepo := doubles.NewInMemoryRepositoriesRepository().WithRepos(audit.Repository)
		command := commands.NewApplyRepositorySettingsCommand(reposRepo)

		// when
		var changes []commands.ApplyRepositorySettingsChange
		command.Execute(context.TODO(), commands.ApplyRepositorySettingsInput{
			Owner: "rios0rios0", Audits: []entities.AuditResult{audit}, DryRun: true,
		}, commands.ApplyRepositorySettingsListeners{
			OnChange:  func(c commands.ApplyRepositorySettingsChange) { changes = append(changes, c) },
			OnSuccess: func(_, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Empty(t, reposRepo.Saves, "dry run must not PATCH")
		require.Len(t, changes, 1)
		assert.False(t, changes[0].Applied)
	})

	t.Run("should call OnError when the Save operation fails", func(t *testing.T) {
		t.Parallel()
		// given
		drift := entities.RepositorySettings{HasWiki: true}
		audit := builders.NewAuditResultBuilder().
			WithRepository(builders.NewRepositoryBuilder().WithName("alpha").WithSettings(drift).Build()).
			Build()
		reposRepo := doubles.NewInMemoryRepositoriesRepository().WithRepos(audit.Repository)
		reposRepo.ErrorOnSave = errors.New("network")
		command := commands.NewApplyRepositorySettingsCommand(reposRepo)

		// when
		var receivedErr error
		command.Execute(context.TODO(), commands.ApplyRepositorySettingsInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}}, commands.ApplyRepositorySettingsListeners{
			OnSuccess: func(_, _ int) {},
			OnError:   func(_ string, err error) { receivedErr = err },
		})

		// then
		require.Error(t, receivedErr)
	})
}
