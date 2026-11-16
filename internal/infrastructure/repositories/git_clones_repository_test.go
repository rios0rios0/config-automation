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

const fixtureToken = "fixture-token-placeholder"

// credentialFill asks git, with the clones' helper installed and credential
// in the environment, which password it would send to host. The environment
// holds nothing of this machine's git configuration.
func credentialFill(t *testing.T, credential entities.GitCredential, host string) (string, error) {
	t.Helper()
	arguments := append(repositories.CredentialHelperArguments(), "credential", "fill")
	command := exec.CommandContext(t.Context(), "git", arguments...)
	command.Env = append([]string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0",
	}, repositories.CredentialEnvironment(credential)...)
	command.Stdin = strings.NewReader("protocol=https\nhost=" + host + "\n\n")
	output, err := command.CombinedOutput()
	return string(output), err
}

// bareRepository returns a bare repository holding one commit on main.
func bareRepository(t *testing.T) string {
	t.Helper()
	work := newWorkingTree(t)
	writeFiles(t, work, map[string]string{"README.md": "# Read me\n"})
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "--quiet", "-m", "Initial fixture")
	bare := filepath.Join(t.TempDir(), "fixture.git")
	runGit(t, work, "clone", "--quiet", "--bare", work, bare)
	return bare
}

func TestGitClonesRepository(t *testing.T) {
	t.Parallel()

	t.Run("should answer git with the token for the expected host", func(t *testing.T) {
		t.Parallel()
		// given
		credential := entities.ProviderGitHub.GitCredential(fixtureToken)

		// when
		output, err := credentialFill(t, credential, "github.com")

		// then
		require.NoError(t, err, output)
		assert.Contains(t, output, "username=x-access-token\n")
		assert.Contains(t, output, "password="+fixtureToken+"\n")
	})

	t.Run("should not answer git for any other host", func(t *testing.T) {
		t.Parallel()
		for _, host := range []string{"example.invalid", "github.com.example.invalid", "dev.azure.com"} {
			t.Run(host, func(t *testing.T) {
				t.Parallel()
				// given
				credential := entities.ProviderGitHub.GitCredential(fixtureToken)

				// when
				output, err := credentialFill(t, credential, host)

				// then
				require.Error(t, err, "git found a credential for %s", host)
				assert.NotContains(t, output, fixtureToken)
			})
		}
	})

	t.Run("should clone and push without writing the credential anywhere in the clone", func(t *testing.T) {
		t.Parallel()
		// given
		bare := bareRepository(t)
		clones := repositories.NewGitClonesRepository()
		directory := filepath.Join(t.TempDir(), "clone")
		credential := entities.ProviderGitHub.GitCredential(fixtureToken)

		// when
		cloneErr := clones.Clone(t.Context(), bare, directory, credential)
		runGit(t, directory, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid",
			"-c", "commit.gpgsign=false", "commit", "--quiet", "--allow-empty", "-m", "Refreshed")
		runGit(t, directory, "checkout", "--quiet", "-b", "chore/config-and-docs-refresh")
		pushErr := clones.Push(t.Context(), directory, "chore/config-and-docs-refresh", credential)

		// then
		require.NoError(t, cloneErr)
		require.NoError(t, pushErr)
		configuration, err := os.ReadFile(filepath.Join(directory, ".git", "config"))
		require.NoError(t, err)
		assert.NotContains(t, string(configuration), fixtureToken)
		assert.NotContains(t, string(configuration), "credential")
		assert.Equal(t, "Refreshed", runGit(t, bare, "log", "-1", "--format=%s", "chore/config-and-docs-refresh"))
	})

	t.Run("should report why a clone failed", func(t *testing.T) {
		t.Parallel()
		// given
		clones := repositories.NewGitClonesRepository()
		missing := filepath.Join(t.TempDir(), "missing.git")

		// when
		err := clones.Clone(t.Context(), missing, filepath.Join(t.TempDir(), "clone"), entities.GitCredential{})

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "git clone")
	})

	t.Run("should discard a clone and accept one that is already gone", func(t *testing.T) {
		t.Parallel()
		// given
		clones := repositories.NewGitClonesRepository()
		directory := filepath.Join(t.TempDir(), "clone")
		writeFiles(t, directory, map[string]string{"README.md": "x\n"})

		// when
		first := clones.Discard(t.Context(), directory)
		second := clones.Discard(t.Context(), directory)

		// then
		require.NoError(t, first)
		require.NoError(t, second)
		assert.NoDirExists(t, directory)
	})
}

func TestGitWorkingTreeRepositoryChanges(t *testing.T) {
	t.Parallel()

	// committed returns a working tree whose main holds files.
	committed := func(t *testing.T, files map[string]string) string {
		t.Helper()
		root := newWorkingTree(t)
		writeFiles(t, root, files)
		runGit(t, root, "add", ".")
		runGit(t, root, "commit", "--quiet", "-m", "Initial fixture")
		return root
	}
	changes := repositories.NewGitWorkingTreeRepository(entities.GitIdentity{})

	t.Run("should ignore a whitespace-only edit and see a real one", func(t *testing.T) {
		t.Parallel()
		// given
		root := committed(t, map[string]string{"README.md": "Original documentation\n", "AGENTS.md": "Guidance\n"})
		writeFiles(t, root, map[string]string{"README.md": "Original   documentation   \n"})

		// when
		whitespace, whitespaceErr := changes.HasChanges(t.Context(), root, []string{"README.md"})
		writeFiles(t, root, map[string]string{"AGENTS.md": "Updated guidance\n"})
		edited, editedErr := changes.HasChanges(t.Context(), root, []string{"README.md", "AGENTS.md"})

		// then
		require.NoError(t, whitespaceErr)
		require.NoError(t, editedErr)
		assert.False(t, whitespace)
		assert.True(t, edited)
	})

	t.Run("should see and diff a new file once it is marked as intended to add", func(t *testing.T) {
		t.Parallel()
		// given
		root := committed(t, map[string]string{"README.md": "Read me\n"})
		writeFiles(t, root, map[string]string{"some folder/AGENTS.md": "New guidance\n"})
		paths := []string{"some folder/AGENTS.md"}

		// when
		before, beforeErr := changes.HasChanges(t.Context(), root, paths)
		markErr := changes.MarkIntentToAdd(t.Context(), root, paths)
		after, afterErr := changes.HasChanges(t.Context(), root, paths)
		diff, diffErr := changes.FindDiff(t.Context(), root, paths)

		// then
		require.NoError(t, beforeErr)
		require.NoError(t, markErr)
		require.NoError(t, afterErr)
		require.NoError(t, diffErr)
		assert.False(t, before, "an untracked file counted before it was marked")
		assert.True(t, after)
		assert.Equal(t, []string{"New guidance"}, entities.AddedLines(diff))
	})

	t.Run("should diff a file the repository marks as binary as text", func(t *testing.T) {
		t.Parallel()
		// given
		root := committed(t, map[string]string{"README.md": "Read me\n", ".gitattributes": "*.md -diff\n"})
		writeFiles(t, root, map[string]string{"README.md": "Read me\nAdded line\n"})

		// when
		diff, err := changes.FindDiff(t.Context(), root, []string{"README.md"})

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"Added line"}, entities.AddedLines(diff))
	})

	t.Run("should tell whether a revision names a commit", func(t *testing.T) {
		t.Parallel()
		// given
		root := committed(t, map[string]string{"README.md": "Read me\n"})

		// when
		present, presentErr := changes.HasRevision(t.Context(), root, "refs/heads/main")
		absent, absentErr := changes.HasRevision(t.Context(), root, "refs/remotes/origin/main")

		// then
		require.NoError(t, presentErr)
		require.NoError(t, absentErr)
		assert.True(t, present)
		assert.False(t, absent)
	})

	t.Run("should take a path literally rather than as a pattern", func(t *testing.T) {
		t.Parallel()
		// given
		root := committed(t, map[string]string{"docs/a.md": "a\n", "docs/b.md": "b\n"})
		writeFiles(t, root, map[string]string{"docs/b.md": "changed\n"})

		// when
		differs, err := changes.HasChanges(t.Context(), root, []string{"docs/*.md"})

		// then
		require.NoError(t, err)
		assert.False(t, differs, "a glob matched a file it does not name")
	})

	t.Run("should report an error rather than a difference when git fails", func(t *testing.T) {
		t.Parallel()
		// given
		root := newWorkingTree(t)

		// when
		differs, err := changes.HasChanges(t.Context(), root, []string{"README.md"})

		// then
		require.Error(t, err)
		assert.False(t, differs)
	})

	t.Run("should restore an unstaged edit from the index", func(t *testing.T) {
		t.Parallel()
		// given
		root := committed(t, map[string]string{"CHANGELOG.md": "# Changelog\n"})
		writeFiles(t, root, map[string]string{"CHANGELOG.md": "# Changelog\n\n- hand-written entry\n"})

		// when
		before, beforeErr := changes.HasUnstagedChanges(t.Context(), root, "CHANGELOG.md")
		restoreErr := changes.Restore(t.Context(), root, "CHANGELOG.md")
		after, afterErr := changes.HasUnstagedChanges(t.Context(), root, "CHANGELOG.md")

		// then
		require.NoError(t, beforeErr)
		require.NoError(t, restoreErr)
		require.NoError(t, afterErr)
		assert.True(t, before)
		assert.False(t, after)
		content, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
		require.NoError(t, err)
		assert.Equal(t, "# Changelog\n", string(content))
	})

	t.Run("should list the untracked files of a directory the ignore rules let through", func(t *testing.T) {
		t.Parallel()
		// given
		root := committed(t, map[string]string{".changes/unreleased/.gitkeep": "", ".gitignore": "*.tmp\n"})
		writeFiles(t, root, map[string]string{
			".changes/unreleased/1-changed.yaml": "kind: Changed\n", ".changes/unreleased/scratch.tmp": "x\n",
			"elsewhere.md": "x\n",
		})

		// when
		files, err := changes.FindUntracked(t.Context(), root, ".changes/unreleased")

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{".changes/unreleased/1-changed.yaml"}, files)
	})

	t.Run("should tell staged changes and commits ahead of the base", func(t *testing.T) {
		t.Parallel()
		// given
		root := committed(t, map[string]string{"README.md": "Read me\n"})
		base := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
		writeFiles(t, root, map[string]string{"README.md": "Updated\n"})

		// when
		unstaged, unstagedErr := changes.HasStagedChanges(t.Context(), root)
		stageErr := changes.Stage(t.Context(), root, []string{"README.md"})
		staged, stagedErr := changes.HasStagedChanges(t.Context(), root)
		aheadBefore, beforeErr := changes.IsAhead(t.Context(), root, base)
		runGit(t, root, "commit", "--quiet", "-m", "Updated")
		aheadAfter, afterErr := changes.IsAhead(t.Context(), root, base)

		// then
		require.NoError(t, unstagedErr)
		require.NoError(t, stageErr)
		require.NoError(t, stagedErr)
		require.NoError(t, beforeErr)
		require.NoError(t, afterErr)
		assert.False(t, unstaged)
		assert.True(t, staged)
		assert.False(t, aheadBefore)
		assert.True(t, aheadAfter)
	})

	t.Run("should commit as the configured identity without touching any configuration", func(t *testing.T) {
		t.Parallel()
		// given
		root := committed(t, map[string]string{"README.md": "Read me\n"})
		writeFiles(t, root, map[string]string{"README.md": "Updated\n"})
		runGit(t, root, "add", "README.md")
		identity := entities.GitIdentity{Name: "fixture-bot", Email: "fixture-bot@example.invalid"}
		committer := repositories.NewGitWorkingTreeRepository(identity)

		// when
		err := committer.Commit(t.Context(), root, "chore(refresh): refreshed configuration and documentation")

		// then
		require.NoError(t, err)
		assert.Equal(t, "fixture-bot <fixture-bot@example.invalid>", runGit(t, root, "log", "-1", "--format=%an <%ae>"))
		assert.Equal(t, "fixture", runGit(t, root, "config", "user.name"))
	})

	t.Run("should switch to a new branch at HEAD and back", func(t *testing.T) {
		t.Parallel()
		// given
		root := committed(t, map[string]string{"README.md": "Read me\n"})

		// when
		createErr := changes.CreateBranch(t.Context(), root, "chore/config-and-docs-refresh")
		current := runGit(t, root, "branch", "--show-current")
		checkoutErr := changes.Checkout(t.Context(), root, "main")

		// then
		require.NoError(t, createErr)
		require.NoError(t, checkoutErr)
		assert.Equal(t, "chore/config-and-docs-refresh", current)
		assert.Equal(t, "main", runGit(t, root, "branch", "--show-current"))
	})
}
