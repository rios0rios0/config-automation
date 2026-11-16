package repositories_test

import (
	"context"
	"errors"
	"testing"

	forge "github.com/rios0rios0/gitforge/v4/pkg/global/domain/entities"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/infrastructure/repositories"
	"github.com/rios0rios0/config-automation/test/domain/builders"
	doubles "github.com/rios0rios0/config-automation/test/domain/doubles/repositories"
)

// forgeDouble is a handwritten gitforge provider that records the pull
// requests it is asked to open. gitforge's own stub lists no repositories,
// which says nothing about the mapping under test.
type forgeDouble struct {
	created     []forge.Repository
	inputs      []forge.PullRequestInput
	createdID   int
	createError error
}

func (d *forgeDouble) Name() string                                { return "double" }
func (d *forgeDouble) MatchesURL(string) bool                      { return false }
func (d *forgeDouble) AuthToken() string                           { return "" }
func (d *forgeDouble) CloneURL(forge.Repository) string            { return "" }
func (d *forgeDouble) SSHCloneURL(forge.Repository, string) string { return "" }
func (d *forgeDouble) PullRequestExists(context.Context, forge.Repository, string) (bool, error) {
	return false, nil
}

func (d *forgeDouble) ClosePullRequest(context.Context, forge.Repository, string) (bool, error) {
	return false, nil
}

func (d *forgeDouble) DiscoverRepositories(context.Context, string) ([]forge.Repository, error) {
	return nil, nil
}

func (d *forgeDouble) CreatePullRequest(
	_ context.Context,
	repository forge.Repository,
	input forge.PullRequestInput,
) (*forge.PullRequest, error) {
	if d.createError != nil {
		return nil, d.createError
	}
	d.created = append(d.created, repository)
	d.inputs = append(d.inputs, input)
	return &forge.PullRequest{ID: d.createdID}, nil
}

// finderForgeDouble also looks up open pull requests.
type finderForgeDouble struct {
	forgeDouble

	open      map[string]forge.PullRequest
	findError error
	searched  []forge.Repository
}

func (d *finderForgeDouble) FindOpenPullRequest(
	_ context.Context,
	repository forge.Repository,
	branch string,
) (forge.PullRequest, bool, error) {
	d.searched = append(d.searched, repository)
	if d.findError != nil {
		return forge.PullRequest{}, false, d.findError
	}
	pullRequest, found := d.open[branch]
	return pullRequest, found, nil
}

// listerDouble lists a fixed organization.
type listerDouble struct {
	repositories []forge.Repository
	err          error
}

func (d *listerDouble) ListOrganizationRepositories(context.Context, string) ([]forge.Repository, error) {
	return d.repositories, d.err
}

// azureTarget is a repository whose project and name hold spaces.
func azureTarget() entities.RefreshTarget {
	return entities.RefreshTarget{
		Organization: "FixtureOrg", Project: "Fixture Project", Name: "fixture repo", ID: "fixture-id",
		DefaultBranch: "main",
	}
}

func TestGitforgePullRequestsRepository(t *testing.T) {
	t.Parallel()

	t.Run("should find an open pull request and link it on the provider", func(t *testing.T) {
		t.Parallel()
		// given
		client := &finderForgeDouble{open: map[string]forge.PullRequest{
			"chore/config-and-docs-refresh": {ID: 42},
		}}
		pullRequests := repositories.NewGitforgePullRequestsRepository(entities.ProviderAzureDevOps, client)

		// when
		pullRequest, found, err := pullRequests.FindOpenBySourceBranch(
			t.Context(), azureTarget(), "chore/config-and-docs-refresh")

		// then
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, 42, pullRequest.Number)
		assert.Equal(t,
			"https://dev.azure.com/FixtureOrg/Fixture%20Project/_git/fixture%20repo/pullrequest/42", pullRequest.URL)
		assert.Equal(t, forge.Repository{
			ID: "fixture-id", Name: "fixture repo", Organization: "FixtureOrg", Project: "Fixture Project",
			DefaultBranch: "refs/heads/main", ProviderName: "azuredevops",
		}, client.searched[0])
	})

	t.Run("should report no pull request when none is open", func(t *testing.T) {
		t.Parallel()
		// given
		client := &finderForgeDouble{open: map[string]forge.PullRequest{}}
		pullRequests := repositories.NewGitforgePullRequestsRepository(entities.ProviderGitHub, client)

		// when
		_, found, err := pullRequests.FindOpenBySourceBranch(t.Context(), azureTarget(), "chore/x")

		// then
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("should fail the lookup rather than report no pull request", func(t *testing.T) {
		t.Parallel()
		// given
		failing := &finderForgeDouble{findError: errors.New("503 Service Unavailable")}
		unsupported := &forgeDouble{}

		// when
		_, failedFound, failedErr := repositories.NewGitforgePullRequestsRepository(entities.ProviderGitHub, failing).
			FindOpenBySourceBranch(t.Context(), azureTarget(), "chore/x")
		_, unsupportedFound, unsupportedErr := repositories.NewGitforgePullRequestsRepository(
			entities.ProviderGitHub, unsupported).FindOpenBySourceBranch(t.Context(), azureTarget(), "chore/x")

		// then
		require.Error(t, failedErr)
		assert.False(t, failedFound)
		require.Error(t, unsupportedErr)
		assert.False(t, unsupportedFound)
	})

	t.Run("should open a pull request from the draft and link it", func(t *testing.T) {
		t.Parallel()
		// given
		client := &finderForgeDouble{createdID: 7}
		pullRequests := repositories.NewGitforgePullRequestsRepository(entities.ProviderGitHub, client)
		target := entities.RefreshTarget{Organization: "fixture-owner", Name: "fixture-repo", DefaultBranch: "main"}
		draft := entities.PullRequestDraft{
			SourceBranch: "chore/config-and-docs-refresh", TargetBranch: "main", Title: "title", Description: "body",
		}

		// when
		pullRequest, err := pullRequests.Insert(t.Context(), target, draft)

		// then
		require.NoError(t, err)
		assert.Equal(t, entities.PullRequest{Number: 7, URL: "https://github.com/fixture-owner/fixture-repo/pull/7"},
			pullRequest)
		assert.Equal(t, []forge.PullRequestInput{{
			SourceBranch: "chore/config-and-docs-refresh", TargetBranch: "main", Title: "title", Description: "body",
		}}, client.inputs)
	})

	t.Run("should fail when the provider cannot open the pull request", func(t *testing.T) {
		t.Parallel()
		// given
		client := &forgeDouble{createError: errors.New("422 Validation Failed")}
		pullRequests := repositories.NewGitforgePullRequestsRepository(entities.ProviderGitHub, client)

		// when
		_, err := pullRequests.Insert(t.Context(), azureTarget(), entities.PullRequestDraft{})

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "422 Validation Failed")
	})
}

func TestGitforgeRefreshTargetsRepository(t *testing.T) {
	t.Parallel()

	t.Run("should map every repository with its state and a bare default branch", func(t *testing.T) {
		t.Parallel()
		// given
		lister := &listerDouble{repositories: []forge.Repository{
			{ID: "1", Name: "active", Organization: "FixtureOrg", Project: "Fixture Project",
				DefaultBranch: "refs/heads/main"},
			{ID: "2", Name: "fork", Organization: "FixtureOrg", Project: "Fixture Project", IsFork: true},
			{ID: "3", Name: "frozen", Organization: "FixtureOrg", Project: "Other", IsDisabled: true},
		}}
		targets := repositories.NewGitforgeRefreshTargetsRepository(lister)

		// when
		listed, err := targets.FindAllByOrganization(t.Context(), "FixtureOrg")

		// then
		require.NoError(t, err)
		assert.Equal(t, []entities.RefreshTarget{
			{Organization: "FixtureOrg", Project: "Fixture Project", Name: "active", ID: "1", DefaultBranch: "main"},
			{Organization: "FixtureOrg", Project: "Fixture Project", Name: "fork", ID: "2", Fork: true},
			{Organization: "FixtureOrg", Project: "Other", Name: "frozen", ID: "3", Disabled: true},
		}, listed)
	})

	t.Run("should fail rather than return a partial listing", func(t *testing.T) {
		t.Parallel()
		// given
		targets := repositories.NewGitforgeRefreshTargetsRepository(&listerDouble{err: errors.New("401")})

		// when
		listed, err := targets.FindAllByOrganization(t.Context(), "FixtureOrg")

		// then
		require.Error(t, err)
		assert.Nil(t, listed)
	})

	t.Run("should find one repository by project and name, whatever their case", func(t *testing.T) {
		t.Parallel()
		// given
		lister := &listerDouble{repositories: []forge.Repository{
			{ID: "1", Name: "chosen", Organization: "FixtureOrg", Project: "Fixture Project",
				DefaultBranch: "refs/heads/main"},
			{ID: "2", Name: "chosen", Organization: "FixtureOrg", Project: "Other", DefaultBranch: "refs/heads/main"},
		}}
		targets := repositories.NewGitforgeRefreshTargetsRepository(lister)

		// when
		found, err := targets.FindByName(t.Context(), entities.RefreshTargetReference{
			Organization: "FixtureOrg", Project: "other", Name: "CHOSEN",
		})
		_, missingErr := targets.FindByName(t.Context(), entities.RefreshTargetReference{
			Organization: "FixtureOrg", Project: "Fixture Project", Name: "missing",
		})

		// then
		require.NoError(t, err)
		assert.Equal(t, "2", found.ID)
		require.Error(t, missingErr)
	})
}

func TestGoGithubRefreshTargetsRepository(t *testing.T) {
	t.Parallel()

	t.Run("should list the owner's repositories with their state", func(t *testing.T) {
		t.Parallel()
		// given
		listing := doubles.NewInMemoryRepositoriesRepository().WithRepos(
			builders.NewRepositoryBuilder().WithName("active").Build(),
			builders.NewRepositoryBuilder().WithName("fork").AsFork().Build(),
		)
		targets := repositories.NewGoGithubRefreshTargetsRepository(listing)

		// when
		listed, err := targets.FindAllByOrganization(t.Context(), "rios0rios0")

		// then
		require.NoError(t, err)
		require.Len(t, listed, 2)
		assert.Equal(t, "active", listed[0].Name)
		assert.Equal(t, "main", listed[0].DefaultBranch)
		assert.False(t, listed[0].Fork)
		assert.True(t, listed[1].Fork)
		assert.Equal(t, "rios0rios0", listed[1].Organization)
	})

	t.Run("should fail when any lookup of the listing fails", func(t *testing.T) {
		t.Parallel()
		// given
		listing := doubles.NewInMemoryRepositoriesRepository()
		listing.ErrorOnOwnerKind = errors.New("404 Not Found")
		targets := repositories.NewGoGithubRefreshTargetsRepository(listing)

		// when
		listed, err := targets.FindAllByOrganization(t.Context(), "rios0rios0")

		// then
		require.Error(t, err)
		assert.Nil(t, listed)
	})

	t.Run("should fetch one repository by name", func(t *testing.T) {
		t.Parallel()
		// given
		listing := doubles.NewInMemoryRepositoriesRepository().WithRepos(
			builders.NewRepositoryBuilder().WithName("chosen").AsArchived().Build())
		targets := repositories.NewGoGithubRefreshTargetsRepository(listing)

		// when
		found, err := targets.FindByName(t.Context(), entities.RefreshTargetReference{
			Organization: "rios0rios0", Name: "chosen",
		})

		// then
		require.NoError(t, err)
		assert.Equal(t, "chosen", found.Name)
		assert.True(t, found.Archived)
	})
}
