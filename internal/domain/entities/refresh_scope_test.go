package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/test/domain/fixtures"
)

func TestIsRefreshPath(t *testing.T) {
	t.Parallel()

	t.Run("should accept every category the refresh maintains", func(t *testing.T) {
		t.Parallel()
		for _, path := range fixtures.SupportedRefreshPaths() {
			// given / when
			accepted := entities.IsRefreshPath(path)

			// then
			assert.True(t, accepted, path)
		}
	})

	t.Run("should reject dependency output, settings, and unmaintained documents", func(t *testing.T) {
		t.Parallel()
		for _, path := range fixtures.ExcludedRefreshPaths() {
			// given / when
			accepted := entities.IsRefreshPath(path)

			// then
			assert.False(t, accepted, path)
		}
	})
}

func TestIsPairPath(t *testing.T) {
	t.Parallel()

	t.Run("should accept a nested CLAUDE.md or AGENTS.md", func(t *testing.T) {
		t.Parallel()
		// given
		paths := []string{"CLAUDE.md", "packages/api/AGENTS.md"}

		// when / then
		for _, path := range paths {
			assert.True(t, entities.IsPairPath(path), path)
		}
	})

	t.Run("should reject the .claude location and other files", func(t *testing.T) {
		t.Parallel()
		// given
		paths := []string{".claude/CLAUDE.md", "packages/web/.claude/AGENTS.md", "README.md"}

		// when / then
		for _, path := range paths {
			assert.False(t, entities.IsPairPath(path), path)
		}
	})
}

func TestIsAgentsImport(t *testing.T) {
	t.Parallel()

	t.Run("should accept either spelling of the import with surrounding whitespace", func(t *testing.T) {
		t.Parallel()
		// given
		texts := []string{entities.AgentsImport, "  @./AGENTS.md\n\n"}

		// when / then
		for _, text := range texts {
			assert.True(t, entities.IsAgentsImport(text), text)
		}
	})

	t.Run("should reject an import followed by guidance", func(t *testing.T) {
		t.Parallel()
		// given
		text := "@AGENTS.md\n\nUse the release skill.\n"

		// when
		accepted := entities.IsAgentsImport(text)

		// then
		assert.False(t, accepted)
	})
}

func TestHasSafePathCharacters(t *testing.T) {
	t.Parallel()

	t.Run("should accept a nested path with a space", func(t *testing.T) {
		t.Parallel()
		// given
		path := "some folder/AGENTS.md"

		// when
		safe := entities.HasSafePathCharacters(path)

		// then
		assert.True(t, safe)
	})

	t.Run("should reject rule delimiters, wildcards, and control characters", func(t *testing.T) {
		t.Parallel()
		// given
		paths := []string{"comma,name/AGENTS.md", "glob*/CLAUDE.md", "array[0]/AGENTS.md", "newline\nname/CLAUDE.md"}

		// when / then
		for _, path := range paths {
			assert.False(t, entities.HasSafePathCharacters(path), path)
		}
	})
}
