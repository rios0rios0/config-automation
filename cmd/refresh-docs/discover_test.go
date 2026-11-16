package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
	doubles "github.com/rios0rios0/config-automation/test/domain/doubles/repositories"
)

// discoverFixture is one discovery over in-memory organizations.
type discoverFixture struct {
	t             *testing.T
	root          string
	provider      entities.Provider
	organizations map[string]*doubles.InMemoryRefreshTargetsRepository
	tokens        map[string]string
	order         []string
	output        string
}

func newDiscoverFixture(t *testing.T, provider entities.Provider) *discoverFixture {
	t.Helper()
	root := t.TempDir()
	return &discoverFixture{
		t: t, root: root, provider: provider, organizations: map[string]*doubles.InMemoryRefreshTargetsRepository{},
		tokens: map[string]string{}, output: filepath.Join(root, "github-output"),
	}
}

// organization adds an organization whose token is token, holding targets.
func (f *discoverFixture) organization(name, token string, targets ...entities.RefreshTarget) {
	f.organizations[token] = doubles.NewInMemoryRefreshTargetsRepository(targets...)
	f.tokens[name] = token
	f.order = append(f.order, name)
}

// run discovers with the configuration of every organization.
func (f *discoverFixture) run(arguments ...string) outcome {
	f.t.Helper()
	var content strings.Builder
	content.WriteString("providers:\n")
	for _, name := range f.order {
		content.WriteString("  - type: '" + string(f.provider) + "'\n    token: '" + f.tokens[name] + "'\n" +
			"    organizations: ['" + name + "']\n    credential: '" + strings.ToUpper(name) + "_SECRET'\n")
	}
	content.WriteString("refresh:\n  exclude: ['first-owner/excluded']\n")
	config := filepath.Join(f.root, "config.yaml")
	require.NoError(f.t, os.WriteFile(config, []byte(content.String()), 0o600))
	providers := make(map[entities.Provider]hosting, 1)
	providers[f.provider] = hosting{
		targets: func(token string) (repositories.RefreshTargetsRepository, error) {
			repository, found := f.organizations[token]
			if !found {
				return nil, errors.New("401 Bad credentials")
			}
			return repository, nil
		},
	}
	wiring := dependencies{
		hosting: providers,
		getenv: func(name string) string {
			return map[string]string{"GITHUB_OUTPUT": f.output}[name]
		},
		now: time.Now,
	}
	var stdout, stderr bytes.Buffer
	code := execute(f.t.Context(), append([]string{"discover", "--config", config}, arguments...),
		streams{stdout: &stdout, stderr: &stderr}, wiring)
	return outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// outputs reads the step outputs written to GITHUB_OUTPUT.
func (f *discoverFixture) outputs() map[string]string {
	f.t.Helper()
	content, err := os.ReadFile(f.output)
	require.NoError(f.t, err)
	values := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(content)), "\n") {
		name, value, found := strings.Cut(line, "=")
		require.True(f.t, found, line)
		values[name] = value
	}
	return values
}

// active returns an active repository of owner.
func active(owner, name string) entities.RefreshTarget {
	return entities.RefreshTarget{Organization: owner, Name: name, DefaultBranch: "main"}
}

func TestDiscoverTargets(t *testing.T) {
	t.Parallel()

	t.Run("should write the GitHub Actions matrix, one credential per leg", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newDiscoverFixture(t, entities.ProviderGitHub)
		fork := active("first-owner", "fork")
		fork.Fork = true
		fixture.organization("first-owner", "first-token", active("first-owner", "b"), active("first-owner", "a"),
			fork, active("first-owner", "excluded"))
		fixture.organization("second-owner", "second-token", active("second-owner", "a"))

		// when
		run := fixture.run("--batch-size", "1", "--ci", dialectGitHubActions)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		outputs := fixture.outputs()
		assert.Equal(t, "3", outputs["count"])
		assert.Equal(t, "[]", outputs["failed_organizations"])
		var matrix []githubBatchResponse
		require.NoError(t, json.Unmarshal([]byte(outputs["matrix"]), &matrix))
		require.Len(t, matrix, 3)
		assert.Equal(t, githubBatchResponse{
			Name: "first-owner batch_01", Organization: "first-owner", Credential: "FIRST-OWNER_SECRET",
			Repos: []refreshTargetResponse{{Organization: "first-owner", Name: "a", DefaultBranch: "main"}},
		}, matrix[0])
		assert.Equal(t, "second-owner batch_01", matrix[2].Name)
		assert.Contains(t, run.stdout, "Discovered 3 target repo(s) across 3 batch(es) of up to 1.")
	})

	t.Run("should report an unreachable organization and still batch the others", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newDiscoverFixture(t, entities.ProviderGitHub)
		fixture.organization("first-owner", "first-token", active("first-owner", "a"))
		fixture.tokens["broken-owner"], fixture.order = "revoked-token", append(fixture.order, "broken-owner")

		// when
		run := fixture.run("--ci", dialectGitHubActions)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Equal(t, `["broken-owner"]`, fixture.outputs()["failed_organizations"])
		assert.Contains(t, run.stdout, "::error::Could not enumerate 'broken-owner'")
		assert.Contains(t, run.stdout, "401 Bad credentials")
	})

	t.Run("should fail when no organization could be enumerated", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newDiscoverFixture(t, entities.ProviderGitHub)
		fixture.tokens["broken-owner"], fixture.order = "", []string{"broken-owner"}

		// when
		run := fixture.run("--ci", dialectGitHubActions)

		// then
		assert.Equal(t, exitFailure, run.code)
		assert.Contains(t, run.stdout, "the 'BROKEN-OWNER_SECRET' secret is empty or not set")
		assert.Contains(t, run.stdout, "No organization could be enumerated")
	})

	t.Run("should write the Azure Pipelines matrix as output variables", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newDiscoverFixture(t, entities.ProviderAzureDevOps)
		target := entities.RefreshTarget{
			Organization: "FixtureOrg", Project: "Fixture Project", Name: "fixture repo", ID: "fixture-id",
			DefaultBranch: "main",
		}
		fixture.organization("FixtureOrg", "fixture-token", target)

		// when
		run := fixture.run("--ci", dialectAzurePipelines)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		prefix := "##vso[task.setvariable variable=repoMatrix;isoutput=true]"
		line := ""
		for candidate := range strings.SplitSeq(run.stdout, "\n") {
			if after, ok := strings.CutPrefix(candidate, prefix); ok {
				line = after
			}
		}
		require.NotEmpty(t, line, run.stdout)
		var matrix map[string]azureBatchResponse
		require.NoError(t, json.Unmarshal([]byte(line), &matrix))
		var repos []refreshTargetResponse
		require.NoError(t, json.Unmarshal([]byte(matrix["batch_01"].Repos), &repos))
		assert.Equal(t, []refreshTargetResponse{{
			Organization: "FixtureOrg", Project: "Fixture Project", Name: "fixture repo", ID: "fixture-id",
			DefaultBranch: "main",
		}}, repos)
		assert.Contains(t, run.stdout, "##vso[task.setvariable variable=count;isoutput=true]1\n")
	})

	t.Run("should refuse a single repository the schedule leaves out", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newDiscoverFixture(t, entities.ProviderGitHub)
		frozen := active("first-owner", "frozen")
		frozen.Archived, frozen.Fork = true, true
		fixture.organization("first-owner", "first-token", frozen)

		// when
		run := fixture.run("--repo", "first-owner/frozen", "--ci", dialectGitHubActions)

		// then
		assert.Equal(t, exitFailure, run.code)
		assert.Contains(t, run.stdout,
			"::error::Refusing to refresh first-owner/frozen: it is a fork and archived")
		assert.NoFileExists(t, fixture.output)
	})

	t.Run("should batch a single Azure DevOps repository named by project", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newDiscoverFixture(t, entities.ProviderAzureDevOps)
		fixture.organization("FixtureOrg", "fixture-token", entities.RefreshTarget{
			Organization: "FixtureOrg", Project: "Fixture Project", Name: "chosen", DefaultBranch: "main",
		})

		// when
		run := fixture.run("--repo", "Fixture Project/chosen", "--ci", dialectAzurePipelines)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, `\"name\":\"chosen\"`)
	})

	t.Run("should reject a usage error", func(t *testing.T) {
		t.Parallel()
		for _, arguments := range [][]string{{"--batch-size", "x"}, {"--repo", "a/b/c"}, {"--ci", "jenkins"}} {
			// given
			fixture := newDiscoverFixture(t, entities.ProviderGitHub)
			fixture.organization("first-owner", "first-token")

			// when
			run := fixture.run(arguments...)

			// then
			assert.Equal(t, exitUsageError, run.code, arguments)
		}
	})
}
