package repositories

import (
	"context"
	"errors"
	"fmt"

	forge "github.com/rios0rios0/gitforge/v4/pkg/global/domain/entities"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// errNoPullRequestFinder reports a provider that cannot return an open pull
// request, which the refresh needs to update one instead of opening another.
var errNoPullRequestFinder = errors.New("the provider cannot look up an open pull request")

// GitforgePullRequestsRepository implements repositories.PullRequestsRepository
// through a gitforge provider, the library the other fleet automations share
// for every Git host they talk to.
type GitforgePullRequestsRepository struct {
	provider entities.Provider
	forge    forge.ForgeProvider
}

// Ensure the port is satisfied at compile time.
var _ repositories.PullRequestsRepository = (*GitforgePullRequestsRepository)(nil)

// NewGitforgePullRequestsRepository is the constructor the composition root calls.
func NewGitforgePullRequestsRepository(
	provider entities.Provider,
	client forge.ForgeProvider,
) *GitforgePullRequestsRepository {
	return &GitforgePullRequestsRepository{provider: provider, forge: client}
}

// FindOpenBySourceBranch asks the provider for the open pull request of branch.
func (r *GitforgePullRequestsRepository) FindOpenBySourceBranch(
	ctx context.Context,
	target entities.RefreshTarget,
	branch string,
) (entities.PullRequest, bool, error) {
	finder, supported := r.forge.(forge.OpenPullRequestFinder)
	if !supported {
		return entities.PullRequest{}, false, errNoPullRequestFinder
	}
	pullRequest, found, err := finder.FindOpenPullRequest(ctx, r.repository(target), branch)
	if err != nil {
		return entities.PullRequest{}, false, fmt.Errorf("finding the open pull request: %w", err)
	}
	if !found {
		return entities.PullRequest{}, false, nil
	}
	return r.pullRequest(target, pullRequest.ID), true, nil
}

// Insert opens a pull request and returns it with its web address.
func (r *GitforgePullRequestsRepository) Insert(
	ctx context.Context,
	target entities.RefreshTarget,
	draft entities.PullRequestDraft,
) (entities.PullRequest, error) {
	created, err := r.forge.CreatePullRequest(ctx, r.repository(target), forge.PullRequestInput{
		SourceBranch: draft.SourceBranch, TargetBranch: draft.TargetBranch,
		Title: draft.Title, Description: draft.Description,
	})
	if err != nil {
		return entities.PullRequest{}, fmt.Errorf("opening the pull request: %w", err)
	}
	if created == nil {
		return entities.PullRequest{}, errors.New("the provider returned no pull request")
	}
	return r.pullRequest(target, created.ID), nil
}

// repository maps a target onto the library's repository.
func (r *GitforgePullRequestsRepository) repository(target entities.RefreshTarget) forge.Repository {
	return forge.Repository{
		ID: target.ID, Name: target.Name, Organization: target.Organization, Project: target.Project,
		DefaultBranch: "refs/heads/" + target.DefaultBranch, ProviderName: string(r.provider),
	}
}

// pullRequest returns the pull request with its web page, which the library
// reports as an API address on some providers.
func (r *GitforgePullRequestsRepository) pullRequest(target entities.RefreshTarget, number int) entities.PullRequest {
	return entities.PullRequest{Number: number, URL: r.provider.PullRequestURL(target, number)}
}
