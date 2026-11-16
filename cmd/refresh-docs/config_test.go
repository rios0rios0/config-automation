package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

// configurationFile writes content to a configuration file.
func configurationFile(t *testing.T, content string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
	return file
}

func TestLoadConfiguration(t *testing.T) {
	t.Parallel()

	t.Run("should read the providers and resolve each organization's token", func(t *testing.T) {
		t.Parallel()
		// given
		file := configurationFile(t, "providers:\n"+
			"  - type: 'azuredevops'\n    token: '${"+fixtureTokenVariable+"}'\n"+
			"    organizations: ['https://dev.azure.com/FixtureOrg/', 'OtherOrg']\n")

		// when
		loaded, err := loadConfiguration(file)

		// then
		require.NoError(t, err)
		provider, err := loaded.provider()
		require.NoError(t, err)
		assert.Equal(t, entities.ProviderAzureDevOps, provider)
		assert.Equal(t, []configuredOrganization{
			{name: "FixtureOrg", token: fixtureToken}, {name: "OtherOrg", token: fixtureToken},
		}, loaded.organizations(provider))
	})

	t.Run("should reject a configuration that cannot drive a run", func(t *testing.T) {
		t.Parallel()
		for name, content := range map[string]string{
			"no providers":    "refresh:\n  branch: 'x'\n",
			"an unknown one":  "providers:\n  - type: 'gitlab'\n    organizations: ['a']\n",
			"two platforms":   "providers:\n  - type: 'github'\n    organizations: ['a']\n  - type: 'azuredevops'\n    organizations: ['b']\n",
			"no organization": "providers:\n  - type: 'github'\n",
			"malformed YAML":  "providers: [\n",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				// given
				file := configurationFile(t, content)

				// when
				_, err := loadConfiguration(file)

				// then
				require.Error(t, err)
			})
		}
	})

	t.Run("should fail on a file that cannot be read", func(t *testing.T) {
		t.Parallel()
		// given / when
		_, err := loadConfiguration(filepath.Join(t.TempDir(), "missing.yaml"))

		// then
		require.Error(t, err)
	})
}

func TestConfiguration(t *testing.T) {
	t.Parallel()

	t.Run("should default every refresh setting", func(t *testing.T) {
		t.Parallel()
		// given
		loaded := configuration{}

		// when / then
		assert.Equal(t, "chore/config-and-docs-refresh", loaded.branch())
		assert.Equal(t, defaultCommitMessage, loaded.pullRequestTitle())
		assert.Equal(t, entities.DefaultGuideURL, loaded.guideURL())
		assert.Equal(t, entities.GitIdentity{}, loaded.identity())
		assert.Equal(t, entities.ProviderAzureDevOps.DefaultInstructionFiles(),
			loaded.instructionFiles(entities.ProviderAzureDevOps))
	})

	t.Run("should apply the configured overrides", func(t *testing.T) {
		t.Parallel()
		// given
		empty, off, on := "", false, true
		loaded := configuration{Refresh: refreshConfiguration{
			Branch: "chore/other", GuideURL: &empty,
			InstructionFiles: instructionFilesConfiguration{Claude: &off, Copilot: &on},
			Git:              gitConfiguration{UserName: "fixture-bot", UserEmail: "fixture-bot@example.invalid"},
			PullRequest:      pullRequestConfiguration{Title: "chore: refreshed", Source: "From the fixture."},
		}}

		// when
		files := loaded.instructionFiles(entities.ProviderAzureDevOps)

		// then
		assert.Equal(t, "chore/other", loaded.branch())
		assert.Empty(t, loaded.guideURL(), "an explicit empty guide did not turn the rule off")
		assert.Equal(t, entities.InstructionFiles{Agents: true, Claude: false, Copilot: true}, files)
		assert.Equal(t, entities.GitIdentity{Name: "fixture-bot", Email: "fixture-bot@example.invalid"},
			loaded.identity())
		assert.Equal(t, "chore: refreshed", loaded.pullRequestTitle())
		assert.Contains(t, loaded.pullRequestBody(files), "From the fixture.\n\n")
		assert.Contains(t, loaded.pullRequestBody(files), "keeps no `CLAUDE.md`")
	})
}

func TestInstructionFlags(t *testing.T) {
	t.Parallel()

	t.Run("should override only the flags that were set", func(t *testing.T) {
		t.Parallel()
		// given
		flags := instructionFlags{copilot: "true", claude: "false"}

		// when
		files, err := flags.apply(entities.InstructionFiles{Agents: true, Claude: true})

		// then
		require.NoError(t, err)
		assert.Equal(t, entities.InstructionFiles{Agents: true, Claude: false, Copilot: true}, files)
	})

	t.Run("should reject a value that is not a boolean", func(t *testing.T) {
		t.Parallel()
		// given
		flags := instructionFlags{agents: "sometimes"}

		// when
		_, err := flags.apply(entities.InstructionFiles{})

		// then
		require.Error(t, err)
	})
}

func TestParseTargetReference(t *testing.T) {
	t.Parallel()

	t.Run("should read each provider's repository forms", func(t *testing.T) {
		t.Parallel()
		for _, test := range []struct {
			provider entities.Provider
			value    string
			expected entities.RefreshTargetReference
		}{
			{entities.ProviderGitHub, "chosen", entities.RefreshTargetReference{Name: "chosen"}},
			{entities.ProviderGitHub, "owner/chosen", entities.RefreshTargetReference{Organization: "owner", Name: "chosen"}},
			{entities.ProviderAzureDevOps, "Fixture Project/chosen",
				entities.RefreshTargetReference{Project: "Fixture Project", Name: "chosen"}},
			{entities.ProviderAzureDevOps, "FixtureOrg/Fixture Project/chosen",
				entities.RefreshTargetReference{Organization: "FixtureOrg", Project: "Fixture Project", Name: "chosen"}},
		} {
			// given / when
			reference, err := parseTargetReference(test.provider, test.value)

			// then
			require.NoError(t, err, test.value)
			assert.Equal(t, test.expected, *reference)
		}
	})

	t.Run("should mean the whole fleet when no repository is named", func(t *testing.T) {
		t.Parallel()
		// given / when
		reference, err := parseTargetReference(entities.ProviderGitHub, "")

		// then
		require.NoError(t, err)
		assert.Nil(t, reference)
	})

	t.Run("should reject a form the provider does not use", func(t *testing.T) {
		t.Parallel()
		for _, test := range []struct {
			provider entities.Provider
			value    string
		}{
			{entities.ProviderGitHub, "a/b/c"}, {entities.ProviderGitHub, "owner/"},
			{entities.ProviderAzureDevOps, "chosen"},
		} {
			// given / when
			_, err := parseTargetReference(test.provider, test.value)

			// then
			require.Error(t, err, test.value)
		}
	})
}
