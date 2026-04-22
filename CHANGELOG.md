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

### Changed

- renamed the `repositories.RepositoriesRepository` port to `repositories.Repository` to remove the package-name stutter flagged by `revive`
- converted `entities.DesiredRepoSettings` and `entities.DesiredWikiAllowlist` from package-level variables to functions, keeping the compliance policy immutable from call sites
- changed the Go version to `1.26.2` and updated all module dependencies

### Fixed

- fixed all `golangci-lint` findings surfaced by CI on the `0.1.0` bump PR: `forbidigo` (table output now uses `fmt.Fprintf(os.Stdout, ...)` instead of `fmt.Print*`), `goconst` (extracted `SecurityStateEnabled`/`SecurityStateDisabled`/`SecurityStateUnknown`), `mnd` (named phase constants `phaseAudit`, `phaseApplyRepo`, `phaseApplySecurity`, `phaseApplyProtection`, `phaseReport`, `exitUsageError`, `secretColumnWidth`, `tableWidth`, `githubListPerPage`), `govet` shadow (renamed inner `err` shadows), `nilnil` (replaced `return nil, nil` with a new `repositories.ErrRulesetNotFound` sentinel handled by `AuditRepositoriesCommand`), `gocognit`/`nestif` (extracted per-concern helpers in `ApplyBranchProtectionCommand`, `ApplySecuritySettingsCommand`, `AuditResult.ComputeIssues`, `mapRulesetToEntity`, `printAuditTable`, and `diffAudits`), and `funlen` (split `diffAudits` into `diffRepoSettings`, `diffSecurity`, `diffBranchProtection`, `diffRuleset`)

## [0.1.0] - 2026-04-21

### Added

- added `.github/copilot-instructions.md`, the AI-assistant context file summarizing the project's architecture, Clean Architecture invariants, build/test/lint commands, environment variables, and policy-change workflow so Copilot / Cursor / Claude Code have consistent grounding without reloading the whole codebase
- added `.github/workflows/ai-docs-refresh.yaml`, the weekly matrix workflow that runs `anthropics/claude-code-action@v1` against every non-fork non-archived `rios0rios0` repo to refresh `CLAUDE.md` and `.github/copilot-instructions.md` and opens a drift PR on `chore/ai-docs-refresh` (migrated from `rios0rios0/.github`)
- added `.github/workflows/repo-compliance-audit.yaml`, the daily scheduled workflow that runs the Go `harden-repos` CLI with `--phase 1 --fail-on-noncompliant` and fails CI when any `rios0rios0` repo drifts from the compliance policy (originally migrated from `rios0rios0/.github` as a Python script, then ported to Go)
- added `.golangci.yaml`, `.gitignore`, and `go.mod` (Go 1.26) with the team-standard linter baseline
- added `cmd/harden-repos/`, a Go CLI following Clean Architecture that enforces repo settings, Dependabot, secret scanning, branch protection, and the `main-protection` ruleset across every `rios0rios0` GitHub repository — supports phases 1-5, `--list-json`, `--dry-run`, `--repo` filter, and `--fail-on-noncompliant`
- added `internal/domain/commands/` with one command per phase (`AuditRepositoriesCommand`, `ApplyRepositorySettingsCommand`, `ApplySecuritySettingsCommand`, `ApplyBranchProtectionCommand`, `ListTargetRepositoriesCommand`, `ReportComplianceChangesCommand`) — each command exposes a listeners struct that maps outcomes to the CLI (controller) layer
- added `internal/domain/entities/` covering `Repository`, `SecuritySettings`, `BranchProtection`, `Ruleset`, and `AuditResult`, with `compliance_policy.go` as the single source of truth for every policy constant (`DesiredRepoSettings`, `DesiredWikiAllowlist`, `DesiredReviewCount`, `DesiredRulesetName`, `DesiredDefaultBranch`, `RepositoryAdminActorType`/`ID`)
- added `internal/domain/repositories/` with three small port interfaces (`Repository`, `SecuritySettingsRepository`, `BranchProtectionsRepository`) so the domain layer never imports the `github.com/google/go-github/v66` SDK
- added `internal/infrastructure/repositories/` with three `GoGithub…Repository` adapters wrapping `github.com/google/go-github/v66` + `golang.org/x/oauth2`
- added `Makefile` with `build`, `run`, `test`, `lint`, `sast`, `setup`, and `clean` targets; `sast` delegates to the SAST toolchain in `rios0rios0/pipelines` per `.claude/rules/ci-cd.md`
- added `README.md`, `CLAUDE.md`, `CONTRIBUTING.md`, `LICENSE`, and `.editorconfig` to bootstrap the repository
- added `scripts/refresh_ai_docs_prompt.md`, the prompt consumed by the refresh workflow that instructs Claude Code to cover both AI-assistant guidance files, record any refresh in `CHANGELOG.md`, and make no edits when the existing files are accurate
- added Uber Dig dependency injection across every layer (`internal/domain/commands/container.go`, `internal/domain/entities/container.go` no-op, `internal/infrastructure/repositories/container.go`) orchestrated by `internal/container.go` and invoked from `cmd/harden-repos/dig.go`
- added unit tests for every command under the `//go:build unit` tag using the `_test` external package, `t.Parallel()`, BDD `// given / // when / // then` blocks, and in-memory doubles preferred over mocks per `.claude/rules/testing.md`; `test/domain/builders/` hosts fluent `RepositoryBuilder` and `AuditResultBuilder` factories and `test/domain/doubles/repositories/` hosts the per-port in-memory doubles

### Changed

- changed `ai-docs-refresh.yaml` to self-checkout `rios0rios0/fleet-maintenance` and read `scripts/refresh_ai_docs_prompt.md` locally instead of fetching it from `rios0rios0/.github` via `gh api`, removing the last hardcoded cross-repo dependency and one network round-trip per refresh
- expanded the `anthropics/claude-code-action@v1` allowlist in `ai-docs-refresh.yaml` to include `Edit(/CHANGELOG.md)` and `Write(/CHANGELOG.md)` so Claude can record every AI-docs refresh in the target repo's changelog
- switched both workflows from `actions/setup-python@v6` + `python3` to `actions/setup-go@v6` + `go run ./cmd/harden-repos` so the scheduled jobs exercise the same Go binary the team maintains locally
- updated `scripts/refresh_ai_docs_prompt.md` to require Claude to add a short `[Unreleased]` entry to the target repo's `CHANGELOG.md` whenever it edits `CLAUDE.md` or `.github/copilot-instructions.md`, and to skip the entry when the target repo has no changelog
- widened the drift-detection step in `ai-docs-refresh.yaml` to stage `CHANGELOG.md` alongside the AI-docs files while keeping the diff gate scoped to the AI docs, so a stray CHANGELOG-only edit cannot open a spurious PR

### Removed

- removed `scripts/harden_repos.py` (superseded by the Go CLI at `cmd/harden-repos/`). The Go port preserves every carve-out from the Python original: fork exclusion for Dependabot and secret scanning, private-repo skip for `AllowAutoMerge`, `secret_scanning`, branch protection, and the ruleset, `DesiredWikiAllowlist` for legitimate wiki repos, and the tri-state distinction between `dependabot_alerts=unknown` and `dependabot_alerts=off`

