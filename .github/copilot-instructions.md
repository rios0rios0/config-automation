# GitHub Copilot Instructions — fleet-maintenance

This file gives AI assistants (GitHub Copilot, Cursor, Claude Code) the minimum context needed to work in this repository without first reading the whole codebase. Keep it in sync with `CLAUDE.md` and `README.md`; any change to the compliance policy, CLI flags, workflows, or build commands must be reflected here in the same commit.

## Project Purpose

`fleet-maintenance` runs scheduled, cross-repo maintenance against every [`rios0rios0`](https://github.com/rios0rios0) repository:

1. **Daily compliance audit** — `.github/workflows/repo-compliance-audit.yaml` runs `go run ./cmd/harden-repos --phase 1 --fail-on-noncompliant` and fails CI when any repo drifts from the hardening policy. Uploads `${TMPDIR:-/tmp}/gh_hardening_audit_before.json` as an artifact.
2. **Weekly AI docs refresh** — `.github/workflows/ai-docs-refresh.yaml` enumerates non-fork non-archived repos via `go run ./cmd/harden-repos --list-json`, checks out this repo to load `scripts/refresh_ai_docs_prompt.md`, and invokes `anthropics/claude-code-action@v1` against each target. Drift detection uses `git add -N` + `git diff -w --quiet` on `CLAUDE.md` and `.github/copilot-instructions.md`; `CHANGELOG.md` is staged with them but excluded from the gate. Branch name `chore/ai-docs-refresh` is force-pushed to keep one open PR per repo.
3. **`cmd/harden-repos/`** — the Go CLI that implements the compliance policy and all phase commands.

## Architecture

Clean Architecture with `domain` (contracts) / `infrastructure` (implementations) split. Dependencies always point inward; the domain layer never imports `github.com/google/go-github`.

```
fleet-maintenance/
├── cmd/
│   └── harden-repos/               # CLI entry point + Uber Dig wiring (`main.go`, `dig.go`)
├── internal/
│   ├── container.go                # top-level DI orchestrator
│   ├── domain/
│   │   ├── commands/               # one command per phase + `--list-json` + `--dry-run`
│   │   ├── entities/               # `Repository`, `AuditResult`, `ComplianceIssue`, `compliance_policy.go`
│   │   └── repositories/           # three port interfaces (repos, security, branch protection)
│   └── infrastructure/
│       └── repositories/           # `GoGithub…Repository` adapters over `github.com/google/go-github/v66`
├── test/
│   └── domain/
│       ├── builders/               # `RepositoryBuilder`, `AuditResultBuilder`
│       └── doubles/repositories/   # in-memory doubles preferred over `testify/mock`
├── .github/workflows/              # `repo-compliance-audit.yaml`, `ai-docs-refresh.yaml`, `default.yaml`
└── scripts/
    └── refresh_ai_docs_prompt.md   # prompt consumed by the AI docs refresh workflow
```

## Load-Bearing Invariants

Do not change these without updating the policy tests and the audit flow together:

- **`AuditResult.ComputeIssues()`** (in `internal/domain/entities/`) is the single source of truth for compliance. Every carve-out lives here: forks skip Dependabot + secret scanning; private repos skip secret scanning + branch protection + rulesets; `DesiredWikiAllowlist` repos keep `has_wiki=true`; `AllowAutoMerge=true` is skipped on private repos because GitHub Free silently ignores the `PATCH`.
- **Policy constants** live in `internal/domain/entities/compliance_policy.go`: `DesiredRepoSettings`, `DesiredWikiAllowlist`, `DesiredReviewCount`, `DesiredRulesetName`, `DesiredDefaultBranch`, `RepositoryAdminActorType` / `RepositoryAdminActorID`.
- **`RepositoriesRepository.FindAllByOwner`** has three branches — authenticated self (`/user/repos` retains private visibility), `OwnerKind=User`, and `OwnerKind=Organization`. Keep all three in sync.
- **`SecuritySettingsRepository.FindByRepositoryName`** returns `DependabotAlerts *bool`: `nil` means "unknown / API failure", pointer-to-false means "disabled". Do not collapse the two.
- **Ruleset compliance** is a three-part check: name match, `non_fast_forward` rule, and `refs/heads/main` in the ref-name include list. Name-only match is not compliant.
- **`BypassActors`** in every ruleset must retain `RepositoryAdminActorType` / `RepositoryAdminActorID` so the owner can force-push.
- **Phases 2/3/4 re-read the Phase 1 audit**, not the live API — never add per-repo round-trips in the apply phases.

## Build / Test / Lint / Run

```bash
make build                          # compile bin/harden-repos
make test                           # go test -race -tags=unit ./...
make lint                           # golangci-lint run ./...
make sast                           # full SAST suite via rios0rios0/pipelines
make run ARGS='--phase 1 --repo autobump'
go test -tags=unit -run TestAuditRepositoriesCommand ./internal/domain/commands/
```

CLI phases:

```bash
HARDEN_OWNER=rios0rios0 go run ./cmd/harden-repos --phase 1   # read-only audit
HARDEN_OWNER=rios0rios0 go run ./cmd/harden-repos --phase 2   # repo settings
HARDEN_OWNER=rios0rios0 go run ./cmd/harden-repos --phase 3   # security settings
HARDEN_OWNER=rios0rios0 go run ./cmd/harden-repos --phase 4   # branch protection + ruleset
HARDEN_OWNER=rios0rios0 go run ./cmd/harden-repos --phase 5   # re-audit + diff snapshot
HARDEN_OWNER=rios0rios0 go run ./cmd/harden-repos --dry-run   # phases 1-4, no mutations
HARDEN_OWNER=rios0rios0 go run ./cmd/harden-repos --list-json # matrix input for ai-docs-refresh
```

## Environment Variables

| Variable                         | Purpose                                                                 |
|----------------------------------|-------------------------------------------------------------------------|
| `HARDEN_OWNER`                   | GitHub owner/org to audit (default: `rios0rios0`).                      |
| `GH_TOKEN` / `GITHUB_TOKEN`      | Bearer token for `github.com/google/go-github`.                         |
| `TMPDIR`                         | Honored by `os.TempDir()` for `gh_hardening_audit_before.json` output.  |

Workflow secrets: `COMPLIANCE_AUDIT_TOKEN` (daily audit), `CLAUDE_MD_REFRESH_TOKEN` (refresh discover + PRs), `CLAUDE_CODE_OAUTH_TOKEN` (refresh Claude Code action).

## Conventions

- **Go style** — `snake_case` file names, one-letter receiver names (`c` for `Command`, `r` for `Repository`), Uber Dig for DI, Logrus for logging, testify for assertions, no framework tags on entities.
- **Tests** — `//go:build unit`, `_test` package suffix, `t.Parallel()` on every top-level test function, BDD `// given` / `// when` / `// then` blocks. Prefer in-memory doubles over `testify/mock`. Builders live under `test/domain/builders/`.
- **YAML files** — `.yaml` (never `.yml`); single-quote string values except where variable interpolation requires double quotes; never quote booleans or numbers.
- **Commits** — `type(SCOPE): message` in simple past tense, no trailing period. See `.claude/rules/git-flow.md` in the user's global rules.
- **Changelog** — every change lands under `[Unreleased]` in `CHANGELOG.md` in the same commit. Keep a Changelog format. Proper nouns capitalized (GitHub, Go, Docker), code identifiers in backticks, versions in backticks.
- **Actions pins** — keep every workflow on the same latest major. Current pins: `actions/checkout@v6`, `actions/upload-artifact@v7`, `actions/setup-go@v6`, `anthropics/claude-code-action@v1`. Bump across both scheduled workflows in the same commit.

## When Editing the Policy

Ruleset and branch-protection changes propagate to every `rios0rios0` repo on the next audit run. When touching `compliance_policy.go` or `ComputeIssues()`:

1. Update the policy tests under `internal/domain/entities/` and the command tests under `internal/domain/commands/`.
2. Run `HARDEN_OWNER=rios0rios0 go run ./cmd/harden-repos --dry-run` and confirm the non-compliant set matches expectations.
3. Update `CLAUDE.md`, `README.md`, and this file together.
4. Record the change under `[Unreleased]` in `CHANGELOG.md`.

## Related Repositories

- [`rios0rios0/.github`](https://github.com/rios0rios0/.github) — community health fallback files, workflow templates, reusable Claude Code workflows.
- [`rios0rios0/pipelines`](https://github.com/rios0rios0/pipelines) — reusable SDLC workflows consumed via `make lint` / `make test` / `make sast`.
- [`rios0rios0/autobump`](https://github.com/rios0rios0/autobump) — releases `[Unreleased]` entries into versioned sections.
- [`rios0rios0/guide`](https://github.com/rios0rios0/guide/wiki) — canonical development standards.
