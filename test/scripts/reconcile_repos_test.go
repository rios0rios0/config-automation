package scripts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// reconcileToken stands in for the owner's token.
	reconcileToken = "fixture-token-placeholder"
	// reconcileTimeout bounds one run of the script.
	reconcileTimeout = 60 * time.Second
	// shortSHALength is how much of a commit the script prints.
	shortSHALength = 8
)

// reconcileGitStub passes every call to the real git, except that it resolves
// a github.com clone URL to the bare fixture repository of the same name and
// fails `git -C <clone> fetch` for FAIL_FETCH_REPO. It records every argument,
// every clone URL, and for each clone and push the password git's configured
// helper answers for github.com, which is what the real HTTPS transport sends.
const reconcileGitStub = `set -u
printf '%s\n' "$@" >> "${FIXTURE_ROOT}/git-arguments"
answer() {
  password="$(printf 'protocol=https\nhost=github.com\n\n' \
    | "${REAL_GIT}" credential fill 2>/dev/null | sed -n 's/^password=//p')"
  printf '%s %s\n' "$1" "${password}" >> "${FIXTURE_ROOT}/credentials"
}
if [ "${1:-}" = 'clone' ]; then
  answer clone
  arguments=()
  for argument in "$@"; do
    case "${argument}" in
      https://github.com/*)
        printf '%s\n' "${argument}" >> "${FIXTURE_ROOT}/clone-urls"
        arguments+=("${FIXTURE_ROOT}/remotes/${argument##*/}") ;;
      *) arguments+=("${argument}") ;;
    esac
  done
  set -- "${arguments[@]}"
elif [ "$#" -ge 3 ] && [ "$1" = '-C' ] && [ "$3" = 'fetch' ] \
    && [ "$(basename -- "$2")" = "${FAIL_FETCH_REPO:-}" ]; then
  echo "fatal: unable to access 'https://github.com/fixture/repository.git/': The requested URL returned error: 500" >&2
  exit 128
elif [ "$#" -ge 3 ] && [ "$1" = '-C' ] && [ "$3" = 'push' ]; then
  answer push
fi
exec "${REAL_GIT}" "$@"
`

// reconcileDetectorStub stands in for the pipelines detector, which the
// script runs with `sh`: it records the password git answers for the
// detector's own commands and prints the gaps the test recorded for the clone.
const reconcileDetectorStub = `printf 'protocol=https\nhost=github.com\n\n' \
  | git credential fill 2>/dev/null | sed -n 's/^password=/detector /p' >> "${FIXTURE_ROOT}/credentials"
cat "${FIXTURE_ROOT}/gaps/$(basename "$1")" 2>/dev/null || true
`

// gap is one row the detector double reports.
type gap struct {
	version     string
	needsReview bool
}

// reconcileFixture is one reconcile-repos.sh run over local bare repositories.
type reconcileFixture struct {
	t     *testing.T
	root  string
	env   map[string]string
	repos []map[string]string
}

// reconcileRun is the outcome of one run.
type reconcileRun struct {
	code   int
	stdout string
	stderr string
}

func newReconcileFixture(t *testing.T) *reconcileFixture {
	t.Helper()
	root := t.TempDir()
	bin := t.TempDir()
	realGit, err := exec.LookPath("git")
	require.NoError(t, err)
	writeStub(t, bin, "git", reconcileGitStub)
	writeTree(t, root, map[string]string{"detector.sh": reconcileDetectorStub})
	return &reconcileFixture{t: t, root: root, env: map[string]string{
		"GIT_CONFIG_GLOBAL": filepath.Join(root, "gitconfig"), "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_TERMINAL_PROMPT": "0", "XDG_CONFIG_HOME": filepath.Join(root, "xdg"),
		"TMPDIR": root, "REAL_GIT": realGit, "FIXTURE_ROOT": root,
		"PATH":     bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"GH_TOKEN": reconcileToken, "RECONCILE_SCRIPT": filepath.Join(root, "detector.sh"),
		"HARDEN_OWNER": "fixture", "GITHUB_STEP_SUMMARY": filepath.Join(root, "summary.md"),
		"DRY_RUN": "false", "FAIL_ON_GAP": "false",
	}}
}

// repository creates a bare repository whose one commit carries tags, records
// the gaps the detector double reports for it at that commit, and returns the
// repository and the commit.
func (f *reconcileFixture) repository(name string, tags []string, gaps ...gap) (string, string) {
	f.t.Helper()
	work := filepath.Join(f.root, "source-"+name)
	f.git(f.root, "init", "--quiet", "-b", "main", work)
	writeTree(f.t, work, map[string]string{"CHANGELOG.md": "# Changelog\n"})
	f.git(work, "add", ".")
	f.git(work, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid",
		"commit", "--quiet", "-m", "chore(bump): bumped the version")
	for _, tag := range tags {
		f.git(work, "tag", tag)
	}
	remote := filepath.Join(f.root, "remotes", name+".git")
	f.git(f.root, "clone", "--quiet", "--bare", work, remote)
	sha := f.git(work, "rev-parse", "HEAD")
	rows := ""
	var rowsSb130 strings.Builder
	for _, row := range gaps {
		if row.needsReview {
			rowsSb130.WriteString(row.version + "\t-\tneeds-review\n")
		} else {
			rowsSb130.WriteString(row.version + "\t" + sha + "\trecoverable\n")
		}
	}
	rows += rowsSb130.String()
	writeTree(f.t, f.root, map[string]string{"gaps/fixture__" + name: rows})
	f.repos = append(f.repos, map[string]string{"owner": "fixture", "name": name})
	return remote, sha
}

// run executes reconcile-repos.sh over every repository the fixture created.
func (f *reconcileFixture) run(overrides map[string]string) reconcileRun {
	f.t.Helper()
	repos, err := json.Marshal(f.repos)
	require.NoError(f.t, err)
	variables := map[string]string{"REPOS_JSON": string(repos)}
	maps.Copy(variables, f.env)
	maps.Copy(variables, overrides)
	ctx, cancel := context.WithTimeout(f.t.Context(), reconcileTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "bash", filepath.Join(projectRoot(), "scripts/reconcile-repos.sh"))
	command.Dir, command.Env = f.root, environment(variables)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	run := reconcileRun{}
	if runErr := command.Run(); runErr != nil {
		var exitErr *exec.ExitError
		require.ErrorAs(f.t, runErr, &exitErr, runErr)
		run.code = exitErr.ExitCode()
	}
	run.stdout, run.stderr = stdout.String(), stderr.String()
	return run
}

// git runs the real git in directory with the fixture's isolated settings.
func (f *reconcileFixture) git(directory string, arguments ...string) string {
	f.t.Helper()
	command := exec.CommandContext(f.t.Context(), f.env["REAL_GIT"], arguments...)
	command.Dir, command.Env = directory, environment(f.env)
	output, err := command.CombinedOutput()
	require.NoError(f.t, err, string(output))
	return strings.TrimSpace(string(output))
}

// tags lists the tags of remote.
func (f *reconcileFixture) tags(remote string) []string {
	f.t.Helper()
	return strings.Fields(f.git(remote, "tag", "--list"))
}

// record returns what the doubles or the script wrote to name below the
// fixture root, or nothing when they wrote nothing.
func (f *reconcileFixture) record(name string) string {
	f.t.Helper()
	content, err := os.ReadFile(filepath.Join(f.root, name))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	require.NoError(f.t, err)
	return string(content)
}

// pushed reports whether the script ran any `git push`.
func (f *reconcileFixture) pushed() bool {
	f.t.Helper()
	return slices.Contains(strings.Split(f.record("git-arguments"), "\n"), "push")
}

// line is the log line the script prints for a gap.
func line(slug, version, sha, action string) string {
	return "  " + slug + " " + version + " (" + sha[:min(len(sha), shortSHALength)] + "): " + action + "\n"
}

// counts is the summary's tally of the gaps.
func counts(recovered, skipped, review string) string {
	return "**" + recovered + "** recovered, **" + skipped + "** skipped as not above the highest tag, **" +
		review + "** need review."
}

func TestReconcileRepos(t *testing.T) {
	t.Parallel()

	t.Run("should skip a superseded bump when a later version is already tagged", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newReconcileFixture(t)
		remote, sha := fixture.repository("frontend", []string{"0.3.0", "0.4.1"}, gap{version: "0.4.0"})

		// when
		run := fixture.run(nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.ElementsMatch(t, []string{"0.3.0", "0.4.1"}, fixture.tags(remote))
		assert.False(t, fixture.pushed())
		assert.Contains(t, run.stdout,
			line("fixture/frontend", "0.4.0", sha, "skipped: 0.4.0 is below the highest tag 0.4.1"))
		assert.Contains(t, fixture.record("summary.md"), counts("0", "1", "0"))
	})

	t.Run("should push a gap whose version is above the highest tag", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newReconcileFixture(t)
		remote, sha := fixture.repository("backend", []string{"2.1.1", "2.1.2"}, gap{version: "2.2.0"})

		// when
		run := fixture.run(nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Equal(t, sha, fixture.git(remote, "rev-parse", "2.2.0^{commit}"))
		assert.Contains(t, run.stdout, line("fixture/backend", "2.2.0", sha, "tag pushed — delivery re-triggered"))
		assert.Contains(t, fixture.record("summary.md"), counts("1", "0", "0"))
	})

	t.Run("should skip a gap whose version equals the highest tag", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newReconcileFixture(t)
		remote, sha := fixture.repository("iac", []string{"0.9.0", "1.0.0+build.5"}, gap{version: "1.0.0"})

		// when
		run := fixture.run(nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.ElementsMatch(t, []string{"0.9.0", "1.0.0+build.5"}, fixture.tags(remote))
		assert.False(t, fixture.pushed())
		assert.Contains(t, run.stdout,
			line("fixture/iac", "1.0.0", sha, "skipped: 1.0.0 equals the highest tag 1.0.0+build.5"))
	})

	t.Run("should order versions by SemVer precedence rather than as text", func(t *testing.T) {
		t.Parallel()
		// given
		cases := []struct {
			name    string
			tags    []string
			version string
			reason  string
		}{
			{name: "numeric", tags: []string{"0.9.2", "0.10.0"}, version: "0.9.3",
				reason: "is below the highest tag 0.10.0"},
			{name: "numeric-above", tags: []string{"0.9.2", "0.10.0"}, version: "0.11.0"},
			{name: "prefixed", tags: []string{"v2.0.0"}, version: "1.9.0", reason: "is below the highest tag v2.0.0"},
			{name: "release-above-prerelease", tags: []string{"1.0.1-rc1"}, version: "1.0.1"},
			{name: "not-semver", tags: []string{"v8", "v2.5", "latest"}, version: "1.0.0"},
			{name: "untagged", version: "0.1.0"},
			{name: "fork", tags: []string{"1.0.0-1"}, version: "1.0.0-2"},
			{name: "fork-after-release", tags: []string{"1.0.0"}, version: "1.0.0-1",
				reason: "is below the highest tag 1.0.0"},
			{name: "release-after-fork", tags: []string{"1.0.0-1"}, version: "1.0.0",
				reason: "is below the highest tag 1.0.0-1"},
		}
		fixture := newReconcileFixture(t)
		remotes, shas := map[string]string{}, map[string]string{}
		for _, c := range cases {
			remotes[c.name], shas[c.name] = fixture.repository(c.name, c.tags, gap{version: c.version})
		}

		// when
		run := fixture.run(nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		for _, c := range cases {
			slug := "fixture/" + c.name
			if c.reason == "" {
				assert.Contains(t, fixture.tags(remotes[c.name]), c.version, c.name)
				assert.Contains(t, run.stdout,
					line(slug, c.version, shas[c.name], "tag pushed — delivery re-triggered"), c.name)
			} else {
				assert.ElementsMatch(t, c.tags, fixture.tags(remotes[c.name]), c.name)
				assert.Contains(t, run.stdout,
					line(slug, c.version, shas[c.name], "skipped: "+c.version+" "+c.reason), c.name)
			}
		}
	})

	t.Run("should push no tag in a dry run", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newReconcileFixture(t)
		remote, sha := fixture.repository("frontend", []string{"0.4.1"}, gap{version: "0.4.0"}, gap{version: "0.5.0"})

		// when
		run := fixture.run(map[string]string{"DRY_RUN": "true"})

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Equal(t, []string{"0.4.1"}, fixture.tags(remote))
		assert.False(t, fixture.pushed())
		assert.Contains(t, run.stdout,
			line("fixture/frontend", "0.4.0", sha, "skipped: 0.4.0 is below the highest tag 0.4.1"))
		assert.Contains(t, run.stdout, line("fixture/frontend", "0.5.0", sha, "would push tag (dry run)"))
	})

	t.Run("should not leave a superseded gap without a bump commit for review", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newReconcileFixture(t)
		fixture.repository("iac", []string{"0.4.0"}, gap{version: "0.2.0", needsReview: true})

		// when
		run := fixture.run(map[string]string{"FAIL_ON_GAP": "true"})

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Contains(
			t,
			run.stdout,
			line("fixture/iac", "0.2.0", "-", "skipped: 0.2.0 is below the highest tag 0.4.0"),
		)
		assert.Contains(t, fixture.record("summary.md"), counts("0", "1", "0"))
	})

	t.Run("should still fail on a gap above the highest tag without a bump commit", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newReconcileFixture(t)
		fixture.repository("iac", []string{"0.4.0"}, gap{version: "0.5.0", needsReview: true})

		// when
		run := fixture.run(map[string]string{"FAIL_ON_GAP": "true"})

		// then
		assert.Equal(t, 1, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, line("fixture/iac", "0.5.0", "-", "needs manual review (no bump commit found)"))
		assert.Contains(t, run.stderr, "FAIL: 1 unrecoverable gap(s) need manual review.")
	})

	t.Run("should log why a tag push failed and authenticate without exposing the token", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newReconcileFixture(t)
		remote, sha := fixture.repository("backend", []string{"4.2.1"}, gap{version: "4.3.0"})
		writeStub(t, filepath.Join(remote, "hooks"), "pre-receive", "echo 'tag creation is restricted' >&2\nexit 1\n")

		// when
		run := fixture.run(nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Equal(t, []string{"4.2.1"}, fixture.tags(remote))
		assert.Contains(t, run.stdout, line("fixture/backend", "4.3.0", sha, "tag push FAILED — investigate")+
			"    remote: tag creation is restricted\n")
		assert.Contains(t, run.stdout, "4.3.0 -> 4.3.0 (pre-receive hook declined)")
		assert.Contains(t, fixture.record("summary.md"), counts("0", "0", "1"))
		assert.Equal(t, "https://github.com/fixture/backend.git\n", fixture.record("clone-urls"))
		assert.Equal(t, "clone "+reconcileToken+"\ndetector "+reconcileToken+"\npush "+reconcileToken+"\n",
			fixture.record("credentials"), "git could not authenticate from GH_TOKEN")
		assert.NotContains(t, fixture.record("git-arguments"), reconcileToken)
		assert.NotContains(t, run.stdout+run.stderr, reconcileToken)
	})

	t.Run("should log why a repository was not scanned", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newReconcileFixture(t)
		fixture.repository("frontend", []string{"0.4.1"}, gap{version: "0.5.0"})
		fixture.repos = append(fixture.repos, map[string]string{"owner": "fixture", "name": "missing"})

		// when
		run := fixture.run(map[string]string{"FAIL_FETCH_REPO": "fixture__frontend", "FAIL_ON_GAP": "true"})

		// then
		assert.Equal(t, 1, run.code, run.stdout+run.stderr)
		assert.False(t, fixture.pushed())
		assert.Contains(t, run.stdout, "  skip fixture/frontend: tag fetch failed\n    fatal: unable to access")
		assert.Contains(t, run.stdout, "  skip fixture/missing: clone failed\n    fatal: repository")
		assert.Contains(t, fixture.record("summary.md"), "(**2** could not be cloned or fetched, not scanned)")
	})
}
