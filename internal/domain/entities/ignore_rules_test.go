package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

func TestIgnoreRulesReport(t *testing.T) {
	t.Parallel()

	t.Run("should count only a created or updated .gitignore as changed", func(t *testing.T) {
		t.Parallel()
		// given
		expected := map[entities.IgnoreRulesOutcome]bool{
			entities.IgnoreRulesOff: false, entities.IgnoreRulesCurrent: false, entities.IgnoreRulesSkipped: false,
			entities.IgnoreRulesCreated: true, entities.IgnoreRulesUpdated: true,
		}

		for outcome, changed := range expected {
			// when
			report := entities.IgnoreRulesReport{Outcome: outcome}

			// then
			assert.Equal(t, changed, report.Changed(), outcome)
		}
	})
}

func TestDescribeIgnoreRules(t *testing.T) {
	t.Parallel()

	t.Run("should leave the prompt section out when the host did not look at .gitignore", func(t *testing.T) {
		t.Parallel()
		// given / when
		note := entities.DescribeIgnoreRules(entities.IgnoreRulesReport{Outcome: entities.IgnoreRulesOff})

		// then
		assert.Empty(t, note)
	})

	t.Run("should ask for an Added entry when the host created .gitignore", func(t *testing.T) {
		t.Parallel()
		// given / when
		note := entities.DescribeIgnoreRules(entities.IgnoreRulesReport{Outcome: entities.IgnoreRulesCreated})

		// then
		assert.Contains(t, note, "- Created `.gitignore` with the shared ignore rules of `rios0rios0/pipelines`")
		assert.Contains(t, note, "record it under `Added`")
	})

	t.Run("should ask for a Changed entry when the host updated .gitignore", func(t *testing.T) {
		t.Parallel()
		// given / when
		note := entities.DescribeIgnoreRules(entities.IgnoreRulesReport{Outcome: entities.IgnoreRulesUpdated})

		// then
		assert.Contains(t, note, "Every line outside the block is unchanged.")
		assert.Contains(t, note, "record it under `Changed`")
	})

	t.Run("should ask for nothing when the shared ignore rules were already current", func(t *testing.T) {
		t.Parallel()
		// given / when
		note := entities.DescribeIgnoreRules(entities.IgnoreRulesReport{Outcome: entities.IgnoreRulesCurrent})

		// then
		assert.Equal(t, "No changes: `.gitignore` already carries the current shared ignore rules.", note)
	})

	t.Run("should ask for a human review when .gitignore is not a regular file", func(t *testing.T) {
		t.Parallel()
		// given / when
		note := entities.DescribeIgnoreRules(entities.IgnoreRulesReport{Outcome: entities.IgnoreRulesSkipped})

		// then
		assert.Contains(t, note, "report it for human review")
		assert.NotContains(t, note, "record it")
	})
}
