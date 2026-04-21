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

func TestAuditRepositoriesCommand(t *testing.T) {
	t.Parallel()

	t.Run("should call OnSuccess with an audit per repo when the owner has repos", func(t *testing.T) {
		t.Parallel()
		// given
		repoA := builders.NewRepositoryBuilder().WithName("alpha").Build()
		repoB := builders.NewRepositoryBuilder().WithName("beta").Build()
		reposRepo := doubles.NewInMemoryRepositoriesRepository().WithRepos(repoA, repoB)
		securityRepo := doubles.NewInMemorySecuritySettingsRepository().
			WithSettings("alpha", entities.SecuritySettings{SecretScanning: "enabled", PushProtection: "enabled", DependabotUpdates: true}).
			WithSettings("beta", entities.SecuritySettings{SecretScanning: "enabled", PushProtection: "enabled", DependabotUpdates: true})
		protectionRepo := doubles.NewInMemoryBranchProtectionsRepository()
		command := commands.NewAuditRepositoriesCommand(reposRepo, securityRepo, protectionRepo)

		var received []entities.AuditResult
		var errored error

		// when
		command.Execute(context.TODO(), commands.AuditRepositoriesInput{Owner: "rios0rios0"}, commands.AuditRepositoriesListeners{
			OnSuccess: func(audits []entities.AuditResult) {
				received = audits
			},
			OnError: func(err error) {
				errored = err
			},
		})

		// then
		require.NoError(t, errored)
		require.Len(t, received, 2)
		assert.Equal(t, "alpha", received[0].Repository.Name)
		assert.Equal(t, "beta", received[1].Repository.Name)
	})

	t.Run("should narrow to one repo when RepoFilter is set", func(t *testing.T) {
		t.Parallel()
		// given
		repoA := builders.NewRepositoryBuilder().WithName("alpha").Build()
		repoB := builders.NewRepositoryBuilder().WithName("beta").Build()
		reposRepo := doubles.NewInMemoryRepositoriesRepository().WithRepos(repoA, repoB)
		securityRepo := doubles.NewInMemorySecuritySettingsRepository().
			WithSettings("beta", entities.SecuritySettings{SecretScanning: "enabled", PushProtection: "enabled", DependabotUpdates: true})
		command := commands.NewAuditRepositoriesCommand(reposRepo, securityRepo, doubles.NewInMemoryBranchProtectionsRepository())

		var received []entities.AuditResult

		// when
		command.Execute(context.TODO(), commands.AuditRepositoriesInput{Owner: "rios0rios0", RepoFilter: "beta"}, commands.AuditRepositoriesListeners{
			OnSuccess: func(audits []entities.AuditResult) { received = audits },
			OnError:   func(err error) { t.Fatalf("unexpected error: %v", err) },
		})

		// then
		require.Len(t, received, 1)
		assert.Equal(t, "beta", received[0].Repository.Name)
	})

	t.Run("should call OnError when the repository listing fails", func(t *testing.T) {
		t.Parallel()
		// given
		expected := errors.New("boom")
		reposRepo := doubles.NewInMemoryRepositoriesRepository()
		reposRepo.ErrorOnList = expected
		command := commands.NewAuditRepositoriesCommand(
			reposRepo,
			doubles.NewInMemorySecuritySettingsRepository(),
			doubles.NewInMemoryBranchProtectionsRepository(),
		)

		var received error

		// when
		command.Execute(context.TODO(), commands.AuditRepositoriesInput{Owner: "rios0rios0"}, commands.AuditRepositoriesListeners{
			OnSuccess: func(_ []entities.AuditResult) { t.Fatal("OnSuccess should not be called") },
			OnError:   func(err error) { received = err },
		})

		// then
		require.Error(t, received)
		assert.ErrorIs(t, received, expected)
	})

	t.Run("should record AuditError when security fetch fails for one repo", func(t *testing.T) {
		t.Parallel()
		// given
		repo := builders.NewRepositoryBuilder().WithName("alpha").Build()
		reposRepo := doubles.NewInMemoryRepositoriesRepository().WithRepos(repo)
		securityRepo := doubles.NewInMemorySecuritySettingsRepository()
		securityRepo.ErrorOnFind = errors.New("permission denied")
		command := commands.NewAuditRepositoriesCommand(reposRepo, securityRepo, doubles.NewInMemoryBranchProtectionsRepository())

		var received []entities.AuditResult

		// when
		command.Execute(context.TODO(), commands.AuditRepositoriesInput{Owner: "rios0rios0"}, commands.AuditRepositoriesListeners{
			OnSuccess: func(audits []entities.AuditResult) { received = audits },
			OnError:   func(err error) { t.Fatalf("unexpected error: %v", err) },
		})

		// then
		require.Len(t, received, 1)
		assert.Contains(t, received[0].AuditError, "permission denied")
	})
}
