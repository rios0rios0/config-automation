package repositories_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/infrastructure/repositories"
)

// newWorkingTree initializes a Git repository whose local configuration
// shuts out the machine's own: no signing, no hooks, no global excludes.
func newWorkingTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init", "--quiet", "-b", "main")
	for _, setting := range [][2]string{
		{"user.name", "fixture"}, {"user.email", "fixture@example.invalid"},
		{"commit.gpgsign", "false"}, {"core.hooksPath", os.DevNull}, {"core.excludesFile", os.DevNull},
	} {
		runGit(t, root, "config", setting[0], setting[1])
	}
	return root
}

// runGit runs git in root and returns its trimmed output.
func runGit(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, arguments...)...).
		CombinedOutput()
	require.NoError(t, err, string(output))
	return strings.TrimSpace(string(output))
}

// writeFiles creates files below root, with their directories.
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		location := filepath.Join(root, filepath.FromSlash(path))
		// A directory needs its execute bit; the rule below is written for files.
		// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
		require.NoError(t, os.MkdirAll(filepath.Dir(location), 0o700))
		require.NoError(t, os.WriteFile(location, []byte(content), 0o600))
	}
}

func TestGitWorkingTreeRepository(t *testing.T) {
	t.Parallel()

	t.Run("should list tracked paths with their modes and stages", func(t *testing.T) {
		t.Parallel()
		// given
		root := newWorkingTree(t)
		writeFiles(t, root, map[string]string{"README.md": "# Read me\n", "untracked.md": "new\n"})
		require.NoError(t, os.Symlink("README.md", filepath.Join(root, "CLAUDE.md")))
		runGit(t, root, "add", "--", "README.md", "CLAUDE.md")
		repository := repositories.NewGitWorkingTreeRepository()

		// when
		entries, err := repository.FindIndexEntries(t.Context(), root)

		// then
		require.NoError(t, err)
		assert.Equal(t, []entities.IndexEntry{
			{Mode: "120000", Stage: 0, Path: "CLAUDE.md"},
			{Mode: "100644", Stage: 0, Path: "README.md"},
		}, entries)
	})

	t.Run("should list untracked files the ignore rules do not match", func(t *testing.T) {
		t.Parallel()
		// given
		root := newWorkingTree(t)
		writeFiles(t, root, map[string]string{
			".gitignore":      "local/\n",
			"AGENTS.md":       "# Guidance\n",
			"new/AGENTS.md":   "# New\n",
			"local/AGENTS.md": "# Local\n",
		})
		runGit(t, root, "add", "--", ".gitignore", "AGENTS.md")
		repository := repositories.NewGitWorkingTreeRepository()

		// when
		files, err := repository.FindFiles(t.Context(), root)

		// then
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{".gitignore", "AGENTS.md", "new/AGENTS.md"}, files)
	})

	t.Run("should classify paths without following symlinks", func(t *testing.T) {
		t.Parallel()
		// given
		root := newWorkingTree(t)
		writeFiles(t, root, map[string]string{"docs/README.md": "# Docs\n"})
		require.NoError(t, os.Symlink("docs", filepath.Join(root, "linked")))
		repository := repositories.NewGitWorkingTreeRepository()
		expected := map[string]entities.PathKind{
			"docs": entities.PathDirectory, "docs/README.md": entities.PathFile, "linked": entities.PathSymlink,
			"missing.md": entities.PathAbsent, "docs/README.md/below.md": entities.PathAbsent,
		}

		for path, kind := range expected {
			// when
			found, err := repository.FindKind(t.Context(), root, path)

			// then
			require.NoError(t, err)
			assert.Equal(t, kind, found, path)
		}
	})

	t.Run("should match ignore rules even against a tracked file", func(t *testing.T) {
		t.Parallel()
		// given
		root := newWorkingTree(t)
		writeFiles(t, root, map[string]string{".gitignore": "AGENTS.md\n", "AGENTS.md": "# Guidance\n"})
		runGit(t, root, "add", "--force", "--", ".gitignore", "AGENTS.md")
		repository := repositories.NewGitWorkingTreeRepository()

		// when
		ignored, err := repository.IsIgnored(t.Context(), root, "AGENTS.md")
		notIgnored, notIgnoredErr := repository.IsIgnored(t.Context(), root, "CLAUDE.md")

		// then
		require.NoError(t, err)
		require.NoError(t, notIgnoredErr)
		assert.True(t, ignored)
		assert.False(t, notIgnored)
	})

	t.Run("should keep the history of a file it moves and commits", func(t *testing.T) {
		t.Parallel()
		// given
		root := newWorkingTree(t)
		writeFiles(t, root, map[string]string{"CLAUDE.md": "# Guidance\n\nmake test\n"})
		runGit(t, root, "add", "--", "CLAUDE.md")
		runGit(t, root, "commit", "--quiet", "-m", "Initial fixture")
		repository := repositories.NewGitWorkingTreeRepository()

		// when
		require.NoError(t, repository.Move(t.Context(), root, "CLAUDE.md", "AGENTS.md"))
		require.NoError(t, repository.Commit(t.Context(), root, entities.RenameMessage))

		// then
		history := runGit(t, root, "log", "--follow", "--format=%s", "--", "AGENTS.md")
		assert.Equal(t, entities.RenameMessage+"\nInitial fixture", history)
	})

	t.Run("should fail to stage a path below an ignored directory", func(t *testing.T) {
		t.Parallel()
		// given
		root := newWorkingTree(t)
		writeFiles(t, root, map[string]string{".gitignore": "/local/\n", "local/AGENTS.md": "# Local\n"})
		runGit(t, root, "add", "--force", "--", ".gitignore", "local/AGENTS.md")
		writeFiles(t, root, map[string]string{"local/AGENTS.md": "# Changed\n"})
		repository := repositories.NewGitWorkingTreeRepository()

		// when
		err := repository.Stage(t.Context(), root, []string{"local/AGENTS.md"})

		// then
		assert.ErrorContains(t, err, "ignored")
	})

	t.Run("should write, read, untrack, and delete files of the working tree", func(t *testing.T) {
		t.Parallel()
		// given
		root := newWorkingTree(t)
		writeFiles(t, root, map[string]string{"AGENTS.md": "# Guidance\n"})
		require.NoError(t, os.Symlink("CLAUDE.md", filepath.Join(root, "LINK.md")))
		runGit(t, root, "add", "--", "AGENTS.md")
		runGit(t, root, "commit", "--quiet", "-m", "Initial fixture")
		repository := repositories.NewGitWorkingTreeRepository()

		// when
		saveErr := repository.SaveContent(t.Context(), root, "CLAUDE.md", []byte(entities.AgentsImport))
		content, readErr := repository.FindContent(t.Context(), root, "CLAUDE.md")
		target, linkErr := repository.FindLinkTarget(t.Context(), root, "LINK.md")
		untrackErr := repository.Untrack(t.Context(), root, "AGENTS.md")
		deleteErr := repository.Delete(t.Context(), root, "LINK.md")

		// then
		require.NoError(t, saveErr)
		require.NoError(t, readErr)
		require.NoError(t, linkErr)
		require.NoError(t, untrackErr)
		require.NoError(t, deleteErr)
		assert.Equal(t, entities.AgentsImport, string(content))
		assert.Equal(t, "CLAUDE.md", target)
		assert.NoFileExists(t, filepath.Join(root, "AGENTS.md"))
		assert.NoFileExists(t, filepath.Join(root, "LINK.md"))
		assert.Empty(t, runGit(t, root, "ls-files"))
	})

	t.Run("should fail outside a Git repository", func(t *testing.T) {
		t.Parallel()
		// given
		root := t.TempDir()
		repository := repositories.NewGitWorkingTreeRepository()

		// when
		_, err := repository.FindFiles(t.Context(), root)

		// then
		assert.ErrorContains(t, err, "not a git repository")
	})
}
