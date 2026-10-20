package scripts_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// auditClaudeStub records every call with its stdin, prints AUDIT_RESULT for
// the audit and a line for the refresh, and exits as told.
const auditClaudeStub = `set -euo pipefail
stdin="$(cat; printf .)"
arguments="$(printf '%s\0' "$@" | jq -Rs 'split("\u0000") | .[:-1]')"
jq -cn --argjson args "${arguments}" --arg stdin "${stdin%.}" '{args: $args, stdin: $stdin}' >> "${CALLS}"
for argument in "$@"; do
  if [ "${argument}" = '--output-format' ]; then
    printf '%s' "${AUDIT_RESULT}"
    exit "${AUDIT_EXIT:-0}"
  fi
done
echo 'Refresh completed'
exit "${REFRESH_EXIT:-0}"
`

const defaultAudit = `{"type":"result","subtype":"success","is_error":false,` +
	`"result":"AGENTS.md:4: stale command; proposed diff: replace it"}`

// claudeCall is one invocation the Claude double recorded.
type claudeCall struct {
	Args  []string `json:"args"`
	Stdin string   `json:"stdin"`
}

// argument returns the value that follows flag.
func (c claudeCall) argument(t *testing.T, flag string) string {
	t.Helper()
	index := slices.Index(c.Args, flag)
	require.GreaterOrEqual(t, index, 0, "missing %s", flag)
	require.Less(t, index+1, len(c.Args), "no value for %s", flag)
	return c.Args[index+1]
}

// helperRun is the outcome of one audit-and-refresh.sh run.
type helperRun struct {
	code   int
	stdout string
	stderr string
	calls  []claudeCall
}

// runHelper runs audit-and-refresh.sh against a fixed scope with the Claude
// double, feeding it the next repository on stdin as the batch loop does.
func runHelper(t *testing.T, audit, changelogTools string, overrides map[string]string) helperRun {
	t.Helper()
	root := t.TempDir()
	writeStub(t, root, "claude", auditClaudeStub)
	scope := filepath.Join(root, "scope.json")
	require.NoError(t, os.WriteFile(scope, []byte(`{
		"existing": ["README.md", "packages/api/AGENTS.md", ".claude/skills/check/references/usage.md"],
		"creatable": ["AGENTS.md"],
		"managed": ["CLAUDE.md", "packages/api/CLAUDE.md"],
		"allowed_tools": ["Read", "Grep", "Glob", "Edit(/AGENTS.md)", "Edit(/README.md)",
			"Edit(/packages/api/AGENTS.md)", "Edit(/.claude/skills/check/references/usage.md)"]
	}`), 0o600))
	calls := filepath.Join(root, "calls.jsonl")
	scratch := t.TempDir()
	variables := map[string]string{
		"PATH":   root + string(os.PathListSeparator) + os.Getenv("PATH"),
		"TMPDIR": scratch, "CALLS": calls, "AUDIT_RESULT": audit,
	}
	maps.Copy(variables, overrides)
	command := exec.CommandContext(t.Context(), "bash", filepath.Join(projectRoot(), "scripts/audit-and-refresh.sh"),
		"Refresh only supported guidance.", "claude-opus-5-5", "7", changelogTools, scope)
	command.Dir, command.Env = root, environment(variables)
	command.Stdin = strings.NewReader("next repository\n")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	run := helperRun{}
	if err := command.Run(); err != nil {
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)
		run.code = exitErr.ExitCode()
	}
	run.stdout, run.stderr = stdout.String(), stderr.String()
	if content, err := os.ReadFile(calls); err == nil {
		for line := range strings.Lines(string(content)) {
			var call claudeCall
			require.NoError(t, json.Unmarshal([]byte(line), &call))
			run.calls = append(run.calls, call)
		}
	}
	leftovers, err := os.ReadDir(scratch)
	require.NoError(t, err)
	assert.Empty(t, leftovers, "audit temp file leaked")
	return run
}

func TestAuditAndRefresh(t *testing.T) {
	t.Parallel()

	t.Run("should run a read-only audit and then a scoped refresh when the audit succeeds", func(t *testing.T) {
		t.Parallel()
		// given / when
		run := runHelper(t, defaultAudit, "", nil)

		// then
		require.Equal(t, 0, run.code, run.stderr)
		require.Len(t, run.calls, 2)
		audit, refresh := run.calls[0], run.calls[1]
		assert.True(t, strings.HasPrefix(audit.Args[1], "/checkup prompt-audit .\n"))
		assert.Contains(t, audit.Args[1], "only the import line `@AGENTS.md`")
		assert.Contains(t, audit.Args[1], "under 200 lines")
		assert.Equal(t, "Read,Grep,Glob", audit.argument(t, "--tools"))
		assert.Equal(t, "Read,Grep,Glob", audit.argument(t, "--allowedTools"))
		assert.Equal(t, "Read,Grep,Glob,Edit,Write", refresh.argument(t, "--tools"))
		assert.Contains(t, refresh.Args[1], "AGENTS.md:4: stale command")
		assert.Contains(t, refresh.Args[1], "review data")
		assert.Contains(t, refresh.Args[1], "## Allowed files for this repository")
		assert.Contains(t, refresh.Args[1], "packages/api/AGENTS.md")
		assert.NotContains(t, refresh.Args[1], "packages/api/CLAUDE.md", "host-managed import offered for edits")
		for _, call := range run.calls {
			assert.Equal(t, "claude-opus-5-5", call.argument(t, "--model"))
			assert.Equal(t, "high", call.argument(t, "--effort"))
			assert.Equal(t, "7", call.argument(t, "--max-turns"))
			// Neither phase may run the target repository's hooks or MCP servers.
			assert.Empty(t, call.argument(t, "--setting-sources"))
			assert.Contains(t, call.Args, "--strict-mcp-config")
			assert.Equal(t, "dontAsk", call.argument(t, "--permission-mode"))
			assert.Empty(t, call.Stdin, "CLI drained the batch's stdin")
		}
	})

	t.Run("should run both phases at the configured effort when it is overridden", func(t *testing.T) {
		t.Parallel()
		// given / when
		run := runHelper(t, defaultAudit, "", map[string]string{"CLAUDE_EFFORT": "xhigh"})

		// then
		require.Equal(t, 0, run.code, run.stderr)
		for _, call := range run.calls {
			assert.Equal(t, "xhigh", call.argument(t, "--effort"))
		}
	})

	t.Run("should keep the changelog grants mutually exclusive", func(t *testing.T) {
		t.Parallel()
		for _, extra := range []string{"", ",Bash(chlog new:*)", ",Edit(/CHANGELOG.md)"} {
			t.Run(extra, func(t *testing.T) {
				t.Parallel()
				// given / when
				run := runHelper(t, defaultAudit, extra, nil)

				// then
				require.Equal(t, 0, run.code, run.stderr)
				refresh := run.calls[1]
				assert.Equal(t, "Read,Grep,Glob,Edit(/AGENTS.md),Edit(/README.md),"+
					"Edit(/packages/api/AGENTS.md),Edit(/.claude/skills/check/references/usage.md)"+extra,
					refresh.argument(t, "--allowedTools"))
				// Bash exists only where `chlog new` is the granted command.
				assert.Equal(
					t,
					strings.Contains(extra, "Bash("),
					strings.HasSuffix(refresh.argument(t, "--tools"), ",Bash"),
				)
			})
		}
	})

	t.Run("should keep the diagnostics and skip the refresh when the audit fails", func(t *testing.T) {
		t.Parallel()
		for _, diagnostic := range []string{"monthly usage limit", "safeguards flagged", "network error"} {
			t.Run(diagnostic, func(t *testing.T) {
				t.Parallel()
				// given / when
				run := runHelper(t, diagnostic, "", map[string]string{"AUDIT_EXIT": "23"})

				// then
				assert.Equal(t, 23, run.code)
				assert.Len(t, run.calls, 1)
				assert.Contains(t, run.stdout, diagnostic)
			})
		}
	})

	t.Run("should skip the refresh when the audit result is invalid or unsuccessful", func(t *testing.T) {
		t.Parallel()
		outputs := []string{
			"", "not json", "{}", "null", "[]",
			`{"type":"result","subtype":"error_max_turns","is_error":true,"result":"Reached max turns"}`,
			`{"type":"result","subtype":"success","is_error":true,"result":"monthly usage limit"}`,
			`{"type":"result","subtype":"success","is_error":false,"result":"   "}`,
		}
		for _, output := range outputs {
			t.Run(output, func(t *testing.T) {
				t.Parallel()
				// given / when
				run := runHelper(t, output, "", nil)

				// then
				assert.NotEqual(t, 0, run.code)
				assert.Len(t, run.calls, 1)
				assert.Contains(t, run.stderr, "did not return a successful")
			})
		}
	})

	t.Run("should still allow a factual refresh when the audit finds nothing", func(t *testing.T) {
		t.Parallel()
		// given
		audit := `{"type":"result","subtype":"success","is_error":false,"result":"No findings. Proposed diff: empty."}`

		// when
		run := runHelper(t, audit, "", nil)

		// then
		require.Equal(t, 0, run.code, run.stderr)
		assert.Len(t, run.calls, 2)
	})

	t.Run("should pass a refresh failure on to the batch", func(t *testing.T) {
		t.Parallel()
		// given / when
		run := runHelper(t, defaultAudit, "", map[string]string{"REFRESH_EXIT": "19"})

		// then
		assert.Equal(t, 19, run.code)
		assert.Len(t, run.calls, 2)
	})
}
