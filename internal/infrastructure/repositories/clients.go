package repositories

import (
	"context"
	"os"
	"strings"

	"github.com/google/go-github/v75/github"
	"golang.org/x/oauth2"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

// NewGithubClient builds the go-github client the GitHub adapters share. It
// reads the token from GH_TOKEN, falling back to GITHUB_TOKEN, the variables
// the gh CLI reads; without either it calls the API anonymously.
func NewGithubClient() *github.Client {
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
// the adapter, as one named type rather than two loose strings.
type SonarConfig struct {
	Host  string
	Token string
}

// NewSonarConfig reads the SonarQube Cloud endpoint and user token from
// the environment. An absent token is not fatal here: every read
// endpoint the adapter uses answers anonymously for public projects, so
// `--sonar-policy --dry-run` reports what would change without a
// credential, and only the mutations fail — with the API's own denial
// message, which is not the same one everywhere. Uncredentialed,
// api/settings/set answers 404 "Project doesn't exist" (SonarQube Cloud
// hides a project from a caller that cannot administer it) while
// api/issues/bulk_change and api/hotspots/change_status answer 401
// "Authentication is required"; a token whose user lacks the right gets
// 403 "Insufficient privileges" instead.
func NewSonarConfig() SonarConfig {
	host := strings.TrimRight(os.Getenv("SONAR_HOST_URL"), "/")
	if host == "" {
		host = entities.DesiredSonarHost
	}
	return SonarConfig{Host: host, Token: os.Getenv("SONAR_TOKEN")}
}
