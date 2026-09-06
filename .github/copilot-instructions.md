# GitHub Copilot Instructions — config-automation

This file gives AI assistants (GitHub Copilot, Cursor, Claude Code) the minimum context needed to work in this repository without first reading the whole codebase. Keep it in sync with `CLAUDE.md` and `README.md`; any change to the compliance policy, CLI flags, workflows, or build commands must be reflected here in the same commit.

## Project Purpose

`config-automation` runs scheduled, cross-repo maintenance against every repository of [`rios0rios0`](https://github.com/rios0rios0), [`medhub-life`](https://github.com/medhub-life), and [`prefy`](https://github.com/prefy). A fine-grained PAT is bound to a single resource owner, so each workflow fans out with `strategy.matrix.owner` and passes each leg one owner (via `HARDEN_OWNER`) and that owner's own token. Repositories are identified by their `owner/name` slug (`entities.Repository.QualifiedName()`), since bare names collide across owners:

1. **Daily compliance audit** — `.github/workflows/repo-compliance-audit.yaml` runs `go run ./cmd/harden-repos --phase 1 --fail-on-noncompliant` and fails CI when any repo drifts from the hardening policy. Uploads `${TMPDIR:-/tmp}/gh_hardening_audit_before.json` as an artifact.
2. **Weekly config and docs refresh** — `.github/workflows/config-and-docs-refresh.yaml` enumerates non-fork non-archived repos via `go run ./cmd/harden-repos --list-json`, chunks them into batches of `batch_size` (default `10`) so the matrix has O(repos / batch_size) legs. Each leg installs `@anthropic-ai/claude-code` via `npm`, loads `scripts/refresh_config_and_docs_prompt.md` from the self-checkout, and loops through its batch internally — cloning each target repo and invoking `claude -p ... --max-turns "${MAX_TURNS}" --allowedTools '...' </dev/null` (the `</dev/null` is load-bearing: without it `claude` inherits the loop's stdin from `jq` and drains the batch after the first repo). `claude` output is tee'd to `${WORK_DIR}/.claude.log` so the loop can detect the org-wide `monthly usage limit` message and short-circuit the rest of the batch (skipped repos surface on a `quota_skipped` summary line; the quota-hitting repo is added to `failed` as `claude-quota:` so the leg still goes red). A Claude safeguard refusal (`safeguards flagged` / `cyber-use-case`) is deterministic for that one repo, so it surfaces on a `safeguard_skipped` summary line plus a `::warning::` annotation and does not fail the leg; every other failure kind still does. Drift detection uses `git add -N` + `git diff -w --quiet` on the in-scope files (today: `CLAUDE.md`, `.github/copilot-instructions.md`, and the tailored Copilot code-review skill at `.github/skills/code-review/SKILL.md`); the changelog target is staged with them but excluded from the gate. The in-scope set is duplicated in three places that must be changed together — `drift_paths`, the step's `--allowedTools` grant, and the file list in `scripts/refresh_config_and_docs_prompt.md`. The changelog target is picked per repo and enforced by the grant, not by the prompt: `changelog_mode=chlog` when the clone has `.chlog.yaml`, `.chlog.yml`, or `.changes/unreleased/` — the same predicate the `pipelines` basic-checks gate and AutoBump's `DetectChlog` use (grant `Edit(/.changes/unreleased/**)`, and the prompt tells Claude to write a fragment), `changelog` when it only has `CHANGELOG.md` (grant `Edit(/CHANGELOG.md)`), `none` otherwise. Hand-editing a generated `CHANGELOG.md` is what the 2026-08-31 run did to 46 chlog repositories; withholding the grant is what makes it impossible now, and a post-refresh guard reverts any `CHANGELOG.md`/existing-fragment change in chlog mode and fails the repo with `fragment:` on a malformed new fragment, or `changelog-guard:` when a restore itself fails (flagged, not bare — a bare command in an `if` body would abort the whole leg under `errexit`). Every grant is an `Edit(...)` rule — the CLI matches file permissions on `Edit` alone (it covers `Write` too); a `Write(...)` rule is inert and only warns. Branch name `chore/config-and-docs-refresh` is force-pushed to keep one open PR per repo. `workflow_dispatch` exposes `repo`, `batch_size`, `max_parallel`, and `max_turns` inputs (defaults: `10`, `2`, `50`). The workflow is named for the broader scope so future refresh targets (diagrams, more config files) can be added without renaming.
3. **Weekly release reconciliation** — `.github/workflows/release-reconcile.yaml` diffs every repo's released `CHANGELOG.md` versions against its git tags and re-pushes any missing tag at its bump commit, re-triggering the `pipelines` tag-push delivery path so a "bumped but never released" gap (a bump whose `main` run failed the quality gate) is recovered. Enumerates repos via `go run ./cmd/harden-repos --list-json`, delegates detection to the single-sourced `pipelines` primitive `global/scripts/shared/reconcile-releases.sh` (cloned at run time), and orchestrates via `scripts/reconcile-repos.sh`. Pushes with that owner's PAT (a `GITHUB_TOKEN`-pushed tag would not start delivery). `workflow_dispatch` exposes `repo`, `dry_run`, and `fail_on_gap` inputs; results go to `$GITHUB_STEP_SUMMARY`.
4. **`cmd/harden-repos/`** — the Go CLI that implements the compliance policy and all phase commands.

## Architecture

Clean Architecture with `domain` (contracts) / `infrastructure` (implementations) split. Dependencies always point inward; the domain layer never imports `github.com/google/go-github`.

```
config-automation/
├── cmd/
│   └── harden-repos/               # CLI entry point + Uber Dig wiring (`main.go`, `dig.go`)
├── internal/
│   ├── container.go                # top-level DI orchestrator
│   ├── domain/
│   │   ├── commands/               # one command per phase + `--list-json` + `--dry-run`
│   │   ├── entities/               # `Repository`, `SecuritySettings`, `BranchProtection`, `Ruleset`, `AuditResult`, `compliance_policy.go`
│   │   └── repositories/           # three port interfaces (repos, security, branch protection)
│   └── infrastructure/
│       └── repositories/           # `GoGithub…Repository` adapters over `github.com/google/go-github/v75`
├── test/
│   └── domain/
│       ├── builders/               # `RepositoryBuilder`, `AuditResultBuilder`
│       └── doubles/repositories/   # in-memory doubles preferred over `testify/mock`
├── .github/workflows/              # `repo-compliance-audit.yaml`, `config-and-docs-refresh.yaml`, `release-reconcile.yaml`, `default.yaml`, `claude-code-review.yaml`, `claude.yaml`
└── scripts/
    └── refresh_config_and_docs_prompt.md   # prompt consumed by the config-and-docs refresh workflow
```

## Load-Bearing Invariants

Do not change these without updating the policy tests and the audit flow together:

- **`AuditResult.ComputeIssues()`** (in `internal/domain/entities/`) is the single source of truth for compliance. Every carve-out lives here: forks skip Dependabot + secret scanning and must instead keep GitHub Actions disabled (`actions_enabled`, enforced by phase 3 through `SecuritySettingsRepository.DisableActions`; non-forks are never checked); private repos skip secret scanning + branch protection + rulesets; `DesiredWikiAllowlist()` repos keep `has_wiki=true`; `AllowAutoMerge=true` is skipped on private repos because GitHub Free silently ignores the `PATCH`.
- **Policy** lives in `internal/domain/entities/compliance_policy.go`: `DesiredRepoSettings()` and `DesiredWikiAllowlist()` are functions (returning fresh values so call sites cannot mutate the policy), while `DesiredReviewCount`, `DesiredForkActionsEnabled`, `DesiredRulesetName`, `DesiredDefaultBranch`, and `RepositoryAdminActorType` / `RepositoryAdminActorID` remain constants.
- **`Repository.FindAllByOwner`** (port in `internal/domain/repositories/repositories_repository.go`) has three branches — authenticated self (`/user/repos` retains private visibility), `OwnerKind=User`, and `OwnerKind=Organization`. Keep all three in sync. `BranchProtectionsRepository.FindRulesetByName` returns the sentinel `repositories.ErrRulesetNotFound` for "no ruleset configured"; callers short-circuit on that with `errors.Is` instead of treating it as an audit failure.
- **`SecuritySettingsRepository.FindByRepositoryName`** returns `DependabotAlerts *bool`: `nil` means "unknown / API failure", pointer-to-false means "disabled". Do not collapse the two. `ActionsEnabled *bool` (the repository-level GitHub Actions switch, from `GET /repos/{owner}/{repo}/actions/permissions`) carries the same tri-state: a fork with `nil` audits as `actions_enabled=unknown` and is still disabled by phase 3.
- **Ruleset compliance** (`Ruleset.IsCompliant`) is a four-part check: name match, the `non_fast_forward` rule, `allowed_merge_methods` on the `pull_request` rule equal to `DesiredAllowedMergeMethods()` (`["merge"]`), and `refs/heads/main` in the ref-name include list. Name-only match is not compliant. `non_fast_forward` and `allowed_merge_methods` are not the same and the names mislead: `non_fast_forward` blocks *force pushes*; restricting merges to merge commits — the actual no-fast-forward-*merge* policy — is `allowed_merge_methods`. `AllowedMergeMethods` is nil when the repo has no `pull_request` rule at all, which the audit reports as `rule_missing`.
- **The merge shape spans repo settings and the ruleset together.** `DesiredRepoSettings()` keeps `AllowRebaseMerge` on — the only toggle that surfaces GitHub's *Update with rebase* — plus `AllowMergeCommit` on, `AllowSquashMerge` off, `AllowUpdateBranch` on; the ruleset then narrows `main` to merge commits via `allowed_merge_methods`. A ruleset can only narrow what the repo already allows, so changing either half alone re-opens the fast-forward or removes the button.
- **Phase 4 creates a ruleset only when the audit found none; a drifted one is updated by ID.** GitHub rejects a POST whose ruleset name is taken with `422 Name must be unique`, so `applyRuleset` branches on `audit.Ruleset != nil` and calls `UpdateRuleset`. Reverting it to a bare `CreateRuleset` breaks every already-hardened repo — the exact failure that hit all 70 repos on the `allowed_merge_methods` rollout.
- **`BypassActors`** in every ruleset must retain `RepositoryAdminActorType` / `RepositoryAdminActorID` so the owner can force-push; GitHub scopes a bypass actor to the whole ruleset, so it also exempts admins from the merge-method rule.
- **Phases 2/3/4 re-read the Phase 1 audit**, not the live API — never add per-repo round-trips in the apply phases.

## Build / Test / Lint / Run

```bash
make build                          # compile bin/harden-repos
make test                           # go test -race -tags=unit ./...
make lint                           # golangci-lint run ./...
make sast                           # full SAST suite via rios0rios0/pipelines
go test -tags=unit -run TestAuditRepositoriesCommand ./internal/domain/commands/
```

CLI phases:

```bash
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --phase 1   # read-only audit
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --phase 2   # repo settings
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --phase 3   # security settings
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --phase 4   # branch protection + ruleset
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --phase 5   # re-audit + diff snapshot
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --dry-run   # phases 1-4, no mutations
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --list-json # matrix input for config-and-docs-refresh
```

## Environment Variables

| Variable                         | Purpose                                                                 |
|----------------------------------|-------------------------------------------------------------------------|
| `HARDEN_OWNER`                   | Comma-separated GitHub owners/orgs to audit, in order (default: `rios0rios0`). The workflows set it to a **single** owner per matrix leg, because a fine-grained PAT is bound to one resource owner. |
| `GH_TOKEN` / `GITHUB_TOKEN`      | Bearer token for `github.com/google/go-github`.                         |
| `TMPDIR`                         | Honored by `os.TempDir()` for `gh_hardening_audit_before.json` output.  |

Workflow secrets, one fine-grained PAT per owner shared by all three workflows (a fine-grained token is bound to a single resource owner, and each token's lifetime must be 366 days or less): `PERSONAL_ACCESS_TOKEN` (`rios0rios0`), `MEDHUB_ACCESS_TOKEN` (`medhub-life`), `PREFY_ACCESS_TOKEN` (`prefy`) — the same names `autobump-automation` and `autoupdate-automation` use — plus `CLAUDE_CODE_OAUTH_TOKEN` (refresh Claude Code CLI, shared).

## Conventions

- **Go style** — `snake_case` file names, one-letter receiver names (`c` for `Command`, `r` for `Repository`), Uber Dig for DI, Logrus for logging, testify for assertions, no framework tags on entities.
- **Tests** — `//go:build unit`, `_test` package suffix, `t.Parallel()` on every top-level test function, BDD `// given` / `// when` / `// then` blocks. Prefer in-memory doubles over `testify/mock`. Builders live under `test/domain/builders/`.
- **YAML files** — `.yaml` (never `.yml`); single-quote string values except where variable interpolation requires double quotes; never quote booleans or numbers.
- **Commits** — `type(SCOPE): message` in simple past tense, no trailing period. See `.claude/rules/git-flow.md` in the user's global rules.
- **Changelog** — every change writes its own fragment under `.changes/unreleased/` with `chlog new --kind <Kind> --body "..."`, in the same commit; `CHANGELOG.md` is generated from them and is never edited by hand. Keep a Changelog kinds. Proper nouns capitalized (GitHub, Go, Docker), code identifiers in backticks, versions in backticks.
- **Actions pins** — every step-level `actions/*` use is pinned to a full commit SHA with a trailing `# vX.Y.Z` comment, never a bare tag (a security decision from `0.3.8`; do not revert a SHA to `@v6`). Keep every workflow on the same latest major: `actions/checkout` v6, `actions/upload-artifact` v7, `actions/setup-go` v6, `actions/setup-node` v6. When bumping, update the SHA and its comment across all three scheduled workflows (`repo-compliance-audit.yaml`, `config-and-docs-refresh.yaml`, `release-reconcile.yaml`) in the same commit. The `@anthropic-ai/claude-code` npm package is the exception — pinned implicitly to `latest` via `npm install -g`.
- **Go toolchain** — every `actions/setup-go` step uses `go-version-file: 'go.mod'`, never a hardcoded `go-version`. `setup-go` exports `GOTOOLCHAIN=local`, so a loose spec like `'1.26'` resolves to whatever patch release the runner cached and then hard-fails every `go run` once `go.mod` requires a newer patch. Bumping `go.mod` is enough; the workflows follow.

## When Editing the Policy

Ruleset and branch-protection changes propagate to every `rios0rios0` repo on the next audit run. When touching `compliance_policy.go` or `ComputeIssues()`:

1. Update the policy tests under `internal/domain/entities/` and the command tests under `internal/domain/commands/`.
2. Run `HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --dry-run` and confirm the non-compliant set matches expectations.
3. Update `CLAUDE.md`, `README.md`, and this file together.
4. Record the change in a fragment: `chlog new --kind Changed --body "..."`.

## Related Repositories

- [`rios0rios0/.github`](https://github.com/rios0rios0/.github) — community health fallback files, workflow templates, reusable Claude Code workflows.
- [`rios0rios0/pipelines`](https://github.com/rios0rios0/pipelines) — reusable SDLC workflows consumed via `make lint` / `make test` / `make sast`.
- [`rios0rios0/autobump`](https://github.com/rios0rios0/autobump) — releases `[Unreleased]` entries into versioned sections.
- [`rios0rios0/guide`](https://github.com/rios0rios0/guide/wiki) — canonical development standards.

<!-- chlog:start -->
## Changelog (chlog) — MANDATORY

If the repository you are working in uses chlog (a `.chlog.yaml` or `.chlog.yml`
config file, or a `.changes/` directory, exists at the project root), the
following is binding and ALWAYS applies: whenever you make ANY change, you MUST
create a changelog fragment as part of the same change — automatically, without
being asked, before committing.

- Do NOT edit CHANGELOG.md directly; it is generated from fragments.
- Create the fragment with:
  `chlog new --kind <Kind> --body "<imperative description>"`
- Valid kinds: Added, Changed, Deprecated, Removed, Fixed, Security
- Choose the kind that best matches the change (e.g., new feature → Added,
  bug fix → Fixed, behavior change → Changed, removal → Removed, security fix → Security).
- If the change is backward-INCOMPATIBLE with the public API (a breaking
  change), you MUST add the `--breaking` flag:
  `chlog new --kind <Kind> --breaking --body "<description>"`.
  This is the ONLY thing that triggers a major version bump — the kind alone
  never does (per SemVer, major = incompatible change). When unsure whether a
  change breaks compatibility, ask the user instead of guessing.
- Fragments are YAML files in `.changes/unreleased/`; stage them with your commit.
- `chlog check` fails the build when a fragment is missing — never skip it.
<!-- chlog:end -->
