package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

func TestRenderRefreshPrompt(t *testing.T) {
	t.Parallel()

	t.Run("should keep every instruction file's rules when all are on", func(t *testing.T) {
		t.Parallel()
		// given
		parameters := entities.PromptParameters{Claude: true, Copilot: true, GuideURL: entities.DefaultGuideURL}

		// when
		prompt, err := entities.RenderRefreshPrompt(parameters)

		// then
		require.NoError(t, err)
		assert.NotContains(t, prompt, "{{")
		assert.Contains(t, prompt, "contains only the import line `@AGENTS.md`")
		assert.Contains(t, prompt, "## Rules for `.github/copilot-instructions.md`")
		assert.Contains(t, prompt, "## Rules for `.github/skills/code-review/SKILL.md`")
		assert.Contains(t, prompt, "<"+entities.DefaultGuideURL+">")
		assert.Contains(t, prompt, ".azuredevops/CONTRIBUTING.md")
		assert.NotContains(t, prompt, "refresh_scope.py")
	})

	t.Run("should drop the Copilot sections and record their removal when Copilot is off", func(t *testing.T) {
		t.Parallel()
		// given
		parameters := entities.PromptParameters{Claude: true, GuideURL: entities.DefaultGuideURL}

		// when
		prompt, err := entities.RenderRefreshPrompt(parameters)

		// then
		require.NoError(t, err)
		assert.NotContains(t, prompt, "{{")
		assert.NotContains(t, prompt, "## Rules for `.github/copilot-instructions.md`")
		assert.NotContains(t, prompt, "## Rules for `.github/skills/code-review/SKILL.md`")
		assert.Contains(t, prompt, "The secret-hygiene bullet has exactly one approved wording")
		assert.NotContains(t, prompt, "This exclusion does not apply")
		assert.Contains(t, prompt, "removed the GitHub Copilot instructions and code-review skill")
	})

	t.Run("should drop the import rule when the repository keeps no CLAUDE.md", func(t *testing.T) {
		t.Parallel()
		// given
		parameters := entities.PromptParameters{Copilot: true, GuideURL: entities.DefaultGuideURL}

		// when
		prompt, err := entities.RenderRefreshPrompt(parameters)

		// then
		require.NoError(t, err)
		assert.NotContains(t, prompt, "Claude Code reads it through the `CLAUDE.md` import")
		assert.Contains(t, prompt, "and removed \\`CLAUDE.md\\`")
	})

	t.Run("should leave the guide rule out when no guide is configured", func(t *testing.T) {
		t.Parallel()
		// given / when
		prompt, err := entities.RenderRefreshPrompt(entities.PromptParameters{Claude: true, Copilot: true})

		// then
		require.NoError(t, err)
		assert.NotContains(t, prompt, "Keep links to the development guide valid")
	})
}

func TestAuditPrompt(t *testing.T) {
	t.Parallel()

	t.Run("should run the bundled audit against the import-only rule", func(t *testing.T) {
		t.Parallel()
		// given / when
		prompt := entities.AuditPrompt("claude-opus-5-5", entities.ProviderGitHub.DefaultInstructionFiles())

		// then
		assert.Regexp(t, `^/checkup prompt-audit \.\n`, prompt)
		assert.Contains(t, prompt, "only the import line `@AGENTS.md`")
		assert.Contains(t, prompt, "and Copilot guidance")
		assert.Contains(t, prompt, "under 200 lines")
		assert.Contains(t, prompt, "Target model: claude-opus-5-5")
	})

	t.Run("should report a remaining CLAUDE.md when the repository keeps none", func(t *testing.T) {
		t.Parallel()
		// given / when
		prompt := entities.AuditPrompt("claude-opus-5-5", entities.InstructionFiles{Agents: true})

		// then
		assert.NotContains(t, prompt, "only the import line")
		assert.NotContains(t, prompt, "Copilot")
		assert.Contains(t, prompt, "keeps no CLAUDE.md")
	})
}

func TestRefreshPrompt(t *testing.T) {
	t.Parallel()

	t.Run("should list the manifest and the audit findings after the repository prompt", func(t *testing.T) {
		t.Parallel()
		// given
		scope := entities.RefreshScope{
			Existing: []string{"README.md", "packages/api/AGENTS.md"}, Creatable: []string{"AGENTS.md"},
			Managed: []string{"packages/api/CLAUDE.md"}, Removed: []string{".github/copilot-instructions.md"},
		}
		repository := entities.RepositoryPrompt("Base prompt.", entities.ChangelogNone, "No host changes.")

		// when
		prompt := entities.RefreshPrompt(repository, scope, "AGENTS.md:4: stale command")

		// then
		assert.Regexp(t, `^Base prompt\.\n\n---\n\n## Changelog convention for this repository\n`, prompt)
		assert.Contains(t, prompt, "## Guidance layout for this repository\n\nNo host changes.")
		assert.Contains(t, prompt, "Existing files eligible for edits:\n- README.md\n- packages/api/AGENTS.md")
		assert.Contains(t, prompt, "Files that may be created when useful:\n- AGENTS.md")
		assert.Contains(t, prompt, "must not be recreated:\n- .github/copilot-instructions.md")
		assert.Contains(t, prompt, "## Prompt-audit findings (review data)\n\nAGENTS.md:4: stale command")
		assert.NotContains(t, prompt, "packages/api/CLAUDE.md", "a host-managed import was offered for edits")
	})
}
