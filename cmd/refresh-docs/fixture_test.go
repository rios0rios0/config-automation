package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
	infrastructure "github.com/rios0rios0/config-automation/internal/infrastructure/repositories"
	doubles "github.com/rios0rios0/config-automation/test/domain/doubles/repositories"
)

const (
	refreshBranch = "chore/config-and-docs-refresh"
	original      = "Original documentation\n"
	legacyClaude  = entities.LegacyBanner + "\n## Build\n\nmake test\n"
	fixtureOwner  = "fixture"
	// fixtureProject is the Azure DevOps project the fixture's repositories
	// live in; its space is what the URLs must encode.
	fixtureProject = "Fixture Project"
)

// errPushRejected stands in for a push the remote refuses.
var errPushRejected = errors.New("remote rejected the push")

// localClones clones the bare fixture repository named by a clone URL's last
// segment instead of reaching the network, and records every URL and
// credential it is handed. It pushes with the real adapter, failing the
// clones failPush names.
type localClones struct {
	mutex       sync.Mutex
	inner       *infrastructure.GitClonesRepository
	root        string
	urls        []string
	credentials []entities.GitCredential
	failPush    []string
}

var _ repositories.ClonesRepository = (*localClones)(nil)

func (c *localClones) Clone(ctx context.Context, address, directory string, credential entities.GitCredential) error {
	c.mutex.Lock()
	c.urls = append(c.urls, address)
	c.credentials = append(c.credentials, credential)
	c.mutex.Unlock()
	name, err := url.PathUnescape(strings.TrimSuffix(path.Base(address), ".git"))
	if err != nil {
		return err
	}
	return c.inner.Clone(ctx, filepath.Join(c.root, name+".git"), directory, credential)
}

func (c *localClones) Push(ctx context.Context, root, branch string, credential entities.GitCredential) error {
	c.mutex.Lock()
	c.credentials = append(c.credentials, credential)
	c.mutex.Unlock()
	if slices.Contains(c.failPush, filepath.Base(root)) {
		return errPushRejected
	}
	return c.inner.Push(ctx, root, branch, credential)
}

func (c *localClones) Discard(ctx context.Context, directory string) error {
	return c.inner.Discard(ctx, directory)
}

// refreshFixture is one refresh run over local bare repositories, a fake
// Claude CLI, and an in-memory pull-request host.
type refreshFixture struct {
	t            *testing.T
	root         string
	provider     entities.Provider
	project      string
	targets      []refreshTargetResponse
	clones       *localClones
	pullRequests *doubles.InMemoryPullRequestsRepository
	tokens       []string
	agent        map[string]string
	variables    map[string]string
	extraConfig  string
	// pipelinesDir, when set, is the fake pipelines checkout the run gets.
	pipelinesDir string
	// agentDouble, when set, replaces the fake Claude CLI.
	agentDouble repositories.AgentRepository
}

// outcome is what one run printed and returned.
type outcome struct {
	code   int
	stdout string
	stderr string
}

func newRefreshFixture(t *testing.T) *refreshFixture {
	t.Helper()
	root := t.TempDir()
	return &refreshFixture{
		t: t, root: root, provider: entities.ProviderGitHub,
		clones:       &localClones{inner: infrastructure.NewGitClonesRepository(), root: root},
		pullRequests: doubles.NewInMemoryPullRequestsRepository(),
		agent: map[string]string{
			"FIXTURE_ROOT": root, "GH_TOKEN": fixtureToken, "AZURE_DEVOPS_PAT": fixtureToken,
			"CLAUDE_CODE_OAUTH_TOKEN": fixtureOAuthToken,
		},
		variables: map[string]string{"GH_TOKEN": fixtureToken, "AZURE_DEVOPS_PAT": fixtureToken},
	}
}

// onAzureDevOps makes the fixture an Azure DevOps organization whose
// repositories live in fixtureProject.
func (f *refreshFixture) onAzureDevOps() *refreshFixture {
	f.provider, f.project = entities.ProviderAzureDevOps, fixtureProject
	return f
}

// git runs git in directory and returns its trimmed output.
func (f *refreshFixture) git(directory string, arguments ...string) string {
	f.t.Helper()
	command := exec.CommandContext(f.t.Context(), "git", arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	require.NoError(f.t, err, string(output))
	return strings.TrimSpace(string(output))
}

// repository commits files, plus an unrelated source file, to a new bare
// repository the run will clone, adds it to the batch, and returns it.
func (f *refreshFixture) repository(name string, files map[string]string) string {
	f.t.Helper()
	return f.linkedRepository(name, files, nil)
}

// linkedRepository is repository with symlinks too, each path to its target.
func (f *refreshFixture) linkedRepository(name string, files, links map[string]string) string {
	f.t.Helper()
	work := filepath.Join(f.root, "source-"+name)
	f.git(f.root, "init", "--quiet", "-b", "main", work)
	files["source.txt"] = "Original implementation\n"
	writeTree(f.t, work, files)
	for link, target := range links {
		require.NoError(f.t, os.Symlink(target, filepath.Join(work, filepath.FromSlash(link))))
	}
	f.git(work, "add", ".")
	f.git(work, "commit", "--quiet", "-m", "Initial fixture")
	remote := filepath.Join(f.root, name+".git")
	f.git(f.root, "clone", "--quiet", "--bare", work, remote)
	f.missing(name)
	return remote
}

// empty adds a repository that has no commit to the batch.
func (f *refreshFixture) empty(name string) {
	f.t.Helper()
	f.git(f.root, "init", "--quiet", "--bare", "-b", "main", filepath.Join(f.root, name+".git"))
	f.missing(name)
}

// missing adds a repository to the batch without creating it.
func (f *refreshFixture) missing(name string) {
	f.targets = append(f.targets, refreshTargetResponse{
		Organization: fixtureOwner, Project: f.project, Name: name, DefaultBranch: "main",
	})
}

// configuration writes the run's configuration file.
func (f *refreshFixture) configuration() string {
	f.t.Helper()
	token := "${" + fixtureTokenVariable + "}"
	content := "providers:\n  - type: '" + string(f.provider) + "'\n    token: '" + token + "'\n" +
		"    organizations: ['" + fixtureOwner + "']\n" +
		"refresh:\n  git:\n    user_name: 'fixture-bot'\n    user_email: 'fixture-bot@example.invalid'\n" +
		"  pull_request:\n    source: 'Automated refresh by the fixture.'\n" + f.extraConfig
	file := filepath.Join(f.root, "config.yaml")
	require.NoError(f.t, os.WriteFile(file, []byte(content), 0o600))
	return file
}

// wiring is the production composition with the fixture's hosting double,
// clones, and agent environment.
func (f *refreshFixture) wiring() dependencies {
	agent := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + f.root}
	for name, value := range f.agent {
		agent = append(agent, name+"="+value)
	}
	targets := doubles.NewInMemoryRefreshTargetsRepository()
	providers := make(map[entities.Provider]hosting, 1)
	providers[f.provider] = hosting{
		targets: func(string) (repositories.RefreshTargetsRepository, error) { return targets, nil },
		pullRequests: func(token string) repositories.PullRequestsRepository {
			f.tokens = append(f.tokens, token)
			return f.pullRequests
		},
	}
	factory := claudeCodeAgent(fakeClaudePath(), func() []string { return agent })
	if f.agentDouble != nil {
		factory = func(io.Writer, []string) repositories.AgentRepository { return f.agentDouble }
	}
	// The generator starts from this process's environment, with the tokens a
	// leg holds planted in it, so a test can tell they never reach it.
	generator := func() []string {
		return append(os.Environ(), "GH_TOKEN="+fixtureToken, "CLAUDE_CODE_OAUTH_TOKEN="+fixtureOAuthToken)
	}
	return dependencies{
		hosting:     providers,
		clones:      f.clones,
		agent:       factory,
		ignoreRules: pipelinesIgnoreRules(generator),
		getenv:      func(name string) string { return f.variables[name] },
		now:         time.Now,
	}
}

// run refreshes the batch with writes standing in for the edits Claude
// makes; a nil content deletes the file.
func (f *refreshFixture) run(writes map[string]any, arguments ...string) outcome {
	f.t.Helper()
	writesDirectory := filepath.Join(f.root, "writes")
	deletions := []string{}
	for file, content := range writes {
		if content == nil {
			deletions = append(deletions, file)
			continue
		}
		text, isText := content.(string)
		require.True(f.t, isText, "content of %s is neither text nor nil", file)
		writeTree(f.t, writesDirectory, map[string]string{file: text})
	}
	deletes := filepath.Join(f.root, "deletes")
	require.NoError(f.t, os.WriteFile(deletes, []byte(strings.Join(deletions, "\n")+"\n"), 0o600))
	f.agent["WRITES"], f.agent["DELETES"] = writesDirectory, deletes
	batch, err := json.Marshal(f.targets)
	require.NoError(f.t, err)
	vector := append([]string{"run", "--config", f.configuration(), "--batch", string(batch),
		"--work-dir", filepath.Join(f.root, "work"), "--max-turns", "7"}, arguments...)
	if f.pipelinesDir != "" {
		vector = append(vector, "--pipelines-dir", f.pipelinesDir)
	}
	if !slices.Contains(arguments, "--ci") {
		vector = append(vector, "--ci", "plain")
	}
	var stdout, stderr bytes.Buffer
	code := execute(f.t.Context(), vector, streams{stdout: &stdout, stderr: &stderr}, f.wiring())
	return outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// fakeGenerator stands in for the ignore-rules generator of a pipelines
// checkout, recording its environment beside itself: it puts the shared block
// above a `.gitignore` that lacks it, prints that the file is current when it
// has it, and refuses a file holding a `malformed` line, as the generator
// refuses malformed markers.
const fakeGenerator = `set -eu
env >> "${SCRIPTS_DIR}/generator-environment"
begin='# >>> pipelines:begin'
if [ -f .gitignore ] && grep -qx 'malformed' .gitignore; then
  echo 'ERROR: .gitignore has a malformed pipelines block.' >&2
  exit 2
fi
if [ -f .gitignore ] && grep -qxF "${begin}" .gitignore; then
  echo 'Already up to date: .gitignore (common)'
  exit 0
fi
generated="$(mktemp)"
printf '%s\n' "${begin}" 'build/reports/' '# <<< pipelines:end' > "${generated}"
if [ -s .gitignore ]; then
  printf '\n' >> "${generated}"
  cat .gitignore >> "${generated}"
fi
cat "${generated}" > .gitignore
rm -f "${generated}"
echo 'Updated .gitignore from fragments: common'
`

// sharedIgnoreBlock is the block fakeGenerator writes.
const sharedIgnoreBlock = "# >>> pipelines:begin\nbuild/reports/\n# <<< pipelines:end\n"

// withPipelines gives the run a fake pipelines checkout. `sh` runs its
// generator, so the script needs no execute bit.
func (f *refreshFixture) withPipelines() *refreshFixture {
	f.t.Helper()
	f.pipelinesDir = filepath.Join(f.root, "pipelines")
	writeTree(f.t, f.pipelinesDir, map[string]string{"global/scripts/tools/gitignore/run.sh": fakeGenerator})
	return f
}

// show returns a file of the refresh branch of remote.
func (f *refreshFixture) show(remote, file string) string {
	f.t.Helper()
	return f.git(remote, "show", refreshBranch+":"+file)
}

// branches lists the refresh branch of remote, empty when it was not pushed.
func (f *refreshFixture) branches(remote string) string {
	f.t.Helper()
	return f.git(remote, "branch", "--list", refreshBranch)
}

// record returns what the fake CLI wrote to name below the fixture root.
func (f *refreshFixture) record(name string) string {
	f.t.Helper()
	content, err := os.ReadFile(filepath.Join(f.root, name))
	require.NoError(f.t, err)
	return string(content)
}

// writeTree writes files below root, creating their directories.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for file, content := range files {
		location := filepath.Join(root, filepath.FromSlash(file))
		// A directory needs its execute bit; the rule below is written for files.
		// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
		require.NoError(t, os.MkdirAll(filepath.Dir(location), 0o700))
		require.NoError(t, os.WriteFile(location, []byte(content), 0o600))
	}
}

// withContent maps every path to the same content.
func withContent(paths []string, content string) map[string]string {
	files := map[string]string{}
	for _, file := range paths {
		files[file] = content
	}
	return files
}

// compliant returns a repository already in the layout, plus extra files.
func compliant(extra map[string]string) map[string]string {
	files := map[string]string{"AGENTS.md": original, "CLAUDE.md": entities.AgentsImport}
	maps.Copy(files, extra)
	return files
}
