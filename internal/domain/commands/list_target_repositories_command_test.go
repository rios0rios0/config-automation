//go:build unit

package commands_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/fleet-maintenance/internal/domain/commands"
	"github.com/rios0rios0/fleet-maintenance/internal/domain/entities"
	"github.com/rios0rios0/fleet-maintenance/test/domain/builders"
	doubles "github.com/rios0rios0/fleet-maintenance/test/domain/doubles/repositories"
)

func TestListTargetRepositoriesCommand(t *testing.T) {
	t.Parallel()

	t.Run("should return non-fork non-archived repos only", func(t *testing.T) {
		t.Parallel()
		// given
		live := builders.NewRepositoryBuilder().WithName("live").Build()
		fork := builders.NewRepositoryBuilder().WithName("forked").AsFork().Build()
		archived := builders.NewRepositoryBuilder().WithName("cold").AsArchived().Build()
		reposRepo := doubles.NewInMemoryRepositoriesRepository().WithRepos(live, fork, archived)
		command := commands.NewListTargetRepositoriesCommand(reposRepo)

		var received []entities.Repository

		// when
		command.Execute(context.TODO(), commands.ListTargetRepositoriesInput{Owner: "rios0rios0"}, commands.ListTargetRepositoriesListeners{
			OnSuccess: func(repos []entities.Repository) { received = repos },
			OnError:   func(err error) { t.Fatalf("unexpected error: %v", err) },
		})

		// then
		require.Len(t, received, 1)
		assert.Equal(t, "live", received[0].Name)
	})

	t.Run("should return an empty slice when the owner has no live repos", func(t *testing.T) {
		t.Parallel()
		// given
		fork := builders.NewRepositoryBuilder().WithName("forked").AsFork().Build()
		reposRepo := doubles.NewInMemoryRepositoriesRepository().WithRepos(fork)
		command := commands.NewListTargetRepositoriesCommand(reposRepo)

		var received []entities.Repository

		// when
		command.Execute(context.TODO(), commands.ListTargetRepositoriesInput{Owner: "rios0rios0"}, commands.ListTargetRepositoriesListeners{
			OnSuccess: func(repos []entities.Repository) { received = repos },
			OnError:   func(err error) { t.Fatalf("unexpected error: %v", err) },
		})

		// then
		assert.Empty(t, received)
	})
}
