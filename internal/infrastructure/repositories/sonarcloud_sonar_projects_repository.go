package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// sonarPageSize is the page size used for every paginated search. 500 is
// the maximum SonarQube Cloud accepts on api/issues/search; using the
// same value everywhere keeps one round trip per project in practice.
const sonarPageSize = 500

// sonarBulkChangeLimit is the maximum number of issue keys SonarQube
// Cloud accepts in a single api/issues/bulk_change call.
const sonarBulkChangeLimit = 500

// sonarErrorBodyLimit caps how much of a failed response is read back
// into the error message. SonarQube's own explanation of a denial lives
// in the first `errors[].msg`, so a few kilobytes is always enough.
const sonarErrorBodyLimit = 4096

// SonarCloudSonarProjectsRepository implements
// repositories.SonarProjectsRepository against the SonarQube Cloud Web
// API. It talks plain net/http rather than pulling in a client library:
// the six endpoints it needs are stable, unauthenticated-readable for
// public projects, and return small payloads.
type SonarCloudSonarProjectsRepository struct {
	client *http.Client
	host   string
	token  string
}

// NewSonarCloudSonarProjectsRepository is the Dig-injectable
// constructor. The host carries no trailing slash by the time it gets
// here (newSonarConfig trims it).
func NewSonarCloudSonarProjectsRepository(config SonarConfig) *SonarCloudSonarProjectsRepository {
	return &SonarCloudSonarProjectsRepository{
		client: http.DefaultClient,
		host:   config.Host,
		token:  config.Token,
	}
}

var _ repositories.SonarProjectsRepository = (*SonarCloudSonarProjectsRepository)(nil)

// FindAllByOrganization pages through api/components/search_projects.
//
// That endpoint is used rather than api/projects/search because the
// latter requires organization administration rights, while this one
// answers for any token that can see the projects — and the enumeration
// must not need more permission than the mutations do.
func (r SonarCloudSonarProjectsRepository) FindAllByOrganization(
	ctx context.Context,
	organization string,
) ([]entities.SonarProject, error) {
	var projects []entities.SonarProject

	for page := 1; ; page++ {
		var payload struct {
			Paging     sonarPaging `json:"paging"`
			Components []struct {
				Key          string `json:"key"`
				Name         string `json:"name"`
				Organization string `json:"organization"`
			} `json:"components"`
		}

		query := url.Values{}
		query.Set("organization", organization)
		query.Set("ps", strconv.Itoa(sonarPageSize))
		query.Set("p", strconv.Itoa(page))
		if err := r.get(ctx, "api/components/search_projects", query, &payload); err != nil {
			return nil, err
		}

		for _, component := range payload.Components {
			projects = append(projects, entities.SonarProject{
				Key:          component.Key,
				Name:         component.Name,
				Organization: component.Organization,
			})
		}

		if !payload.Paging.hasMore(len(projects)) {
			return projects, nil
		}
	}
}

// FindIssueExclusionsByProjectKey reads the property set from
// api/settings/values. SonarQube omits the setting entirely when a
// project has never had one, which is a normal state and not an error.
func (r SonarCloudSonarProjectsRepository) FindIssueExclusionsByProjectKey(
	ctx context.Context,
	projectKey string,
) ([]entities.SonarIssueExclusion, error) {
	var payload struct {
		Settings []struct {
			Key         string              `json:"key"`
			FieldValues []map[string]string `json:"fieldValues"`
		} `json:"settings"`
	}

	query := url.Values{}
	query.Set("component", projectKey)
	query.Set("keys", entities.SonarIssueExclusionsSettingKey)
	if err := r.get(ctx, "api/settings/values", query, &payload); err != nil {
		return nil, err
	}

	exclusions := make([]entities.SonarIssueExclusion, 0)
	for _, setting := range payload.Settings {
		if setting.Key != entities.SonarIssueExclusionsSettingKey {
			continue
		}
		for _, field := range setting.FieldValues {
			exclusions = append(exclusions, entities.SonarIssueExclusion{
				RuleKey:     field["ruleKey"],
				ResourceKey: field["resourceKey"],
			})
		}
	}
	return exclusions, nil
}

// UpdateIssueExclusions writes the whole property set back with
// api/settings/set, one `fieldValues` parameter per pair. Requires the
// `Administer` permission on the project.
func (r SonarCloudSonarProjectsRepository) UpdateIssueExclusions(
	ctx context.Context,
	projectKey string,
	exclusions []entities.SonarIssueExclusion,
) error {
	form := url.Values{}
	form.Set("component", projectKey)
	form.Set("key", entities.SonarIssueExclusionsSettingKey)
	for _, exclusion := range exclusions {
		encoded, err := json.Marshal(map[string]string{
			"ruleKey":     exclusion.RuleKey,
			"resourceKey": exclusion.ResourceKey,
		})
		if err != nil {
			return fmt.Errorf("encoding exclusion %s: %w", exclusion.RuleKey, err)
		}
		form.Add("fieldValues", string(encoded))
	}
	return r.post(ctx, "api/settings/set", form, nil)
}

// FindOpenIssuesByRules pages through api/issues/search restricted to
// the given rules and to unresolved issues.
func (r SonarCloudSonarProjectsRepository) FindOpenIssuesByRules(
	ctx context.Context,
	projectKey string,
	ruleKeys []string,
) ([]entities.SonarIssue, error) {
	if len(ruleKeys) == 0 {
		return nil, nil
	}

	var issues []entities.SonarIssue

	for page := 1; ; page++ {
		var payload struct {
			Paging sonarPaging `json:"paging"`
			Issues []struct {
				Key       string `json:"key"`
				Rule      string `json:"rule"`
				Component string `json:"component"`
				Line      int    `json:"line"`
			} `json:"issues"`
		}

		query := url.Values{}
		query.Set("componentKeys", projectKey)
		query.Set("rules", strings.Join(ruleKeys, ","))
		query.Set("resolved", "false")
		query.Set("ps", strconv.Itoa(sonarPageSize))
		query.Set("p", strconv.Itoa(page))
		if err := r.get(ctx, "api/issues/search", query, &payload); err != nil {
			return nil, err
		}

		for _, issue := range payload.Issues {
			issues = append(issues, entities.SonarIssue{
				Key:       issue.Key,
				RuleKey:   issue.Rule,
				Component: issue.Component,
				Line:      issue.Line,
			})
		}

		if !payload.Paging.hasMore(len(issues)) {
			return issues, nil
		}
	}
}

// AcceptIssues transitions issues to accepted via api/issues/bulk_change
// in chunks of sonarBulkChangeLimit, returning how many the server
// actually moved. Requires `Administer Issues` on the project.
//
// **This endpoint does not report a refused transition through the
// status line.** It answers 200 with a per-issue tally, and an issue the
// caller may not transition — the token's user is authenticated but
// lacks `Administer Issues` there — is counted in `ignored`, not raised
// as an error. Treating any 2xx as success would let the daily run log
// "26 issues accepted", exit 0, and leave all 26 open with the gate
// still red, which is the exact silent pass the sibling write paths
// avoid only because they do return 403.
func (r SonarCloudSonarProjectsRepository) AcceptIssues(
	ctx context.Context,
	issueKeys []string,
	comment string,
) (int, error) {
	accepted := 0

	for start := 0; start < len(issueKeys); start += sonarBulkChangeLimit {
		end := min(start+sonarBulkChangeLimit, len(issueKeys))

		var tally struct {
			Total    int `json:"total"`
			Success  int `json:"success"`
			Ignored  int `json:"ignored"`
			Failures int `json:"failures"`
		}

		form := url.Values{}
		form.Set("issues", strings.Join(issueKeys[start:end], ","))
		form.Set("do_transition", "accept")
		form.Set("comment", comment)
		if err := r.post(ctx, "api/issues/bulk_change", form, &tally); err != nil {
			return accepted, err
		}

		accepted += tally.Success
		if tally.Ignored > 0 || tally.Failures > 0 {
			return accepted, fmt.Errorf(
				"api/issues/bulk_change accepted %d of %d issues (%d ignored, %d failed): "+
					"the token's user is most likely missing `Administer Issues` on the project",
				tally.Success, tally.Total, tally.Ignored, tally.Failures,
			)
		}
	}
	return accepted, nil
}

// FindHotspotsToReviewByRules pages through api/hotspots/search for
// TO_REVIEW hotspots and filters by rule client-side — the endpoint
// accepts no rule parameter.
func (r SonarCloudSonarProjectsRepository) FindHotspotsToReviewByRules(
	ctx context.Context,
	projectKey string,
	ruleKeys []string,
) ([]entities.SonarHotspot, error) {
	if len(ruleKeys) == 0 {
		return nil, nil
	}

	wanted := make(map[string]struct{}, len(ruleKeys))
	for _, ruleKey := range ruleKeys {
		wanted[ruleKey] = struct{}{}
	}

	var hotspots []entities.SonarHotspot
	seen := 0

	for page := 1; ; page++ {
		var payload struct {
			Paging   sonarPaging `json:"paging"`
			Hotspots []struct {
				Key       string `json:"key"`
				RuleKey   string `json:"ruleKey"`
				Component string `json:"component"`
				Line      int    `json:"line"`
			} `json:"hotspots"`
		}

		query := url.Values{}
		query.Set("projectKey", projectKey)
		query.Set("status", "TO_REVIEW")
		query.Set("ps", strconv.Itoa(sonarPageSize))
		query.Set("p", strconv.Itoa(page))
		if err := r.get(ctx, "api/hotspots/search", query, &payload); err != nil {
			return nil, err
		}

		seen += len(payload.Hotspots)
		for _, hotspot := range payload.Hotspots {
			if _, match := wanted[hotspot.RuleKey]; !match {
				continue
			}
			hotspots = append(hotspots, entities.SonarHotspot{
				Key:       hotspot.Key,
				RuleKey:   hotspot.RuleKey,
				Component: hotspot.Component,
				Line:      hotspot.Line,
			})
		}

		// Paging is counted over what the endpoint returned, not over
		// what survived the rule filter — otherwise a page whose hotspots
		// all belong to other rules would end the walk early.
		if !payload.Paging.hasMore(seen) {
			return hotspots, nil
		}
	}
}

// MarkHotspotReviewedSafe sets one hotspot to REVIEWED/SAFE. Requires
// `Administer Security Hotspots` on the project.
func (r SonarCloudSonarProjectsRepository) MarkHotspotReviewedSafe(
	ctx context.Context,
	hotspotKey, comment string,
) error {
	form := url.Values{}
	form.Set("hotspot", hotspotKey)
	form.Set("status", "REVIEWED")
	form.Set("resolution", "SAFE")
	form.Set("comment", comment)
	return r.post(ctx, "api/hotspots/change_status", form, nil)
}

// sonarPaging is the paging block every SonarQube search returns.
type sonarPaging struct {
	PageIndex int `json:"pageIndex"`
	PageSize  int `json:"pageSize"`
	Total     int `json:"total"`
}

// hasMore reports whether another page is worth fetching. It guards on
// PageSize as well as on the running count: a zero-size page would
// otherwise never advance and the caller would loop forever.
func (p sonarPaging) hasMore(collected int) bool {
	return p.PageSize > 0 && collected < p.Total
}

func (r SonarCloudSonarProjectsRepository) get(
	ctx context.Context,
	path string,
	query url.Values,
	out any,
) error {
	endpoint := r.host + "/" + path + "?" + query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("building %s request: %w", path, err)
	}
	r.authorize(request)

	response, err := r.client.Do(request)
	if err != nil {
		return fmt.Errorf("calling %s: %w", path, err)
	}
	defer func() { _ = response.Body.Close() }()

	if err = sonarStatusError(path, response); err != nil {
		return err
	}
	if err = json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding %s response: %w", path, err)
	}
	return nil
}

// post sends a form-encoded POST. When out is non-nil the response body
// is decoded into it, which is how AcceptIssues reads the only feedback
// api/issues/bulk_change gives; the endpoints that answer 204 pass nil.
func (r SonarCloudSonarProjectsRepository) post(
	ctx context.Context,
	path string,
	form url.Values,
	out any,
) error {
	endpoint := r.host + "/" + path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("building %s request: %w", path, err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.authorize(request)

	response, err := r.client.Do(request)
	if err != nil {
		return fmt.Errorf("calling %s: %w", path, err)
	}
	defer func() { _ = response.Body.Close() }()

	if err = sonarStatusError(path, response); err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err = json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding %s response: %w", path, err)
	}
	return nil
}

// authorize adds the bearer token. It is skipped when no token is
// configured so the read-only endpoints still answer for public
// projects, which is what makes `--dry-run` usable without a credential.
func (r SonarCloudSonarProjectsRepository) authorize(request *http.Request) {
	if r.token == "" {
		return
	}
	request.Header.Set("Authorization", "Bearer "+r.token)
}

// sonarStatusError turns a non-2xx response into an error carrying the
// API's own `errors[].msg` text, which is where SonarQube explains a
// denial ("Insufficient privileges") rather than in the status line.
func sonarStatusError(path string, response *http.Response) error {
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(response.Body, sonarErrorBodyLimit))

	var payload struct {
		Errors []struct {
			Msg string `json:"msg"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &payload) == nil && len(payload.Errors) > 0 {
		messages := make([]string, 0, len(payload.Errors))
		for _, apiError := range payload.Errors {
			messages = append(messages, apiError.Msg)
		}
		return fmt.Errorf("%s returned %d: %s", path, response.StatusCode, strings.Join(messages, "; "))
	}
	return fmt.Errorf("%s returned %d: %s", path, response.StatusCode, strings.TrimSpace(string(body)))
}
