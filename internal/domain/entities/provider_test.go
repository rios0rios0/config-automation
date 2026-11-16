package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

func TestParseProvider(t *testing.T) {
	t.Parallel()

	t.Run("should accept every supported provider", func(t *testing.T) {
		t.Parallel()
		for _, name := range []string{"github", "azuredevops"} {
			// given / when
			provider, err := entities.ParseProvider(name)

			// then
			require.NoError(t, err)
			assert.Equal(t, entities.Provider(name), provider)
		}
	})

	t.Run("should reject an unsupported provider and name the supported ones", func(t *testing.T) {
		t.Parallel()
		// given / when
		_, err := entities.ParseProvider("gitlab")

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "[azuredevops github]")
	})
}

func TestProvider(t *testing.T) {
	t.Parallel()

	t.Run("should build GitHub URLs without a credential", func(t *testing.T) {
		t.Parallel()
		// given
		target := entities.RefreshTarget{Organization: "fixture-owner", Name: "fixture-repo"}

		// when
		clone := entities.ProviderGitHub.CloneURL(target)
		pullRequest := entities.ProviderGitHub.PullRequestURL(target, 7)

		// then
		assert.Equal(t, "https://github.com/fixture-owner/fixture-repo.git", clone)
		assert.Equal(t, "https://github.com/fixture-owner/fixture-repo/pull/7", pullRequest)
	})

	t.Run("should percent-encode every Azure DevOps path segment", func(t *testing.T) {
		t.Parallel()
		// given
		target := entities.RefreshTarget{Organization: "FixtureOrg", Project: "Fixture Project", Name: "fixture repo"}

		// when
		clone := entities.ProviderAzureDevOps.CloneURL(target)
		pullRequest := entities.ProviderAzureDevOps.PullRequestURL(target, 42)

		// then
		assert.Equal(t, "https://dev.azure.com/FixtureOrg/Fixture%20Project/_git/fixture%20repo", clone)
		assert.Equal(t,
			"https://dev.azure.com/FixtureOrg/Fixture%20Project/_git/fixture%20repo/pullrequest/42", pullRequest)
	})

	t.Run("should scope the git credential to the provider's host and user", func(t *testing.T) {
		t.Parallel()
		// given / when
		github := entities.ProviderGitHub.GitCredential("fixture-token-placeholder")
		azure := entities.ProviderAzureDevOps.GitCredential("fixture-token-placeholder")

		// then
		assert.Equal(t, entities.GitCredential{
			Host: "github.com", Username: "x-access-token", Token: "fixture-token-placeholder",
		}, github)
		assert.Equal(t, entities.GitCredential{
			Host: "dev.azure.com", Username: "pat", Token: "fixture-token-placeholder",
		}, azure)
	})

	t.Run("should keep Copilot files on GitHub only by default", func(t *testing.T) {
		t.Parallel()
		// given / when
		github := entities.ProviderGitHub.DefaultInstructionFiles()
		azure := entities.ProviderAzureDevOps.DefaultInstructionFiles()

		// then
		assert.Equal(t, entities.InstructionFiles{Agents: true, Claude: true, Copilot: true}, github)
		assert.Equal(t, entities.InstructionFiles{Agents: true, Claude: true, Copilot: false}, azure)
	})
}
