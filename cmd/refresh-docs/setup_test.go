package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const (
	// fixtureTokenVariable holds the token every fixture configuration
	// references; TestMain sets it, since gitforge resolves `${VAR}` from the
	// process environment.
	fixtureTokenVariable = "CONFIG_AUTOMATION_FIXTURE_TOKEN"
	fixtureToken         = "fixture-token-placeholder"
	fixtureOAuthToken    = "fixture-oauth-placeholder"
)

// fakeClaude records each call below FIXTURE_ROOT and, unless the clone is
// FAIL_AUDIT_REPO, audits successfully; the refresh records its prompt,
// copies WRITES over the clone, deletes the paths DELETES lists, and echoes
// CLAUDE_ECHO.
const fakeClaude = `#!/usr/bin/env bash
set -euo pipefail
basename -- "${PWD}" >> "${FIXTURE_ROOT}/claude-calls"
env >> "${FIXTURE_ROOT}/claude-environment"
if [ "$(wc -c)" -ne 0 ]; then
  echo 'batch stdin leaked' >&2
  exit 1
fi
for argument in "$@"; do
  if [ "${argument}" = '--output-format' ]; then
    if [ "$(basename -- "${PWD}")" = "${FAIL_AUDIT_REPO:-}" ]; then
      echo "${FAIL_AUDIT_MESSAGE:-network error}"
      exit 23
    fi
    echo '{"type":"result","subtype":"success","is_error":false,"result":"Audit completed; proposed corrections."}'
    exit 0
  fi
done
printf '%s' "$2" > "${FIXTURE_ROOT}/refresh-prompt"
if [ -d "${WRITES:-/nonexistent}" ]; then
  cp -R "${WRITES}/." .
fi
if [ -f "${DELETES:-/nonexistent}" ]; then
  while IFS= read -r path; do rm -f -- "${path}"; done < "${DELETES}"
fi
if [ -n "${CLAUDE_ECHO:-}" ]; then
  printf '%s\n' "${CLAUDE_ECHO}"
fi
echo 'Refresh completed'
`

func TestMain(m *testing.M) {
	os.Exit(isolateAndRun(m))
}

// isolateAndRun shuts this machine's git configuration out of every test,
// so no commit is signed and no hook runs, and writes the fake Claude CLI
// before any parallel test can fork while it is open for writing.
func isolateAndRun(m *testing.M) int {
	directory := filepath.Dir(fakeClaudePath())
	// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
	if err := os.MkdirAll(directory, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(directory)
	gitConfig := filepath.Join(directory, "gitconfig")
	settings := "[init]\n\tdefaultBranch = main\n[commit]\n\tgpgsign = false\n[tag]\n\tgpgsign = false\n" +
		"[user]\n\tname = fixture\n\temail = fixture@example.invalid\n"
	if err := os.WriteFile(gitConfig, []byte(settings), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// The fake must be executable to stand in for the CLI.
	// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
	if err := os.WriteFile(fakeClaudePath(), []byte(fakeClaude), 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for name, value := range map[string]string{
		"GIT_CONFIG_GLOBAL": gitConfig, "GIT_CONFIG_NOSYSTEM": "1", "GIT_TERMINAL_PROMPT": "0",
		fixtureTokenVariable: fixtureToken,
	} {
		if err := os.Setenv(name, value); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	return m.Run()
}

// fakeClaudePath is where TestMain writes the fake Claude CLI.
func fakeClaudePath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("refresh-docs-fakes-%d", os.Getpid()), "claude")
}
