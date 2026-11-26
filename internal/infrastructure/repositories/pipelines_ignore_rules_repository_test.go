package repositories_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	infrastructure "github.com/rios0rios0/config-automation/internal/infrastructure/repositories"
)

// fakeGenerator stands in for the generator of a pipelines checkout: it
// records its environment, arguments and directory beside itself, writes the
// shared block into `.gitignore`, and fails with the generator's own refusal
// when the file says so. `sh` runs it, so it needs no execute bit.
const fakeGenerator = `set -eu
records="${SCRIPTS_DIR}/records"
mkdir -p "${records}"
env > "${records}/environment"
printf '%s\n' "$@" > "${records}/arguments"
pwd > "${records}/directory"
if [ -f .gitignore ] && grep -qx 'malformed' .gitignore; then
  echo 'ERROR: .gitignore has a malformed pipelines block.' >&2
  exit 2
fi
printf '%s\n' '# >>> pipelines:begin' 'build/reports/' '# <<< pipelines:end' > .gitignore
echo 'Updated .gitignore from fragments: common'
`

// pipelinesCheckout writes a pipelines checkout holding the fake generator
// and returns its directory.
func pipelinesCheckout(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	script := filepath.Join(directory, "global", "scripts", "tools", "gitignore", "run.sh")
	// A directory needs its execute bit; the rule below is written for files.
	// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
	require.NoError(t, os.MkdirAll(filepath.Dir(script), 0o700))
	require.NoError(t, os.WriteFile(script, []byte(fakeGenerator), 0o600))
	return directory
}

// record reads what the fake generator recorded under name.
func record(t *testing.T, checkout, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(checkout, "records", name))
	require.NoError(t, err)
	return string(content)
}

func TestPipelinesIgnoreRulesRepository(t *testing.T) {
	t.Parallel()

	t.Run("should refuse a directory that is not a pipelines checkout", func(t *testing.T) {
		t.Parallel()
		// given
		directory := t.TempDir()

		// when
		_, err := infrastructure.NewPipelinesIgnoreRulesRepository(directory, os.Environ)

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a checkout of rios0rios0/pipelines")
		assert.Contains(t, err.Error(), "global/scripts/tools/gitignore/run.sh")
	})

	t.Run("should run the generator in the clone with no token in its environment", func(t *testing.T) {
		t.Parallel()
		// given
		checkout := pipelinesCheckout(t)
		root := t.TempDir()
		environ := func() []string {
			return []string{
				"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "GH_TOKEN=fixture-token-placeholder",
				"CLAUDE_CODE_OAUTH_TOKEN=fixture-oauth-placeholder", "SCRIPTS_DIR=/elsewhere", "HOME=/home/fixture",
			}
		}
		generator, err := infrastructure.NewPipelinesIgnoreRulesRepository(checkout, environ)
		require.NoError(t, err)

		// when
		output, err := generator.Generate(t.Context(), root)

		// then
		require.NoError(t, err)
		assert.Equal(t, "Updated .gitignore from fragments: common", output)
		content, readErr := os.ReadFile(filepath.Join(root, ".gitignore"))
		require.NoError(t, readErr)
		assert.Equal(t, "# >>> pipelines:begin\nbuild/reports/\n# <<< pipelines:end\n", string(content))
		assert.Equal(t, ".\n", record(t, checkout, "arguments"))
		resolved, resolveErr := filepath.EvalSymlinks(root)
		require.NoError(t, resolveErr)
		assert.Equal(t, resolved, strings.TrimSpace(record(t, checkout, "directory")))
		environment := record(t, checkout, "environment")
		assert.Contains(t, environment, "SCRIPTS_DIR="+checkout+"\n")
		assert.Contains(t, environment, "LANG=C.UTF-8\n")
		assert.NotContains(t, environment, "fixture-token-placeholder")
		assert.NotContains(t, environment, "fixture-oauth-placeholder")
		assert.NotContains(t, environment, "/elsewhere")
		assert.NotContains(t, environment, "HOME=/home/fixture")
	})

	t.Run("should return what the generator printed when it refuses the file", func(t *testing.T) {
		t.Parallel()
		// given
		checkout := pipelinesCheckout(t)
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("malformed\n"), 0o600))
		generator, err := infrastructure.NewPipelinesIgnoreRulesRepository(checkout, os.Environ)
		require.NoError(t, err)

		// when
		output, err := generator.Generate(t.Context(), root)

		// then
		require.Error(t, err)
		assert.Equal(t, "ERROR: .gitignore has a malformed pipelines block.", output)
		assert.Contains(t, err.Error(), "malformed pipelines block")
	})
}
