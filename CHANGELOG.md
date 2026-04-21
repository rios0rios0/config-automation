# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

When a new release is proposed:

1. Create a new branch `bump/x.x.x` (this isn't a long-lived branch!!!);
2. The Unreleased section on `CHANGELOG.md` gets a version number and date;
3. Open a Pull Request with the bump version changes targeting the `main` branch;
4. When the Pull Request is merged, a new Git tag must be created using <LINK TO THE PLATFORM TO OPEN THE PULL REQUEST>.

Releases to productive environments should run from a tagged version.
Exceptions are acceptable depending on the circumstances (critical bug fixes that can be cherry-picked, etc.).

## [Unreleased]

### Fixed

- fixed `scripts/harden_repos.py` writing audit snapshots to a hardcoded `/tmp/` path that fails on hosts where `/tmp` is not writable (Termux, some sandboxes); the script now derives the path from `tempfile.gettempdir()`, which honors `TMPDIR` and still resolves to `/tmp` on Ubuntu runners so CI artifact paths are unchanged

## [1.0.0] - 2026-04-21

### Added

- added `.github/workflows/repo-compliance-audit.yaml`, the daily scheduled workflow that runs `scripts/harden_repos.py --phase 1 --fail-on-noncompliant` and fails CI when any `rios0rios0` repo drifts from the compliance policy (migrated from `rios0rios0/.github`)
- added `.github/workflows/ai-docs-refresh.yaml`, the weekly matrix workflow that runs `anthropics/claude-code-action@v1` against every non-fork non-archived `rios0rios0` repo to refresh `CLAUDE.md` and `.github/copilot-instructions.md` and opens a drift PR on `chore/ai-docs-refresh` (migrated from `rios0rios0/.github`)
- added `scripts/harden_repos.py`, the compliance audit and hardening script that enforces repo settings, Dependabot, secret scanning, branch protection, and the `main-protection` ruleset across every `rios0rios0` GitHub repository (migrated from `rios0rios0/.github`)
- added `scripts/refresh_ai_docs_prompt.md`, the prompt consumed by the refresh workflow that instructs Claude Code to cover both AI-assistant guidance files, record any refresh in `CHANGELOG.md`, and make no edits when the existing files are accurate
- added `README.md`, `CLAUDE.md`, `CONTRIBUTING.md`, `LICENSE`, and `.editorconfig` to bootstrap the repository

### Changed

- changed `ai-docs-refresh.yaml` to self-checkout `rios0rios0/fleet-maintenance` and read `scripts/refresh_ai_docs_prompt.md` locally instead of fetching it from `rios0rios0/.github` via `gh api`, removing the last hardcoded cross-repo dependency and one network round-trip per refresh
- expanded the `anthropics/claude-code-action@v1` allowlist in `ai-docs-refresh.yaml` to include `Edit(/CHANGELOG.md)` and `Write(/CHANGELOG.md)` so Claude can record every AI-docs refresh in the target repo's changelog
- updated `scripts/refresh_ai_docs_prompt.md` to require Claude to add a short `[Unreleased]` entry to the target repo's `CHANGELOG.md` whenever it edits `CLAUDE.md` or `.github/copilot-instructions.md`, and to skip the entry when the target repo has no changelog
- widened the drift-detection step in `ai-docs-refresh.yaml` to stage `CHANGELOG.md` alongside the AI-docs files while keeping the diff gate scoped to the AI docs, so a stray CHANGELOG-only edit cannot open a spurious PR
