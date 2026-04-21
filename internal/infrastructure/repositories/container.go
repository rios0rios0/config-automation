package repositories

import (
	"context"
	"os"

	"github.com/google/go-github/v66/github"
	"go.uber.org/dig"
	"golang.org/x/oauth2"

	"github.com/rios0rios0/fleet-maintenance/internal/domain/repositories"
)

// RegisterProviders wires the go-github client and the three repository
// implementations into the Dig container. Callers register this layer
// first so domain providers can resolve their dependencies.
func RegisterProviders(container *dig.Container) error {
	providers := []any{
		newGithubClient,
		NewGoGithubRepositoriesRepository,
		NewGoGithubSecuritySettingsRepository,
		NewGoGithubBranchProtectionsRepository,
		// Bind the concrete structs to the domain interfaces so
		// constructors that depend on the interface type can resolve.
		func(impl *GoGithubRepositoriesRepository) repositories.RepositoriesRepository {
			return impl
		},
		func(impl *GoGithubSecuritySettingsRepository) repositories.SecuritySettingsRepository {
			return impl
		},
		func(impl *GoGithubBranchProtectionsRepository) repositories.BranchProtectionsRepository {
			return impl
		},
	}
	for _, p := range providers {
		if err := container.Provide(p); err != nil {
			return err
		}
	}
	return nil
}

// newGithubClient builds an authenticated go-github client. It accepts
// the token via the GH_TOKEN env var (same convention as the Python
// script's subprocess call to `gh api`).
func newGithubClient() *github.Client {
	token := os.Getenv("GH_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		return github.NewClient(nil)
	}
	httpClient := oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}))
	return github.NewClient(httpClient)
}
