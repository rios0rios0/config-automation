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

	"github.com/rios0rios0/config-automation/internal/domain/commands"
	"github.com/rios0rios0/config-automation/internal/domain/entities"
	infra "github.com/rios0rios0/config-automation/internal/infrastructure/repositories"
)

// newCapturingClient returns a go-github client pointed at a test server
// that records the request body and replies with a minimal ruleset. The
// point of these tests is the exact JSON GitHub receives: go-github's
// RepositoryRulesetRules uses custom marshaling, so a wrong field name or
// a rule silently dropped would compile, pass a dry run, and only fail
// against the live API.
func newCapturingClient(t *testing.T, captured *[]byte, method, path string) *github.Client {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, method, r.Method)
		assert.Equal(t, path, r.URL.Path)

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		*captured = body

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"name":"main-protection","enforcement":"active"}`))
	}))
	t.Cleanup(server.Close)

	client, err := github.NewClient(server.Client()).WithEnterpriseURLs(server.URL, server.URL)
	require.NoError(t, err)
	return client
}

func TestGoGithubBranchProtectionsRepositoryCreateRuleset(t *testing.T) {
	t.Parallel()

	t.Run("should send a pull_request rule pinned to merge commits", func(t *testing.T) {
		t.Parallel()
		// given
		var captured []byte
		client := newCapturingClient(t, &captured, http.MethodPost, "/api/v3/repos/rios0rios0/example/rulesets")
		repository := infra.NewGoGithubBranchProtectionsRepository(client)

		// when
		err := repository.CreateRuleset(context.Background(), "rios0rios0", "example", commands.DesiredRuleset())

		// then
		require.NoError(t, err)

		var body map[string]any
		require.NoError(t, json.Unmarshal(captured, &body))

		rules, ok := body["rules"].([]any)
		require.True(t, ok, "rules must marshal as a list, got %T", body["rules"])

		pullRequest := findRule(t, rules, "pull_request")
		params, ok := pullRequest["parameters"].(map[string]any)
		require.True(t, ok, "pull_request rule must carry parameters")
		assert.Equal(t, []any{"merge"}, params["allowed_merge_methods"])
	})

	t.Run("should keep the non_fast_forward rule alongside it", func(t *testing.T) {
		t.Parallel()
		// given — non_fast_forward blocks force pushes and is a separate
		// concern from the merge methods; adding one must not drop the other.
		var captured []byte
		client := newCapturingClient(t, &captured, http.MethodPost, "/api/v3/repos/rios0rios0/example/rulesets")
		repository := infra.NewGoGithubBranchProtectionsRepository(client)

		// when
		err := repository.CreateRuleset(context.Background(), "rios0rios0", "example", commands.DesiredRuleset())

		// then
		require.NoError(t, err)

		var body map[string]any
		require.NoError(t, json.Unmarshal(captured, &body))
		rules, ok := body["rules"].([]any)
		require.True(t, ok)

		findRule(t, rules, "non_fast_forward")
	})

	t.Run("should target refs/heads/main and keep the admin bypass actor", func(t *testing.T) {
		t.Parallel()
		// given
		var captured []byte
		client := newCapturingClient(t, &captured, http.MethodPost, "/api/v3/repos/rios0rios0/example/rulesets")
		repository := infra.NewGoGithubBranchProtectionsRepository(client)

		// when
		err := repository.CreateRuleset(context.Background(), "rios0rios0", "example", commands.DesiredRuleset())

		// then
		require.NoError(t, err)

		var body map[string]any
		require.NoError(t, json.Unmarshal(captured, &body))

		assert.Equal(t, "branch", body["target"])
		assert.Equal(t, "active", body["enforcement"])

		conditions, ok := body["conditions"].(map[string]any)
		require.True(t, ok)
		refName, ok := conditions["ref_name"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, []any{"refs/heads/main"}, refName["include"])

		actors, ok := body["bypass_actors"].([]any)
		require.True(t, ok)
		require.Len(t, actors, 1)
		actor, ok := actors[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, entities.RepositoryAdminActorType, actor["actor_type"])
		assert.InDelta(t, float64(entities.RepositoryAdminActorID), actor["actor_id"], 0)
		assert.Equal(t, "always", actor["bypass_mode"])
	})

	t.Run("should omit the pull_request rule when the policy sets no merge methods", func(t *testing.T) {
		t.Parallel()
		// given — a nil list means "no pull_request rule", which must not
		// marshal as a rule permitting nothing.
		var captured []byte
		client := newCapturingClient(t, &captured, http.MethodPost, "/api/v3/repos/rios0rios0/example/rulesets")
		repository := infra.NewGoGithubBranchProtectionsRepository(client)

		ruleset := commands.DesiredRuleset()
		ruleset.AllowedMergeMethods = nil

		// when
		err := repository.CreateRuleset(context.Background(), "rios0rios0", "example", ruleset)

		// then
		require.NoError(t, err)

		var body map[string]any
		require.NoError(t, json.Unmarshal(captured, &body))
		rules, ok := body["rules"].([]any)
		require.True(t, ok)

		for _, raw := range rules {
			rule, isMap := raw.(map[string]any)
			require.True(t, isMap)
			assert.NotEqual(t, "pull_request", rule["type"])
		}
	})
}

// realRulesetResponse is the body GitHub actually returned for a ruleset
// created with DesiredRuleset() — captured from a live probe against
// rios0rios0/config-automation. Note `require_extra_approval_for_unattributed_changes`,
// which GitHub defaults in without the client sending it: the read path
// must tolerate parameters it never wrote.
const realRulesetResponse = `{
  "id": 21524226,
  "name": "main-protection",
  "target": "branch",
  "source_type": "Repository",
  "source": "rios0rios0/config-automation",
  "enforcement": "active",
  "bypass_actors": [
    {"actor_id": 5, "actor_type": "RepositoryRole", "bypass_mode": "always"}
  ],
  "conditions": {"ref_name": {"exclude": [], "include": ["refs/heads/main"]}},
  "rules": [
    {
      "type": "pull_request",
      "parameters": {
        "allowed_merge_methods": ["merge"],
        "dismiss_stale_reviews_on_push": true,
        "require_code_owner_review": false,
        "require_extra_approval_for_unattributed_changes": true,
        "require_last_push_approval": false,
        "required_approving_review_count": 1,
        "required_review_thread_resolution": true,
        "required_reviewers": []
      }
    },
    {"type": "non_fast_forward"}
  ]
}`

func TestGoGithubBranchProtectionsRepositoryFindRulesetByName(t *testing.T) {
	t.Parallel()

	// newRulesetServer answers both calls FindRulesetByName makes: the list,
	// then the per-ruleset detail fetch.
	newRulesetServer := func(t *testing.T, detail string) *github.Client {
		t.Helper()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if strings.HasSuffix(r.URL.Path, "/rulesets") {
				_, _ = w.Write([]byte(`[{"id":21524226,"name":"main-protection"}]`))
				return
			}
			_, _ = w.Write([]byte(detail))
		}))
		t.Cleanup(server.Close)

		client, err := github.NewClient(server.Client()).WithEnterpriseURLs(server.URL, server.URL)
		require.NoError(t, err)
		return client
	}

	t.Run("should read allowed_merge_methods back off a real GitHub response", func(t *testing.T) {
		t.Parallel()
		// given
		repository := infra.NewGoGithubBranchProtectionsRepository(newRulesetServer(t, realRulesetResponse))

		// when
		ruleset, err := repository.FindRulesetByName(
			context.Background(), "rios0rios0", "config-automation", entities.DesiredRulesetName)

		// then
		require.NoError(t, err)
		require.NotNil(t, ruleset)
		assert.Equal(t, []string{"merge"}, ruleset.AllowedMergeMethods)
		assert.True(t, ruleset.HasNonFastForward)
		assert.True(t, ruleset.TargetsMain)
		assert.True(t, ruleset.AdminBypass)
		assert.True(t, ruleset.IsCompliant(), "a ruleset created by DesiredRuleset must audit as compliant")
	})

	t.Run("should report nil merge methods when the ruleset has no pull_request rule", func(t *testing.T) {
		t.Parallel()
		// given — the pre-change shape every repo is currently in. Nil, not
		// empty, so the audit can say `rule_missing` rather than `none`.
		legacy := `{
			"id": 15329479,
			"name": "main-protection",
			"target": "branch",
			"enforcement": "active",
			"bypass_actors": [{"actor_id": 5, "actor_type": "RepositoryRole", "bypass_mode": "always"}],
			"conditions": {"ref_name": {"exclude": [], "include": ["refs/heads/main"]}},
			"rules": [{"type": "non_fast_forward"}]
		}`
		repository := infra.NewGoGithubBranchProtectionsRepository(newRulesetServer(t, legacy))

		// when
		ruleset, err := repository.FindRulesetByName(
			context.Background(), "rios0rios0", "config-automation", entities.DesiredRulesetName)

		// then
		require.NoError(t, err)
		require.NotNil(t, ruleset)
		assert.Nil(t, ruleset.AllowedMergeMethods)
		assert.True(t, ruleset.HasNonFastForward, "force-push protection is present and must not mask the gap")
		assert.False(t, ruleset.IsCompliant())
	})
}

func findRule(t *testing.T, rules []any, ruleType string) map[string]any {
	t.Helper()

	for _, raw := range rules {
		rule, ok := raw.(map[string]any)
		require.True(t, ok, "each rule must be an object, got %T", raw)
		if rule["type"] == ruleType {
			return rule
		}
	}

	require.Failf(t, "rule not found", "no %q rule in %v", ruleType, rules)
	return nil
}
