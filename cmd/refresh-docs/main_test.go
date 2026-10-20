package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	t.Parallel()

	t.Run("should exit with a usage error when the arguments are wrong", func(t *testing.T) {
		t.Parallel()
		for _, arguments := range [][]string{{}, {"scope"}, {"publish", "."}} {
			// given
			var stdout, stderr bytes.Buffer

			// when
			code := run(t.Context(), arguments, &stdout, &stderr)

			// then
			assert.Equal(t, exitUsageError, code)
			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), "usage: refresh-docs")
		}
	})

	t.Run("should print the scope manifest of a repository as JSON", func(t *testing.T) {
		t.Parallel()
		// given
		root := t.TempDir()
		for _, arguments := range [][]string{{"init", "--quiet"}, {"config", "core.excludesFile", os.DevNull}} {
			output, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, arguments...)...).
				CombinedOutput()
			require.NoError(t, err, string(output))
		}
		require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Guidance\n"), 0o600))
		output, err := exec.CommandContext(t.Context(), "git", "-C", root, "add", "AGENTS.md").CombinedOutput()
		require.NoError(t, err, string(output))
		var stdout, stderr bytes.Buffer

		// when
		code := run(t.Context(), []string{"scope", root}, &stdout, &stderr)

		// then
		require.Equal(t, 0, code, stderr.String())
		var scope refreshScopeResponse
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &scope))
		assert.Equal(t, []string{"AGENTS.md"}, scope.Existing)
		assert.Equal(t, []string{"CLAUDE.md"}, scope.Managed)
		assert.True(t, scope.HasGuidance)
	})

	t.Run("should fail when the directory is not a repository", func(t *testing.T) {
		t.Parallel()
		// given
		var stdout, stderr bytes.Buffer

		// when
		code := run(t.Context(), []string{"layout", t.TempDir()}, &stdout, &stderr)

		// then
		assert.Equal(t, exitFailure, code)
		assert.Empty(t, stdout.String())
		assert.Contains(t, stderr.String(), "subcommand=layout")
	})
}
