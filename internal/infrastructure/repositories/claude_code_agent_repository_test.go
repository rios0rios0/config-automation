package repositories_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/infrastructure/repositories"
)

// fakeClaude records each call's arguments, stdin, and environment below
// CALLS, then answers as AUDIT_RESULT, AUDIT_EXIT, AUDIT_STDERR and
// REFRESH_EXIT tell it.
const fakeClaude = `#!/usr/bin/env bash
set -u
call="${CALLS}/$(ls "${CALLS}" | wc -l)"
mkdir -p "${call}"
printf '%s\0' "$@" > "${call}/args"
cat > "${call}/stdin"
env > "${call}/env"
for argument in "$@"; do
  if [ "${argument}" = '--output-format' ]; then
    if [ -n "${AUDIT_STDERR:-}" ]; then printf '%s\n' "${AUDIT_STDERR}" >&2; fi
    printf '%s' "${AUDIT_RESULT:-}"
    exit "${AUDIT_EXIT:-0}"
  fi
done
echo 'Refresh completed'
echo 'refresh diagnostics' >&2
exit "${REFRESH_EXIT:-0}"
`

const successfulAudit = `{"type":"result","subtype":"success","is_error":false,` +
	`"result":"AGENTS.md:4: stale command; proposed diff: replace it"}`

// agentCall is one invocation the fake recorded.
type agentCall struct {
	arguments   []string
	stdin       string
	environment string
}

// argument returns the value that follows flag.
func (c agentCall) argument(t *testing.T, flag string) string {
	t.Helper()
	index := slices.Index(c.arguments, flag)
	require.GreaterOrEqual(t, index, 0, "missing %s", flag)
	require.Less(t, index+1, len(c.arguments), "no value for %s", flag)
	return c.arguments[index+1]
}

// agentFixture is the fake CLI and what it recorded.
type agentFixture struct {
	t      *testing.T
	binary string
	calls  string
	live   bytes.Buffer
}

func newAgentFixture(t *testing.T) *agentFixture {
	t.Helper()
	calls := filepath.Join(t.TempDir(), "calls")
	// A directory needs its execute bit; the rule below is written for files.
	// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
	require.NoError(t, os.Mkdir(calls, 0o700))
	return &agentFixture{t: t, binary: fakeClaudePath(), calls: calls}
}

// repository returns the adapter over the fake, starting from variables.
func (f *agentFixture) repository(variables map[string]string) *repositories.ClaudeCodeAgentRepository {
	environment := []string{"PATH=" + os.Getenv("PATH"), "CALLS=" + f.calls}
	for name, value := range variables {
		environment = append(environment, name+"="+value)
	}
	return repositories.NewClaudeCodeAgentRepository(
		f.binary, func() []string { return environment }, &f.live, []string{"GH_TOKEN", "AZURE_DEVOPS_PAT"})
}

// recorded returns the calls the fake recorded, in order.
func (f *agentFixture) recorded() []agentCall {
	f.t.Helper()
	entries, err := os.ReadDir(f.calls)
	require.NoError(f.t, err)
	calls := make([]agentCall, len(entries))
	for _, entry := range entries {
		index, parseErr := strconv.Atoi(entry.Name())
		require.NoError(f.t, parseErr)
		read := func(name string) string {
			content, readErr := os.ReadFile(filepath.Join(f.calls, entry.Name(), name))
			require.NoError(f.t, readErr)
			return string(content)
		}
		calls[index] = agentCall{
			arguments:   strings.Split(strings.TrimSuffix(read("args"), "\x00"), "\x00"),
			stdin:       read("stdin"),
			environment: read("env"),
		}
	}
	return calls
}

// run returns an agent run in a scratch directory.
func run(t *testing.T, tools, allowed string) entities.AgentRun {
	t.Helper()
	return entities.AgentRun{
		Directory: t.TempDir(), Prompt: "Refresh only supported guidance.", Model: "claude-opus-5-5",
		Effort: "high", MaxTurns: 7, Tools: tools, AllowedTools: allowed,
	}
}

func TestClaudeCodeAgentRepository(t *testing.T) {
	t.Parallel()

	t.Run("should run a read-only audit locked down like the refresh and return its report", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newAgentFixture(t)
		agent := fixture.repository(map[string]string{
			"AUDIT_RESULT": successfulAudit, "AUDIT_STDERR": "Permission allow rule warning",
			"GH_TOKEN": "fixture-token-placeholder", "AZURE_DEVOPS_PAT": "fixture-token-placeholder",
			"CLAUDE_CODE_OAUTH_TOKEN": "fixture-oauth-placeholder",
		})

		// when
		result, err := agent.Audit(t.Context(), run(t, entities.AuditTools(), entities.AuditTools()))

		// then
		require.NoError(t, err)
		assert.Equal(t, "AGENTS.md:4: stale command; proposed diff: replace it", result.Report)
		assert.Contains(t, result.Output, "Permission allow rule warning")
		calls := fixture.recorded()
		require.Len(t, calls, 1)
		audit := calls[0]
		assert.Equal(t, "Refresh only supported guidance.", audit.argument(t, "-p"))
		assert.Equal(t, "Read,Grep,Glob", audit.argument(t, "--tools"))
		assert.Equal(t, "Read,Grep,Glob", audit.argument(t, "--allowedTools"))
		assert.Equal(t, "json", audit.argument(t, "--output-format"))
		assert.Equal(t, "claude-opus-5-5", audit.argument(t, "--model"))
		assert.Equal(t, "high", audit.argument(t, "--effort"))
		assert.Equal(t, "7", audit.argument(t, "--max-turns"))
		assert.Empty(t, audit.argument(t, "--setting-sources"))
		assert.Contains(t, audit.arguments, "--strict-mcp-config")
		assert.Equal(t, "dontAsk", audit.argument(t, "--permission-mode"))
		assert.Equal(t, "Read(//proc/**)", audit.argument(t, "--disallowedTools"))
		assert.Empty(t, audit.stdin, "the CLI read from the caller's stdin")
		assert.NotContains(t, audit.environment, "fixture-token-placeholder")
		assert.Contains(t, audit.environment, "CLAUDE_CODE_OAUTH_TOKEN=fixture-oauth-placeholder\n")
		assert.Contains(t, fixture.live.String(), "AGENTS.md:4: stale command")
	})

	t.Run("should keep the diagnostics and fail when the audit exits non-zero", func(t *testing.T) {
		t.Parallel()
		for _, diagnostic := range []string{"monthly usage limit", "safeguards flagged", "network error"} {
			t.Run(diagnostic, func(t *testing.T) {
				t.Parallel()
				// given
				fixture := newAgentFixture(t)
				agent := fixture.repository(map[string]string{"AUDIT_RESULT": diagnostic, "AUDIT_EXIT": "23"})

				// when
				result, err := agent.Audit(t.Context(), run(t, entities.AuditTools(), entities.AuditTools()))

				// then
				require.Error(t, err)
				assert.Contains(t, result.Output, diagnostic)
				assert.Contains(t, fixture.live.String(), diagnostic)
				assert.Empty(t, result.Report)
			})
		}
	})

	t.Run("should fail an audit whose result is invalid or unsuccessful", func(t *testing.T) {
		t.Parallel()
		for _, output := range []string{
			"", "not json", "{}", "null", "[]",
			`{"type":"result","subtype":"error_max_turns","is_error":true,"result":"Reached max turns"}`,
			`{"type":"result","subtype":"success","is_error":true,"result":"monthly usage limit"}`,
			`{"type":"result","subtype":"success","is_error":false,"result":"   "}`,
			`{"type":"result","subtype":"success","result":"no error flag"}`,
		} {
			t.Run(output, func(t *testing.T) {
				t.Parallel()
				// given
				fixture := newAgentFixture(t)
				agent := fixture.repository(map[string]string{"AUDIT_RESULT": output})

				// when
				result, err := agent.Audit(t.Context(), run(t, entities.AuditTools(), entities.AuditTools()))

				// then
				require.Error(t, err)
				assert.Contains(t, err.Error(), "did not return a successful")
				assert.Contains(t, fixture.live.String(), "did not return a successful")
				assert.Empty(t, result.Report)
			})
		}
	})

	t.Run("should run the refresh with its grants and stream its output", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newAgentFixture(t)
		agent := fixture.repository(map[string]string{"GH_TOKEN": "fixture-token-placeholder"})
		allowed := "Read,Grep,Glob,Edit(/README.md),Bash(chlog new:*)"

		// when
		result, err := agent.Refresh(t.Context(), run(t, entities.RefreshTools("Bash(chlog new:*)"), allowed))

		// then
		require.NoError(t, err)
		calls := fixture.recorded()
		require.Len(t, calls, 1)
		assert.Equal(t, "Read,Grep,Glob,Edit,Write,Bash", calls[0].argument(t, "--tools"))
		assert.Equal(t, allowed, calls[0].argument(t, "--allowedTools"))
		assert.Equal(t, "Read(//proc/**)", calls[0].argument(t, "--disallowedTools"))
		assert.NotContains(t, calls[0].arguments, "--output-format")
		assert.Empty(t, calls[0].stdin)
		assert.NotContains(t, calls[0].environment, "fixture-token-placeholder")
		assert.Contains(t, fixture.live.String(), "Refresh completed")
		assert.Contains(t, result.Output, "refresh diagnostics")
	})

	t.Run("should fail a refresh that exits non-zero and keep its output", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newAgentFixture(t)
		agent := fixture.repository(map[string]string{"REFRESH_EXIT": "19"})

		// when
		result, err := agent.Refresh(t.Context(), run(t, entities.RefreshTools(""), "Read"))

		// then
		require.Error(t, err)
		assert.Contains(t, result.Output, "Refresh completed")
	})
}
