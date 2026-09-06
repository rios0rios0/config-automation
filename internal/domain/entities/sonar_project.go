package entities

import "slices"

// SonarProject is one project in a SonarQube Cloud organization. The
// project key is *not* derivable from the repository name: a repo that
// was renamed keeps the key it was onboarded with (`dev-toolkit` is
// still `rios0rios0_versainit`), which is why the fleet is enumerated
// from SonarQube Cloud rather than composed from the GitHub listing.
type SonarProject struct {
	Key          string
	Name         string
	Organization string
}

// SonarIssueExclusion is one `ruleKey` / `resourceKey` pair of the
// `sonar.issue.ignore.multicriteria` property set: rule RuleKey raises
// nothing on files matching ResourceKey. Every other rule keeps firing
// on those files, which is the whole point of using an issue exclusion
// instead of `sonar.exclusions`.
type SonarIssueExclusion struct {
	RuleKey     string
	ResourceKey string
}

// SonarIssue is one unresolved issue returned by the issue search,
// reduced to what the triage step needs.
type SonarIssue struct {
	Key       string
	RuleKey   string
	Component string
	Line      int
}

// SonarHotspot is one security hotspot still awaiting review. Hotspots
// are a separate resource from issues in SonarQube Cloud — a different
// search endpoint, a different status transition, and a different
// project permission — even when they come from the same rule.
type SonarHotspot struct {
	Key       string
	RuleKey   string
	Component string
	Line      int
}

// ContainsSonarIssueExclusion reports whether the pair is already part
// of the set. Comparison is exact on both fields: SonarQube matches
// `resourceKey` as a path pattern, so two patterns that happen to select
// the same files today are still different entries.
func ContainsSonarIssueExclusion(existing []SonarIssueExclusion, wanted SonarIssueExclusion) bool {
	return slices.Contains(existing, wanted)
}

// MergeSonarIssueExclusions returns the union of the exclusions already
// configured on a project and the ones the policy wants, preserving the
// existing order and appending only what is missing.
//
// The union — rather than the policy list alone — is what makes the
// apply idempotent *and* non-destructive: `POST api/settings/set`
// replaces a property set wholesale, so writing only the policy pairs
// would silently drop any exclusion configured by hand in the SonarQube
// Cloud UI. The second return value reports whether anything was added,
// so a project that already carries the policy is left untouched instead
// of being rewritten (and re-logged) on every run.
func MergeSonarIssueExclusions(
	existing []SonarIssueExclusion,
	desired []SonarIssueExclusion,
) ([]SonarIssueExclusion, bool) {
	merged := make([]SonarIssueExclusion, 0, len(existing)+len(desired))
	merged = append(merged, existing...)

	added := false
	for _, wanted := range desired {
		if ContainsSonarIssueExclusion(merged, wanted) {
			continue
		}
		merged = append(merged, wanted)
		added = true
	}
	return merged, added
}
