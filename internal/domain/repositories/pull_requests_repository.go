package repositories

import (
	"context"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

// PullRequestsRepository is the port to the pull requests of the repositories
// a refresh targets, on whichever provider hosts them.
type PullRequestsRepository interface {
	// FindOpenBySourceBranch returns the open pull request whose source is
	// branch. The boolean reports whether one exists.
	FindOpenBySourceBranch(
		ctx context.Context,
		target entities.RefreshTarget,
		branch string,
	) (entities.PullRequest, bool, error)

	// Insert opens a pull request.
	Insert(
		ctx context.Context,
		target entities.RefreshTarget,
		draft entities.PullRequestDraft,
	) (entities.PullRequest, error)
}
