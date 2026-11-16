package repositories

import (
	"context"
	"fmt"
	"sync"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// firstPullRequestNumber numbers the first pull request the double opens.
const firstPullRequestNumber = 1

// InMemoryPullRequestsRepository is the in-memory double for
// repositories.PullRequestsRepository. It keeps the open pull requests by
// qualified name and source branch, and records every one it opens.
type InMemoryPullRequestsRepository struct {
	mutex   sync.Mutex
	open    map[string]entities.PullRequest
	Created []entities.PullRequestDraft
	Lookups int

	// ErrorOnFind and ErrorOnInsert are returned by their method when set.
	ErrorOnFind   error
	ErrorOnInsert error
	// PanicOnFind makes FindOpenBySourceBranch panic, standing in for a
	// defect in an adapter.
	PanicOnFind bool
}

// Ensure the port is satisfied at compile time.
var _ repositories.PullRequestsRepository = (*InMemoryPullRequestsRepository)(nil)

// NewInMemoryPullRequestsRepository builds a double with no open pull request.
func NewInMemoryPullRequestsRepository() *InMemoryPullRequestsRepository {
	return &InMemoryPullRequestsRepository{open: map[string]entities.PullRequest{}}
}

// WithOpen records an open pull request from branch in target.
func (r *InMemoryPullRequestsRepository) WithOpen(
	target entities.RefreshTarget,
	branch string,
	pullRequest entities.PullRequest,
) *InMemoryPullRequestsRepository {
	r.open[key(target, branch)] = pullRequest
	return r
}

// FindOpenBySourceBranch returns the open pull request from branch.
func (r *InMemoryPullRequestsRepository) FindOpenBySourceBranch(
	_ context.Context,
	target entities.RefreshTarget,
	branch string,
) (entities.PullRequest, bool, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.Lookups++
	if r.PanicOnFind {
		panic("fixture adapter defect")
	}
	if r.ErrorOnFind != nil {
		return entities.PullRequest{}, false, r.ErrorOnFind
	}
	pullRequest, found := r.open[key(target, branch)]
	return pullRequest, found, nil
}

// Insert opens a pull request and records its draft.
func (r *InMemoryPullRequestsRepository) Insert(
	_ context.Context,
	target entities.RefreshTarget,
	draft entities.PullRequestDraft,
) (entities.PullRequest, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.ErrorOnInsert != nil {
		return entities.PullRequest{}, r.ErrorOnInsert
	}
	r.Created = append(r.Created, draft)
	number := firstPullRequestNumber + len(r.open)
	pullRequest := entities.PullRequest{
		Number: number, URL: fmt.Sprintf("https://example.invalid/%s/pull/%d", target.QualifiedName(), number),
	}
	r.open[key(target, draft.SourceBranch)] = pullRequest
	return pullRequest, nil
}

// key identifies an open pull request.
func key(target entities.RefreshTarget, branch string) string {
	return target.QualifiedName() + "@" + branch
}
