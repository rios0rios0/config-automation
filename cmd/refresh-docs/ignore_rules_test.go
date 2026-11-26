package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefreshBatchIgnoreRules(t *testing.T) {
	t.Parallel()

	t.Run("should bring the shared ignore rules into the PR and ask Claude to record them", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t).withPipelines()
		remote := fixture.repository(
			"ignores",
			compliant(map[string]string{"README.md": original, ".gitignore": "bin/\n"}),
		)

		// when
		run := fixture.run(nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Equal(t, ".gitignore", fixture.git(remote, "diff", "--name-only", "main", refreshBranch))
		assert.Equal(t, sharedIgnoreBlock+"\nbin/", fixture.show(remote, ".gitignore"))
		assert.Contains(t, run.stdout, "--> Updated .gitignore from fragments: common\n")
		prompt := fixture.record("refresh-prompt")
		assert.Contains(t, prompt, "`.gitignore` is the host's: you hold no grant for it, so never edit it.")
		assert.Contains(t, prompt, "## Shared ignore rules for this repository\n\n- Updated the shared ignore rules")
		require.Len(t, fixture.pullRequests.Created, 1)
		assert.Contains(t, fixture.pullRequests.Created[0].Description,
			"It also writes the shared ignore rules of `rios0rios0/pipelines` into `.gitignore`")
	})

	t.Run("should keep every token out of the generator's environment", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t).withPipelines()
		fixture.repository("environment", compliant(map[string]string{"README.md": original}))

		// when
		run := fixture.run(nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		environment, err := os.ReadFile(filepath.Join(fixture.pipelinesDir, "generator-environment"))
		require.NoError(t, err)
		assert.Contains(t, string(environment), "SCRIPTS_DIR="+fixture.pipelinesDir+"\n")
		assert.NotContains(t, string(environment), fixtureToken)
		assert.NotContains(t, string(environment), fixtureOAuthToken)
	})

	t.Run("should create .gitignore in a repository that has none", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t).withPipelines()
		remote := fixture.repository("created", compliant(map[string]string{"README.md": original}))

		// when
		run := fixture.run(map[string]any{"README.md": "Updated\n"})

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Equal(t, strings.TrimSpace(sharedIgnoreBlock), fixture.show(remote, ".gitignore"))
		assert.Equal(t, "Updated", fixture.show(remote, "README.md"))
		assert.Contains(t, fixture.record("refresh-prompt"), "- Created `.gitignore` with the shared ignore rules")
	})

	t.Run("should open no PR when the shared ignore rules are already current", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t).withPipelines()
		fixture.repository(
			"current",
			compliant(map[string]string{"README.md": original, ".gitignore": sharedIgnoreBlock}),
		)

		// when
		run := fixture.run(nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "no_drift (1): fixture/current")
		assert.Empty(t, fixture.pullRequests.Created)
		assert.Contains(t, fixture.record("refresh-prompt"),
			"No changes: `.gitignore` already carries the current shared ignore rules.")
	})

	t.Run("should fail only the repository whose ignore rules cannot be written", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t).withPipelines()
		broken := fixture.repository(
			"broken",
			compliant(map[string]string{"README.md": original, ".gitignore": "malformed\n"}),
		)
		remote := fixture.repository("next", compliant(map[string]string{"README.md": original}))

		// when
		run := fixture.run(map[string]any{"README.md": "Updated\n"})

		// then
		assert.Equal(t, exitFailure, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "  - gitignore: fixture/broken")
		assert.Contains(t, run.stdout, "malformed pipelines block")
		assert.Contains(t, run.stdout, "processed (2/2)")
		assert.NotContains(t, fixture.record("claude-calls"), "fixture__broken", "Claude ran on a failed repository")
		assert.Empty(t, fixture.branches(broken))
		assert.Equal(t, "Updated", fixture.show(remote, "README.md"))
	})

	t.Run("should never write through a symlinked .gitignore", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t).withPipelines()
		remote := fixture.linkedRepository("linked",
			compliant(map[string]string{"README.md": original, "shared/ignore": "dist/\n"}),
			map[string]string{".gitignore": "shared/ignore"})

		// when
		run := fixture.run(map[string]any{"README.md": "Updated\n"})

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout,
			"warning: Left out of the shared ignore rules: \".gitignore\" is a symlink or not a regular file\n")
		assert.Equal(t, "dist/", fixture.show(remote, "shared/ignore"))
		assert.Equal(t, "README.md", fixture.git(remote, "diff", "--name-only", "main", refreshBranch))
		assert.NoFileExists(t, filepath.Join(fixture.pipelinesDir, "generator-environment"))
		assert.Contains(
			t,
			fixture.record("refresh-prompt"),
			"- Left `.gitignore` unchanged because it is not a regular file",
		)
	})

	t.Run("should leave .gitignore alone when the run names no pipelines checkout", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		remote := fixture.repository("off", compliant(map[string]string{"README.md": original, ".gitignore": "bin/\n"}))

		// when
		run := fixture.run(map[string]any{"README.md": "Updated\n"})

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Equal(t, "bin/", fixture.show(remote, ".gitignore"))
		assert.NotContains(t, fixture.record("refresh-prompt"), ".gitignore")
		require.Len(t, fixture.pullRequests.Created, 1)
		assert.NotContains(t, fixture.pullRequests.Created[0].Description, ".gitignore")
	})

	t.Run("should refuse to start when --pipelines-dir is not a pipelines checkout", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		fixture.repository("untouched", map[string]string{"README.md": original})
		fixture.pipelinesDir = t.TempDir()

		// when
		run := fixture.run(nil)

		// then
		assert.Equal(t, exitFailure, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stderr, "--pipelines-dir")
		assert.Contains(t, run.stderr, "not a checkout of rios0rios0/pipelines")
		assert.Empty(t, fixture.clones.urls)
	})
}
