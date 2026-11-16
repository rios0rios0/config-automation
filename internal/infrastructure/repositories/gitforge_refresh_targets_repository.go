package repositories

import (
	"context"
	"fmt"
	"strings"

	forge "github.com/rios0rios0/gitforge/v4/pkg/global/domain/entities"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// GitforgeRefreshTargetsRepository implements repositories.RefreshTargetsRepository
// with a gitforge provider's strict organization listing: one call that either
// returns the whole organization or fails, never a quietly shorter list.
type GitforgeRefreshTargetsRepository struct {
	lister forge.OrganizationRepositoryLister
}

// Ensure the port is satisfied at compile time.
var _ repositories.RefreshTargetsRepository = (*GitforgeRefreshTargetsRepository)(nil)

// NewGitforgeRefreshTargetsRepository is the constructor the composition root calls.
func NewGitforgeRefreshTargetsRepository(lister forge.OrganizationRepositoryLister) *GitforgeRefreshTargetsRepository {
	return &GitforgeRefreshTargetsRepository{lister: lister}
}

// FindAllByOrganization lists every repository of organization with its state.
func (r *GitforgeRefreshTargetsRepository) FindAllByOrganization(
	ctx context.Context,
	organization string,
) ([]entities.RefreshTarget, error) {
	listed, err := r.lister.ListOrganizationRepositories(ctx, organization)
	if err != nil {
		return nil, fmt.Errorf("listing the repositories of %s: %w", organization, err)
	}
	targets := make([]entities.RefreshTarget, 0, len(listed))
	for _, repository := range listed {
		targets = append(targets, entities.RefreshTarget{
			Organization:  repository.Organization,
			Project:       repository.Project,
			Name:          repository.Name,
			ID:            repository.ID,
			DefaultBranch: strings.TrimPrefix(repository.DefaultBranch, "refs/heads/"),
			Fork:          repository.IsFork,
			Archived:      repository.IsArchived,
			Disabled:      repository.IsDisabled,
		})
	}
	return targets, nil
}

// FindByName finds one repository in its organization's listing, which is
// a single call and carries the same state the schedule filters on. Names
// compare without regard to case, as the provider treats them.
func (r *GitforgeRefreshTargetsRepository) FindByName(
	ctx context.Context,
	reference entities.RefreshTargetReference,
) (entities.RefreshTarget, error) {
	targets, err := r.FindAllByOrganization(ctx, reference.Organization)
	if err != nil {
		return entities.RefreshTarget{}, err
	}
	for _, target := range targets {
		if strings.EqualFold(target.Project, reference.Project) && strings.EqualFold(target.Name, reference.Name) {
			return target, nil
		}
	}
	return entities.RefreshTarget{}, fmt.Errorf("no repository %s/%s in %s",
		reference.Project, reference.Name, reference.Organization)
}
