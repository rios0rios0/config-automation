package repositories

import (
	"context"
	"errors"
	"strings"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// errTargetNotFound is what FindByName returns for a repository the double
// does not hold.
var errTargetNotFound = errors.New("repository not found")

// InMemoryRefreshTargetsRepository is the in-memory double for
// repositories.RefreshTargetsRepository. It returns its targets for any
// organization and records each organization it is asked to list.
type InMemoryRefreshTargetsRepository struct {
	Targets []entities.RefreshTarget
	Listed  []string

	// ErrorOnList is returned by FindAllByOrganization when set.
	ErrorOnList error
}

// Ensure the port is satisfied at compile time.
var _ repositories.RefreshTargetsRepository = (*InMemoryRefreshTargetsRepository)(nil)

// NewInMemoryRefreshTargetsRepository builds a double holding targets.
func NewInMemoryRefreshTargetsRepository(targets ...entities.RefreshTarget) *InMemoryRefreshTargetsRepository {
	return &InMemoryRefreshTargetsRepository{Targets: targets}
}

// FindAllByOrganization returns every target.
func (r *InMemoryRefreshTargetsRepository) FindAllByOrganization(
	_ context.Context,
	organization string,
) ([]entities.RefreshTarget, error) {
	r.Listed = append(r.Listed, organization)
	if r.ErrorOnList != nil {
		return nil, r.ErrorOnList
	}
	return r.Targets, nil
}

// FindByName returns the target whose project and name match.
func (r *InMemoryRefreshTargetsRepository) FindByName(
	_ context.Context,
	reference entities.RefreshTargetReference,
) (entities.RefreshTarget, error) {
	for _, target := range r.Targets {
		if strings.EqualFold(target.Project, reference.Project) && strings.EqualFold(target.Name, reference.Name) {
			return target, nil
		}
	}
	return entities.RefreshTarget{}, errTargetNotFound
}
