package main

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

// panickingAgent stands in for an agent adapter with a defect.
type panickingAgent struct{}

func (panickingAgent) Audit(context.Context, entities.AgentRun) (entities.AgentResult, error) {
	panic("fixture agent defect")
}

func (panickingAgent) Refresh(context.Context, entities.AgentRun) (entities.AgentResult, error) {
	panic("fixture agent defect")
}

func TestRefreshBatchHardening(t *testing.T) {
	t.Parallel()

	t.Run("should withhold every token variable the configuration names from Claude", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		fixture.repository("withheld", compliant(map[string]string{"README.md": original}))
		fixture.agent[fixtureTokenVariable] = "fixture-referenced-token"

		// when
		run := fixture.run(nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.NotContains(t, fixture.record("claude-environment"), "fixture-referenced-token")
	})

	t.Run("should refuse a token Azure Pipelines left as the macro of an undefined variable", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t).onAzureDevOps()
		fixture.repository("macro", map[string]string{"README.md": original})
		fixture.variables["AZURE_DEVOPS_PAT"] = "$(AZURE_DEVOPS_PAT)"

		// when
		run := fixture.run(nil)

		// then
		assert.Equal(t, exitFailure, run.code)
		assert.Contains(t, run.stderr, "AZURE_DEVOPS_PAT is empty or not set")
		assert.Empty(t, fixture.clones.urls)
	})

	t.Run("should read the added lines of a file the repository marks as binary", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		remote := fixture.repository("binary", compliant(map[string]string{
			"README.md": original, ".gitattributes": "*.md -diff\n",
		}))

		// when
		run := fixture.run(map[string]any{"README.md": "Example: " + fineGrainedTokenShape() + "\n"})

		// then
		assert.Equal(t, exitFailure, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "  - secret-shape: fixture/binary")
		assert.Empty(t, fixture.branches(remote))
	})

	t.Run("should close the stop-commands block when the agent panics", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		fixture.repository("defect", compliant(map[string]string{"README.md": original}))
		fixture.agentDouble = panickingAgent{}

		// when
		run := fixture.run(nil, "--ci", dialectGitHubActions)

		// then
		assert.Equal(t, exitFailure, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "  - internal: fixture/defect")
		stops := regexp.MustCompile(`(?m)^::stop-commands::([0-9a-f]{32})$`).FindAllStringSubmatch(run.stdout, -1)
		require.NotEmpty(t, stops, run.stdout)
		for _, stop := range stops {
			start := strings.Index(run.stdout, stop[0])
			assert.Contains(t, run.stdout[start:], "\n::"+stop[1]+"::\n", "a stop-commands block was left open")
		}
	})

	t.Run("should read a symlinked chlog configuration as chlog", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		fixture.linkedRepository("linked", compliant(map[string]string{
			"README.md": original, "CHANGELOG.md": "# Changelog\n", "config/chlog.yaml": "kinds: []\n",
		}), map[string]string{".chlog.yaml": "config/chlog.yaml"})

		// when
		run := fixture.run(nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "--> changelog convention: chlog\n")
	})

	t.Run("should leave an empty repository alone and refresh the rest", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		fixture.empty("blank")
		remote := fixture.repository("next", compliant(map[string]string{"README.md": original}))

		// when
		run := fixture.run(map[string]any{"README.md": "Updated\n"})

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "--> the default branch has no commit yet; nothing to refresh.")
		assert.Contains(t, run.stdout, "no_drift (1): fixture/blank")
		assert.Equal(t, "Updated", fixture.show(remote, "README.md"))
	})
}
