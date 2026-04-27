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

// NewListTargetRepositoriesCommand is the Dig-injectable constructor.
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
	authenticated, err := c.reposRepo.FindAuthenticatedLogin(ctx)
	if err != nil {
		listeners.OnError(fmt.Errorf("finding authenticated login: %w", err))
		return
	}

	kind, err := c.reposRepo.FindOwnerKind(ctx, input.Owner)
	if err != nil {
		listeners.OnError(fmt.Errorf("finding owner kind for %s: %w", input.Owner, err))
		return
	}

	all, err := c.reposRepo.FindAllByOwner(ctx, input.Owner, authenticated == input.Owner, kind)
	if err != nil {
		listeners.OnError(fmt.Errorf("listing repos: %w", err))
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
