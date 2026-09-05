//go:build unit

package repositories_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-github/v75/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/test/domain/builders"
	infra "github.com/rios0rios0/config-automation/internal/infrastructure/repositories"
)

// newSecurityServer answers every call FindByRepositoryName makes — the
// repo detail, both Dependabot endpoints, and the Actions permissions
// endpoint — and records the body of any PUT to the last one. The other
// three answer with a healthy, compliant shape so a test can vary the
// Actions response alone.
func newSecurityServer(t *testing.T, actionsStatus int, actionsBody string, captured *[]byte) *github.Client {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/actions/permissions"):
			if r.Method == http.MethodPut {
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				*captured = body
			}
			w.WriteHeader(actionsStatus)
			_, _ = w.Write([]byte(actionsBody))
		case strings.HasSuffix(r.URL.Path, "/vulnerability-alerts"):
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/automated-security-fixes"):
			_, _ = w.Write([]byte(`{"enabled":true,"paused":false}`))
		default:
			_, _ = w.Write([]byte(`{"id":1,"name":"forked","fork":true,` +
				`"security_and_analysis":{"secret_scanning":{"status":"disabled"}}}`))
		}
	}))
	t.Cleanup(server.Close)

	client, err := github.NewClient(server.Client()).WithEnterpriseURLs(server.URL, server.URL)
	require.NoError(t, err)
	return client
}

func TestGoGithubSecuritySettingsRepositoryFindByRepositoryName(t *testing.T) {
	t.Parallel()

	t.Run("should read the Actions switch off the permissions endpoint when it answers", func(t *testing.T) {
		t.Parallel()
		// given — the shape GitHub returns for a repo that still runs
		// workflows; `allowed_actions` rides along and must not matter.
		var captured []byte
		client := newSecurityServer(t, http.StatusOK, `{"enabled":true,"allowed_actions":"all"}`, &captured)
		repository := infra.NewGoGithubSecuritySettingsRepository(client)
		repo := builders.NewRepositoryBuilder().WithName("forked").AsFork().Build()

		// when
		settings, err := repository.FindByRepositoryName(context.Background(), repo)

		// then
		require.NoError(t, err)
		require.NotNil(t, settings.ActionsEnabled)
		assert.True(t, *settings.ActionsEnabled)
		assert.False(t, settings.IsActionsDisabled())
	})

	t.Run("should read a disabled Actions switch when GitHub omits allowed_actions", func(t *testing.T) {
		t.Parallel()
		// given — with Actions off GitHub returns only the switch.
		var captured []byte
		client := newSecurityServer(t, http.StatusOK, `{"enabled":false}`, &captured)
		repository := infra.NewGoGithubSecuritySettingsRepository(client)
		repo := builders.NewRepositoryBuilder().WithName("forked").AsFork().Build()

		// when
		settings, err := repository.FindByRepositoryName(context.Background(), repo)

		// then
		require.NoError(t, err)
		assert.True(t, settings.IsActionsDisabled())
	})

	t.Run("should leave the Actions switch unknown when the permissions endpoint fails", func(t *testing.T) {
		t.Parallel()
		// given — a token without `Administration: read` gets a 403 here
		// while every other endpoint still answers. The audit must see
		// unknown, not disabled, and the rest of the snapshot must survive.
		var captured []byte
		client := newSecurityServer(
			t, http.StatusForbidden, `{"message":"Resource not accessible by personal access token"}`, &captured)
		repository := infra.NewGoGithubSecuritySettingsRepository(client)
		repo := builders.NewRepositoryBuilder().WithName("forked").AsFork().Build()

		// when
		settings, err := repository.FindByRepositoryName(context.Background(), repo)

		// then
		require.NoError(t, err, "an unreadable Actions switch is drift to report, not an audit failure")
		assert.Nil(t, settings.ActionsEnabled)
		assert.False(t, settings.IsActionsDisabled())
		require.NotNil(t, settings.DependabotAlerts)
		assert.True(t, *settings.DependabotAlerts)
		assert.True(t, settings.DependabotUpdates)
	})
}

func TestGoGithubSecuritySettingsRepositoryDisableActions(t *testing.T) {
	t.Parallel()

	t.Run("should PUT enabled=false and nothing else to the permissions endpoint", func(t *testing.T) {
		t.Parallel()
		// given — the same endpoint carries the allowed-actions policy;
		// sending it would overwrite whatever the fork had.
		var captured []byte
		client := newSecurityServer(t, http.StatusOK, `{"enabled":false}`, &captured)
		repository := infra.NewGoGithubSecuritySettingsRepository(client)

		// when
		err := repository.DisableActions(context.Background(), "rios0rios0", "forked")

		// then
		require.NoError(t, err)

		var body map[string]any
		require.NoError(t, json.Unmarshal(captured, &body))
		assert.Equal(t, map[string]any{"enabled": false}, body)
	})

	t.Run("should return the API error when the permissions endpoint rejects the PUT", func(t *testing.T) {
		t.Parallel()
		// given
		var captured []byte
		client := newSecurityServer(
			t, http.StatusForbidden, `{"message":"Resource not accessible by personal access token"}`, &captured)
		repository := infra.NewGoGithubSecuritySettingsRepository(client)

		// when
		err := repository.DisableActions(context.Background(), "rios0rios0", "forked")

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "403")
	})
}
