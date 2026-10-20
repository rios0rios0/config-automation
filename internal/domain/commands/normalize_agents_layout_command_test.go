package commands_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/commands"
	"github.com/rios0rios0/config-automation/internal/domain/entities"
	doubles "github.com/rios0rios0/config-automation/test/domain/doubles/repositories"
)

// normalizeLayout runs the command against tree and returns what reached
// the listeners.
func normalizeLayout(
	t *testing.T,
	tree *doubles.InMemoryWorkingTreeRepository,
) (entities.AgentsLayoutReport, error) {
	t.Helper()
	var report entities.AgentsLayoutReport
	var failure error
	commands.NewNormalizeAgentsLayoutCommand(tree).Execute(
		t.Context(),
		commands.NormalizeAgentsLayoutInput{Root: "/repository"},
		commands.NormalizeAgentsLayoutListeners{
			OnSuccess: func(result entities.AgentsLayoutReport) { report = result },
			OnError:   func(err error) { failure = err },
		},
	)
	return report, failure
}

func TestNormalizeAgentsLayoutCommand(t *testing.T) {
	t.Parallel()

	t.Run("should rename a legacy CLAUDE.md in its own commit when AGENTS.md is absent", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(map[string]string{
			"CLAUDE.md": entities.LegacyBanner + "\n## Build\n\nmake test\n",
		})

		// when
		report, err := normalizeLayout(t, tree)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"CLAUDE.md"}, report.Renamed)
		assert.Empty(t, report.Pointed)
		assert.True(t, report.Changed)
		assert.Equal(t, []string{entities.RenameMessage}, tree.Commits)
		assert.Equal(t, entities.AgentsBanner+"\n## Build\n\nmake test\n", tree.Files["AGENTS.md"])
		assert.Equal(t, entities.AgentsImport, tree.Files["CLAUDE.md"])
		assert.Equal(t, []string{"AGENTS.md", "CLAUDE.md"}, tree.Staged)
		assert.True(t, tree.Tracked["CLAUDE.md"])
	})

	t.Run("should merge only what CLAUDE.md adds when both files exist", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(map[string]string{
			"packages/api/AGENTS.md": "# API\n\nShared facts.\n",
			"packages/api/CLAUDE.md": "@AGENTS.md\n\nUse the release skill.\n",
		})

		// when
		report, err := normalizeLayout(t, tree)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"packages/api/CLAUDE.md"}, report.Merged)
		assert.Equal(t, "# API\n\nShared facts.\n\n## Migrated from CLAUDE.md\n\nUse the release skill.\n",
			tree.Files["packages/api/AGENTS.md"])
		assert.Equal(t, entities.AgentsImport, tree.Files["packages/api/CLAUDE.md"])
		assert.Empty(t, tree.Commits)
	})

	t.Run("should not append a copy when CLAUDE.md repeats AGENTS.md", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(map[string]string{
			"AGENTS.md": "# Guidance\n\nRun make test.\n", "CLAUDE.md": "# Guidance\n\nRun make test.\n",
		})

		// when
		_, err := normalizeLayout(t, tree)

		// then
		require.NoError(t, err)
		assert.Equal(t, "# Guidance\n\nRun make test.\n", tree.Files["AGENTS.md"])
		assert.Equal(t, entities.AgentsImport, tree.Files["CLAUDE.md"])
	})

	t.Run("should append CLAUDE.md text that only appears inside a longer AGENTS.md line", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(map[string]string{
			"AGENTS.md": "# Guidance\n\nDo not run make test.\n", "CLAUDE.md": "run make test.\n",
		})

		// when
		_, err := normalizeLayout(t, tree)

		// then
		require.NoError(t, err)
		assert.Equal(t, "# Guidance\n\nDo not run make test.\n\n## Migrated from CLAUDE.md\n\nrun make test.\n",
			tree.Files["AGENTS.md"])
	})

	t.Run("should keep import examples inside code blocks when merging", func(t *testing.T) {
		t.Parallel()
		// given
		example := "Import it with:\n\n```markdown\n@AGENTS.md\n```\n\n" +
			"Or nest it:\n\n~~~markdown\n```\n@AGENTS.md\n```\n~~~\n"
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(map[string]string{
			"AGENTS.md": "# Guidance\n", "CLAUDE.md": "@AGENTS.md\n\n" + example,
		})

		// when
		report, err := normalizeLayout(t, tree)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"CLAUDE.md"}, report.Merged)
		assert.Equal(t, "# Guidance\n\n## Migrated from CLAUDE.md\n\n"+example, tree.Files["AGENTS.md"])
		assert.Equal(t, entities.AgentsImport, tree.Files["CLAUDE.md"])
	})

	t.Run("should add the import when only AGENTS.md exists", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(map[string]string{
			"AGENTS.md": "# Guidance\n", "tools/AGENTS.md": "# Tools\n",
		})

		// when
		report, err := normalizeLayout(t, tree)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"CLAUDE.md", "tools/CLAUDE.md"}, report.Pointed)
		assert.Equal(t, entities.AgentsImport, tree.Files["CLAUDE.md"])
		assert.True(t, tree.Tracked["CLAUDE.md"])
		assert.True(t, tree.Tracked["tools/CLAUDE.md"])
	})

	t.Run("should replace symlinks between the two files with a regular import", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().
			WithTrackedFiles(map[string]string{"AGENTS.md": "# Linked\n", "legacy/CLAUDE.md": "# Legacy\n"}).
			WithTrackedLink("CLAUDE.md", "AGENTS.md").
			WithTrackedLink("legacy/AGENTS.md", "CLAUDE.md")

		// when
		report, err := normalizeLayout(t, tree)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"CLAUDE.md"}, report.Pointed)
		assert.Equal(t, []string{"legacy/CLAUDE.md"}, report.Renamed)
		assert.Empty(t, tree.Links)
		assert.Equal(t, entities.AgentsImport, tree.Files["CLAUDE.md"])
		assert.Equal(t, "# Legacy\n", tree.Files["legacy/AGENTS.md"])
		assert.Equal(t, entities.AgentsImport, tree.Files["legacy/CLAUDE.md"])
	})

	t.Run("should treat a dot-slash link as the sibling and skip any other target", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().
			WithTrackedFiles(map[string]string{"dotted/CLAUDE.md": "# Dotted\n", "parent/CLAUDE.md": "# Parent\n"}).
			WithTrackedLink("dotted/AGENTS.md", "./CLAUDE.md").
			WithTrackedLink("parent/AGENTS.md", "../parent/CLAUDE.md")

		// when
		report, err := normalizeLayout(t, tree)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"dotted/CLAUDE.md"}, report.Renamed)
		assert.Equal(t, []string{"parent/CLAUDE.md"}, report.Skipped)
		assert.Equal(t, "# Dotted\n", tree.Files["dotted/AGENTS.md"])
		assert.Equal(t, entities.AgentsImport, tree.Files["dotted/CLAUDE.md"])
		assert.Equal(t, "../parent/CLAUDE.md", tree.Links["parent/AGENTS.md"])
	})

	t.Run("should change nothing when the layout already complies", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(map[string]string{
			"CLAUDE.md": entities.LegacyBanner + "\nGuidance.\n",
		})
		_, err := normalizeLayout(t, tree)
		require.NoError(t, err)

		// when
		report, err := normalizeLayout(t, tree)

		// then
		require.NoError(t, err)
		assert.False(t, report.Changed)
		assert.Len(t, tree.Commits, 1)
		assert.Contains(t, report.Note, "No changes")
	})

	t.Run("should report an import whose AGENTS.md is missing", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(map[string]string{
			"tools/CLAUDE.md": entities.AgentsImport,
		})

		// when
		report, err := normalizeLayout(t, tree)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"tools/CLAUDE.md"}, report.Dangling)
		assert.False(t, report.Changed)
		assert.Contains(t, report.Note, "create it if that directory needs guidance")
	})

	t.Run("should skip a symlink to another file and a destination the repository ignores", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().
			WithTrackedFiles(map[string]string{
				"README.md": "# Read me\n", ".gitignore": "AGENTS.md\n",
				"ignored/CLAUDE.md": "# Ignored destination\n",
			}).
			WithTrackedLink("CLAUDE.md", "README.md").
			WithIgnored("AGENTS.md", "ignored/AGENTS.md")

		// when
		report, err := normalizeLayout(t, tree)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"CLAUDE.md", "ignored/CLAUDE.md"}, report.Skipped)
		assert.False(t, report.Changed)
		assert.Equal(t, "README.md", tree.Links["CLAUDE.md"])
		assert.Empty(t, tree.Staged)
		assert.Empty(t, tree.Commits)
	})

	t.Run("should skip a CLAUDE.md that is not UTF-8 text", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(map[string]string{
			"AGENTS.md": "# Guidance\n", "CLAUDE.md": "\xff\xfe",
		})

		// when
		report, err := normalizeLayout(t, tree)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"CLAUDE.md"}, report.Skipped)
		assert.Equal(t, "\xff\xfe", tree.Files["CLAUDE.md"])
	})

	t.Run("should leave both files of a skipped pair unchanged", func(t *testing.T) {
		t.Parallel()
		// given
		files := map[string]string{
			"README.md": "# Read me\n", "AGENTS.md": entities.LegacyBanner + "\nShared.\n",
			".gitignore": "/local/\n", "local/AGENTS.md": "# Local\n", "local/CLAUDE.md": "# Local Claude\n",
		}
		tree := doubles.NewInMemoryWorkingTreeRepository().
			WithTrackedFiles(files).
			WithTrackedLink("CLAUDE.md", "README.md").
			WithIgnored("local/")

		// when
		report, err := normalizeLayout(t, tree)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"CLAUDE.md", "local/CLAUDE.md"}, report.Skipped)
		assert.Empty(t, report.Rebannered)
		assert.False(t, report.Changed)
		for path, content := range files {
			assert.Equal(t, content, tree.Files[path], path)
		}
		assert.Empty(t, tree.Staged)
	})

	t.Run("should ignore the .claude directory and dependency paths", func(t *testing.T) {
		t.Parallel()
		// given
		files := map[string]string{
			".claude/CLAUDE.md": "# Claude only\n", "node_modules/package/CLAUDE.md": "# Vendored\n",
			"vendor/package/AGENTS.md": "# Vendored\n",
		}
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(files)

		// when
		report, err := normalizeLayout(t, tree)

		// then
		require.NoError(t, err)
		assert.False(t, report.Changed)
		for path, content := range files {
			assert.Equal(t, content, tree.Files[path], path)
		}
	})

	t.Run("should fail when the files cannot be listed", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository()
		tree.ErrorOnFiles = errors.New("not a git repository")

		// when
		_, err := normalizeLayout(t, tree)

		// then
		require.ErrorIs(t, err, tree.ErrorOnFiles)
	})

	t.Run("should fail when the renames cannot be committed", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(map[string]string{
			"CLAUDE.md": "# Guidance\n",
		})
		tree.ErrorOnCommit = errors.New("signing failed")

		// when
		_, err := normalizeLayout(t, tree)

		// then
		require.ErrorIs(t, err, tree.ErrorOnCommit)
		assert.ErrorContains(t, err, "committing the renames")
	})

	t.Run("should fail when the layout cannot be staged", func(t *testing.T) {
		t.Parallel()
		// given
		tree := doubles.NewInMemoryWorkingTreeRepository().WithTrackedFiles(map[string]string{
			"AGENTS.md": "# Guidance\n",
		})
		tree.ErrorOnStage = errors.New("index locked")

		// when
		_, err := normalizeLayout(t, tree)

		// then
		require.ErrorIs(t, err, tree.ErrorOnStage)
		assert.ErrorContains(t, err, "staging the layout")
	})
}
