package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

// environmentOf returns a getenv over values.
func environmentOf(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestNewPresenter(t *testing.T) {
	t.Parallel()

	t.Run("should detect the dialect from the CI's own variables", func(t *testing.T) {
		t.Parallel()
		// given / when / then
		assert.Equal(t, dialectGitHubActions, detectDialect(environmentOf(map[string]string{"GITHUB_ACTIONS": "true"})))
		assert.Equal(t, dialectAzurePipelines, detectDialect(environmentOf(map[string]string{"TF_BUILD": "True"})))
		assert.Equal(t, dialectPlain, detectDialect(environmentOf(nil)))
	})

	t.Run("should reject an unknown dialect", func(t *testing.T) {
		t.Parallel()
		// given / when
		_, err := newPresenter("jenkins", &bytes.Buffer{}, environmentOf(nil))

		// then
		require.Error(t, err)
	})
}

func TestGithubActionsPresenter(t *testing.T) {
	t.Parallel()

	t.Run("should escape annotation data and defuse the legacy command form", func(t *testing.T) {
		t.Parallel()
		// given
		var stdout bytes.Buffer
		printer, err := newPresenter(dialectGitHubActions, &stdout, environmentOf(nil))
		require.NoError(t, err)

		// when
		printer.Warning("100% done\n::error::x ##[add-mask]y")

		// then
		assert.Equal(t, "::warning::100%25 done%0A::error::x ##_[add-mask]y\n", stdout.String())
	})

	t.Run("should run untrusted text between stop-commands and a fresh resume token", func(t *testing.T) {
		t.Parallel()
		// given
		var stdout bytes.Buffer
		printer, err := newPresenter(dialectGitHubActions, &stdout, environmentOf(nil))
		require.NoError(t, err)

		// when
		printer.UntrustedNotice("::add-mask::secret")
		printer.UntrustedNotice("::add-mask::again")

		// then
		tokens := regexp.MustCompile(`(?m)^::stop-commands::([0-9a-f]{32})$`).FindAllStringSubmatch(stdout.String(), -1)
		require.Len(t, tokens, 2)
		assert.NotEqual(t, tokens[0][1], tokens[1][1])
		assert.Contains(t, stdout.String(), "::add-mask::secret\n::"+tokens[0][1]+"::\n")
	})

	t.Run("should append the outputs to the step's output file", func(t *testing.T) {
		t.Parallel()
		// given
		file := filepath.Join(t.TempDir(), "output")
		printer, err := newPresenter(dialectGitHubActions, &bytes.Buffer{},
			environmentOf(map[string]string{"GITHUB_OUTPUT": file}))
		require.NoError(t, err)

		// when
		err = printer.Outputs([]output{{name: "count", value: "2"}, {name: "matrix", value: "[]"}})

		// then
		require.NoError(t, err)
		content, readErr := os.ReadFile(file)
		require.NoError(t, readErr)
		assert.Equal(t, "count=2\nmatrix=[]\n", string(content))
	})

	t.Run("should exit non-zero only when a repository failed", func(t *testing.T) {
		t.Parallel()
		// given
		printer, err := newPresenter(dialectGitHubActions, &bytes.Buffer{}, environmentOf(nil))
		require.NoError(t, err)
		clean := entities.NewRefreshSummary(1)
		clean.SafeguardSkipped = []string{"fixture/refused"}
		failed := entities.NewRefreshSummary(1)
		failed.Failed = []entities.RefreshFailure{{Reason: entities.FailureClaude, Target: "fixture/x"}}

		// when / then
		assert.Equal(t, 0, printer.Finish(clean))
		assert.Equal(t, exitFailure, printer.Finish(failed))
	})
}

func TestAzurePipelinesPresenter(t *testing.T) {
	t.Parallel()

	t.Run("should defuse every logging command in untrusted text, whatever its case", func(t *testing.T) {
		t.Parallel()
		// given
		var stdout bytes.Buffer
		printer, err := newPresenter(dialectAzurePipelines, &stdout, environmentOf(nil))
		require.NoError(t, err)

		// when
		printer.UntrustedNotice("--> clone failed: ##vso[task.setvariable variable=x]y ##VsO[task.complete]")
		printer.Warning("Left out: \"##vso[task.logissue]\"\n100%")

		// then
		assert.Equal(t, "--> clone failed: ##_vso[task.setvariable variable=x]y ##_VsO[task.complete]\n"+
			"##vso[task.logissue type=warning]Left out: \"##_vso[task.logissue]\"%0A100%AZP25\n", stdout.String())
	})

	t.Run("should defuse a command the agent's output splits across writes", func(t *testing.T) {
		t.Parallel()
		// given
		var stdout bytes.Buffer
		printer, err := newPresenter(dialectAzurePipelines, &stdout, environmentOf(nil))
		require.NoError(t, err)
		printer.AgentStarted()
		agent := printer.AgentOutput()

		// when
		_, _ = agent.Write([]byte("first line\n##v"))
		_, _ = agent.Write([]byte("so[task.complete result=Failed;]\nunterminated ##vso["))
		printer.AgentFinished()

		// then
		assert.Equal(t, "first line\n##_vso[task.complete result=Failed;]\nunterminated ##_vso[\n", stdout.String())
	})

	t.Run("should set output variables and end a safeguard-only batch with issues", func(t *testing.T) {
		t.Parallel()
		// given
		var stdout bytes.Buffer
		printer, err := newPresenter(dialectAzurePipelines, &stdout, environmentOf(nil))
		require.NoError(t, err)
		summary := entities.NewRefreshSummary(1)
		summary.SafeguardSkipped = []string{"Fixture Project/refused"}

		// when
		outputsErr := printer.Outputs([]output{{name: "count", value: "1"}})
		code := printer.Finish(summary)

		// then
		require.NoError(t, outputsErr)
		assert.Equal(t, 0, code)
		lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
		assert.Equal(t, "##vso[task.setvariable variable=count;isoutput=true]1", lines[0])
		assert.Equal(t, "##vso[task.complete result=SucceededWithIssues;]", lines[len(lines)-1])
	})
}

func TestPlainPresenter(t *testing.T) {
	t.Parallel()

	t.Run("should print plain text without any CI command", func(t *testing.T) {
		t.Parallel()
		// given
		var stdout bytes.Buffer
		printer, err := newPresenter(dialectPlain, &stdout, environmentOf(nil))
		require.NoError(t, err)

		// when
		printer.Warning("careful")
		printer.Error("broken")
		outputsErr := printer.Outputs([]output{{name: "count", value: "0"}})

		// then
		require.NoError(t, outputsErr)
		assert.Equal(t, "warning: careful\nerror: broken\ncount=0\n", stdout.String())
	})
}
