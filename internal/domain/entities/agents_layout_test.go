package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

func TestWithoutAgentsImports(t *testing.T) {
	t.Parallel()

	t.Run("should drop import lines outside code blocks", func(t *testing.T) {
		t.Parallel()
		// given
		text := "@AGENTS.md\n\nUse the release skill.\n@./AGENTS.md\n"

		// when
		result := entities.WithoutAgentsImports(text)

		// then
		assert.Equal(t, "\nUse the release skill.", result)
	})

	t.Run("should keep import examples inside fenced code blocks", func(t *testing.T) {
		t.Parallel()
		// given
		text := "```markdown\n@AGENTS.md\n```\n~~~\n```\n@AGENTS.md\n```\n~~~\n````\n```\n@AGENTS.md\n````"

		// when
		result := entities.WithoutAgentsImports(text)

		// then
		assert.Equal(t, text, result)
	})
}

func TestContainsLines(t *testing.T) {
	t.Parallel()

	t.Run("should find text repeated line for line, ignoring blank lines and trailing spaces", func(t *testing.T) {
		t.Parallel()
		// given
		text := "# Guidance\n\nRun make test.   \n\nUse Go.\n"

		// when
		found := entities.ContainsLines(text, "Run make test.\nUse Go.")

		// then
		assert.True(t, found)
	})

	t.Run("should not find a line that only appears inside a longer one", func(t *testing.T) {
		t.Parallel()
		// given
		text := "# Guidance\n\nDo not run make test.\n"

		// when
		found := entities.ContainsLines(text, "run make test.")

		// then
		assert.False(t, found)
	})

	t.Run("should not find lines that appear in another order", func(t *testing.T) {
		t.Parallel()
		// given
		text := "Use Go.\nRun make test.\n"

		// when
		found := entities.ContainsLines(text, "Run make test.\nUse Go.")

		// then
		assert.False(t, found)
	})
}

func TestMergedAgentsGuidance(t *testing.T) {
	t.Parallel()

	t.Run("should append what CLAUDE.md adds under the migrated heading", func(t *testing.T) {
		t.Parallel()
		// given
		claude := entities.LegacyBanner + "\n@AGENTS.md\n\nUse the release skill.\n"

		// when
		merged, changed := entities.MergedAgentsGuidance("# API\n\nShared facts.\n\n", claude)

		// then
		assert.True(t, changed)
		assert.Equal(t, "# API\n\nShared facts.\n\n## Migrated from CLAUDE.md\n\nUse the release skill.\n", merged)
	})

	t.Run("should leave AGENTS.md alone when it already holds the text", func(t *testing.T) {
		t.Parallel()
		// given
		agents := "# Guidance\n\nRun make test.\n"

		// when
		merged, changed := entities.MergedAgentsGuidance(agents, "# Guidance\n\nRun make test.\n")

		// then
		assert.False(t, changed)
		assert.Equal(t, agents, merged)
	})
}

func TestWithAgentsBanner(t *testing.T) {
	t.Parallel()

	t.Run("should replace the legacy banner", func(t *testing.T) {
		t.Parallel()
		// given
		text := entities.LegacyBanner + "\nGuidance.\n"

		// when
		result, changed := entities.WithAgentsBanner(text)

		// then
		assert.True(t, changed)
		assert.Equal(t, entities.AgentsBanner+"\nGuidance.\n", result)
	})

	t.Run("should keep any other start", func(t *testing.T) {
		t.Parallel()
		// given
		text := "# Project\n"

		// when
		result, changed := entities.WithAgentsBanner(text)

		// then
		assert.False(t, changed)
		assert.Equal(t, text, result)
	})
}

func TestSameLinkTarget(t *testing.T) {
	t.Parallel()

	t.Run("should match the sibling written with a dot-slash or repeated slashes", func(t *testing.T) {
		t.Parallel()
		// given
		targets := []string{"CLAUDE.md", "./CLAUDE.md", ".//CLAUDE.md"}

		// when / then
		for _, target := range targets {
			assert.True(t, entities.SameLinkTarget(target, entities.ClaudeFile), target)
		}
	})

	t.Run("should not resolve parent directories", func(t *testing.T) {
		t.Parallel()
		// given
		targets := []string{"../parent/CLAUDE.md", "x/../CLAUDE.md", "/CLAUDE.md"}

		// when / then
		for _, target := range targets {
			assert.False(t, entities.SameLinkTarget(target, entities.ClaudeFile), target)
		}
	})
}

func TestAgentsLayoutReport(t *testing.T) {
	t.Parallel()

	t.Run("should describe a repository already in the layout", func(t *testing.T) {
		t.Parallel()
		// given
		report := entities.NewAgentsLayoutReport()

		// when
		note := entities.DescribeAgentsLayout(report)

		// then
		assert.Contains(t, note, "No changes")
	})

	t.Run("should describe every recorded path in reporting order", func(t *testing.T) {
		t.Parallel()
		// given
		report := entities.NewAgentsLayoutReport()
		report.Record(entities.OutcomeSkipped, "legacy/CLAUDE.md")
		report.Record(entities.OutcomeMerged, "CLAUDE.md")
		report.Record(entities.OutcomeKept, "tools/CLAUDE.md")

		// when
		note := entities.DescribeAgentsLayout(report)

		// then
		assert.Equal(t, []string{"CLAUDE.md"}, report.Merged)
		assert.Equal(t, "- Appended the former `CLAUDE.md` text to the `AGENTS.md` beside it under "+
			"`## Migrated from CLAUDE.md`, then reduced `CLAUDE.md` to the import line.\n"+
			"- Left `legacy/CLAUDE.md` and its `AGENTS.md` unchanged (a symlink to another file, "+
			"an ignored path, or not UTF-8 text); report it for human review.", note)
	})
}
