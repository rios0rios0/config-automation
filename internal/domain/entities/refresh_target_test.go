package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

func TestRefreshTarget(t *testing.T) {
	t.Parallel()

	t.Run("should qualify a repository by its project when it has one", func(t *testing.T) {
		t.Parallel()
		// given
		github := entities.RefreshTarget{Organization: "fixture-owner", Name: "frontend"}
		azure := entities.RefreshTarget{Organization: "FixtureOrg", Project: "Fixture Project", Name: "frontend"}

		// when / then
		assert.Equal(t, "fixture-owner/frontend", github.QualifiedName())
		assert.Equal(t, "Fixture Project/frontend", azure.QualifiedName())
	})

	t.Run("should build a work key no two targets share and that holds no separator", func(t *testing.T) {
		t.Parallel()
		// given
		first := entities.RefreshTarget{Organization: "fixture-owner", Name: "frontend"}
		second := entities.RefreshTarget{Organization: "other-owner", Name: "frontend"}
		nested := entities.RefreshTarget{Organization: "FixtureOrg", Project: "A/B", Name: `c\d`}

		// when / then
		assert.Equal(t, "fixture-owner__frontend", first.WorkKey())
		assert.NotEqual(t, first.WorkKey(), second.WorkKey())
		assert.Equal(t, "FixtureOrg__A_B__c_d", nested.WorkKey())
	})

	t.Run("should explain every reason a repository is left out", func(t *testing.T) {
		t.Parallel()
		// given
		target := entities.RefreshTarget{Name: "frozen", Fork: true, Archived: true, Disabled: true}

		// when
		reasons := target.ExclusionReasons()

		// then
		assert.Equal(t, []string{"a fork", "archived", "disabled", "empty"}, reasons)
		assert.Empty(t, entities.RefreshTarget{Name: "active", DefaultBranch: "main"}.ExclusionReasons())
	})
}

func TestChunkRefreshTargets(t *testing.T) {
	t.Parallel()

	t.Run("should sort the targets and chunk them into named batches of the given size", func(t *testing.T) {
		t.Parallel()
		// given
		targets := []entities.RefreshTarget{
			{Organization: "fixture-owner", Name: "c"}, {Organization: "fixture-owner", Name: "a"},
			{Organization: "fixture-owner", Name: "b"},
		}

		// when
		batches := entities.ChunkRefreshTargets("fixture-owner", "FIXTURE_SECRET", targets, 2)

		// then
		assert.Len(t, batches, 2)
		assert.Equal(t, "fixture-owner batch_01", batches[0].Name)
		assert.Equal(t, "fixture-owner batch_02", batches[1].Name)
		assert.Equal(t, "FIXTURE_SECRET", batches[1].Credential)
		assert.Equal(t, "a", batches[0].Targets[0].Name)
		assert.Equal(t, "b", batches[0].Targets[1].Name)
		assert.Equal(t, "c", batches[1].Targets[0].Name)
		assert.Equal(t, "c", targets[0].Name, "the caller's slice was reordered")
	})

	t.Run("should return no batch for no target", func(t *testing.T) {
		t.Parallel()
		// given / when
		batches := entities.ChunkRefreshTargets("fixture-owner", "", nil, 10)

		// then
		assert.Empty(t, batches)
	})
}
