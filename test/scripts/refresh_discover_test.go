package scripts_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// discoverGhStub answers `gh api <path> --jq <filter>` by running the real
// jq over REPOSITORY_METADATA, printing a string raw and anything else as
// compact JSON, as gh does, and records every call.
const discoverGhStub = `set -euo pipefail
printf '%s\n' "$*" >> "${FIXTURE_ROOT}/gh-calls"
if [ "$#" -eq 4 ] && [ "$1" = 'api' ] && [ "$3" = '--jq' ]; then
  jq -rc "$4" <<< "${REPOSITORY_METADATA}"
  exit 0
fi
echo "unexpected gh call: $*" >&2
exit 1
`

// discoverRun is the outcome of the matrix step for one dispatch.
type discoverRun struct {
	code   int
	stdout string
	stderr string
	// outputs holds the step outputs written to GITHUB_OUTPUT.
	outputs map[string]string
	calls   []string
}

// runDiscover runs the workflow's `Build matrix` step for a single-repository
// dispatch of fixture/project, with the repository API answering metadata.
func runDiscover(t *testing.T, metadata string) discoverRun {
	t.Helper()
	root := t.TempDir()
	writeStub(t, root, "gh", discoverGhStub)
	workflow, err := os.ReadFile(filepath.Join(projectRoot(), ".github/workflows/config-and-docs-refresh.yaml"))
	require.NoError(t, err)
	output := filepath.Join(root, "github-output")
	command := exec.CommandContext(t.Context(), "bash", "-c", stepScript(t, string(workflow), "Build matrix"))
	command.Dir, command.Env = root, environment(map[string]string{
		"PATH":         root + string(os.PathListSeparator) + os.Getenv("PATH"),
		"FIXTURE_ROOT": root, "REPOSITORY_METADATA": metadata, "GITHUB_OUTPUT": output, "RUNNER_TEMP": root,
		"OWNERS":        `[{"name": "fixture", "secret": "FIXTURE_ACCESS_TOKEN"}]`,
		"DEFAULT_OWNER": "fixture", "TOKEN_fixture": "fixture-token-placeholder",
		"REPO_INPUT": "fixture/project", "BATCH_SIZE": "10",
	})
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	run := discoverRun{outputs: map[string]string{}}
	if runErr := command.Run(); runErr != nil {
		var exitErr *exec.ExitError
		require.ErrorAs(t, runErr, &exitErr, stderr.String())
		run.code = exitErr.ExitCode()
	}
	run.stdout, run.stderr = stdout.String(), stderr.String()
	if content, readErr := os.ReadFile(output); readErr == nil {
		for line := range strings.Lines(string(content)) {
			name, value, _ := strings.Cut(strings.TrimSuffix(line, "\n"), "=")
			run.outputs[name] = value
		}
	}
	if content, readErr := os.ReadFile(filepath.Join(root, "gh-calls")); readErr == nil {
		run.calls = strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	}
	return run
}

func TestRefreshDiscover(t *testing.T) {
	t.Parallel()

	t.Run("should refuse a single-repository dispatch of a repository the schedule leaves out", func(t *testing.T) {
		t.Parallel()
		for reason, metadata := range map[string]string{
			"a fork":              `{"default_branch": "main", "fork": true, "archived": false}`,
			"archived":            `{"default_branch": "main", "fork": false, "archived": true}`,
			"a fork and archived": `{"default_branch": "main", "fork": true, "archived": true}`,
		} {
			t.Run(reason, func(t *testing.T) {
				t.Parallel()
				// given / when
				run := runDiscover(t, metadata)

				// then
				assert.Equal(t, 1, run.code, run.stdout+run.stderr)
				assert.Contains(t, run.stdout, "::error::Refusing to refresh fixture/project: it is "+reason+",")
				assert.Empty(t, run.outputs, "a refused dispatch produced a matrix")
			})
		}
	})

	t.Run(
		"should build a one-repository matrix from the same call when the repository is eligible",
		func(t *testing.T) {
			t.Parallel()
			// given / when
			run := runDiscover(t, `{"default_branch": "trunk", "fork": false, "archived": false}`)

			// then
			require.Equal(t, 0, run.code, run.stdout+run.stderr)
			require.Len(t, run.calls, 1, "the flags and the branch must come from one API call")
			assert.True(t, strings.HasPrefix(run.calls[0], "api /repos/fixture/project --jq "), run.calls[0])
			assert.Equal(t, "1", run.outputs["count"])
			assert.JSONEq(t, `[{"name": "fixture batch_01", "owner": "fixture", "secret": "FIXTURE_ACCESS_TOKEN",
			"repos": [{"owner": "fixture", "name": "project", "default_branch": "trunk"}]}]`, run.outputs["matrix"])
		},
	)
}
