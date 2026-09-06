//go:build unit

package commands_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/commands"
	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/test/domain/builders"
	doubles "github.com/rios0rios0/config-automation/test/domain/doubles/repositories"
)

const sonarOrganization = "rios0rios0"

// firstPartyRule is the rule the fleet policy silences: it flags the
// deliberate `uses: rios0rios0/pipelines/...@main` references.
const firstPartyRule = "githubactions:S7637"

func noopSonarListeners() commands.ApplySonarPolicyListeners {
	return commands.ApplySonarPolicyListeners{
		OnSuccess: func(_, _, _ int) {},
		OnError:   func(_ string, _ error) {},
	}
}

func TestApplySonarPolicyCommand(t *testing.T) {
	t.Parallel()

	t.Run("should call OnChange with issue_exclusions when a project carries none", func(t *testing.T) {
		t.Parallel()
		// given
		project := builders.NewSonarProjectBuilder().WithName("autobump").Build()
		sonarRepo := doubles.NewInMemorySonarProjectsRepository().WithProject(project)
		command := commands.NewApplySonarPolicyCommand(sonarRepo)

		// when
		var changes []commands.ApplySonarPolicyChange
		listeners := noopSonarListeners()
		listeners.OnChange = func(c commands.ApplySonarPolicyChange) { changes = append(changes, c) }
		command.Execute(context.TODO(), commands.ApplySonarPolicyInput{Organization: sonarOrganization}, listeners)

		// then
		require.Len(t, changes, 1)
		assert.Equal(t, commands.SonarActionIssueExclusions, changes[0].Action)
		assert.Equal(t, project.Key, changes[0].ProjectKey)
		assert.True(t, changes[0].Applied)
		assert.Equal(t, entities.DesiredSonarIssueExclusions(), sonarRepo.ExclusionsByKey[project.Key])
	})

	t.Run("should call OnSkip when the project already carries the policy and has nothing to triage", func(t *testing.T) {
		t.Parallel()
		// given — the steady state a daily run must stay quiet on.
		project := builders.NewSonarProjectBuilder().WithName("autobump").Build()
		sonarRepo := doubles.NewInMemorySonarProjectsRepository().
			WithProject(project).
			WithExclusions(project.Key, entities.DesiredSonarIssueExclusions()...)
		command := commands.NewApplySonarPolicyCommand(sonarRepo)

		// when
		var changes []commands.ApplySonarPolicyChange
		var skipped []string
		listeners := noopSonarListeners()
		listeners.OnChange = func(c commands.ApplySonarPolicyChange) { changes = append(changes, c) }
		listeners.OnSkip = func(key, _ string) { skipped = append(skipped, key) }
		command.Execute(context.TODO(), commands.ApplySonarPolicyInput{Organization: sonarOrganization}, listeners)

		// then
		assert.Empty(t, changes)
		assert.Equal(t, []string{project.Key}, skipped)
	})

	t.Run("should call OnChange with issues_accepted when open findings match the policy rules", func(t *testing.T) {
		t.Parallel()
		// given — the exclusion only takes effect on the next analysis, so
		// the already-recorded issue has to be accepted for the gate to
		// recompute without waiting for a push.
		project := builders.NewSonarProjectBuilder().WithName("terra").Build()
		sonarRepo := doubles.NewInMemorySonarProjectsRepository().
			WithProject(project).
			WithExclusions(project.Key, entities.DesiredSonarIssueExclusions()...).
			WithOpenIssues(project.Key, entities.SonarIssue{
				Key:       "issue-1",
				RuleKey:   firstPartyRule,
				Component: project.Key + ":.github/workflows/claude-mention.yaml",
				Line:      15,
			})
		command := commands.NewApplySonarPolicyCommand(sonarRepo)

		// when
		var changes []commands.ApplySonarPolicyChange
		var accepted int
		listeners := noopSonarListeners()
		listeners.OnChange = func(c commands.ApplySonarPolicyChange) { changes = append(changes, c) }
		listeners.OnSuccess = func(_, issues, _ int) { accepted = issues }
		command.Execute(context.TODO(), commands.ApplySonarPolicyInput{Organization: sonarOrganization}, listeners)

		// then
		require.Len(t, changes, 1)
		assert.Equal(t, commands.SonarActionIssuesAccepted, changes[0].Action)
		assert.Equal(t, 1, changes[0].Count)
		assert.Equal(t, []string{"issue-1"}, sonarRepo.AcceptedIssueKeys)
		assert.Contains(t, sonarRepo.Comments, entities.DesiredSonarTriageComment)
		assert.Equal(t, 1, accepted)
	})

	t.Run("should call OnChange with hotspots_reviewed when hotspots match the policy rules", func(t *testing.T) {
		t.Parallel()
		// given — the same rule raises a hotspot on one workflow file and
		// a vulnerability on another, and only the hotspot half clears
		// new_security_hotspots_reviewed.
		project := builders.NewSonarProjectBuilder().WithName("testkit").Build()
		sonarRepo := doubles.NewInMemorySonarProjectsRepository().
			WithProject(project).
			WithExclusions(project.Key, entities.DesiredSonarIssueExclusions()...).
			WithHotspots(project.Key,
				entities.SonarHotspot{Key: "hotspot-1", RuleKey: firstPartyRule},
				entities.SonarHotspot{Key: "hotspot-2", RuleKey: firstPartyRule},
			)
		command := commands.NewApplySonarPolicyCommand(sonarRepo)

		// when
		var changes []commands.ApplySonarPolicyChange
		var reviewed int
		listeners := noopSonarListeners()
		listeners.OnChange = func(c commands.ApplySonarPolicyChange) { changes = append(changes, c) }
		listeners.OnSuccess = func(_, _, hotspots int) { reviewed = hotspots }
		command.Execute(context.TODO(), commands.ApplySonarPolicyInput{Organization: sonarOrganization}, listeners)

		// then
		require.Len(t, changes, 1)
		assert.Equal(t, commands.SonarActionHotspotsSafe, changes[0].Action)
		assert.Equal(t, 2, changes[0].Count)
		assert.Equal(t, []string{"hotspot-1", "hotspot-2"}, sonarRepo.ReviewedHotspotIDs)
		assert.Equal(t, 2, reviewed)
	})

	t.Run("should leave findings of other rules alone", func(t *testing.T) {
		t.Parallel()
		// given — the older docker:S6471 and terraform hotspots are real
		// findings outside the policy; triaging them would hide work the
		// audit exists to surface.
		project := builders.NewSonarProjectBuilder().WithName("iac-modules").Build()
		sonarRepo := doubles.NewInMemorySonarProjectsRepository().
			WithProject(project).
			WithExclusions(project.Key, entities.DesiredSonarIssueExclusions()...).
			WithOpenIssues(project.Key, entities.SonarIssue{Key: "issue-9", RuleKey: "yaml:S2068"}).
			WithHotspots(project.Key, entities.SonarHotspot{Key: "hotspot-9", RuleKey: "docker:S6471"})
		command := commands.NewApplySonarPolicyCommand(sonarRepo)

		// when
		var skipped []string
		listeners := noopSonarListeners()
		listeners.OnChange = func(_ commands.ApplySonarPolicyChange) { t.Error("OnChange should not be called") }
		listeners.OnSkip = func(key, _ string) { skipped = append(skipped, key) }
		command.Execute(context.TODO(), commands.ApplySonarPolicyInput{Organization: sonarOrganization}, listeners)

		// then
		assert.Empty(t, sonarRepo.AcceptedIssueKeys)
		assert.Empty(t, sonarRepo.ReviewedHotspotIDs)
		assert.Equal(t, []string{project.Key}, skipped)
	})

	t.Run("should target only the matching project when a filter is set", func(t *testing.T) {
		t.Parallel()
		// given — --repo takes the repository name, while the project it
		// maps to may carry an unrelated key after a rename.
		renamed := builders.NewSonarProjectBuilder().
			WithName("dev-toolkit").
			WithKey("rios0rios0_versainit").
			Build()
		other := builders.NewSonarProjectBuilder().WithName("guide").Build()
		sonarRepo := doubles.NewInMemorySonarProjectsRepository().WithProject(renamed).WithProject(other)
		command := commands.NewApplySonarPolicyCommand(sonarRepo)

		// when
		command.Execute(context.TODO(), commands.ApplySonarPolicyInput{
			Organization:  sonarOrganization,
			ProjectFilter: "dev-toolkit",
		}, noopSonarListeners())

		// then
		assert.Equal(t, entities.DesiredSonarIssueExclusions(), sonarRepo.ExclusionsByKey["rios0rios0_versainit"])
		assert.Empty(t, sonarRepo.ExclusionsByKey[other.Key])
	})

	t.Run("should not mutate when DryRun is set", func(t *testing.T) {
		t.Parallel()
		// given
		project := builders.NewSonarProjectBuilder().WithName("boss").Build()
		sonarRepo := doubles.NewInMemorySonarProjectsRepository().
			WithProject(project).
			WithOpenIssues(project.Key, entities.SonarIssue{Key: "issue-1", RuleKey: firstPartyRule}).
			WithHotspots(project.Key, entities.SonarHotspot{Key: "hotspot-1", RuleKey: firstPartyRule})
		command := commands.NewApplySonarPolicyCommand(sonarRepo)

		// when
		var changes []commands.ApplySonarPolicyChange
		listeners := noopSonarListeners()
		listeners.OnChange = func(c commands.ApplySonarPolicyChange) { changes = append(changes, c) }
		command.Execute(context.TODO(), commands.ApplySonarPolicyInput{
			Organization: sonarOrganization,
			DryRun:       true,
		}, listeners)

		// then
		require.Len(t, changes, 3)
		for _, change := range changes {
			assert.False(t, change.Applied)
		}
		assert.Empty(t, sonarRepo.ExclusionsByKey[project.Key])
		assert.Empty(t, sonarRepo.AcceptedIssueKeys)
		assert.Empty(t, sonarRepo.ReviewedHotspotIDs)
	})

	t.Run("should still triage when writing the exclusion setting is denied", func(t *testing.T) {
		t.Parallel()
		// given — Administer, Administer Issues and Administer Security
		// Hotspots are three separate SonarQube Cloud permissions, so one
		// denial says nothing about the other two.
		expected := errors.New("api/settings/set returned 403: Insufficient privileges")
		project := builders.NewSonarProjectBuilder().WithName("langforge").Build()
		sonarRepo := doubles.NewInMemorySonarProjectsRepository().
			WithProject(project).
			WithOpenIssues(project.Key, entities.SonarIssue{Key: "issue-1", RuleKey: firstPartyRule}).
			WithHotspots(project.Key, entities.SonarHotspot{Key: "hotspot-1", RuleKey: firstPartyRule})
		sonarRepo.ErrorOnUpdate = expected
		command := commands.NewApplySonarPolicyCommand(sonarRepo)

		// when
		var received error
		listeners := noopSonarListeners()
		listeners.OnError = func(_ string, err error) { received = err }
		command.Execute(context.TODO(), commands.ApplySonarPolicyInput{Organization: sonarOrganization}, listeners)

		// then
		require.ErrorIs(t, received, expected)
		assert.Equal(t, []string{"issue-1"}, sonarRepo.AcceptedIssueKeys)
		assert.Equal(t, []string{"hotspot-1"}, sonarRepo.ReviewedHotspotIDs)
	})

	t.Run("should call OnError once per project when listing succeeds but a project fails", func(t *testing.T) {
		t.Parallel()
		// given
		expected := errors.New("api/issues/bulk_change returned 403")
		first := builders.NewSonarProjectBuilder().WithName("terra").Build()
		second := builders.NewSonarProjectBuilder().WithName("testkit").Build()
		sonarRepo := doubles.NewInMemorySonarProjectsRepository().
			WithProject(first).
			WithProject(second).
			WithOpenIssues(first.Key, entities.SonarIssue{Key: "issue-1", RuleKey: firstPartyRule}).
			WithOpenIssues(second.Key, entities.SonarIssue{Key: "issue-2", RuleKey: firstPartyRule})
		sonarRepo.ErrorOnAccept = expected
		command := commands.NewApplySonarPolicyCommand(sonarRepo)

		// when
		var errored []string
		listeners := noopSonarListeners()
		listeners.OnError = func(key string, _ error) { errored = append(errored, key) }
		command.Execute(context.TODO(), commands.ApplySonarPolicyInput{Organization: sonarOrganization}, listeners)

		// then — the walk continues past the failure, and both projects
		// still get their exclusion written.
		assert.Equal(t, []string{first.Key, second.Key}, errored)
		assert.Equal(t, entities.DesiredSonarIssueExclusions(), sonarRepo.ExclusionsByKey[second.Key])
	})

	t.Run("should call OnError with an empty project key when the organization cannot be listed", func(t *testing.T) {
		t.Parallel()
		// given — a partially enumerated organization must not be mistaken
		// for a compliant one, so listing failure is terminal.
		expected := errors.New("api/components/search_projects returned 401")
		sonarRepo := doubles.NewInMemorySonarProjectsRepository()
		sonarRepo.ErrorOnFindProjects = expected
		command := commands.NewApplySonarPolicyCommand(sonarRepo)

		// when
		var erroredKeys []string
		var received error
		var succeeded bool
		listeners := commands.ApplySonarPolicyListeners{
			OnSuccess: func(_, _, _ int) { succeeded = true },
			OnError:   func(key string, err error) { erroredKeys = append(erroredKeys, key); received = err },
		}
		command.Execute(context.TODO(), commands.ApplySonarPolicyInput{Organization: sonarOrganization}, listeners)

		// then
		assert.Equal(t, []string{""}, erroredKeys)
		require.ErrorIs(t, received, expected)
		assert.False(t, succeeded)
	})

	t.Run("should review the remaining hotspots when one transition fails", func(t *testing.T) {
		t.Parallel()
		// given — abandoning the rest would keep the gate red for hotspots
		// that had nothing to do with the failure.
		project := builders.NewSonarProjectBuilder().WithName("gitforge").Build()
		sonarRepo := doubles.NewInMemorySonarProjectsRepository().
			WithProject(project).
			WithExclusions(project.Key, entities.DesiredSonarIssueExclusions()...).
			WithHotspots(project.Key,
				entities.SonarHotspot{Key: "hotspot-1", RuleKey: firstPartyRule},
				entities.SonarHotspot{Key: "hotspot-2", RuleKey: firstPartyRule},
			)
		command := commands.NewApplySonarPolicyCommand(sonarRepo)

		// when — the double fails every call, so the assertion is that the
		// loop reported both rather than stopping at the first.
		sonarRepo.ErrorOnReview = errors.New("api/hotspots/change_status returned 403")
		var errored []string
		listeners := noopSonarListeners()
		listeners.OnError = func(key string, _ error) { errored = append(errored, key) }
		command.Execute(context.TODO(), commands.ApplySonarPolicyInput{Organization: sonarOrganization}, listeners)

		// then
		assert.Equal(t, []string{project.Key, project.Key}, errored)
		assert.Empty(t, sonarRepo.ReviewedHotspotIDs)
	})
}
