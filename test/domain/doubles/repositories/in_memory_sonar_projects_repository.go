package repositories

import (
	"context"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// InMemorySonarProjectsRepository is the in-memory double for the
// SonarQube Cloud port. It keeps the exclusions, issues, and hotspots it
// was seeded with and applies the same state transitions the real API
// would, so a test can assert on the resulting state rather than on the
// call sequence.
type InMemorySonarProjectsRepository struct {
	Projects           []entities.SonarProject
	ExclusionsByKey    map[string][]entities.SonarIssueExclusion
	OpenIssuesByKey    map[string][]entities.SonarIssue
	HotspotsByKey      map[string][]entities.SonarHotspot
	AcceptedIssueKeys  []string
	ReviewedHotspotIDs []string
	Comments           []string

	ErrorOnFindProjects   error
	ErrorOnFindExclusions error
	ErrorOnUpdate         error
	ErrorOnFindIssues     error
	ErrorOnAccept         error
	// AcceptedOnError is how many issues the double reports as accepted
	// alongside ErrorOnAccept, so tests can cover the partial-tally case
	// api/issues/bulk_change reports in its 200 body.
	AcceptedOnError     int
	ErrorOnFindHotspots error
	ErrorOnReview       error
}

// NewInMemorySonarProjectsRepository builds the double.
func NewInMemorySonarProjectsRepository() *InMemorySonarProjectsRepository {
	return &InMemorySonarProjectsRepository{
		ExclusionsByKey: map[string][]entities.SonarIssueExclusion{},
		OpenIssuesByKey: map[string][]entities.SonarIssue{},
		HotspotsByKey:   map[string][]entities.SonarHotspot{},
	}
}

// WithProject seeds one project into the organization listing.
func (r *InMemorySonarProjectsRepository) WithProject(
	project entities.SonarProject,
) *InMemorySonarProjectsRepository {
	r.Projects = append(r.Projects, project)
	return r
}

// WithExclusions seeds a project's existing property set.
func (r *InMemorySonarProjectsRepository) WithExclusions(
	projectKey string,
	exclusions ...entities.SonarIssueExclusion,
) *InMemorySonarProjectsRepository {
	r.ExclusionsByKey[projectKey] = exclusions
	return r
}

// WithOpenIssues seeds a project's unresolved issues.
func (r *InMemorySonarProjectsRepository) WithOpenIssues(
	projectKey string,
	issues ...entities.SonarIssue,
) *InMemorySonarProjectsRepository {
	r.OpenIssuesByKey[projectKey] = issues
	return r
}

// WithHotspots seeds a project's TO_REVIEW hotspots.
func (r *InMemorySonarProjectsRepository) WithHotspots(
	projectKey string,
	hotspots ...entities.SonarHotspot,
) *InMemorySonarProjectsRepository {
	r.HotspotsByKey[projectKey] = hotspots
	return r
}

func (r *InMemorySonarProjectsRepository) FindAllByOrganization(
	_ context.Context,
	organization string,
) ([]entities.SonarProject, error) {
	if r.ErrorOnFindProjects != nil {
		return nil, r.ErrorOnFindProjects
	}

	matching := make([]entities.SonarProject, 0, len(r.Projects))
	for _, project := range r.Projects {
		if project.Organization == "" || project.Organization == organization {
			matching = append(matching, project)
		}
	}
	return matching, nil
}

func (r *InMemorySonarProjectsRepository) FindIssueExclusionsByProjectKey(
	_ context.Context,
	projectKey string,
) ([]entities.SonarIssueExclusion, error) {
	if r.ErrorOnFindExclusions != nil {
		return nil, r.ErrorOnFindExclusions
	}
	return r.ExclusionsByKey[projectKey], nil
}

func (r *InMemorySonarProjectsRepository) UpdateIssueExclusions(
	_ context.Context,
	projectKey string,
	exclusions []entities.SonarIssueExclusion,
) error {
	if r.ErrorOnUpdate != nil {
		return r.ErrorOnUpdate
	}
	r.ExclusionsByKey[projectKey] = exclusions
	return nil
}

func (r *InMemorySonarProjectsRepository) FindOpenIssuesByRules(
	_ context.Context,
	projectKey string,
	ruleKeys []string,
) ([]entities.SonarIssue, error) {
	if r.ErrorOnFindIssues != nil {
		return nil, r.ErrorOnFindIssues
	}
	return filterByRule(r.OpenIssuesByKey[projectKey], ruleKeys, func(i entities.SonarIssue) string {
		return i.RuleKey
	}), nil
}

func (r *InMemorySonarProjectsRepository) AcceptIssues(
	_ context.Context,
	issueKeys []string,
	comment string,
) (int, error) {
	if r.ErrorOnAccept != nil {
		return r.AcceptedOnError, r.ErrorOnAccept
	}
	r.AcceptedIssueKeys = append(r.AcceptedIssueKeys, issueKeys...)
	r.Comments = append(r.Comments, comment)

	for projectKey, issues := range r.OpenIssuesByKey {
		r.OpenIssuesByKey[projectKey] = removeIssues(issues, issueKeys)
	}
	return len(issueKeys), nil
}

func (r *InMemorySonarProjectsRepository) FindHotspotsToReviewByRules(
	_ context.Context,
	projectKey string,
	ruleKeys []string,
) ([]entities.SonarHotspot, error) {
	if r.ErrorOnFindHotspots != nil {
		return nil, r.ErrorOnFindHotspots
	}
	return filterByRule(r.HotspotsByKey[projectKey], ruleKeys, func(h entities.SonarHotspot) string {
		return h.RuleKey
	}), nil
}

func (r *InMemorySonarProjectsRepository) MarkHotspotReviewedSafe(
	_ context.Context,
	hotspotKey, comment string,
) error {
	if r.ErrorOnReview != nil {
		return r.ErrorOnReview
	}
	r.ReviewedHotspotIDs = append(r.ReviewedHotspotIDs, hotspotKey)
	r.Comments = append(r.Comments, comment)

	for projectKey, hotspots := range r.HotspotsByKey {
		remaining := make([]entities.SonarHotspot, 0, len(hotspots))
		for _, hotspot := range hotspots {
			if hotspot.Key != hotspotKey {
				remaining = append(remaining, hotspot)
			}
		}
		r.HotspotsByKey[projectKey] = remaining
	}
	return nil
}

// filterByRule mirrors the adapter's client-side rule filter so the
// double answers the same subset the real API would.
func filterByRule[T any](all []T, ruleKeys []string, ruleOf func(T) string) []T {
	wanted := make(map[string]struct{}, len(ruleKeys))
	for _, ruleKey := range ruleKeys {
		wanted[ruleKey] = struct{}{}
	}

	matching := make([]T, 0, len(all))
	for _, item := range all {
		if _, match := wanted[ruleOf(item)]; match {
			matching = append(matching, item)
		}
	}
	return matching
}

func removeIssues(issues []entities.SonarIssue, removedKeys []string) []entities.SonarIssue {
	removed := make(map[string]struct{}, len(removedKeys))
	for _, key := range removedKeys {
		removed[key] = struct{}{}
	}

	remaining := make([]entities.SonarIssue, 0, len(issues))
	for _, issue := range issues {
		if _, gone := removed[issue.Key]; !gone {
			remaining = append(remaining, issue)
		}
	}
	return remaining
}

// Ensure interface compliance.
var _ repositories.SonarProjectsRepository = (*InMemorySonarProjectsRepository)(nil)
