//go:build unit

package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

func TestMergeSonarIssueExclusions(t *testing.T) {
	t.Parallel()

	t.Run("should append the policy pair when the project has no exclusions", func(t *testing.T) {
		t.Parallel()
		// given
		desired := entities.DesiredSonarIssueExclusions()

		// when
		merged, added := entities.MergeSonarIssueExclusions(nil, desired)

		// then
		assert.True(t, added)
		assert.Equal(t, desired, merged)
	})

	t.Run("should report no change when the policy pair is already configured", func(t *testing.T) {
		t.Parallel()
		// given — a re-run against an already-hardened project; rewriting
		// the set would be a no-op mutation logged as a change every day.
		existing := entities.DesiredSonarIssueExclusions()

		// when
		merged, added := entities.MergeSonarIssueExclusions(existing, entities.DesiredSonarIssueExclusions())

		// then
		assert.False(t, added)
		assert.Equal(t, existing, merged)
	})

	t.Run("should keep exclusions the policy does not know about", func(t *testing.T) {
		t.Parallel()
		// given — api/settings/set replaces the property set wholesale, so
		// a pair added by hand in the UI has to survive the merge.
		handmade := entities.SonarIssueExclusion{RuleKey: "go:S1192", ResourceKey: "**/*_test.go"}

		// when
		merged, added := entities.MergeSonarIssueExclusions(
			[]entities.SonarIssueExclusion{handmade},
			entities.DesiredSonarIssueExclusions(),
		)

		// then
		assert.True(t, added)
		require.Len(t, merged, 1+len(entities.DesiredSonarIssueExclusions()))
		assert.Equal(t, handmade, merged[0])
		assert.True(t, entities.ContainsSonarIssueExclusion(merged, entities.DesiredSonarIssueExclusions()[0]))
	})

	t.Run("should not mutate the slice it was given", func(t *testing.T) {
		t.Parallel()
		// given
		handmade := entities.SonarIssueExclusion{RuleKey: "go:S1192", ResourceKey: "**/*_test.go"}
		existing := []entities.SonarIssueExclusion{handmade}

		// when
		_, _ = entities.MergeSonarIssueExclusions(existing, entities.DesiredSonarIssueExclusions())

		// then
		assert.Equal(t, []entities.SonarIssueExclusion{handmade}, existing)
	})

	t.Run("should treat a different resource pattern for the same rule as a new pair", func(t *testing.T) {
		t.Parallel()
		// given — SonarQube matches resourceKey as a path pattern, so two
		// patterns selecting the same files today are still distinct.
		narrower := entities.SonarIssueExclusion{
			RuleKey:     "githubactions:S7637",
			ResourceKey: ".github/workflows/**",
		}

		// when
		merged, added := entities.MergeSonarIssueExclusions(
			[]entities.SonarIssueExclusion{narrower},
			entities.DesiredSonarIssueExclusions(),
		)

		// then
		assert.True(t, added)
		assert.Len(t, merged, 2)
	})
}

func TestDesiredSonarTriagedRuleKeys(t *testing.T) {
	t.Parallel()

	t.Run("should return every rule the exclusion policy silences", func(t *testing.T) {
		t.Parallel()
		// given — the triage list must not drift from the exclusion list:
		// a rule excluded but not triaged leaves the gate red until the
		// project is analysed again.
		exclusions := entities.DesiredSonarIssueExclusions()

		// when
		keys := entities.DesiredSonarTriagedRuleKeys()

		// then
		assert.Contains(t, keys, "githubactions:S7637")
		for _, exclusion := range exclusions {
			assert.Contains(t, keys, exclusion.RuleKey)
		}
	})

	t.Run("should collapse duplicates when one rule is excluded on several patterns", func(t *testing.T) {
		t.Parallel()
		// given / when — the derivation is over the policy itself, so this
		// asserts the shape the derivation guarantees.
		keys := entities.DesiredSonarTriagedRuleKeys()

		// then
		seen := map[string]int{}
		for _, key := range keys {
			seen[key]++
		}
		for key, count := range seen {
			assert.Equal(t, 1, count, "rule %s should appear once", key)
		}
	})
}
