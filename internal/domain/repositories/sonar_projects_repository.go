package repositories

import (
	"context"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

// SonarProjectsRepository is the port for the SonarQube Cloud side of
// the fleet policy: enumerating an organization's projects, reading and
// writing their issue-exclusion setting, and triaging the findings the
// exclusion is meant to silence.
//
// The three write operations sit behind three *different* SonarQube
// Cloud project permissions — `Administer` for the setting,
// `Administer Issues` for the transition, `Administer Security Hotspots`
// for the hotspot status — so a caller must not infer from one failing
// that the others would.
type SonarProjectsRepository interface {
	// FindAllByOrganization lists every project the token can see in the
	// organization (GET api/components/search_projects), paging until the
	// listing is exhausted.
	FindAllByOrganization(ctx context.Context, organization string) ([]entities.SonarProject, error)

	// FindIssueExclusionsByProjectKey reads the project's current
	// `sonar.issue.ignore.multicriteria` property set
	// (GET api/settings/values). A project that has never had one returns
	// an empty slice and no error.
	FindIssueExclusionsByProjectKey(ctx context.Context, projectKey string) ([]entities.SonarIssueExclusion, error)

	// UpdateIssueExclusions writes the property set
	// (POST api/settings/set). SonarQube replaces the set wholesale, so
	// callers pass the full merged list, never just the additions.
	UpdateIssueExclusions(ctx context.Context, projectKey string, exclusions []entities.SonarIssueExclusion) error

	// FindOpenIssuesByRules returns the project's unresolved issues for
	// the given rules (GET api/issues/search with resolved=false).
	FindOpenIssuesByRules(ctx context.Context, projectKey string, ruleKeys []string) ([]entities.SonarIssue, error)

	// AcceptIssues transitions the issues to accepted with a comment
	// (POST api/issues/bulk_change, do_transition=accept). An accepted
	// issue stops counting toward `new_security_rating`.
	AcceptIssues(ctx context.Context, issueKeys []string, comment string) error

	// FindHotspotsToReviewByRules returns the project's TO_REVIEW
	// hotspots for the given rules (GET api/hotspots/search). The search
	// endpoint cannot filter by rule, so the adapter filters client-side.
	FindHotspotsToReviewByRules(
		ctx context.Context,
		projectKey string,
		ruleKeys []string,
	) ([]entities.SonarHotspot, error)

	// MarkHotspotReviewedSafe sets one hotspot to REVIEWED/SAFE with a
	// comment (POST api/hotspots/change_status). There is no bulk form,
	// so callers loop.
	MarkHotspotReviewedSafe(ctx context.Context, hotspotKey, comment string) error
}
