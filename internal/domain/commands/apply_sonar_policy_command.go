package commands

import (
	"context"
	"fmt"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// Stable action tags reported through ApplySonarPolicyChange.Action.
const (
	SonarActionIssueExclusions = "issue_exclusions"
	SonarActionIssuesAccepted  = "issues_accepted"
	SonarActionHotspotsSafe    = "hotspots_reviewed"
)

// ApplySonarPolicyCommand enforces the SonarQube Cloud half of the fleet
// policy on every project of an organization: it adds the
// `sonar.issue.ignore.multicriteria` pairs from
// entities.DesiredSonarIssueExclusions() and triages the findings those
// rules already raised.
//
// Both halves are needed and neither replaces the other. The exclusion
// only takes effect on the *next* analysis, and under automatic analysis
// that means the next push — so on its own it leaves every quality gate
// red in the meantime. The triage clears what is already recorded and
// makes the gate recompute now, but says nothing about the next
// occurrence — a new workflow file would raise the finding again.
type ApplySonarPolicyCommand struct {
	sonarRepo repositories.SonarProjectsRepository
}

// NewApplySonarPolicyCommand is the Dig-injectable constructor.
func NewApplySonarPolicyCommand(sonarRepo repositories.SonarProjectsRepository) *ApplySonarPolicyCommand {
	return &ApplySonarPolicyCommand{sonarRepo: sonarRepo}
}

// ApplySonarPolicyInput selects the organization to walk. ProjectFilter,
// when set, narrows the run to the single project whose name or key
// matches it exactly — the name is the repository name and is what the
// CLI's `--repo` normally carries, while the key covers a renamed
// project whose key no longer follows from its name. DryRun reports
// every mutation without performing it.
type ApplySonarPolicyInput struct {
	Organization  string
	ProjectFilter string
	DryRun        bool
}

// ApplySonarPolicyChange describes one mutation against one project.
// Count is the number of underlying records the action covered: the
// exclusion pairs added, the issues accepted, or the hotspots marked
// safe.
type ApplySonarPolicyChange struct {
	ProjectKey  string
	ProjectName string
	Action      string
	Count       int
	Applied     bool
}

// ApplySonarPolicyListeners mirrors the phase-3 listener shape. OnSkip
// fires for a project that already carries the exclusions and has
// nothing left to triage.
type ApplySonarPolicyListeners struct {
	OnChange  func(change ApplySonarPolicyChange)
	OnSkip    func(projectKey, reason string)
	OnSuccess func(exclusionChanges, issuesAccepted, hotspotsReviewed int)
	OnError   func(projectKey string, err error)
}

// Execute walks the organization's projects and applies the policy to
// each. A listing failure is terminal — a partially enumerated
// organization must not be mistaken for a compliant one — while a
// per-project failure is reported and the walk continues.
func (c ApplySonarPolicyCommand) Execute(
	ctx context.Context,
	input ApplySonarPolicyInput,
	listeners ApplySonarPolicyListeners,
) {
	projects, err := c.sonarRepo.FindAllByOrganization(ctx, input.Organization)
	if err != nil {
		listeners.OnError("", fmt.Errorf("listing projects of %s: %w", input.Organization, err))
		return
	}

	exclusionChanges := 0
	issuesAccepted := 0
	hotspotsReviewed := 0

	for _, project := range projects {
		if input.ProjectFilter != "" && project.Name != input.ProjectFilter && project.Key != input.ProjectFilter {
			continue
		}

		// The three sub-steps are attempted independently on purpose.
		// They sit behind three different SonarQube Cloud permissions
		// (Administer, Administer Issues, Administer Security Hotspots),
		// so one of them being denied says nothing about the other two —
		// unlike phase 3, where a failure aborts the remaining sub-steps.
		exclusions := c.applyExclusions(ctx, input, project, listeners)
		issues := c.acceptIssues(ctx, input, project, listeners)
		hotspots := c.reviewHotspots(ctx, input, project, listeners)

		if exclusions+issues+hotspots == 0 && listeners.OnSkip != nil {
			listeners.OnSkip(project.Key, "compliant")
		}

		exclusionChanges += exclusions
		issuesAccepted += issues
		hotspotsReviewed += hotspots
	}

	listeners.OnSuccess(exclusionChanges, issuesAccepted, hotspotsReviewed)
}

// applyExclusions adds the missing policy pairs to the project's
// property set and returns how many were added. Reading before writing
// is what keeps repeated runs quiet and non-destructive: SonarQube
// replaces the whole set on write, so the merge preserves pairs someone
// added in the UI, and an already-compliant project is never rewritten.
func (c ApplySonarPolicyCommand) applyExclusions(
	ctx context.Context,
	input ApplySonarPolicyInput,
	project entities.SonarProject,
	listeners ApplySonarPolicyListeners,
) int {
	existing, err := c.sonarRepo.FindIssueExclusionsByProjectKey(ctx, project.Key)
	if err != nil {
		listeners.OnError(project.Key, fmt.Errorf("reading issue exclusions: %w", err))
		return 0
	}

	merged, added := entities.MergeSonarIssueExclusions(existing, entities.DesiredSonarIssueExclusions())
	if !added {
		return 0
	}

	count := len(merged) - len(existing)
	change := ApplySonarPolicyChange{
		ProjectKey:  project.Key,
		ProjectName: project.Name,
		Action:      SonarActionIssueExclusions,
		Count:       count,
	}
	if input.DryRun {
		emitSonarChange(listeners.OnChange, change)
		return count
	}

	if updateErr := c.sonarRepo.UpdateIssueExclusions(ctx, project.Key, merged); updateErr != nil {
		listeners.OnError(project.Key, fmt.Errorf("setting issue exclusions: %w", updateErr))
		return 0
	}
	change.Applied = true
	emitSonarChange(listeners.OnChange, change)
	return count
}

// acceptIssues transitions the project's still-open findings for the
// policy rules to accepted, and returns how many were accepted. An
// accepted issue stops counting toward `new_security_rating`, which is
// one of the two conditions these rules fail.
func (c ApplySonarPolicyCommand) acceptIssues(
	ctx context.Context,
	input ApplySonarPolicyInput,
	project entities.SonarProject,
	listeners ApplySonarPolicyListeners,
) int {
	issues, err := c.sonarRepo.FindOpenIssuesByRules(ctx, project.Key, entities.DesiredSonarTriagedRuleKeys())
	if err != nil {
		listeners.OnError(project.Key, fmt.Errorf("searching open issues: %w", err))
		return 0
	}
	if len(issues) == 0 {
		return 0
	}

	keys := make([]string, 0, len(issues))
	for _, issue := range issues {
		keys = append(keys, issue.Key)
	}

	change := ApplySonarPolicyChange{
		ProjectKey:  project.Key,
		ProjectName: project.Name,
		Action:      SonarActionIssuesAccepted,
		Count:       len(keys),
	}
	if input.DryRun {
		emitSonarChange(listeners.OnChange, change)
		return len(keys)
	}

	// The count comes back from the repository rather than being assumed
	// to be len(keys): api/issues/bulk_change answers 200 with a tally,
	// so a denied transition arrives as "0 accepted", not as an error
	// status. Reporting len(keys) here would log a fully successful run
	// that changed nothing.
	accepted, acceptErr := c.sonarRepo.AcceptIssues(ctx, keys, entities.DesiredSonarTriageComment)
	if acceptErr != nil {
		listeners.OnError(project.Key, fmt.Errorf("accepting issues: %w", acceptErr))
	}
	if accepted == 0 {
		return 0
	}
	change.Applied = true
	change.Count = accepted
	emitSonarChange(listeners.OnChange, change)
	return accepted
}

// reviewHotspots marks the project's TO_REVIEW hotspots for the policy
// rules as REVIEWED/SAFE and returns how many were transitioned. This is
// the half that clears `new_security_hotspots_reviewed`, the other
// failing condition — and it is separate from acceptIssues because the
// same rule raises both kinds of finding depending on the file.
//
// Each hotspot is transitioned on its own (SonarQube Cloud has no bulk
// form), and a failure on one is reported without abandoning the rest:
// leaving the remainder TO_REVIEW would keep the gate red for a reason
// that has nothing to do with them.
func (c ApplySonarPolicyCommand) reviewHotspots(
	ctx context.Context,
	input ApplySonarPolicyInput,
	project entities.SonarProject,
	listeners ApplySonarPolicyListeners,
) int {
	hotspots, err := c.sonarRepo.FindHotspotsToReviewByRules(ctx, project.Key, entities.DesiredSonarTriagedRuleKeys())
	if err != nil {
		listeners.OnError(project.Key, fmt.Errorf("searching hotspots: %w", err))
		return 0
	}
	if len(hotspots) == 0 {
		return 0
	}

	if input.DryRun {
		emitSonarChange(listeners.OnChange, ApplySonarPolicyChange{
			ProjectKey:  project.Key,
			ProjectName: project.Name,
			Action:      SonarActionHotspotsSafe,
			Count:       len(hotspots),
		})
		return len(hotspots)
	}

	reviewed := 0
	for _, hotspot := range hotspots {
		markErr := c.sonarRepo.MarkHotspotReviewedSafe(ctx, hotspot.Key, entities.DesiredSonarTriageComment)
		if markErr != nil {
			listeners.OnError(project.Key, fmt.Errorf("reviewing hotspot %s: %w", hotspot.Key, markErr))
			continue
		}
		reviewed++
	}
	if reviewed == 0 {
		return 0
	}

	emitSonarChange(listeners.OnChange, ApplySonarPolicyChange{
		ProjectKey:  project.Key,
		ProjectName: project.Name,
		Action:      SonarActionHotspotsSafe,
		Count:       reviewed,
		Applied:     true,
	})
	return reviewed
}

func emitSonarChange(cb func(change ApplySonarPolicyChange), change ApplySonarPolicyChange) {
	if cb != nil {
		cb(change)
	}
}
