package commands

import (
	"context"
	"fmt"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// ListTargetRepositoriesCommand powers `--list-json` for the
// config-and-docs refresh matrix. It filters out forks and archived
// repos so the weekly refresh only targets live repos.
type ListTargetRepositoriesCommand struct {
	reposRepo repositories.Repository
}

// NewListTargetRepositoriesCommand is the constructor the composition root calls.
func NewListTargetRepositoriesCommand(reposRepo repositories.Repository) *ListTargetRepositoriesCommand {
	return &ListTargetRepositoriesCommand{reposRepo: reposRepo}
}

// ListTargetRepositoriesInput takes only the owner.
type ListTargetRepositoriesInput struct {
	Owner string
}

// ListTargetRepositoriesListeners returns the filtered repo list.
type ListTargetRepositoriesListeners struct {
	OnSuccess func(repos []entities.Repository)
	OnError   func(err error)
}

// Execute returns non-fork non-archived repos for the matrix.
func (c ListTargetRepositoriesCommand) Execute(
	ctx context.Context,
	input ListTargetRepositoriesInput,
	listeners ListTargetRepositoriesListeners,
) {
	all, err := FindOwnedRepositories(ctx, c.reposRepo, input.Owner)
	if err != nil {
		listeners.OnError(err)
		return
	}

	filtered := make([]entities.Repository, 0, len(all))
	for _, r := range all {
		if r.Fork || r.Archived {
			continue
		}
		filtered = append(filtered, r)
	}

	listeners.OnSuccess(filtered)
}

// FindOwnedRepositories lists every repository of a GitHub owner through the
// endpoint that keeps them all visible: the authenticated user's own listing
// when the owner is that user, so private repositories stay in, and otherwise
// the organization's or the user's. Each lookup failing is an error, never a
// quietly shorter list.
func FindOwnedRepositories(
	ctx context.Context,
	reposRepo repositories.Repository,
	owner string,
) ([]entities.Repository, error) {
	authenticated, err := reposRepo.FindAuthenticatedLogin(ctx)
	if err != nil {
		return nil, fmt.Errorf("finding authenticated login: %w", err)
	}
	kind, err := reposRepo.FindOwnerKind(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("finding owner kind for %s: %w", owner, err)
	}
	all, err := reposRepo.FindAllByOwner(ctx, owner, authenticated == owner, kind)
	if err != nil {
		return nil, fmt.Errorf("listing repos: %w", err)
	}
	return all, nil
}
