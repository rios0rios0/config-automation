Review the in-scope configuration and documentation files in this repository against the actual code and update them **only if they have meaningfully drifted** from the current state of the codebase. The host workflow (`config-and-docs-refresh.yaml`) is intentionally named for the broader scope so additional refresh targets (diagrams, more config files) can be added later by extending this prompt and the workflow's `drift_paths` together.

Today the in-scope set is the AI-assistant guidance files only. Three files are in scope, all optional:

- `CLAUDE.md` at the repo root — guidance for Claude Code sessions.
- `.github/copilot-instructions.md` — guidance for GitHub Copilot sessions.
- `.github/skills/code-review/SKILL.md` — the repository's tailored GitHub Copilot code-review skill.

## Your task

1. **Read each file if it exists.** Start with whichever already exists. Do not create a file unless the repository clearly benefits from it.
2. **Skim the repo** to gather truth:
   - `README.md`, `CONTRIBUTING.md`, `CHANGELOG.md` (recent `[Unreleased]` entries are often the most reliable signal of drift).
   - The manifest/build files that define the project's language and commands: `package.json`, `pyproject.toml`, `go.mod`, `build.gradle`, `Makefile`, `Taskfile.yaml`, `Dockerfile`.
   - Top-level source directories to get a feel for architecture.
   - Any `.github/workflows/` files if CI commands are documented.
3. **Compare** each existing file against that reality.
4. **Decide, per file:**
   - If every factual claim still holds and nothing materially new has been added, **make no edits to that file**.
   - If a claim is wrong, a load-bearing piece of context is missing, or a documented command no longer works, **rewrite the affected sections only**. Keep the rest intact.
   - If the file does not exist but the repo would clearly benefit (it has custom build commands, non-obvious architecture, or specific conventions), create it following the structure below. If the repo is trivial or the existing `README.md` already covers everything, do not create the file.

## Rules for `CLAUDE.md`

- **Always start with this banner** (exact text) when creating:
  ```
  # CLAUDE.md

  This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.
  ```

## Rules for `.github/copilot-instructions.md`

- The file is loaded automatically by GitHub Copilot Chat and inlined into prompts. Keep it focused and under a few hundred lines.
- No banner is required — start directly with content.
- Content should overlap in substance with `CLAUDE.md` (both serve the same purpose for different assistants) but phrase it for GitHub Copilot's context model. Do not duplicate text verbatim — cross-reference or summarize.

## Rules for `.github/skills/code-review/SKILL.md`

- This is a [GitHub Copilot agent skill](https://docs.github.com/en/copilot/concepts/agents/about-agent-skills). It teaches Copilot **how a change in this repository is judged**, where `.github/copilot-instructions.md` teaches it **how the code works**. Keep that division; do not let the two converge into the same file twice.
- **The YAML frontmatter is load-bearing.** `name` must stay `code-review` (lowercase, matching the directory) and `description` must stay a single quoted line naming this repository and when Copilot should reach for the skill. A frontmatter that fails to parse silently disables the skill.
- **Preserve the section skeleton** when editing: when to use it, source of truth and precedence, how to run the review, the repo-specific checklist, the shared guide rules, what not to flag, the output format, and the severity table. Rewrite the affected sections only.
- **The repo-specific checklist is the part that drifts.** It names concrete files, functions, invariants, and commands. Re-verify each bullet against the current code and fix the ones that no longer hold — a stale invariant makes Copilot flag correct code. Delete a bullet whose subject no longer exists rather than inventing a replacement.
- **Verify the commands.** The commands block must match the repository's actual `Makefile`, `package.json` scripts, or build tooling, including whether a `sast` target exists at all.
- **Keep the guide links valid.** They point at <https://github.com/rios0rios0/guide/wiki>, whose pages are flat — the page name is the source file's base name, so `Life-Cycle/Git-Flow.md` is `/wiki/Git-Flow`. Only link pages that exist, and only ones relevant to this repository's stack.
- **Keep "what not to flag" honest.** For preserved or discontinued projects it must keep saying that modernising the code is out of scope; removing that turns every weekly review into noise.
- Do not create this file for a repository that has no `.github/copilot-instructions.md` and no clear conventions of its own — a generic review skill is worse than none.

## Shared rules for what goes into any of these files

- Focus on the **big picture** that takes reading multiple files to understand: architectural invariants, dependency direction, non-obvious coupling between modules.
- Include **build / test / lint commands** that are commonly used, including how to run a single test.
- Include **conventions specific to this repo** — things a reader would get wrong by following generic best practices.

## What NOT to include

- Generic development advice ("write tests", "use meaningful names", "handle errors").
- Obvious file-structure descriptions that `ls` would reveal.
- Made-up sections like "Common Development Tasks", "Tips for Development", or "Support and Documentation" unless they already exist in the repo's own docs.
- Restatements of what `README.md` already covers well — link or summarize, don't duplicate.
- Per-language conventions that come from the user's global rules (those are already in the assistant's context). **This exclusion does not apply to `.github/skills/code-review/SKILL.md`**: a Copilot review running on a pull request has none of those global rules loaded, which is exactly why the skill restates them and links the guide pages they come from.

## Commit discipline

- If and only if you modify `CLAUDE.md`, `.github/copilot-instructions.md`, or `.github/skills/code-review/SKILL.md`, the host workflow will detect the diff and open a PR. You do not need to run git commands yourself.
- If you decide all three files are accurate (or should not be created), do nothing. Weekly no-op runs are expected and correct.
- **If (and only if) you modified one of the three in-scope files, also add a short entry to `CHANGELOG.md` under the `[Unreleased]` section describing the refresh.** Use `### Changed` for edits to existing files and `### Added` if you created one of them. Write the entry in simple past tense, starting with a lowercase verb, and wrap file names in backticks — example: `- refreshed \`CLAUDE.md\` to document the new \`make test-integration\` target`. If `CHANGELOG.md` does not exist in the repo, skip this step — do not create one. If the `[Unreleased]` section does not exist, add it immediately above the most recent version heading.
- Never edit any file other than `CLAUDE.md`, `.github/copilot-instructions.md`, `.github/skills/code-review/SKILL.md`, or (when the above rule applies) `CHANGELOG.md`. Never run destructive commands. Never push, tag, or merge.

## Tone

- Terse and declarative. Short sentences. No filler.
- Match the style of the existing file if one is present.
- When in doubt about a claim, leave it out rather than guess.
