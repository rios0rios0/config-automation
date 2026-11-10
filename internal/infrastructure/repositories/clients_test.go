package repositories_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	infra "github.com/rios0rios0/config-automation/internal/infrastructure/repositories"
)

// TestNewGithubClient is deliberately not parallel: every case sets both
// token variables with t.Setenv, which also keeps a token from the
// environment the suite runs in out of the assertion.
func TestNewGithubClient(t *testing.T) {
	tests := []struct {
		name          string
		ghToken       string
		githubToken   string
		authorization string
	}{
		{
			name:          "should send GH_TOKEN as a bearer token when both variables are set",
			ghToken:       "gh-token-placeholder",
			githubToken:   "github-token-placeholder",
			authorization: "Bearer gh-token-placeholder",
		},
		{
			name:          "should fall back to GITHUB_TOKEN when GH_TOKEN is empty",
			githubToken:   "github-token-placeholder",
			authorization: "Bearer github-token-placeholder",
		},
		{
			name:          "should call the API anonymously when neither variable is set",
			authorization: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// given
			t.Setenv("GH_TOKEN", test.ghToken)
			t.Setenv("GITHUB_TOKEN", test.githubToken)
			var received string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received = r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"login":"rios0rios0"}`))
			}))
			t.Cleanup(server.Close)
			client, err := infra.NewGithubClient(
				github.WithHTTPClient(server.Client()),
				github.WithEnterpriseURLs(server.URL, server.URL),
			)
			require.NoError(t, err)

			// when
			_, _, err = client.Users.Get(context.Background(), "")

			// then
			require.NoError(t, err)
			assert.Equal(t, test.authorization, received)
		})
	}
}
