package repositories

import (
	"context"

	"github.com/rios0rios0/fleet-maintenance/internal/domain/entities"
)

// OwnerKind reports whether an owner is a `User` or an `Organization`
// account. The value decides which list endpoint the authenticated
// listing path falls back to.
type OwnerKind string

const (
	OwnerKindUser         OwnerKind = "User"
	OwnerKindOrganization OwnerKind = "Organization"
)

// RepositoriesRepository is the port for listing and mutating the
// repository-level settings exposed by `GET/PATCH /repos/{owner}/{repo}`.
// Implementations live in internal/infrastructure/repositories.
type RepositoriesRepository interface {
	// FindAuthenticatedLogin returns the login of the token's owner, or
	// an error when the /user endpoint is unreachable.
	FindAuthenticatedLogin(ctx context.Context) (string, error)

	// FindOwnerKind classifies the owner account (User vs Organization)
	// so listing can pick /user/repos vs /orgs/{owner}/repos vs
	// /users/{owner}/repos. When the owner equals the authenticated login,
	// callers should prefer /user/repos to retain private access.
	FindOwnerKind(ctx context.Context, owner string) (OwnerKind, error)

	// FindAllByOwner lists every repository owned by `owner`, sorted by
	// name. Archived repos are included; callers filter as needed.
	FindAllByOwner(ctx context.Context, owner string, authenticated bool, kind OwnerKind) ([]entities.Repository, error)

	// FindByName fetches a single repo's full details. The list endpoint
	// omits `security_and_analysis`, so phase 1 calls this for every repo.
	FindByName(ctx context.Context, owner, name string) (entities.Repository, error)

	// Save persists the settings in entities.Repository.Settings via
	// PATCH /repos/{owner}/{name}. Fields not covered by the policy are
	// left untouched.
	Save(ctx context.Context, repo entities.Repository) error
}
