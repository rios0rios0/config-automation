package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

func TestClassifyAgentFailure(t *testing.T) {
	t.Parallel()

	t.Run("should recognise every usage-limit wording as account-wide", func(t *testing.T) {
		t.Parallel()
		// The org spend cap and the 5-hour limit each name neither a week nor
		// a month, and an audit result can be worded so that only the error
		// code marks the limit.
		for _, output := range []string{
			"You have hit your weekly limit",
			"Claude AI monthly usage limit reached",
			"You've hit your org's monthly spend limit · ask your admin to raise it",
			"You've hit your 5-hour limit · resets 9am (UTC)",
			`{"type":"result","is_error":true,"api_error":"usage_limit_reached","result":"reworded"}`,
		} {
			// given / when
			kind := entities.ClassifyAgentFailure(output)

			// then
			assert.Equal(t, entities.AgentFailureUsageLimit, kind, output)
		}
	})

	t.Run("should recognise a safeguard refusal", func(t *testing.T) {
		t.Parallel()
		// The second wording is the current one: it shares no phrase with
		// the first but "flagged this message for a cybersecurity topic".
		for _, output := range []string{
			"API Error: Claude Code's safeguards flagged this request",
			"API Error: Opus has safety measures that flagged this message for a cybersecurity topic.",
			`{"error":"cyber-use-case"}`,
			"See https://support.claude.com/en/articles/14604842-real-time-cyber-safeguards-on-claude",
			"Apply to the Cyber Verification Program for access.",
		} {
			// given / when
			kind := entities.ClassifyAgentFailure(output)

			// then
			assert.Equal(t, entities.AgentFailureSafeguard, kind, output)
		}
	})

	t.Run("should leave any other failure to the repository it happened in", func(t *testing.T) {
		t.Parallel()
		// given / when
		kind := entities.ClassifyAgentFailure("network error: connection reset")

		// then
		assert.Equal(t, entities.AgentFailureOther, kind)
	})
}
