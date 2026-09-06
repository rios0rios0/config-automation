package entities

// DesiredSonarOrganization is the SonarQube Cloud organization the
// policy is applied to when SONAR_ORGANIZATION is unset. Only
// `rios0rios0` has a SonarQube Cloud organization today; `medhub-life`
// and `prefy` have none, so — unlike the GitHub phases — this mode does
// not fan out per owner.
const DesiredSonarOrganization = "rios0rios0"

// DesiredSonarHost is the SonarQube Cloud base URL used when
// SONAR_HOST_URL is unset.
const DesiredSonarHost = "https://sonarcloud.io"

// SonarIssueExclusionsSettingKey is the property set that carries the
// per-project issue exclusions. It is a server-side setting because
// SonarQube Cloud's **automatic analysis** reads only a fixed list of
// keys from `.sonarcloud.properties` — `sonar.sources`,
// `sonar.exclusions`, `sonar.inclusions`, `sonar.tests`,
// `sonar.test.exclusions`, `sonar.test.inclusions`,
// `sonar.sourceEncoding`, `sonar.cpd.exclusions`, `sonar.python.version`
// and `sonar.cfamily.reportingCppStandardOverride` — and no issue
// exclusion key among them. The setting therefore cannot live in the
// analyzed repository; it has to be written to each project.
//
// See https://docs.sonarsource.com/sonarqube-cloud/analyzing-source-code/automatic-analysis
const SonarIssueExclusionsSettingKey = "sonar.issue.ignore.multicriteria"

// DesiredSonarIssueExclusions is the fleet-wide analysis policy: rule
// `githubactions:S7637` ("Use full commit SHA hash for this dependency")
// raises nothing on YAML.
//
// Every repository here calls the shared CI as
// `uses: rios0rios0/pipelines/.github/workflows/<name>.yaml@main`, and
// that floating reference is deliberate — `pipelines` is the single
// source of truth, and pinning those ~216 references to a commit SHA
// would break the centralised update model outright. S7637 cannot tell a
// first-party reference from a third-party one, so on this fleet it is
// pure noise that costs every project a `new_security_rating` of C and a
// `new_security_hotspots_reviewed` of 0%.
//
// The pattern covers `**/*.y*ml` rather than `.github/workflows/**`
// because composite actions (`action.yaml`, at any depth) are analysed
// by the same `githubactions` language and carry the same first-party
// references. Scoping by rule key is what keeps this narrow: every other
// GitHub Actions rule — `S7634` (only pass required secrets), `S7630`
// (script injection) — still fires on the very same files, which an
// `sonar.exclusions` entry would have silenced too.
//
// Third-party actions are still pinned: the policy is enforced by the
// `pipelines` scheduled dependency job, which fails when any pin goes
// stale, not by S7637.
//
// Returns a fresh slice each call so the policy stays immutable at call
// sites, matching DesiredRepoSettings() and DesiredAllowedMergeMethods().
func DesiredSonarIssueExclusions() []SonarIssueExclusion {
	return []SonarIssueExclusion{
		{RuleKey: "githubactions:S7637", ResourceKey: "**/*.y*ml"},
	}
}

// DesiredSonarTriagedRuleKeys lists the rules whose already-raised
// findings are triaged away. It is derived from
// DesiredSonarIssueExclusions() so the two can never drift: the
// exclusion stops the rule from raising anything on the *next* analysis,
// while the triage clears what the *previous* analyses already recorded.
// Without the second half a project stays red until it is analysed
// again, which under automatic analysis means until someone pushes.
func DesiredSonarTriagedRuleKeys() []string {
	exclusions := DesiredSonarIssueExclusions()
	keys := make([]string, 0, len(exclusions))
	seen := make(map[string]struct{}, len(exclusions))
	for _, exclusion := range exclusions {
		if _, duplicate := seen[exclusion.RuleKey]; duplicate {
			continue
		}
		seen[exclusion.RuleKey] = struct{}{}
		keys = append(keys, exclusion.RuleKey)
	}
	return keys
}

// DesiredSonarTriageComment is recorded on every issue accepted and
// every hotspot marked safe, so the resolution carries its reason in
// SonarQube Cloud instead of only in this repository.
const DesiredSonarTriageComment = "Accepted by config-automation: references to rios0rios0 repositories " +
	"float on @main by design (pipelines is the single source of truth); third-party actions are SHA-pinned " +
	"and their pins are gated by the pipelines dependency job."
