package repositories

import (
	"context"
	"os"
	"strings"

	"github.com/google/go-github/v75/github"
	"go.uber.org/dig"
	"golang.org/x/oauth2"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// RegisterProviders wires the go-github client, the SonarQube Cloud
// configuration, and the four repository implementations into the Dig
// container. Callers register this layer first so domain providers can
// resolve their dependencies.
func RegisterProviders(container *dig.Container) error {
	providers := []any{
		newGithubClient,
		NewGoGithubRepositoriesRepository,
		NewGoGithubSecuritySettingsRepository,
		NewGoGithubBranchProtectionsRepository,
		newSonarConfig,
		NewSonarCloudSonarProjectsRepository,
		// Bind the concrete structs to the domain interfaces so
		// constructors that depend on the interface type can resolve.
		func(impl *GoGithubRepositoriesRepository) repositories.Repository {
			return impl
		},
		func(impl *GoGithubSecuritySettingsRepository) repositories.SecuritySettingsRepository {
			return impl
		},
		func(impl *GoGithubBranchProtectionsRepository) repositories.BranchProtectionsRepository {
			return impl
		},
		func(impl *SonarCloudSonarProjectsRepository) repositories.SonarProjectsRepository {
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

// SonarConfig carries the SonarQube Cloud endpoint and credential into
// the adapter. It is a named type rather than two loose strings so Dig
// can resolve it by type without ambiguity.
type SonarConfig struct {
	Host  string
	Token string
}

// newSonarConfig reads the SonarQube Cloud endpoint and user token from
// the environment. An absent token is not fatal here: every read
// endpoint the adapter uses answers anonymously for public projects, so
// `--sonar-policy --dry-run` reports what would change without a
// credential, and only the mutations fail with the API's own
// "Insufficient privileges".
func newSonarConfig() SonarConfig {
	host := strings.TrimRight(os.Getenv("SONAR_HOST_URL"), "/")
	if host == "" {
		host = entities.DesiredSonarHost
	}
	return SonarConfig{Host: host, Token: os.Getenv("SONAR_TOKEN")}
}
