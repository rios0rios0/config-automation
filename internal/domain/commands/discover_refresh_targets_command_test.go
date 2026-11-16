package commands_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/commands"
	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/test/domain/doubles/repositories"
)

// discovery is what one run of the discovery command reported.
type discovery struct {
	batches     []entities.RefreshBatch
	unreachable []commands.UnreachableOrganization
	refused     string
	reasons     []string
	err         error
}

// discover runs the command and collects what its listeners received.
func discover(t *testing.T, input commands.DiscoverRefreshTargetsInput) discovery {
	t.Helper()
	result := discovery{}
	commands.NewDiscoverRefreshTargetsCommand().Execute(t.Context(), input, commands.DiscoverRefreshTargetsListeners{
		OnSuccess: func(batches []entities.RefreshBatch, unreachable []commands.UnreachableOrganization) {
			result.batches, result.unreachable = batches, unreachable
		},
		OnRefused: func(target string, reasons []string) { result.refused, result.reasons = target, reasons },
		OnError:   func(err error) { result.err = err },
	})
	return result
}

// target returns an active repository of owner.
func target(owner, name string) entities.RefreshTarget {
	return entities.RefreshTarget{Organization: owner, Name: name, DefaultBranch: "main"}
}

func TestDiscoverRefreshTargetsCommand(t *testing.T) {
	t.Parallel()

	t.Run("should batch each organization's active repositories under its own credential", func(t *testing.T) {
		t.Parallel()
		// given
		frozen := target("first-owner", "frozen")
		frozen.Archived = true
		fork := target("first-owner", "fork")
		fork.Fork = true
		empty := entities.RefreshTarget{Organization: "first-owner", Name: "empty"}
		first := repositories.NewInMemoryRefreshTargetsRepository(
			target("first-owner", "b"), target("first-owner", "a"), frozen, fork, empty,
			target("first-owner", "excluded"))
		second := repositories.NewInMemoryRefreshTargetsRepository(target("second-owner", "a"))
		input := commands.DiscoverRefreshTargetsInput{
			Sources: []commands.RefreshTargetSource{
				{Organization: "first-owner", Credential: "FIRST_SECRET", Targets: first},
				{Organization: "second-owner", Credential: "SECOND_SECRET", Targets: second},
			},
			Exclude: []string{"first-owner/excluded"}, BatchSize: 1,
		}

		// when
		result := discover(t, input)

		// then
		require.NoError(t, result.err)
		require.Len(t, result.batches, 3)
		assert.Equal(t, "first-owner batch_01", result.batches[0].Name)
		assert.Equal(t, "a", result.batches[0].Targets[0].Name)
		assert.Equal(t, "first-owner batch_02", result.batches[1].Name)
		assert.Equal(t, "second-owner batch_01", result.batches[2].Name)
		assert.Equal(t, "SECOND_SECRET", result.batches[2].Credential)
		assert.Empty(t, result.unreachable)
	})

	t.Run("should record an unreachable organization and still batch the others", func(t *testing.T) {
		t.Parallel()
		// given
		broken := repositories.NewInMemoryRefreshTargetsRepository()
		broken.ErrorOnList = errors.New("401 Bad credentials")
		input := commands.DiscoverRefreshTargetsInput{
			Sources: []commands.RefreshTargetSource{
				{Organization: "broken-owner", Targets: broken},
				{Organization: "missing-owner", Unavailable: "no token"},
				{Organization: "working-owner", Targets: repositories.NewInMemoryRefreshTargetsRepository(
					target("working-owner", "a"))},
			},
			BatchSize: 10,
		}

		// when
		result := discover(t, input)

		// then
		require.NoError(t, result.err)
		assert.Equal(t, []commands.UnreachableOrganization{
			{Organization: "broken-owner", Reason: "401 Bad credentials"},
			{Organization: "missing-owner", Reason: "no token"},
		}, result.unreachable)
		require.Len(t, result.batches, 1)
		assert.Equal(t, "working-owner", result.batches[0].Organization)
	})

	t.Run("should reject a batch size below one", func(t *testing.T) {
		t.Parallel()
		// given / when
		result := discover(t, commands.DiscoverRefreshTargetsInput{BatchSize: 0})

		// then
		require.Error(t, result.err)
		assert.Nil(t, result.batches)
	})

	t.Run("should batch a single named repository of the first organization", func(t *testing.T) {
		t.Parallel()
		// given
		source := repositories.NewInMemoryRefreshTargetsRepository(target("fixture-owner", "chosen"))
		input := commands.DiscoverRefreshTargetsInput{
			Sources:   []commands.RefreshTargetSource{{Organization: "fixture-owner", Targets: source}},
			BatchSize: 10, Target: &entities.RefreshTargetReference{Name: "chosen"},
		}

		// when
		result := discover(t, input)

		// then
		require.NoError(t, result.err)
		require.Len(t, result.batches, 1)
		assert.Equal(t, "chosen", result.batches[0].Targets[0].Name)
		assert.Empty(t, source.Listed, "a single target listed the whole organization")
	})

	t.Run("should refuse a single repository the schedule leaves out", func(t *testing.T) {
		t.Parallel()
		for _, test := range []struct {
			name    string
			adjust  func(*entities.RefreshTarget)
			exclude []string
			reasons []string
		}{
			{"a fork", func(t *entities.RefreshTarget) { t.Fork = true }, nil, []string{"a fork"}},
			{"an archived repository", func(t *entities.RefreshTarget) { t.Archived = true }, nil, []string{"archived"}},
			{"a disabled repository", func(t *entities.RefreshTarget) { t.Disabled = true }, nil, []string{"disabled"}},
			{"an excluded repository", func(*entities.RefreshTarget) {}, []string{"fixture-owner/chosen"},
				[]string{"excluded by the configuration"}},
		} {
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()
				// given
				chosen := target("fixture-owner", "chosen")
				test.adjust(&chosen)
				input := commands.DiscoverRefreshTargetsInput{
					Sources: []commands.RefreshTargetSource{
						{
							Organization: "fixture-owner",
							Targets:      repositories.NewInMemoryRefreshTargetsRepository(chosen),
						},
					},
					Exclude: test.exclude, BatchSize: 10,
					Target: &entities.RefreshTargetReference{Organization: "fixture-owner", Name: "chosen"},
				}

				// when
				result := discover(t, input)

				// then
				assert.Equal(t, "fixture-owner/chosen", result.refused)
				assert.Equal(t, test.reasons, result.reasons)
				assert.Nil(t, result.batches)
			})
		}
	})

	t.Run("should fail a single repository of an organization that is not configured", func(t *testing.T) {
		t.Parallel()
		// given
		input := commands.DiscoverRefreshTargetsInput{
			Sources: []commands.RefreshTargetSource{{
				Organization: "fixture-owner", Targets: repositories.NewInMemoryRefreshTargetsRepository(),
			}},
			BatchSize: 10, Target: &entities.RefreshTargetReference{Organization: "other-owner", Name: "chosen"},
		}

		// when
		result := discover(t, input)

		// then
		require.Error(t, result.err)
		assert.Contains(t, result.err.Error(), `"other-owner" is not configured`)
	})
}
