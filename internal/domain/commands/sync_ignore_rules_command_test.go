package commands_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/commands"
	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
	doubles "github.com/rios0rios0/config-automation/test/domain/doubles/repositories"
)

// sharedBlock is what the generator double writes as the shared ignore rules.
const sharedBlock = "# >>> pipelines:begin\nbuild/reports/\n# <<< pipelines:end\n"

// syncIgnoreRules runs the command against tree with generator and returns
// what reached the listeners.
func syncIgnoreRules(
	t *testing.T,
	tree *doubles.InMemoryWorkingTreeRepository,
	generator repositories.IgnoreRulesRepository,
) (entities.IgnoreRulesReport, error) {
	t.Helper()
	var report entities.IgnoreRulesReport
	var failure error
	commands.NewSyncIgnoreRulesCommand(tree, generator).Execute(
		t.Context(),
		commands.SyncIgnoreRulesInput{Root: "/repository"},
		commands.SyncIgnoreRulesListeners{
			OnSuccess: func(result entities.IgnoreRulesReport) { report = result },
			OnError:   func(err error) { failure = err },
		},
	)
	return report, failure
}

func TestSyncIgnoreRulesCommand(t *testing.T) {
	t.Parallel()

	t.Run("should call OnSuccess with off and leave .gitignore alone when no generator is wired", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(map[string]string{".gitignore": "bin/\n"})

		// when
		report, err := syncIgnoreRules(t, tree, nil)

		// then
		require.NoError(t, err)
		assert.Equal(t, entities.IgnoreRulesReport{Outcome: entities.IgnoreRulesOff}, report)
		assert.Equal(t, "bin/\n", tree.Files[".gitignore"])
	})

	t.Run("should call OnSuccess with created when the generator writes a missing .gitignore", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(map[string]string{"Makefile": "all:\n"})
		generator := doubles.NewInMemoryIgnoreRulesRepository(tree).
			WithContent(sharedBlock, "Updated .gitignore from fragments: common")

		// when
		report, err := syncIgnoreRules(t, tree, generator)

		// then
		require.NoError(t, err)
		assert.Equal(t, entities.IgnoreRulesReport{
			Outcome: entities.IgnoreRulesCreated, Output: "Updated .gitignore from fragments: common",
		}, report)
		assert.Equal(t, []string{"/repository"}, generator.Roots)
		assert.Equal(t, sharedBlock, tree.Files[".gitignore"])
	})

	t.Run("should call OnSuccess with updated when the generator changes .gitignore", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(map[string]string{".gitignore": "bin/\n"})
		generator := doubles.NewInMemoryIgnoreRulesRepository(tree).WithContent(sharedBlock+"\nbin/\n", "")

		// when
		report, err := syncIgnoreRules(t, tree, generator)

		// then
		require.NoError(t, err)
		assert.Equal(t, entities.IgnoreRulesUpdated, report.Outcome)
		assert.True(t, report.Changed())
	})

	t.Run("should call OnSuccess with current when the generator changes nothing", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().
			WithTrackedFiles(map[string]string{".gitignore": sharedBlock})
		generator := doubles.NewInMemoryIgnoreRulesRepository(tree).
			WithContent(sharedBlock, "Already up to date: .gitignore (common)")

		// when
		report, err := syncIgnoreRules(t, tree, generator)

		// then
		require.NoError(t, err)
		assert.Equal(t, entities.IgnoreRulesCurrent, report.Outcome)
		assert.False(t, report.Changed())
	})

	t.Run("should call OnSuccess with skipped without running the generator through a symlink", func(t *testing.T) {
		t.Parallel()
		for name, tree := range map[string]*doubles.InMemoryWorkingTreeRepository{
			"symlink":   doubles.NewInMemoryWorkingTreeRepository().WithTrackedLink(".gitignore", "../outside"),
			"directory": doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(map[string]string{".gitignore/x": ""}),
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				// given
				generator := doubles.NewInMemoryIgnoreRulesRepository(tree).WithContent(sharedBlock, "")

				// when
				report, err := syncIgnoreRules(t, tree, generator)

				// then
				require.NoError(t, err)
				assert.Equal(t, entities.IgnoreRulesSkipped, report.Outcome)
				assert.Empty(t, generator.Roots, "the generator ran on a path that is not a regular file")
			})
		}
	})

	t.Run("should call OnError when the generator fails", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(map[string]string{".gitignore": "bin/\n"})
		generator := doubles.NewInMemoryIgnoreRulesRepository(tree)
		generator.ErrorOnGenerate = errors.New("ERROR: .gitignore has a malformed pipelines block")

		// when
		_, err := syncIgnoreRules(t, tree, generator)

		// then
		require.ErrorIs(t, err, generator.ErrorOnGenerate)
		assert.Equal(t, "bin/\n", tree.Files[".gitignore"])
	})

	t.Run("should call OnError when the generator leaves no .gitignore behind", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository()
		generator := doubles.NewInMemoryIgnoreRulesRepository(tree)

		// when
		_, err := syncIgnoreRules(t, tree, generator)

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "left no regular file at .gitignore")
	})
}
