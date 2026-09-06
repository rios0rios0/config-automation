//go:build unit

package repositories_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	infra "github.com/rios0rios0/config-automation/internal/infrastructure/repositories"
)

// sonarRequest records one request the adapter sent, reduced to what the
// wire format tests assert on.
type sonarRequest struct {
	Method string
	Path   string
	Query  url.Values
	Form   url.Values
	Auth   string
}

// newSonarServer stands up a fake SonarQube Cloud, recording every
// request and replying with the canned body registered for the path.
//
// These tests exist for the same reason as the go-github ruleset ones:
// the adapter's whole job is the exact parameter names SonarQube
// receives, and a wrong one — `fieldValues` sent as separate `ruleKey`
// and `resourceKey` params, say — compiles, passes a dry run, and only
// fails against the live API.
func newSonarServer(t *testing.T, bodies map[string]string) (*infra.SonarCloudSonarProjectsRepository, *[]sonarRequest) {
	t.Helper()

	recorded := make([]sonarRequest, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		recorded = append(recorded, sonarRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Form:   r.PostForm,
			Auth:   r.Header.Get("Authorization"),
		})

		body, known := bodies[r.URL.Path]
		if !known {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"msg":"unexpected path"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	repository := infra.NewSonarCloudSonarProjectsRepository(infra.SonarConfig{
		Host:  server.URL,
		Token: "test-token",
	})
	return repository, &recorded
}

func TestSonarCloudSonarProjectsRepositoryFindAllByOrganization(t *testing.T) {
	t.Parallel()

	t.Run("should page until the listing is exhausted", func(t *testing.T) {
		t.Parallel()
		// given — a page carrying fewer components than `total`; stopping
		// after the first would silently shrink the fleet.
		page := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			page++
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"paging":{"pageIndex":%d,"pageSize":1,"total":2},
				"components":[{"key":"rios0rios0_p%d","name":"p%d","organization":"rios0rios0"}]}`, page, page, page)
		}))
		t.Cleanup(server.Close)
		repository := infra.NewSonarCloudSonarProjectsRepository(infra.SonarConfig{Host: server.URL})

		// when
		projects, err := repository.FindAllByOrganization(context.Background(), "rios0rios0")

		// then
		require.NoError(t, err)
		require.Len(t, projects, 2)
		assert.Equal(t, "rios0rios0_p1", projects[0].Key)
		assert.Equal(t, "p2", projects[1].Name)
	})

	t.Run("should return the API's own message when the call is refused", func(t *testing.T) {
		t.Parallel()
		// given — SonarQube explains a denial in errors[].msg, not in the
		// status line, so the message has to survive into the error.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"errors":[{"msg":"Authentication is required"}]}`))
		}))
		t.Cleanup(server.Close)
		repository := infra.NewSonarCloudSonarProjectsRepository(infra.SonarConfig{Host: server.URL})

		// when
		_, err := repository.FindAllByOrganization(context.Background(), "rios0rios0")

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Authentication is required")
		assert.Contains(t, err.Error(), "401")
	})
}

func TestSonarCloudSonarProjectsRepositoryIssueExclusions(t *testing.T) {
	t.Parallel()

	t.Run("should read the ruleKey and resourceKey of every configured pair", func(t *testing.T) {
		t.Parallel()
		// given
		repository, requests := newSonarServer(t, map[string]string{
			"/api/settings/values": `{"settings":[{"key":"sonar.issue.ignore.multicriteria",
				"fieldValues":[{"ruleKey":"githubactions:S7637","resourceKey":"**/*.y*ml"}]}]}`,
		})

		// when
		exclusions, err := repository.FindIssueExclusionsByProjectKey(context.Background(), "rios0rios0_autobump")

		// then
		require.NoError(t, err)
		require.Len(t, exclusions, 1)
		assert.Equal(t, "githubactions:S7637", exclusions[0].RuleKey)
		assert.Equal(t, "**/*.y*ml", exclusions[0].ResourceKey)
		require.Len(t, *requests, 1)
		assert.Equal(t, "rios0rios0_autobump", (*requests)[0].Query.Get("component"))
		assert.Equal(t, entities.SonarIssueExclusionsSettingKey, (*requests)[0].Query.Get("keys"))
		assert.Equal(t, "Bearer test-token", (*requests)[0].Auth)
	})

	t.Run("should return an empty set when the project has never had one", func(t *testing.T) {
		t.Parallel()
		// given — SonarQube omits the setting entirely, which is a normal
		// state and not an error.
		repository, _ := newSonarServer(t, map[string]string{"/api/settings/values": `{"settings":[]}`})

		// when
		exclusions, err := repository.FindIssueExclusionsByProjectKey(context.Background(), "rios0rios0_autobump")

		// then
		require.NoError(t, err)
		assert.Empty(t, exclusions)
	})

	t.Run("should post one JSON fieldValues parameter per pair", func(t *testing.T) {
		t.Parallel()
		// given — the property set is a multi-value field, so each pair
		// travels as its own JSON-encoded `fieldValues` parameter.
		repository, requests := newSonarServer(t, map[string]string{"/api/settings/set": `{}`})
		exclusions := append(entities.DesiredSonarIssueExclusions(),
			entities.SonarIssueExclusion{RuleKey: "go:S1192", ResourceKey: "**/*_test.go"})

		// when
		err := repository.UpdateIssueExclusions(context.Background(), "rios0rios0_autobump", exclusions)

		// then
		require.NoError(t, err)
		require.Len(t, *requests, 1)
		sent := (*requests)[0]
		assert.Equal(t, http.MethodPost, sent.Method)
		assert.Equal(t, "rios0rios0_autobump", sent.Form.Get("component"))
		assert.Equal(t, entities.SonarIssueExclusionsSettingKey, sent.Form.Get("key"))

		values := sent.Form["fieldValues"]
		require.Len(t, values, 2)
		var first map[string]string
		require.NoError(t, json.Unmarshal([]byte(values[0]), &first))
		assert.Equal(t, "githubactions:S7637", first["ruleKey"])
		assert.Equal(t, "**/*.y*ml", first["resourceKey"])
	})
}

func TestSonarCloudSonarProjectsRepositoryIssues(t *testing.T) {
	t.Parallel()

	t.Run("should search unresolved issues restricted to the given rules", func(t *testing.T) {
		t.Parallel()
		// given
		repository, requests := newSonarServer(t, map[string]string{
			"/api/issues/search": `{"paging":{"pageIndex":1,"pageSize":500,"total":1},
				"issues":[{"key":"i1","rule":"githubactions:S7637",
				"component":"rios0rios0_autobump:.github/workflows/claude-mention.yaml","line":15}]}`,
		})

		// when
		issues, err := repository.FindOpenIssuesByRules(
			context.Background(),
			"rios0rios0_autobump",
			[]string{"githubactions:S7637"},
		)

		// then
		require.NoError(t, err)
		require.Len(t, issues, 1)
		assert.Equal(t, "i1", issues[0].Key)
		assert.Equal(t, 15, issues[0].Line)
		sent := (*requests)[0]
		assert.Equal(t, "false", sent.Query.Get("resolved"))
		assert.Equal(t, "githubactions:S7637", sent.Query.Get("rules"))
		assert.Equal(t, "rios0rios0_autobump", sent.Query.Get("componentKeys"))
	})

	t.Run("should transition the issues with do_transition=accept and a comment", func(t *testing.T) {
		t.Parallel()
		// given
		repository, requests := newSonarServer(t, map[string]string{"/api/issues/bulk_change": `{"total":2}`})

		// when
		err := repository.AcceptIssues(context.Background(), []string{"i1", "i2"}, entities.DesiredSonarTriageComment)

		// then
		require.NoError(t, err)
		require.Len(t, *requests, 1)
		sent := (*requests)[0]
		assert.Equal(t, http.MethodPost, sent.Method)
		assert.Equal(t, "i1,i2", sent.Form.Get("issues"))
		assert.Equal(t, "accept", sent.Form.Get("do_transition"))
		assert.Equal(t, entities.DesiredSonarTriageComment, sent.Form.Get("comment"))
	})

	t.Run("should send nothing when no rule is given", func(t *testing.T) {
		t.Parallel()
		// given — an empty rule list would otherwise search for every rule
		// and triage findings the policy says nothing about.
		repository, requests := newSonarServer(t, map[string]string{})

		// when
		issues, err := repository.FindOpenIssuesByRules(context.Background(), "rios0rios0_autobump", nil)

		// then
		require.NoError(t, err)
		assert.Empty(t, issues)
		assert.Empty(t, *requests)
	})
}

func TestSonarCloudSonarProjectsRepositoryHotspots(t *testing.T) {
	t.Parallel()

	t.Run("should keep only the hotspots of the given rules", func(t *testing.T) {
		t.Parallel()
		// given — api/hotspots/search takes no rule parameter, and the
		// other rules here are real findings that must stay TO_REVIEW.
		repository, requests := newSonarServer(t, map[string]string{
			"/api/hotspots/search": `{"paging":{"pageIndex":1,"pageSize":500,"total":3},
				"hotspots":[{"key":"h1","ruleKey":"githubactions:S7637","component":"c","line":9},
				{"key":"h2","ruleKey":"docker:S6471","component":"c","line":1},
				{"key":"h3","ruleKey":"terraform:S6378","component":"c","line":2}]}`,
		})

		// when
		hotspots, err := repository.FindHotspotsToReviewByRules(
			context.Background(),
			"rios0rios0_iac-modules",
			[]string{"githubactions:S7637"},
		)

		// then
		require.NoError(t, err)
		require.Len(t, hotspots, 1)
		assert.Equal(t, "h1", hotspots[0].Key)
		assert.Equal(t, "TO_REVIEW", (*requests)[0].Query.Get("status"))
	})

	t.Run("should keep paging when a whole page belongs to other rules", func(t *testing.T) {
		t.Parallel()
		// given — paging counts what the endpoint returned, not what
		// survived the filter; counting the filtered set would end the
		// walk on the first page of unrelated hotspots.
		page := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			page++
			rule := "docker:S6471"
			if page == 2 {
				rule = "githubactions:S7637"
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"paging":{"pageIndex":%d,"pageSize":1,"total":2},
				"hotspots":[{"key":"h%d","ruleKey":%q,"component":"c","line":1}]}`, page, page, rule)
		}))
		t.Cleanup(server.Close)
		repository := infra.NewSonarCloudSonarProjectsRepository(infra.SonarConfig{Host: server.URL})

		// when
		hotspots, err := repository.FindHotspotsToReviewByRules(
			context.Background(),
			"rios0rios0_boss",
			[]string{"githubactions:S7637"},
		)

		// then
		require.NoError(t, err)
		require.Len(t, hotspots, 1)
		assert.Equal(t, "h2", hotspots[0].Key)
	})

	t.Run("should mark a hotspot REVIEWED and SAFE with a comment", func(t *testing.T) {
		t.Parallel()
		// given
		repository, requests := newSonarServer(t, map[string]string{"/api/hotspots/change_status": `{}`})

		// when
		err := repository.MarkHotspotReviewedSafe(context.Background(), "h1", entities.DesiredSonarTriageComment)

		// then
		require.NoError(t, err)
		require.Len(t, *requests, 1)
		sent := (*requests)[0]
		assert.Equal(t, http.MethodPost, sent.Method)
		assert.Equal(t, "h1", sent.Form.Get("hotspot"))
		assert.Equal(t, "REVIEWED", sent.Form.Get("status"))
		assert.Equal(t, "SAFE", sent.Form.Get("resolution"))
		assert.Equal(t, entities.DesiredSonarTriageComment, sent.Form.Get("comment"))
	})
}

func TestSonarCloudSonarProjectsRepositoryAuthorization(t *testing.T) {
	t.Parallel()

	t.Run("should omit the Authorization header when no token is configured", func(t *testing.T) {
		t.Parallel()
		// given — the read endpoints answer anonymously for public
		// projects, which is what makes --sonar-policy --dry-run usable
		// without a credential.
		var auth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"settings":[]}`))
		}))
		t.Cleanup(server.Close)
		repository := infra.NewSonarCloudSonarProjectsRepository(infra.SonarConfig{Host: server.URL})

		// when
		_, err := repository.FindIssueExclusionsByProjectKey(context.Background(), "rios0rios0_autobump")

		// then
		require.NoError(t, err)
		assert.Empty(t, auth)
	})
}
