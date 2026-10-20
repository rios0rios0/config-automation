package scripts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/test/domain/fixtures"
)

const (
	refreshBranch = "chore/config-and-docs-refresh"
	original      = "Original documentation\n"
	batchTimeout  = 90 * time.Second
	legacyClaude  = entities.LegacyBanner + "\n## Build\n\nmake test\n"
)

// batchGitStub passes every call to the real git, except that it fails
// `git -C <clone> add --` for FAIL_STAGE_REPO and resolves a clone URL to
// the bare fixture repository of the same name.
const batchGitStub = `set -u
if [ "$#" -gt 3 ] && [ "$1" = '-C' ] && [ "$3" = 'add' ] && [ "$4" = '--' ] \
    && [ "$(basename -- "$2")" = "${FAIL_STAGE_REPO:-}" ]; then
  exit 22
fi
if [ "$#" -ge 3 ] && [ "$1" = 'clone' ] && [ "$2" = '--quiet' ]; then
  url="$3"
  shift 3
  set -- clone --quiet "${FIXTURE_ROOT}/${url##*/}" "$@"
fi
exec "${REAL_GIT}" "$@"
`

// batchGhStub records every PR it is asked to create.
const batchGhStub = `if [ "${1:-}" = 'pr' ] && [ "${2:-}" = 'create' ]; then
  echo 'created' >> "${FIXTURE_ROOT}/prs"
  echo 'https://github.com/fixture/repository/pull/1'
fi
`

// batchClaudeStub audits successfully unless FAIL_AUDIT_REPO names the
// clone, and on refresh records the prompt, applies REFRESH_WRITES (a JSON
// object of path to content, null deleting), and echoes CLAUDE_ECHO.
const batchClaudeStub = `set -euo pipefail
basename -- "${PWD}" >> "${FIXTURE_ROOT}/claude-calls"
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
while IFS= read -r -d '' path; do
  if jq -e --arg path "${path}" '.[$path] == null' <<< "${REFRESH_WRITES}" > /dev/null; then
    rm -f -- "${path}"
  else
    mkdir -p -- "$(dirname -- "${path}")"
    jq -j --arg path "${path}" '.[$path]' <<< "${REFRESH_WRITES}" > "${path}"
  fi
done < <(jq -j 'keys_unsorted[] | . + "\u0000"' <<< "${REFRESH_WRITES}")
if [ -n "${CLAUDE_ECHO:-}" ]; then
  printf '%s\n' "${CLAUDE_ECHO}"
fi
echo 'Refresh completed'
`

// batchFixture is one workflow batch over local bare repositories.
type batchFixture struct {
	t     *testing.T
	root  string
	env   map[string]string
	repos []map[string]string
}

// batchRun is the outcome of one batch.
type batchRun struct {
	code   int
	stdout string
	stderr string
}

func newBatchFixture(t *testing.T) *batchFixture {
	t.Helper()
	root := t.TempDir()
	bin := t.TempDir()
	realGit, err := exec.LookPath("git")
	require.NoError(t, err)
	for name, body := range map[string]string{"git": batchGitStub, "gh": batchGhStub, "claude": batchClaudeStub} {
		writeStub(t, bin, name, body)
	}
	separator := string(os.PathListSeparator)
	return &batchFixture{t: t, root: root, env: map[string]string{
		"GIT_CONFIG_GLOBAL": filepath.Join(root, "gitconfig"), "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_TERMINAL_PROMPT": "0", "XDG_CONFIG_HOME": filepath.Join(root, "xdg"),
		"TMPDIR": root, "GITHUB_WORKSPACE": projectRoot(), "REAL_GIT": realGit, "FIXTURE_ROOT": root,
		"PATH":     bin + separator + os.Getenv(binaryDirectoryVariable) + separator + os.Getenv("PATH"),
		"GH_TOKEN": "fixture-token-placeholder", "CLAUDE_MODEL": "claude-opus-5-5", "MAX_TURNS": "7",
		"STALE_AFTER_DAYS": "0", "REPO_INPUT": "", "REFRESH_BRANCH": refreshBranch, "REFRESH_WRITES": "{}",
	}}
}

// git runs the real git in directory with the fixture's isolated settings.
func (f *batchFixture) git(directory string, arguments ...string) string {
	f.t.Helper()
	return f.gitWith(directory, nil, arguments...)
}

// gitWith runs git with extra environment variables.
func (f *batchFixture) gitWith(directory string, extra map[string]string, arguments ...string) string {
	f.t.Helper()
	variables := map[string]string{}
	maps.Copy(variables, f.env)
	maps.Copy(variables, extra)
	command := exec.CommandContext(f.t.Context(), f.env["REAL_GIT"], arguments...)
	command.Dir, command.Env = directory, environment(variables)
	output, err := command.CombinedOutput()
	require.NoError(f.t, err, string(output))
	return strings.TrimSpace(string(output))
}

// repository commits files, plus an unrelated source file, to a new bare
// repository the batch will clone. An old one predates the activity gate.
func (f *batchFixture) repository(name string, files map[string]string, old bool) (string, string) {
	f.t.Helper()
	work := filepath.Join(f.root, "source-"+name)
	f.git(f.root, "init", "--quiet", "-b", "main", work)
	f.git(work, "config", "user.name", "fixture")
	f.git(work, "config", "user.email", "fixture@example.invalid")
	files["source.txt"] = "Original implementation\n"
	writeTree(f.t, work, files)
	f.git(work, "add", ".")
	dates := map[string]string{}
	if old {
		dates = map[string]string{
			"GIT_AUTHOR_DATE":    "2020-01-01T00:00:00Z",
			"GIT_COMMITTER_DATE": "2020-01-01T00:00:00Z",
		}
	}
	f.gitWith(work, dates, "commit", "--quiet", "-m", "Initial fixture")
	remote := filepath.Join(f.root, name+".git")
	f.git(f.root, "clone", "--quiet", "--bare", work, remote)
	f.repos = append(f.repos, map[string]string{"owner": "fixture", "name": name, "default_branch": "main"})
	return remote, work
}

// run executes the workflow's batch step with writes standing in for the
// edits the refresh makes; a nil content deletes the file.
func (f *batchFixture) run(writes map[string]any, overrides map[string]string) batchRun {
	f.t.Helper()
	workflow, err := os.ReadFile(filepath.Join(projectRoot(), ".github/workflows/config-and-docs-refresh.yaml"))
	require.NoError(f.t, err)
	repos, err := json.Marshal(f.repos)
	require.NoError(f.t, err)
	if writes == nil {
		writes = map[string]any{}
	}
	edits, err := json.Marshal(writes)
	require.NoError(f.t, err)
	variables := map[string]string{"REPOS": string(repos), "REFRESH_WRITES": string(edits)}
	for name, value := range f.env {
		if _, set := variables[name]; !set {
			variables[name] = value
		}
	}
	maps.Copy(variables, overrides)
	ctx, cancel := context.WithTimeout(f.t.Context(), batchTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "bash", "-c", batchScript(f.t, string(workflow)))
	command.Dir, command.Env = f.root, environment(variables)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	run := batchRun{}
	if runErr := command.Run(); runErr != nil {
		var exitErr *exec.ExitError
		require.ErrorAs(f.t, runErr, &exitErr, runErr)
		run.code = exitErr.ExitCode()
	}
	run.stdout, run.stderr = stdout.String(), stderr.String()
	return run
}

// show returns a file of the refresh branch of remote.
func (f *batchFixture) show(remote, path string) string {
	f.t.Helper()
	return f.git(remote, "show", refreshBranch+":"+path)
}

// batchScript extracts the `run` block of the workflow's batch step.
func batchScript(t *testing.T, workflow string) string {
	t.Helper()
	_, step, found := strings.Cut(workflow, "      - name: 'Process batch")
	require.True(t, found, "batch step not found")
	_, block, found := strings.Cut(step, "        run: |\n")
	require.True(t, found, "batch script not found")
	lines := []string{}
	for line := range strings.Lines(block) {
		line = strings.TrimSuffix(line, "\n")
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, strings.Repeat(" ", 10)) {
			break
		}
		lines = append(lines, strings.TrimPrefix(line, strings.Repeat(" ", 10)))
	}
	return strings.Join(lines, "\n")
}

// withContent maps every path to the same content.
func withContent(paths []string, content string) map[string]string {
	files := map[string]string{}
	for _, path := range paths {
		files[path] = content
	}
	return files
}

// compliant returns a repository already in the layout, plus extra files.
func compliant(extra map[string]string) map[string]string {
	files := map[string]string{"AGENTS.md": original, "CLAUDE.md": entities.AgentsImport}
	maps.Copy(files, extra)
	return files
}

func TestRefreshBatch(t *testing.T) {
	t.Parallel()

	t.Run("should bring every category into one PR without staging unrelated files", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newBatchFixture(t)
		paths := slices.Concat(fixtures.SupportedRefreshPaths(), slices.DeleteFunc(
			fixtures.ExcludedRefreshPaths(), func(path string) bool { return path == "CHANGELOG.md" }))
		remote, _ := fixture.repository("complete", withContent(paths, original), false)
		writes := map[string]any{"source.txt": "Unexpected out-of-scope change\n"}
		for _, path := range fixtures.SupportedRefreshPaths() {
			if !slices.Contains(fixtures.PairedImports(), path) {
				writes[path] = "Updated documentation\n"
			}
		}

		// when
		run := fixture.run(writes, nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		changed := strings.Split(fixture.git(remote, "diff", "--name-only", "main", refreshBranch), "\n")
		expected := fixtures.SupportedRefreshPaths()
		for _, path := range fixtures.PairedImports() {
			if !slices.Contains(expected, path) {
				expected = append(expected, path)
			}
		}
		assert.ElementsMatch(t, expected, changed)
		for _, path := range fixtures.PairedImports() {
			assert.Equal(t, strings.TrimSpace(entities.AgentsImport), fixture.show(remote, path))
		}
		prs, err := os.ReadFile(filepath.Join(fixture.root, "prs"))
		require.NoError(t, err)
		assert.Equal(t, "created\n", string(prs))
	})

	t.Run("should keep processing the batch when an audit fails", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newBatchFixture(t)
		fixture.repository("first", map[string]string{"README.md": original}, false)
		remote, _ := fixture.repository("second", map[string]string{"README.md": original}, false)

		// when
		run := fixture.run(
			map[string]any{"README.md": "Updated\n"},
			map[string]string{"FAIL_AUDIT_REPO": "fixture__first"},
		)

		// then
		assert.Equal(t, 1, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "claude: fixture/first")
		assert.Contains(t, run.stdout, "processed (2/2)")
		assert.Equal(t, "Updated", fixture.show(remote, "README.md"))
	})

	t.Run("should not open a PR when nothing changes", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newBatchFixture(t)
		fixture.repository("quiet", compliant(map[string]string{"README.md": original}), false)

		// when
		run := fixture.run(nil, nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "no_drift (1)")
		assert.NoFileExists(t, filepath.Join(fixture.root, "prs"))
	})

	t.Run("should not open a PR for a changelog-only edit", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newBatchFixture(t)
		fixture.repository("changelog", compliant(map[string]string{"CHANGELOG.md": original}), false)

		// when
		run := fixture.run(map[string]any{"CHANGELOG.md": "Stray changelog entry\n"}, nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "no_drift (1)")
		assert.NoFileExists(t, filepath.Join(fixture.root, "prs"))
	})

	t.Run("should not open a PR for a whitespace-only edit", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newBatchFixture(t)
		fixture.repository("whitespace", compliant(map[string]string{"README.md": original}), false)

		// when
		run := fixture.run(map[string]any{"README.md": "Original   documentation   \n"}, nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "no_drift (1)")
		assert.NoFileExists(t, filepath.Join(fixture.root, "prs"))
	})

	t.Run("should keep processing the batch when staging fails", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newBatchFixture(t)
		fixture.repository("first", map[string]string{"README.md": original}, false)
		remote, _ := fixture.repository("second", map[string]string{"README.md": original}, false)

		// when
		run := fixture.run(
			map[string]any{"README.md": "Updated\n"},
			map[string]string{"FAIL_STAGE_REPO": "fixture__first"},
		)

		// then
		assert.Equal(t, 1, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "stage: fixture/first")
		assert.Contains(t, run.stdout, "processed (2/2)")
		assert.Equal(t, "Updated", fixture.show(remote, "README.md"))
	})

	t.Run("should not reactivate a quiet repository with a documentation-only commit", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newBatchFixture(t)
		files := withContent(fixtures.SupportedRefreshPaths(), original)
		for _, path := range fixtures.PairedImports() {
			files[path] = entities.AgentsImport
		}
		remote, work := fixture.repository("quiet", files, true)
		for _, path := range fixtures.SupportedRefreshPaths() {
			if !slices.Contains(fixtures.PairedImports(), path) {
				require.NoError(
					t,
					os.WriteFile(filepath.Join(work, filepath.FromSlash(path)), []byte("Recent refresh\n"), 0o600),
				)
			}
		}
		fixture.git(work, "commit", "--quiet", "-am", "Refreshed documentation")
		fixture.git(work, "push", "--quiet", remote, "main")

		// when
		run := fixture.run(nil, map[string]string{"STALE_AFTER_DAYS": "8"})

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "unchanged (1)")
		assert.NoFileExists(t, filepath.Join(fixture.root, "claude-calls"))
	})

	t.Run("should still give a README-only repository initial guidance", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newBatchFixture(t)
		remote, _ := fixture.repository("bootstrap", map[string]string{"README.md": original}, true)

		// when
		run := fixture.run(
			map[string]any{"AGENTS.md": "New useful guidance\n"},
			map[string]string{"STALE_AFTER_DAYS": "8"},
		)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Equal(t, "New useful guidance", fixture.show(remote, "AGENTS.md"))
		assert.Equal(t, strings.TrimSpace(entities.AgentsImport), fixture.show(remote, "CLAUDE.md"))
	})

	t.Run("should detect and stage a tracked deletion", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newBatchFixture(t)
		remote, _ := fixture.repository("deletion", compliant(map[string]string{"README.md": original}), false)

		// when
		run := fixture.run(map[string]any{"README.md": nil}, nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Equal(t, "D\tREADME.md", fixture.git(remote, "diff", "--name-status", "main", refreshBranch))
	})

	t.Run("should move a legacy CLAUDE.md to AGENTS.md even when the repository is quiet", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newBatchFixture(t)
		remote, _ := fixture.repository(
			"legacy",
			map[string]string{"CLAUDE.md": legacyClaude, "README.md": "Read me\n"},
			true,
		)

		// when
		run := fixture.run(nil, map[string]string{"STALE_AFTER_DAYS": "8"})

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "unchanged (0)")
		assert.Equal(t, "chore(refresh): refreshed configuration and documentation\n"+entities.RenameMessage,
			fixture.git(remote, "log", "--format=%s", "main.."+refreshBranch))
		assert.Contains(t, fixture.git(remote, "log", "--follow", "--format=%s", refreshBranch, "--", "AGENTS.md"),
			"Initial fixture")
		assert.Equal(t, strings.TrimSpace(entities.AgentsImport), fixture.show(remote, "CLAUDE.md"))
		agents := fixture.show(remote, "AGENTS.md")
		assert.True(t, strings.HasPrefix(agents, "# AGENTS.md\n"))
		assert.True(t, strings.HasSuffix(agents, "## Build\n\nmake test"))
		prompt, err := os.ReadFile(filepath.Join(fixture.root, "refresh-prompt"))
		require.NoError(t, err)
		assert.Contains(t, string(prompt), "## Guidance layout for this repository")
		assert.Contains(t, string(prompt), "Renamed `CLAUDE.md` to `AGENTS.md`")
	})

	t.Run("should keep processing the batch when the layout cannot be staged", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newBatchFixture(t)
		fixture.repository("first", map[string]string{"AGENTS.md": "Guidance\n"}, false)
		remote, _ := fixture.repository("second", map[string]string{"README.md": original}, false)

		// when
		run := fixture.run(
			map[string]any{"README.md": "Updated\n"},
			map[string]string{"FAIL_STAGE_REPO": "fixture__first"},
		)

		// then
		assert.Equal(t, 1, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "layout: fixture/first")
		assert.Contains(t, run.stdout, "processed (2/2)")
		assert.Equal(t, "Updated", fixture.show(remote, "README.md"))
	})

	t.Run("should stop workflow commands while Claude's output is printed", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newBatchFixture(t)
		fixture.repository("echo", compliant(map[string]string{"README.md": original}), false)
		echoed := "::warning::injected ##[add-mask]value"

		// when
		run := fixture.run(nil, map[string]string{"CLAUDE_ECHO": echoed})

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		stop := regexp.MustCompile(`(?m)^::stop-commands::([0-9a-f]{32})$`).FindStringSubmatch(run.stdout)
		require.Len(t, stop, 2, run.stdout)
		resume := "\n::" + stop[1] + "::\n"
		require.Contains(t, run.stdout, resume)
		assert.Less(t, strings.Index(run.stdout, stop[0]), strings.Index(run.stdout, echoed))
		assert.Less(t, strings.Index(run.stdout, echoed), strings.Index(run.stdout, resume))
	})
}

func TestRefreshBatchUsageLimit(t *testing.T) {
	t.Parallel()

	for _, message := range []string{"You have hit your weekly limit", "Claude AI monthly usage limit reached"} {
		t.Run("should skip the rest of the batch when Claude reports: "+message, func(t *testing.T) {
			t.Parallel()
			// given
			fixture := newBatchFixture(t)
			fixture.repository("first", map[string]string{"README.md": original}, false)
			fixture.repository("second", map[string]string{"README.md": original}, false)

			// when
			run := fixture.run(nil, map[string]string{
				"FAIL_AUDIT_REPO": "fixture__first", "FAIL_AUDIT_MESSAGE": message,
			})

			// then
			assert.Equal(t, 1, run.code, run.stdout+run.stderr)
			assert.Contains(t, run.stdout, "claude-quota: fixture/first")
			assert.Contains(t, run.stdout, "quota_skipped (1)")
			assert.Contains(t, run.stdout, "fixture/second (SKIPPED: Claude usage limit hit earlier in batch)")
			calls, err := os.ReadFile(filepath.Join(fixture.root, "claude-calls"))
			require.NoError(t, err)
			assert.Equal(t, "fixture__first\n", string(calls))
		})
	}
}
