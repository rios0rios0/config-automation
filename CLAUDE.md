# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Repository Is

This repo houses **scheduled, cross-repo maintenance workflows** that run from a central location and act on every `rios0rios0` repository, plus the Go CLI they drive:

1. **`.github/workflows/repo-compliance-audit.yaml`** — daily (06:00 UTC) audit that runs `go run ./cmd/harden-repos --phase 1 --fail-on-noncompliant` against every `rios0rios0` repo. Fails if any repo drifts from the compliance policy. Uploads `/tmp/gh_hardening_audit_before.json` as an artifact.

2. **`.github/workflows/config-and-docs-refresh.yaml`** — weekly (Mondays 07:00 UTC) **batched-matrix** workflow. Named for the broader scope (configuration + documentation) so future targets — diagrams, additional config files — can be added without renaming; today the in-scope set is the AI-assistant guidance files. The `discover` job calls `go run ./cmd/harden-repos --list-json` to enumerate non-fork non-archived repos, then chunks the sorted list into fixed-size batches (default `batch_size: 10`, overridable per-run via `workflow_dispatch`). The `refresh` job is a matrix with one leg per batch (`max_parallel: 2` by default); each leg installs `@anthropic-ai/claude-code` via `npm`, self-checks-out this repo to load `scripts/refresh_config_and_docs_prompt.md`, and then loops through its batch internally — cloning each target repo into a scratch directory, invoking the Claude Code CLI with a tool allowlist locked to `Read,Grep,Glob,Edit(/CLAUDE.md),Edit(/.github/copilot-instructions.md),Edit(/CHANGELOG.md),Write(/CLAUDE.md),Write(/.github/copilot-instructions.md),Write(/CHANGELOG.md)`, detecting drift, and opening a PR. The `claude` invocation is fed `</dev/null` — load-bearing because otherwise `claude -p` inherits the outer `while read` loop's stdin (the `jq` pipe) and silently drains the rest of the batch after the first repo. Its output is also tee'd to `${WORK_DIR}/.claude.log` so the loop can detect the org-wide `monthly usage limit` message and short-circuit the rest of the batch (each surviving repo is reported on a `quota_skipped` line in the summary instead of burning ~3min/repo against an exhausted quota). Drift detection uses `git add -N` (intent-to-add) followed by `git diff -w --quiet` so modified and newly-created in-scope files both count; whitespace-only diffs are ignored. `CHANGELOG.md` is staged alongside the in-scope files (so Claude's release-note entry lands in the same PR) but is intentionally **not** part of the drift gate — a stray CHANGELOG-only edit cannot open a spurious PR. Branch name is stable (`chore/config-and-docs-refresh`) and force-pushed so repeated runs update a single open PR. Each leg prints a `==== Batch summary ====` footer listing `no_drift / prs_created / prs_updated / quota_skipped / failed` and exits non-zero only if at least one repo failed (`quota_skipped` does not by itself fail the leg, but the quota-hitting repo is recorded as `claude-quota:` in `failed`, so a quota event still surfaces as a red leg).

3. **`cmd/harden-repos/`** — the Go CLI that implements the compliance policy. Understanding its architecture is required before editing.

## The `harden-repos` CLI

Clean Architecture split:

- **`internal/domain/entities/`** — framework-agnostic data types: `Repository`, `SecuritySettings`, `BranchProtection`, `Ruleset`, `AuditResult`. `compliance_policy.go` exposes the policy every other package references: `DesiredRepoSettings()` and `DesiredWikiAllowlist()` are functions (returning fresh values so the policy stays immutable at call sites); `DesiredReviewCount`, `DesiredRulesetName`, `DesiredDefaultBranch`, and `RepositoryAdminActorType`/`ID` remain constants. `AuditResult.ComputeIssues()` is the single source of truth for policy compliance and encodes every carve-out (forks skip Dependabot + secret scanning, private repos skip secret scanning + branch protection + rulesets, `DesiredWikiAllowlist()` repos keep `has_wiki=true`, `AllowAutoMerge=true` is skipped on private repos because GitHub Free silently ignores the PATCH).
- **`internal/domain/repositories/`** — three ports: `Repository`, `SecuritySettingsRepository`, `BranchProtectionsRepository`. Each is a small interface with the methods the commands actually need. The domain layer never imports `github.com/google/go-github`. `BranchProtectionsRepository.FindRulesetByName` returns the sentinel `repositories.ErrRulesetNotFound` when no ruleset is configured; callers treat it as "no ruleset" rather than propagating it as an error.
- **`internal/domain/commands/`** — one command per phase plus `--list-json`: `AuditRepositoriesCommand`, `ApplyRepositorySettingsCommand`, `ApplySecuritySettingsCommand`, `ApplyBranchProtectionCommand`, `ListTargetRepositoriesCommand`, `ReportComplianceChangesCommand`. Every command exposes a listeners struct (`OnSuccess`, `OnError`, `OnChange`, etc.) so the CLI layer (which plays the controller role) maps outcomes to stdout and exit codes.
- **`internal/infrastructure/repositories/`** — `GoGithub…Repository` adapters that implement the ports by calling `github.com/google/go-github/v66`. Each also handles the HTTP-status idioms the Python original encoded: 204 vs 404 for vulnerability alerts, 403/404 for branch protection unavailability, rate limits, etc.
- **`cmd/harden-repos/main.go`** — parses flags, dispatches to commands with listeners that print via Logrus and set exit codes.

Key invariants to preserve when editing:

- `Repository.FindAllByOwner` branches on whether the owner equals the authenticated login (then `/user/repos` retains private visibility) and on `OwnerKind` (`User` vs `Organization`). Keep all three paths in sync.
- `SecuritySettingsRepository.FindByRepositoryName` returns `DependabotAlerts *bool` — nil means "unknown" (API failure), pointer-to-false means "disabled". `AuditResult.ComputeIssues` distinguishes the two; don't collapse them.
- Ruleset compliance is a three-part check: name match, `non_fast_forward` rule, and `refs/heads/main` in the ref-name include list. A name-only match is not compliant (see `Ruleset.IsCompliant`).
- `RepositoryAdminActorType`/`RepositoryAdminActorID` (from `entities/compliance_policy.go`) stays in every ruleset's `BypassActors` so the owner can force-push when needed.
- `AuditResult.ComputeIssues` is the policy. Every command that decides whether to mutate consults the audit, not the live API — so phases 2/3/4 are re-reading a single audit list, not round-tripping to GitHub per repo.

Environment variables:

- `HARDEN_OWNER` (default: `rios0rios0`) — GitHub owner/org to audit.
- `GH_TOKEN` / `GITHUB_TOKEN` — bearer token for `github.com/google/go-github`. Both workflows set this from `secrets.COMPLIANCE_AUDIT_TOKEN` (audit) or `secrets.CLAUDE_MD_REFRESH_TOKEN` (refresh discover job).
- `TMPDIR` — respected by `os.TempDir()` for the audit snapshot paths, so the binary runs on hosts where `/tmp` is not writable.

## Build / Test / Lint

```bash
make build                          # compile bin/harden-repos
make test                           # go test -race -tags=unit ./...
make lint                           # golangci-lint run ./...
make sast                           # run the full SAST suite from rios0rios0/pipelines
go test -tags=unit -run TestAuditRepositoriesCommand ./internal/domain/commands/
```

Test files use `//go:build unit`, the `_test` package suffix (external tests), `t.Parallel()` on every top-level test function, BDD-style `// given / // when / // then` blocks, and the in-memory doubles in `test/domain/doubles/repositories/` rather than interface mocks.

## Conventions Specific to This Repo

- **YAML files use `.yaml`** (not `.yml`), with string values single-quoted except where interpolation requires double quotes.
- **Go conventions** follow `.claude/rules/golang.md` in the user's global rules: `snake_case` file names, one-letter receiver names (`c` for Command, `r` for Repository), Uber Dig for DI, Logrus for logging, testify for tests, no framework tags on entities.
- **Actions pins:** keep every workflow on the same latest major (currently `actions/checkout@v6`, `actions/upload-artifact@v7`, `actions/setup-go@v6`, `actions/setup-node@v6`). When bumping, bump across both workflows in the same commit. The `@anthropic-ai/claude-code` npm package is pinned implicitly to `latest` via `npm install -g`; rely on the CLI's own version skew tolerance rather than pinning a specific version.
- **Changelog discipline:** every change goes under `[Unreleased]` in `CHANGELOG.md` in the same commit. Keep a Changelog format, simple past tense, backticks around code identifiers.
- **Commits:** `type(SCOPE): message` in simple past tense, no trailing period. See `.claude/rules/git-flow.md` in the user's global rules.
- **Ruleset / branch-protection changes are load-bearing.** Every `rios0rios0` repo inherits the same policy; a change here propagates to all of them on the next audit run.

## Related Repositories

- [`rios0rios0/.github`](https://github.com/rios0rios0/.github) — community health fallback files, workflow templates, and reusable Claude Code workflows. Community health changes belong there, not here.
- [`rios0rios0/pipelines`](https://github.com/rios0rios0/pipelines) — reusable workflows consumed by the workflow templates in `.github`.
- [`rios0rios0/autobump`](https://github.com/rios0rios0/autobump) — releases `CHANGELOG.md` entries into versioned sections.
- [`rios0rios0/guide`](https://github.com/rios0rios0/guide/wiki) — canonical development standards.
