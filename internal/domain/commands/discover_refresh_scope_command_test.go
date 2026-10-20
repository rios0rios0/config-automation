package commands_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/commands"
	"github.com/rios0rios0/config-automation/internal/domain/entities"
	doubles "github.com/rios0rios0/config-automation/test/domain/doubles/repositories"
	"github.com/rios0rios0/config-automation/test/domain/fixtures"
)

const documentation = "# Documentation\n"

// discoverScope runs the command against tree and returns what reached the
// listeners.
func discoverScope(t *testing.T, tree *doubles.InMemoryWorkingTreeRepository) (entities.RefreshScope, error) {
	t.Helper()
	var scope entities.RefreshScope
	var failure error
	commands.NewDiscoverRefreshScopeCommand(tree).Execute(
		t.Context(),
		commands.DiscoverRefreshScopeInput{Root: "/repository"},
		commands.DiscoverRefreshScopeListeners{
			OnSuccess: func(result entities.RefreshScope) { scope = result },
			OnError:   func(err error) { failure = err },
		},
	)
	return scope, failure
}

// withContent maps every path to the same content.
func withContent(paths []string, content string) map[string]string {
	files := map[string]string{}
	for _, path := range paths {
		files[path] = content
	}
	return files
}

// sortedUnion returns the members of every list, sorted.
func sortedUnion(lists ...[]string) []string {
	members := []string{}
	for _, list := range lists {
		for _, member := range list {
			if !slices.Contains(members, member) {
				members = append(members, member)
			}
		}
	}
	slices.Sort(members)
	return members
}

func TestDiscoverRefreshScopeCommand(t *testing.T) {
	t.Parallel()

	t.Run("should grant exact edits for every supported category and none for excluded paths", func(t *testing.T) {
		t.Parallel()
		// given
		paths := slices.Concat(fixtures.SupportedRefreshPaths(), fixtures.ExcludedRefreshPaths())
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(withContent(paths, documentation))

		// when
		scope, err := discoverScope(t, tree)

		// then
		require.NoError(t, err)
		assert.Equal(t, sortedUnion(fixtures.SupportedRefreshPaths()), scope.Existing)
		assert.Equal(t, sortedUnion(fixtures.SupportedRefreshPaths(), entities.RefreshCreatablePaths()), scope.Paths)
		grants := []string{}
		for _, path := range scope.Paths {
			grants = append(grants, "Edit(/"+path+")")
		}
		assert.Equal(t, slices.Concat(entities.ReadOnlyTools(), grants), scope.AllowedTools)
		assert.True(t, scope.HasGuidance)
	})

	t.Run(
		"should offer only the platform targets and keep CLAUDE.md host-managed when nothing is tracked",
		func(t *testing.T) {
			t.Parallel()
			// given
			tree := doubles.NewInMemoryWorkingTreeRepository()

			// when
			scope, err := discoverScope(t, tree)

			// then
			require.NoError(t, err)
			assert.Equal(t, sortedUnion(entities.RefreshCreatablePaths()), scope.Creatable)
			assert.Equal(t, []string{entities.ClaudeFile}, scope.Managed)
			assert.Equal(t, sortedUnion(entities.RefreshCreatablePaths(), []string{entities.ClaudeFile}), scope.Paths)
			assert.NotContains(t, scope.AllowedTools, "Edit(/CLAUDE.md)")
			assert.Empty(t, scope.Existing)
			assert.Empty(t, scope.Skipped)
			assert.False(t, scope.HasGuidance)
		},
	)

	t.Run("should never grant a CLAUDE.md that only imports AGENTS.md", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(map[string]string{
			"AGENTS.md": "# Guidance\n", "CLAUDE.md": entities.AgentsImport,
			"packages/api/AGENTS.md": "# API\n", "packages/api/CLAUDE.md": "@./AGENTS.md\n",
			"tools/CLAUDE.md": entities.AgentsImport, ".claude/CLAUDE.md": entities.AgentsImport,
		})

		// when
		scope, err := discoverScope(t, tree)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"CLAUDE.md", "packages/api/CLAUDE.md", "tools/CLAUDE.md"}, scope.Managed)
		assert.Contains(t, scope.Creatable, "tools/AGENTS.md")
		assert.NotContains(t, scope.Creatable, "AGENTS.md")
		assert.Contains(t, scope.Existing, ".claude/CLAUDE.md")
		for _, path := range scope.Managed {
			assert.Contains(t, scope.Paths, path)
			assert.NotContains(t, scope.AllowedTools, "Edit(/"+path+")")
		}
		assert.Contains(t, scope.AllowedTools, "Edit(/tools/AGENTS.md)")
		assert.True(t, scope.HasGuidance)
	})

	t.Run("should not count an import as guidance when its AGENTS.md is missing", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(map[string]string{
			"README.md": "# Read me\n", "CLAUDE.md": entities.AgentsImport,
		})

		// when
		scope, err := discoverScope(t, tree)

		// then
		require.NoError(t, err)
		assert.False(t, scope.HasGuidance)
		assert.Contains(t, scope.Creatable, "AGENTS.md")
	})

	t.Run("should not offer AGENTS.md when the repository ignores it", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().
			WithTrackedFiles(map[string]string{"README.md": "# Read me\n", ".gitignore": "AGENTS.md\n"}).
			WithIgnored("AGENTS.md")

		// when
		scope, err := discoverScope(t, tree)

		// then
		require.NoError(t, err)
		assert.NotContains(t, scope.Creatable, "AGENTS.md")
		assert.NotContains(t, scope.AllowedTools, "Edit(/AGENTS.md)")
	})

	t.Run("should not count README and CONTRIBUTING as agent guidance", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().
			WithTrackedFiles(withContent([]string{"README.md", "CONTRIBUTING.md"}, documentation))

		// when
		scope, err := discoverScope(t, tree)

		// then
		require.NoError(t, err)
		assert.False(t, scope.HasGuidance)
	})

	t.Run("should not grant an untracked file of a supported category", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().
			WithTrackedFiles(map[string]string{"README.md": documentation}).
			WithUntrackedFile(".claude/rules/new.md", "untracked instructions")

		// when
		scope, err := discoverScope(t, tree)

		// then
		require.NoError(t, err)
		assert.NotContains(t, scope.Paths, ".claude/rules/new.md")
	})

	t.Run("should skip symlinks and refuse creation below a symlinked directory", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().
			WithTrackedFiles(map[string]string{"README.md": documentation}).
			WithTrackedLink("CLAUDE.md", "README.md").
			WithTrackedLink(".github", "outside")

		// when
		scope, err := discoverScope(t, tree)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"AGENTS.md", "README.md"}, scope.Paths)
		assert.Contains(t, scope.Skipped, "CLAUDE.md")
	})

	t.Run("should skip a tracked path that is not a regular file", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().
			WithTrackedFiles(map[string]string{"README.md": documentation, "docs/CONTRIBUTING.md": ""}).
			WithMode("docs/CONTRIBUTING.md", "160000")

		// when
		scope, err := discoverScope(t, tree)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"docs/CONTRIBUTING.md"}, scope.Skipped)
		assert.NotContains(t, scope.Paths, "docs/CONTRIBUTING.md")
	})

	t.Run("should skip paths holding rule delimiters, wildcards, or control characters", func(t *testing.T) {
		t.Parallel()
		// given
		paths := []string{"comma,name/AGENTS.md", "glob*/CLAUDE.md", "array[0]/AGENTS.md", "newline\nname/CLAUDE.md"}
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(withContent(paths, documentation))

		// when
		scope, err := discoverScope(t, tree)

		// then
		require.NoError(t, err)
		assert.Equal(t, sortedUnion(paths), scope.Skipped)
		assert.Equal(t, sortedUnion(entities.RefreshCreatablePaths(), []string{entities.ClaudeFile}), scope.Paths)
	})

	t.Run("should fail on a refresh path in a merge conflict", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().
			WithTrackedFiles(map[string]string{"README.md": documentation}).
			WithUnmerged("README.md")

		// when
		_, err := discoverScope(t, tree)

		// then
		require.EqualError(t, err, "unmerged refresh path: README.md")
	})

	t.Run("should fail when the index cannot be read", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository()
		tree.ErrorOnIndex = errors.New("index locked")

		// when
		_, err := discoverScope(t, tree)

		// then
		require.ErrorIs(t, err, tree.ErrorOnIndex)
	})
}
