package repositories

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/go-github/v66/github"

	"github.com/rios0rios0/fleet-maintenance/internal/domain/entities"
	"github.com/rios0rios0/fleet-maintenance/internal/domain/repositories"
)

// githubListPerPage is the upper bound the GitHub API accepts for
// paginated list endpoints.
const githubListPerPage = 100

// GoGithubRepositoriesRepository implements repositories.Repository
// by wrapping the google/go-github client. The mapping from go-github's
// `*github.Repository` into entities.Repository lives inline in this file;
// a separate mapper package would be overkill for one adapter.
type GoGithubRepositoriesRepository struct {
	client *github.Client
}

// NewGoGithubRepositoriesRepository is the Dig-injectable constructor.
func NewGoGithubRepositoriesRepository(client *github.Client) *GoGithubRepositoriesRepository {
	return &GoGithubRepositoriesRepository{client: client}
}

// Ensure the interface is satisfied at compile time.
var _ repositories.Repository = (*GoGithubRepositoriesRepository)(nil)

// FindAuthenticatedLogin returns the login for the token the client
// was configured with.
func (r GoGithubRepositoriesRepository) FindAuthenticatedLogin(ctx context.Context) (string, error) {
	user, _, err := r.client.Users.Get(ctx, "")
	if err != nil {
		return "", err
	}
	if user == nil || user.Login == nil {
		return "", errors.New("authenticated user has no login")
	}
	return *user.Login, nil
}

// FindOwnerKind returns whether the owner is a User or Organization.
func (r GoGithubRepositoriesRepository) FindOwnerKind(
	ctx context.Context,
	owner string,
) (repositories.OwnerKind, error) {
	user, _, err := r.client.Users.Get(ctx, owner)
	if err != nil {
		return "", err
	}
	if user == nil || user.Type == nil {
		return "", fmt.Errorf("owner %s has no type", owner)
	}
	return repositories.OwnerKind(*user.Type), nil
}

// FindAllByOwner paginates the appropriate list endpoint based on
// whether the owner equals the authenticated user (so private repos
// stay visible) and whether the owner is a User or Organization.
func (r GoGithubRepositoriesRepository) FindAllByOwner(
	ctx context.Context,
	owner string,
	authenticated bool,
	kind repositories.OwnerKind,
) ([]entities.Repository, error) {
	opts := &github.ListOptions{PerPage: githubListPerPage}
	collected := make([]*github.Repository, 0)

	for {
		var (
			batch []*github.Repository
			resp  *github.Response
			err   error
		)

		switch {
		case authenticated:
			// /user/repos retains private repo visibility.
			listOpts := &github.RepositoryListByAuthenticatedUserOptions{
				Affiliation: "owner",
				ListOptions: *opts,
			}
			batch, resp, err = r.client.Repositories.ListByAuthenticatedUser(ctx, listOpts)
		case kind == repositories.OwnerKindOrganization:
			listOpts := &github.RepositoryListByOrgOptions{
				Type:        "all",
				ListOptions: *opts,
			}
			batch, resp, err = r.client.Repositories.ListByOrg(ctx, owner, listOpts)
		default:
			listOpts := &github.RepositoryListByUserOptions{
				Type:        "owner",
				ListOptions: *opts,
			}
			batch, resp, err = r.client.Repositories.ListByUser(ctx, owner, listOpts)
		}

		if err != nil {
			return nil, err
		}
		collected = append(collected, batch...)
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	sort.Slice(collected, func(i, j int) bool {
		return nameOrEmpty(collected[i]) < nameOrEmpty(collected[j])
	})

	result := make([]entities.Repository, 0, len(collected))
	for _, repo := range collected {
		result = append(result, mapRepoListingToEntity(repo))
	}
	return result, nil
}

// FindByName fetches full detail for one repository. This endpoint
// returns security_and_analysis, which the list endpoints omit.
func (r GoGithubRepositoriesRepository) FindByName(
	ctx context.Context,
	owner, name string,
) (entities.Repository, error) {
	repo, _, err := r.client.Repositories.Get(ctx, owner, name)
	if err != nil {
		return entities.Repository{}, err
	}
	if repo == nil {
		return entities.Repository{}, fmt.Errorf("repo %s/%s not found", owner, name)
	}
	return mapRepoDetailToEntity(repo), nil
}

// Save PATCHes the policy-covered settings. Fields outside the
// policy are not touched.
func (r GoGithubRepositoriesRepository) Save(ctx context.Context, repo entities.Repository) error {
	patch := &github.Repository{
		DeleteBranchOnMerge: new(repo.Settings.DeleteBranchOnMerge),
		AllowAutoMerge:      new(repo.Settings.AllowAutoMerge),
		AllowSquashMerge:    new(repo.Settings.AllowSquashMerge),
		AllowRebaseMerge:    new(repo.Settings.AllowRebaseMerge),
		AllowMergeCommit:    new(repo.Settings.AllowMergeCommit),
		HasWiki:             new(repo.Settings.HasWiki),
		HasProjects:         new(repo.Settings.HasProjects),
	}
	_, _, err := r.client.Repositories.Edit(ctx, repo.Owner, repo.Name, patch)
	return err
}

func mapRepoListingToEntity(repo *github.Repository) entities.Repository {
	return entities.Repository{
		Name:          stringOrEmpty(repo.Name),
		Owner:         ownerLogin(repo),
		Visibility:    stringOrEmpty(repo.Visibility),
		Private:       boolOrFalse(repo.Private),
		Fork:          boolOrFalse(repo.Fork),
		Archived:      boolOrFalse(repo.Archived),
		DefaultBranch: stringOrEmpty(repo.DefaultBranch),
	}
}

func mapRepoDetailToEntity(repo *github.Repository) entities.Repository {
	entity := mapRepoListingToEntity(repo)
	entity.Settings = entities.RepositorySettings{
		DeleteBranchOnMerge: boolOrFalse(repo.DeleteBranchOnMerge),
		AllowAutoMerge:      boolOrFalse(repo.AllowAutoMerge),
		AllowSquashMerge:    boolOrDefault(repo.AllowSquashMerge, true),
		AllowRebaseMerge:    boolOrDefault(repo.AllowRebaseMerge, true),
		AllowMergeCommit:    boolOrDefault(repo.AllowMergeCommit, true),
		HasWiki:             boolOrFalse(repo.HasWiki),
		HasProjects:         boolOrFalse(repo.HasProjects),
	}
	return entity
}

func ownerLogin(repo *github.Repository) string {
	if repo.Owner == nil || repo.Owner.Login == nil {
		return ""
	}
	return *repo.Owner.Login
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func boolOrFalse(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}

func boolOrDefault(b *bool, d bool) bool {
	if b == nil {
		return d
	}
	return *b
}

func nameOrEmpty(repo *github.Repository) string {
	if repo == nil || repo.Name == nil {
		return ""
	}
	return *repo.Name
}
