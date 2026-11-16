package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

func TestInstructionFiles(t *testing.T) {
	t.Parallel()

	t.Run("should offer AGENTS.md and the Copilot files for creation when all are on", func(t *testing.T) {
		t.Parallel()
		// given
		files := entities.InstructionFiles{Agents: true, Claude: true, Copilot: true}

		// when
		paths := files.CreatablePaths()

		// then
		assert.Equal(t, []string{"AGENTS.md", ".github/copilot-instructions.md", ".github/skills/code-review/SKILL.md"},
			paths)
	})

	t.Run("should offer nothing for creation when AGENTS.md and Copilot are off", func(t *testing.T) {
		t.Parallel()
		// given
		files := entities.InstructionFiles{Claude: true}

		// when
		paths := files.CreatablePaths()

		// then
		assert.Empty(t, paths)
	})

	t.Run("should remove exactly the Copilot instructions and review skill when Copilot is off", func(t *testing.T) {
		t.Parallel()
		// given
		files := entities.InstructionFiles{Agents: true, Claude: true}

		// when / then
		for _, removed := range []string{
			".github/copilot-instructions.md", ".github/skills/code-review/SKILL.md",
			".github/skills/code-review/references/go.md",
		} {
			assert.True(t, files.IsRemovedPath(removed), removed)
		}
		for _, kept := range []string{
			".github/instructions/go.instructions.md", ".github/prompts/check.prompt.md",
			".github/skills/review/SKILL.md", ".github/skills/code-review-extra/SKILL.md",
			"CLAUDE.md", "AGENTS.md",
		} {
			assert.False(t, files.IsRemovedPath(kept), kept)
		}
	})

	t.Run("should remove paired CLAUDE.md files but not .claude/CLAUDE.md when Claude is off", func(t *testing.T) {
		t.Parallel()
		// given
		files := entities.InstructionFiles{Agents: true, Copilot: true}

		// when / then
		assert.True(t, files.IsRemovedPath("CLAUDE.md"))
		assert.True(t, files.IsRemovedPath("packages/api/CLAUDE.md"))
		assert.False(t, files.IsRemovedPath(".claude/CLAUDE.md"))
		assert.False(t, files.IsRemovedPath("AGENTS.md"))
		assert.False(t, files.IsRemovedPath(".github/copilot-instructions.md"))
	})
}
