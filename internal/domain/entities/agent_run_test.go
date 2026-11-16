package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

func TestAgentTools(t *testing.T) {
	t.Parallel()

	t.Run("should keep the audit read-only", func(t *testing.T) {
		t.Parallel()
		// given / when / then
		assert.Equal(t, "Read,Grep,Glob", entities.AuditTools())
	})

	t.Run("should offer Bash only where chlog new is the granted command", func(t *testing.T) {
		t.Parallel()
		// given / when / then
		assert.Equal(t, "Read,Grep,Glob,Edit,Write,Bash", entities.RefreshTools(entities.ChangelogChlog.Grant()))
		assert.Equal(t, "Read,Grep,Glob,Edit,Write", entities.RefreshTools(entities.ChangelogFileMode.Grant()))
		assert.Equal(t, "Read,Grep,Glob,Edit,Write", entities.RefreshTools(""))
	})

	t.Run("should append the changelog grant to the scope's exact grants", func(t *testing.T) {
		t.Parallel()
		// given
		scope := entities.RefreshScope{AllowedTools: []string{"Read", "Grep", "Glob", "Edit(/README.md)"}}

		// when / then
		assert.Equal(t, "Read,Grep,Glob,Edit(/README.md),Edit(/CHANGELOG.md)",
			entities.RefreshAllowedTools(scope, entities.ChangelogFileMode.Grant()))
		assert.Equal(t, "Read,Grep,Glob,Edit(/README.md)", entities.RefreshAllowedTools(scope, ""))
	})
}
