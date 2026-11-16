package repositories

import (
	"context"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

// RefreshTargetsRepository is the port to the repositories of one
// organization on one provider. It reports every repository with its fork,
// archived, and disabled state; which of them to refresh is the domain's
// decision. A listing either is complete or fails: a partial one would read
// as an organization that shrank.
type RefreshTargetsRepository interface {
	// FindAllByOrganization lists every repository of organization.
	FindAllByOrganization(ctx context.Context, organization string) ([]entities.RefreshTarget, error)

	// FindByName fetches one repository with its state.
	FindByName(ctx context.Context, reference entities.RefreshTargetReference) (entities.RefreshTarget, error)
}
