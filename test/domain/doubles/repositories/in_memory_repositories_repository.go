package repositories

import (
	"context"
	"errors"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// InMemoryRepositoriesRepository is the in-memory double for
// repositories.Repository used by command tests. It records
// every Save call so assertions can verify what the command mutated.
type InMemoryRepositoriesRepository struct {
	AuthenticatedLogin string
	OwnerKind          repositories.OwnerKind
	Repos              []entities.Repository
	Saves              []entities.Repository

	// Error hooks — set to surface a specific error from a method.
	ErrorOnLogin     error
	ErrorOnOwnerKind error
	ErrorOnList      error
	ErrorOnFindName  error
	ErrorOnSave      error
}

// NewInMemoryRepositoriesRepository builds a fresh double with defaults.
func NewInMemoryRepositoriesRepository() *InMemoryRepositoriesRepository {
	return &InMemoryRepositoriesRepository{
		AuthenticatedLogin: "rios0rios0",
		OwnerKind:          repositories.OwnerKindUser,
	}
}

// WithRepos seeds the underlying store so FindAllByOwner and FindByName
// return them.
func (r *InMemoryRepositoriesRepository) WithRepos(repos ...entities.Repository) *InMemoryRepositoriesRepository {
	r.Repos = repos
	return r
}

func (r *InMemoryRepositoriesRepository) FindAuthenticatedLogin(_ context.Context) (string, error) {
	return r.AuthenticatedLogin, r.ErrorOnLogin
}

func (r *InMemoryRepositoriesRepository) FindOwnerKind(_ context.Context, _ string) (repositories.OwnerKind, error) {
	return r.OwnerKind, r.ErrorOnOwnerKind
}

func (r *InMemoryRepositoriesRepository) FindAllByOwner(
	_ context.Context,
	_ string,
	_ bool,
	_ repositories.OwnerKind,
) ([]entities.Repository, error) {
	return r.Repos, r.ErrorOnList
}

func (r *InMemoryRepositoriesRepository) FindByName(_ context.Context, _, name string) (entities.Repository, error) {
	if r.ErrorOnFindName != nil {
		return entities.Repository{}, r.ErrorOnFindName
	}
	for _, repo := range r.Repos {
		if repo.Name == name {
			return repo, nil
		}
	}
	return entities.Repository{}, errors.New("not found")
}

func (r *InMemoryRepositoriesRepository) Save(_ context.Context, repo entities.Repository) error {
	if r.ErrorOnSave != nil {
		return r.ErrorOnSave
	}
	r.Saves = append(r.Saves, repo)

	for i, stored := range r.Repos {
		if stored.Name == repo.Name {
			r.Repos[i] = repo
			break
		}
	}
	return nil
}
