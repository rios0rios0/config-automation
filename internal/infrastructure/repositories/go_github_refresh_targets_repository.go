package repositories

import (
	"context"

	"github.com/rios0rios0/config-automation/internal/domain/commands"
	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// GoGithubRefreshTargetsRepository implements repositories.RefreshTargetsRepository
// for GitHub over the go-github listing the compliance audit already uses, so
// GitHub repositories are enumerated one way only, with its owner and
// authenticated-login rules.
type GoGithubRefreshTargetsRepository struct {
	repositories repositories.Repository
}

// Ensure the port is satisfied at compile time.
var _ repositories.RefreshTargetsRepository = (*GoGithubRefreshTargetsRepository)(nil)

// NewGoGithubRefreshTargetsRepository is the constructor the composition root calls.
func NewGoGithubRefreshTargetsRepository(listing repositories.Repository) *GoGithubRefreshTargetsRepository {
	return &GoGithubRefreshTargetsRepository{repositories: listing}
}

// FindAllByOrganization lists every repository of the owner with its state.
func (r *GoGithubRefreshTargetsRepository) FindAllByOrganization(
	ctx context.Context,
	organization string,
) ([]entities.RefreshTarget, error) {
	owned, err := commands.FindOwnedRepositories(ctx, r.repositories, organization)
	if err != nil {
		return nil, err
	}
	targets := make([]entities.RefreshTarget, 0, len(owned))
	for _, repository := range owned {
		targets = append(targets, githubTarget(organization, repository))
	}
	return targets, nil
}

// FindByName fetches one repository with its state.
func (r *GoGithubRefreshTargetsRepository) FindByName(
	ctx context.Context,
	reference entities.RefreshTargetReference,
) (entities.RefreshTarget, error) {
	repository, err := r.repositories.FindByName(ctx, reference.Organization, reference.Name)
	if err != nil {
		return entities.RefreshTarget{}, err
	}
	return githubTarget(reference.Organization, repository), nil
}

// githubTarget maps a GitHub repository onto a target; the listing's owner
// wins over the requested one, which can differ in case.
func githubTarget(owner string, repository entities.Repository) entities.RefreshTarget {
	if repository.Owner != "" {
		owner = repository.Owner
	}
	return entities.RefreshTarget{
		Organization:  owner,
		Name:          repository.Name,
		DefaultBranch: repository.DefaultBranch,
		Fork:          repository.Fork,
		Archived:      repository.Archived,
		Disabled:      repository.Disabled,
	}
}
