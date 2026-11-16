package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

func TestDetectChangelogMode(t *testing.T) {
	t.Parallel()

	t.Run("should follow the pipelines gate: chlog wins, then CHANGELOG.md, then none", func(t *testing.T) {
		t.Parallel()
		// given / when / then
		assert.Equal(t, entities.ChangelogChlog, entities.DetectChangelogMode(true, false, true))
		assert.Equal(t, entities.ChangelogChlog, entities.DetectChangelogMode(false, true, false))
		assert.Equal(t, entities.ChangelogFileMode, entities.DetectChangelogMode(false, false, true))
		assert.Equal(t, entities.ChangelogNone, entities.DetectChangelogMode(false, false, false))
	})
}

func TestChangelogMode(t *testing.T) {
	t.Parallel()

	t.Run("should grant only the tool each convention needs", func(t *testing.T) {
		t.Parallel()
		// given / when / then
		assert.Equal(t, "Bash(chlog new:*)", entities.ChangelogChlog.Grant())
		assert.Equal(t, "Edit(/CHANGELOG.md)", entities.ChangelogFileMode.Grant())
		assert.Empty(t, entities.ChangelogNone.Grant())
	})

	t.Run("should stage only the entry each convention writes", func(t *testing.T) {
		t.Parallel()
		// given / when / then
		assert.Equal(t, []string{".changes/unreleased"}, entities.ChangelogChlog.StagePaths())
		assert.Equal(t, []string{"CHANGELOG.md"}, entities.ChangelogFileMode.StagePaths())
		assert.Empty(t, entities.ChangelogNone.StagePaths())
	})

	t.Run("should tell Claude which convention applies", func(t *testing.T) {
		t.Parallel()
		// given / when / then
		assert.Contains(t, entities.ChangelogChlog.Note(), "chlog new")
		assert.Contains(t, entities.ChangelogFileMode.Note(), "does not use chlog")
		assert.Contains(t, entities.ChangelogNone.Note(), "Do not record the refresh")
	})
}
