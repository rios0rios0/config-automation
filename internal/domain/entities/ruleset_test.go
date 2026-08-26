//go:build unit

package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

func TestRulesetHasAllowedMergeMethods(t *testing.T) {
	t.Parallel()

	t.Run("should report compliant when the method set matches exactly", func(t *testing.T) {
		t.Parallel()
		// given
		ruleset := entities.Ruleset{AllowedMergeMethods: []string{"merge"}}

		// when
		compliant := ruleset.HasAllowedMergeMethods([]string{"merge"})

		// then
		assert.True(t, compliant)
	})

	t.Run("should report compliant regardless of order", func(t *testing.T) {
		t.Parallel()
		// given
		ruleset := entities.Ruleset{AllowedMergeMethods: []string{"squash", "merge"}}

		// when
		compliant := ruleset.HasAllowedMergeMethods([]string{"merge", "squash"})

		// then
		assert.True(t, compliant)
	})

	t.Run("should reject a superset that still permits rebase", func(t *testing.T) {
		t.Parallel()
		// given — the exact drift the policy exists to catch: `rebase` left
		// in the list is the fast-forward merge path.
		ruleset := entities.Ruleset{AllowedMergeMethods: []string{"merge", "rebase"}}

		// when
		compliant := ruleset.HasAllowedMergeMethods([]string{"merge"})

		// then
		assert.False(t, compliant)
	})

	t.Run("should reject the wrong single method", func(t *testing.T) {
		t.Parallel()
		// given
		ruleset := entities.Ruleset{AllowedMergeMethods: []string{"rebase"}}

		// when
		compliant := ruleset.HasAllowedMergeMethods([]string{"merge"})

		// then
		assert.False(t, compliant)
	})

	t.Run("should reject a nil list, meaning no pull_request rule exists", func(t *testing.T) {
		t.Parallel()
		// given
		ruleset := entities.Ruleset{AllowedMergeMethods: nil}

		// when
		compliant := ruleset.HasAllowedMergeMethods([]string{"merge"})

		// then
		assert.False(t, compliant)
	})
}

func TestRulesetIsCompliant(t *testing.T) {
	t.Parallel()

	compliant := func() entities.Ruleset {
		return entities.Ruleset{
			Name:                entities.DesiredRulesetName,
			Enforcement:         "active",
			HasNonFastForward:   true,
			AllowedMergeMethods: entities.DesiredAllowedMergeMethods(),
			TargetsMain:         true,
			AdminBypass:         true,
		}
	}

	t.Run("should be compliant when every part of the policy is satisfied", func(t *testing.T) {
		t.Parallel()
		// given
		ruleset := compliant()

		// when
		result := ruleset.IsCompliant()

		// then
		assert.True(t, result)
	})

	t.Run("should not be compliant when the merge methods still allow rebase", func(t *testing.T) {
		t.Parallel()
		// given — force-push protection is on, so a check that only looked
		// at non_fast_forward would wrongly pass this.
		ruleset := compliant()
		ruleset.AllowedMergeMethods = []string{"merge", "rebase"}

		// when
		result := ruleset.IsCompliant()

		// then
		assert.False(t, result)
	})

	t.Run("should not be compliant when the pull_request rule is missing", func(t *testing.T) {
		t.Parallel()
		// given
		ruleset := compliant()
		ruleset.AllowedMergeMethods = nil

		// when
		result := ruleset.IsCompliant()

		// then
		assert.False(t, result)
	})

	t.Run("should not be compliant when non_fast_forward is missing", func(t *testing.T) {
		t.Parallel()
		// given
		ruleset := compliant()
		ruleset.HasNonFastForward = false

		// when
		result := ruleset.IsCompliant()

		// then
		assert.False(t, result)
	})
}

func TestDesiredAllowedMergeMethods(t *testing.T) {
	t.Parallel()

	t.Run("should allow merge commits and nothing else", func(t *testing.T) {
		t.Parallel()
		// given / when
		methods := entities.DesiredAllowedMergeMethods()

		// then — rebase and squash are both fast-forward shapes; either one
		// present here defeats the policy.
		assert.Equal(t, []string{"merge"}, methods)
	})

	t.Run("should return a fresh slice each call so callers cannot mutate the policy", func(t *testing.T) {
		t.Parallel()
		// given
		first := entities.DesiredAllowedMergeMethods()

		// when
		first[0] = "rebase"

		// then
		assert.Equal(t, []string{"merge"}, entities.DesiredAllowedMergeMethods())
	})
}
