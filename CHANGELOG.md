# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

This file is not edited by hand. Every change writes its own fragment under
`.changes/unreleased/` with [chlog](https://github.com/luizjhonata/chlog), and a release compiles
the pending fragments into a version section here — so two branches each adding an entry no
longer touch the same lines, and a rebase that used to conflict on this file now conflicts on
nothing.

When a new release is proposed:

1. Create a new branch `bump/x.x.x` (this isn't a long-lived branch!!!);
2. The fragments pending under `.changes/unreleased/` are compiled into a version section by `chlog batch auto && chlog merge` (AutoBump does this for you — it reads the fragments directly);
3. Open a Pull Request with the bump version changes targeting the `main` branch;
4. When the Pull Request is merged, a new Git tag must be created using <LINK TO THE PLATFORM TO OPEN THE PULL REQUEST>.

Releases to productive environments should run from a tagged version.
Exceptions are acceptable depending on the circumstances (critical bug fixes that can be cherry-picked, etc.).

## [Unreleased]

## [0.8.1] - 2026-08-28

### Changed

- changed the Claude workflows to call the reusable workflows in `rios0rios0/pipelines` instead of `rios0rios0/.github`, which is where every other reusable workflow and composite action already lives, and renamed them to `claude-review.yaml` and `claude-mention.yaml`, matching the `reusable-claude-review.yaml` / `reusable-claude-mention.yaml` definitions they call

### Fixed

- restored the `.changes/unreleased/` directory with a `.gitkeep`, so the release tooling keeps recognising this project as [chlog](https://github.com/luizjhonata/chlog)-based after a release consumes the last fragment. Git tracks files rather than directories, so the bump commit that removed the final fragment removed the directory too, and the next run read the empty `[Unreleased]` section as "nothing to release"
- restored the `id-token: write` permission on both Claude workflow callers. Without it the caller grants less than the reusable workflow declares, which GitHub rejects before the job starts -- runs ended in `startup_failure`. The action needs the scope because `setupGitHubToken()` exchanges a GitHub OIDC token for the GitHub App token it posts with, unless a `github_token` is passed explicitly.

### Removed

- removed the unused `id-token: write` permission from the Claude workflow callers, and changed `claude-review.yaml`'s display name to `Claude Review` so it matches its file name and its `Claude Mention` sibling. `anthropics/claude-code-action` needs `id-token: write` only for workload identity federation or the Bedrock / Vertex / Foundry OIDC paths; these authenticate with `claude_code_oauth_token`, so the scope allowed minting OIDC tokens for any audience without ever being used.

## [0.8.0] - 2026-08-26

### Added

- added `.github/skills/code-review/SKILL.md` to the weekly config-and-docs refresh scope, so the tailored GitHub Copilot code-review skill is reviewed against the code and updated alongside `CLAUDE.md` and `.github/copilot-instructions.md`
- added a tailored `code-review` skill under `.github/skills/` so GitHub Copilot reviews changes against the [rios0rios0/guide](https://github.com/rios0rios0/guide/wiki) standards and this repository's own load-bearing invariants

### Changed

- bumped `github.com/google/go-github` from `v66` to `v75`. The ruleset API was reshaped across those majors: `github.Ruleset` became `github.RepositoryRuleset`, its `Rules` slice became the typed `RepositoryRulesetRules` struct, and `Target`/`Enforcement`/`ActorType`/`BypassMode` became named string types. The bump was required because `PullRequestRuleParameters` only gained `AllowedMergeMethods` after `v66`, and that field is what pins `main` to merge commits.
- changed the changelog to [chlog](https://github.com/luizjhonata/chlog) fragments: a change now writes its own YAML file under `.changes/unreleased/` through `chlog new --kind <Kind> --body "..."`, and `CHANGELOG.md` is GENERATED from them at release time by `chlog batch auto && chlog merge`. That is the one thing a single shared file cannot do — two branches each adding an entry no longer touch the same lines, so a rebase that used to conflict on `CHANGELOG.md` now conflicts on nothing. The `[Unreleased]` section was empty, so nothing had to be carried across. AutoBump already reads the fragments directly, so the release flow is unchanged.
- changed the Go module dependencies to their latest versions
- closed the fast-forward merge path on `main` and made the rebase-update button part of the policy. `allow_rebase_merge` stays on repo-wide because it is the only toggle that surfaces GitHub's "Update with rebase" control — but it also surfaces "Rebase and merge", which lands a pull request without the merge commit. The fast-forward is now blocked one level down: the `main-protection` ruleset carries a `pull_request` rule whose `allowed_merge_methods` is pinned to `merge` alone, so `main` accepts merge commits and nothing else while the update button survives. A ruleset can only narrow what the repository already allows, so the repo settings and the ruleset are two halves of one decision. The policy also now enforces `allow_update_branch` ("Always suggest updating pull request branches"), which was previously unmanaged and off on 58 of 61 repos: without it the "Update branch" control only appears when branch protection requires the branch be up to date before merging, which this policy does not require. The audit reports both as `allow_update_branch=` and `ruleset_allowed_merge_methods=`, and the phase 1 table gained `UPD-BR` and `MERGE-M` columns — the latter sitting next to `NO-FP` precisely because the two are easy to confuse: `non_fast_forward` blocks FORCE PUSHES and says nothing about how pull requests land.

### Fixed

- fixed a repository with an unprotected default branch being dropped from every phase. `go-github` collapses the `404 Branch not protected` response into its `ErrBranchNotProtected` sentinel — a plain `errors.New` that carries neither the status code nor the API's own wording — so `FindProtectionByBranch` matched neither `isStatusCode(err, 404)` nor the literal `"Branch not protected"` string, and the error escaped as a hard `AuditError`. Because phases 2-4 consult the audit rather than the live API, a repo in that state was silently skipped by settings, security, and protection alike: `rios0rios0/ccswitch` sat completely unhardened while the audit reported only `audit_error`. The check now tests the sentinel with `errors.Is` first. This predates the `v75` bump; the sentinel exists in `v66` too.
- fixed phase 4 failing with `422 Name must be unique` on every repo that already had a `main-protection` ruleset. `applyRuleset` only ever POSTed a new ruleset, which was correct while a non-compliant ruleset almost always meant no ruleset existed. Pinning `allowed_merge_methods` changed that: every existing ruleset became non-compliant because it predates the `pull_request` rule, so the common case is now "exists but drifted" and GitHub rejects a POST whose name is taken. `BranchProtectionsRepository` gained `UpdateRuleset`, and `applyRuleset` now rewrites the ruleset by the ID the audit already carries, creating only when there is genuinely nothing there. Applied against the fleet this was the difference between 0 and 70 rulesets updated.
- fixed the `main` pipeline, which every repository's `sast:gitleaks` job had been failing since the code-review skill landed: the skill's own security bullet listed credential prefixes verbatim to warn against writing them, and the scanner's second pass matches those prefixes on their own, so the warning tripped the rule it was describing. The bullet now names the vendors instead, and the commit that carried the original wording is allowlisted by fingerprint in `.gitleaksignore`, because the scan walks the whole history reachable from `HEAD` and no edit at the tip can clear a past commit. No credential was ever committed.

## [0.7.1] - 2026-08-25

### Changed

- changed the compliance policy to turn `allow_squash_merge` off fleet-wide, so `allow_merge_commit` + `allow_rebase_merge` on and squash off now spell a semi-linear history on every audited repository: a pull request is rebased onto its base with *Update with rebase* and then landed with a merge commit, leaving `main` with one merge commit per pull request over an otherwise linear ancestry — GitHub offers no single "semi-linear merge" option the way Azure DevOps does, so the policy removes the buttons that break the shape rather than selecting one

### Fixed

- fixed every scheduled workflow targeting the `medhub-tech` organization, which was renamed to `medhub-life` and now returns `404` on every API call: the daily compliance audit, the weekly config-and-docs refresh and the weekly release reconciliation each had a dead matrix leg, and because the CLI treats a listing failure for any owner as fatal that leg failed on every run while the other owners passed — the `MEDHUB_ACCESS_TOKEN` secret name is unchanged, only the owner it addresses

## [0.7.0] - 2026-08-24

### Added

- added a terminal `verify` job to `config-and-docs-refresh` that fails the run when any owner could not be enumerated, so tolerating a broken owner never passes as a fully green fleet refresh
- added per-owner secrets `MEDHUB_ACCESS_TOKEN` and `PREFY_ACCESS_TOKEN`, documented in `README.md` alongside the 366-day lifetime limit both organizations enforce

### Changed

- changed `config-and-docs-refresh` to enumerate each owner separately and continue past an unreachable one, so the reachable owners are still refreshed, and to batch repositories within an owner so every matrix leg carries exactly one credential
- changed all three scheduled workflows to fan out into one job per owner via `strategy.matrix.owner` with `fail-fast: false`, each job setting `HARDEN_OWNER` to its own owner and authenticating with that owner's own fine-grained PAT
- changed the compliance audit's artifact name to include the owner, since one artifact name per run collided once the job fanned out
- changed the credentials to one PAT per owner shared by all three workflows — `PERSONAL_ACCESS_TOKEN`, `MEDHUB_ACCESS_TOKEN` and `PREFY_ACCESS_TOKEN`, the same names `autobump-automation` and `autoupdate-automation` use — replacing the separate audit and refresh tokens, so each owner needs exactly one token for the whole automation fleet instead of one per privilege tier
- changed the Go module dependencies to their latest versions
- changed the Go version to `1.27.0` and updated all module dependencies

### Fixed

- fixed every scheduled workflow aborting before it reached `prefy`: a fine-grained PAT is bound to a single resource owner, so the shared token was rejected by `medhub-tech` with `403 ... forbids access via a fine-grained personal access tokens if the token's lifetime is greater than 366 days`, and because the CLI treats a listing failure for any owner as fatal, the run died on the second owner and never attempted the third — the daily audit, the weekly config/docs refresh and the weekly release reconciliation all failed this way
- fixed the daily audit reporting an authentication error where it used to report compliance drift, by auditing each owner in its own job so one owner's rejected grant no longer masks the others' results

## [0.6.1] - 2026-08-17

### Changed

- changed the Go module dependencies to their latest versions

## [0.6.0] - 2026-08-15

### Added

- added `Repository.QualifiedName()`, the `owner/name` slug used by the audit table, the non-compliance report, and the phase 5 diff, since repository names are only unique within a single owner
- added multi-owner support to `harden-repos`: `HARDEN_OWNER` is now a comma-separated list that every phase walks in order, with whitespace trimmed, blank entries dropped, and duplicates collapsed
- added the `medhub-tech` and `prefy` organizations to the maintenance scope, so the daily compliance audit, the weekly config-and-docs refresh, and the weekly release reconciliation now cover them alongside `rios0rios0`
- added the `owner` field to every `--list-json` entry, so the refresh matrix and the release-reconcile script clone the right organization instead of assuming one

### Changed

- changed `workflow_dispatch`'s `repo` input on the config-and-docs refresh and release reconciliation workflows to accept `owner/name`; a bare name still resolves against the first configured owner
- changed the Go version to `1.26.6` and updated all module dependencies

### Fixed

- fixed the phase 5 compliance report matching repositories by bare name, which made two owners sharing a repository name diff against each other — reporting drift on a fleet that had not changed, and attributing one owner's changes to the other's repository

## [0.5.1] - 2026-07-22

### Changed

- refreshed `CLAUDE.md` and `.github/copilot-instructions.md` to correct the Actions-pins convention, which still described bare-tag pins (`actions/checkout@v6`) even though the workflows have been pinned to full commit SHAs with `# vX.Y.Z` comments since `0.3.8`

## [0.5.0] - 2026-07-16

### Added

- added a weekly **release reconciliation** workflow (`.github/workflows/release-reconcile.yaml` + `scripts/reconcile-repos.sh`) that diffs every `rios0rios0` repo's released `CHANGELOG.md` versions against its git tags and re-pushes any missing tag at its bump commit, re-triggering the [`pipelines`](https://github.com/rios0rios0/pipelines) tag-push delivery path so a bump whose `main` run failed the quality gate ("bumped but never released") is recovered automatically. Gap detection is delegated to the single-sourced pipelines primitive `global/scripts/shared/reconcile-releases.sh` (cloned at run time rather than vendored); the job enumerates repos via `harden-repos --list-json`, pushes recovery tags with the `CLAUDE_MD_REFRESH_TOKEN` PAT (a `GITHUB_TOKEN`-pushed tag would not re-trigger delivery), and writes a consolidated `$GITHUB_STEP_SUMMARY`. Supports `workflow_dispatch` with `repo`, `dry_run`, and `fail_on_gap` inputs; runs Mondays 08:00 UTC after the compliance audit and config/docs refresh

## [0.4.0] - 2026-07-14

### Added

- added a `claude-safeguard:` failure reason so a repository refused by Claude's safeguards is distinguishable in the batch summary from a quota exhaustion or a transient API error
- added a `processed (n/total)` line to the batch summary, and listed `failed` entries one per line so multiple failures stay readable

### Fixed

- fixed `git checkout -B` aborting the batch on failure instead of being recorded against the offending repository, matching every other `git` and `gh` call in the loop
- fixed the `monthly usage limit` short-circuit and its `quota_skipped` reporting, which were unreachable for the same reason and had therefore never run
- fixed the config-and-docs refresh workflow aborting an entire matrix leg when the Claude Code CLI failed on a single repository: the step runs under `set -euo pipefail`, so with `pipefail` the `claude | tee` pipeline returned `claude`'s exit code and `errexit` killed the leg before the next line could read `PIPESTATUS[0]`. A cybersecurity-safeguard refusal on the 7th of 10 repositories stranded the remaining 3 and skipped the batch summary. The pipeline is now wrapped in `set +e` / `set -e`, so a failure is attributed to the offending repository and the loop continues.

## [0.3.9] - 2026-07-13

### Fixed

- fixed the scheduled audit and refresh workflows failing every run since `0.3.8` with `go.mod requires go >= 1.26.5 (running go 1.26.4; GOTOOLCHAIN=local)`, by deriving the toolchain from `go-version-file: 'go.mod'` instead of the hardcoded `go-version: '1.26'` that resolved to whatever patch release the runner had cached

## [0.3.8] - 2026-07-10

### Changed

- changed the Go version to `1.26.5` and updated all module dependencies

### Security

- pinned all step-level GitHub Actions to full commit SHAs in the automation workflows
- replaced `secrets: inherit` with an explicit `CLAUDE_CODE_OAUTH_TOKEN` secret in the Claude workflow callers, following the least-privilege principle

## [0.3.7] - 2026-06-09

### Changed

- changed the Go module dependencies to their latest versions

## [0.3.6] - 2026-06-03

### Changed

- changed the Go version to `1.26.4` and updated all module dependencies
- refreshed `CLAUDE.md` and `.github/copilot-instructions.md` to attribute `FindRulesetByName` to the `BranchProtectionsRepository` port instead of `Repository`, matching the actual interface

## [0.3.5] - 2026-05-29

### Changed

- changed the `.github/workflows/config-and-docs-refresh.yaml` workflow to invoke the Claude Code CLI with `claude-opus-4-8` instead of `claude-opus-4-6` for the weekly configuration and documentation refresh

## [0.3.4] - 2026-05-25

### Changed

- refreshed `CLAUDE.md` and `.github/copilot-instructions.md` to update the `actions/setup-node` pin from `@v4` to `@v6`, matching the actual workflow

## [0.3.3] - 2026-05-22

### Changed

- changed the Go module dependencies to their latest versions

## [0.3.2] - 2026-05-19

### Changed

- refreshed `.github/copilot-instructions.md` to remove the non-functional `make run ARGS='...'` example (the Makefile's `run` target does not interpolate `$(ARGS)`)
- renamed the git committer identity used by the refresh workflow from `rios0rios0-bot` to `config-bot` so the bot identity reflects this project's scope rather than the org

### Fixed

- fixed the per-repo "PR already exists" check in `.github/workflows/config-and-docs-refresh.yaml` to filter by `--state open`; `gh pr view <branch>` previously matched merged/closed PRs as well, so once a refresh PR was merged the next run logged `prs_updated` and skipped `gh pr create`, leaving the new force-pushed commit stranded on a branch with no open PR (observed on `rios0rios0/guide` where PR #55 was merged and the 2026-05-04 run force-pushed `f3ae5d3` without opening a new PR)
- wrapped the new `gh pr list` call in `.github/workflows/config-and-docs-refresh.yaml` in an `if !` conditional so a transient `gh` failure (auth, network, permissions) is captured as `failed+=("pr-list: ${target_repo}")` and the per-repo cleanup runs, instead of `set -euo pipefail` aborting the whole batch mid-loop on a bare command substitution

## [0.3.1] - 2026-05-08

### Changed

- bumped the Go directive in `go.mod` from `1.26.2` to `1.26.3` and updated the indirect dependency `golang.org/x/sys` from `v0.43.0` to `v0.44.0`

## [0.3.0] - 2026-04-28

### Added

- added `max_turns` `workflow_dispatch` input to `.github/workflows/config-and-docs-refresh.yaml` (default `50`, was hard-coded `30`) so the per-repo `claude --max-turns` cap can be raised at queue time when a legitimately complex repo trips the safety limit; observed in run `25008617829` where `rios0rios0/gitforge` exited with `Error: Reached max turns (30)` after processing the 9 simpler repos in its batch
- added `quota_skipped` tracking and a fail-fast skip path to the per-batch refresh loop: when `claude` fails and its captured output contains `monthly usage limit`, the loop sets a flag that short-circuits every remaining repo (printing them as `(SKIPPED: monthly Claude usage limit hit earlier in batch)` and adding them to a new `quota_skipped` summary line) instead of re-invoking `claude` for ~3 minutes per repo against the same exhausted quota; the first quota-hitting repo is recorded as `claude-quota: <repo>` in `failed` so the overall batch still exits non-zero

### Changed

- raised the default `max_parallel` for the refresh matrix from `1` to `2` so the weekly run finishes in roughly half the wall-clock; the per-batch sequential drip still keeps the steady-state Anthropic request rate well inside the per-minute budget
- refreshed `.github/copilot-instructions.md` to replace the non-existent `ComplianceIssue` entity with the actual types (`SecuritySettings`, `BranchProtection`, `Ruleset`) and added the missing `claude-code-review.yaml` and `claude.yaml` workflows to the tree diagram
- renamed `.github/workflows/ai-docs-refresh.yaml` to `.github/workflows/config-and-docs-refresh.yaml`, `scripts/refresh_ai_docs_prompt.md` to `scripts/refresh_config_and_docs_prompt.md`, the stable PR branch from `chore/ai-docs-refresh` to `chore/config-and-docs-refresh`, and the concurrency group to `config-and-docs-refresh` so the workflow's name reflects its broader scope (configuration + documentation); today the in-scope set is still `CLAUDE.md` and `.github/copilot-instructions.md`, and the rename leaves room for future targets like diagrams or additional config files without renaming again
- updated the per-repo commit message and PR title produced by the refresh workflow from `chore(ai-docs): refreshed AI assistant guidance` to `chore(refresh): refreshed configuration and documentation` to match the broadened scope

### Fixed

- fixed the per-batch refresh loop in `.github/workflows/config-and-docs-refresh.yaml` (formerly `ai-docs-refresh.yaml`) to redirect `</dev/null` into the `claude -p` invocation; without it `claude` inherited the outer `while read` loop's stdin (the `jq` pipe), drained the rest of the batch's repo list after the first iteration, and silently exited with a misleading per-batch summary — observed in run `24982782411` where every batch processed only `[1/N]` repos before reporting success

## [0.2.1] - 2026-04-25

### Changed

- renamed the repository from `fleet-maintenance` to `config-automation` and updated the Go module path from `github.com/rios0rios0/fleet-maintenance` to `github.com/rios0rios0/config-automation`; all internal imports, `README.md`, `CONTRIBUTING.md`, `.github/copilot-instructions.md`, and the `ai-docs-refresh.yaml` workflow were updated in lockstep

## [0.2.0] - 2026-04-24

### Added

- added `.github/workflows/claude-code-review.yaml`, the PR-opened/synchronize/reopen wrapper that calls the reusable `rios0rios0/.github/.github/workflows/claude-code-review.yaml@main` workflow with `secrets: inherit` so every new PR on this repo gets an automated Claude Code review
- added `.github/workflows/claude.yaml`, the issue/PR-comment wrapper that calls the reusable `rios0rios0/.github/.github/workflows/claude.yaml@main` workflow with `secrets: inherit` so `@claude` mentions on issues, PR comments, and PR reviews trigger the Claude Code assistant (gated to `OWNER`/`MEMBER`/`COLLABORATOR` by the reusable workflow)
- added `batch_size` and `max_parallel` `workflow_dispatch` inputs so the matrix shape can be retuned per-run without editing the workflow; defaults preserve the previous serial rate-limit behavior
- added a per-batch summary footer (`no_drift / prs_created / prs_updated / failed`) and a `--max-turns 30` safety cap on each `claude` invocation so a stuck reasoning loop cannot exhaust the job-timeout budget

### Changed

- changed `.github/workflows/ai-docs-refresh.yaml` to a batched-matrix shape: the `discover` job now chunks the sorted `harden-repos --list-json` output into groups of `batch_size` repos (default `10`) and the `refresh` job runs one leg per batch (`max_parallel: 1` by default) that installs `@anthropic-ai/claude-code` via `npm` and loops through its batch sequentially, replacing the former one-job-per-repo matrix that relied on `anthropics/claude-code-action@v1`
- changed the Actions-pins note in `CLAUDE.md` and `.github/copilot-instructions.md` to drop `anthropics/claude-code-action@v1` and add `actions/setup-node@v4` now that the workflow installs the Claude Code CLI directly via `npm`

### Fixed

- changed the drift-detection step in `.github/workflows/ai-docs-refresh.yaml` to build a `drift_paths` list of existing AI-doc files and treat "no AI-doc files present" as no drift, preventing `git diff` from being invoked with a pathspec that doesn't exist in the repo
- changed the per-batch `run` script in `.github/workflows/ai-docs-refresh.yaml` from `set -uo pipefail` to `set -euo pipefail` so a failing `cat`, `jq`, or similar unchecked command aborts the batch instead of emitting misleading per-repo failures
- fixed `GoGithubBranchProtectionsRepository.FindRulesetByName` to translate `403 Upgrade to GitHub Pro` responses into `repositories.ErrRulesetNotFound` so the daily compliance audit no longer errors on every private repo on GitHub Free — mirrors the existing 403/upgrade-required handling in `FindProtectionByBranch` and lets `AuditResult.ComputeIssues` apply its private-repo carve-out
- fixed the `discover` job in `.github/workflows/ai-docs-refresh.yaml` to validate `inputs.batch_size` before passing it to `jq`, falling back to `10` when the value is not a positive integer so a malformed `workflow_dispatch` input can no longer crash the matrix build
- fixed the tool-allowlist description in `CLAUDE.md` to spell out the fully-scoped `Write(/CLAUDE.md),Write(/.github/copilot-instructions.md),Write(/CHANGELOG.md)` entries instead of the `Write(...)` shorthand, matching the actual CLI args passed to `claude -p`

### Removed

- removed the unused `id-token: write` permission from the `refresh` job in `.github/workflows/ai-docs-refresh.yaml` since the workflow authenticates via a PAT and the Claude Code OAuth token rather than OIDC

## [0.1.1] - 2026-04-22

### Changed

- changed the Go version to `1.26.2` and updated all module dependencies
- converted `entities.DesiredRepoSettings` and `entities.DesiredWikiAllowlist` from package-level variables to functions, keeping the compliance policy immutable from call sites
- renamed the `repositories.RepositoriesRepository` port to `repositories.Repository` to remove the package-name stutter flagged by `revive`

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

- changed `ai-docs-refresh.yaml` to self-checkout `rios0rios0/config-automation` and read `scripts/refresh_ai_docs_prompt.md` locally instead of fetching it from `rios0rios0/.github` via `gh api`, removing the last hardcoded cross-repo dependency and one network round-trip per refresh
- expanded the `anthropics/claude-code-action@v1` allowlist in `ai-docs-refresh.yaml` to include `Edit(/CHANGELOG.md)` and `Write(/CHANGELOG.md)` so Claude can record every AI-docs refresh in the target repo's changelog
- switched both workflows from `actions/setup-python@v6` + `python3` to `actions/setup-go@v6` + `go run ./cmd/harden-repos` so the scheduled jobs exercise the same Go binary the team maintains locally
- updated `scripts/refresh_ai_docs_prompt.md` to require Claude to add a short `[Unreleased]` entry to the target repo's `CHANGELOG.md` whenever it edits `CLAUDE.md` or `.github/copilot-instructions.md`, and to skip the entry when the target repo has no changelog
- widened the drift-detection step in `ai-docs-refresh.yaml` to stage `CHANGELOG.md` alongside the AI-docs files while keeping the diff gate scoped to the AI docs, so a stray CHANGELOG-only edit cannot open a spurious PR

### Removed

- removed `scripts/harden_repos.py` (superseded by the Go CLI at `cmd/harden-repos/`). The Go port preserves every carve-out from the Python original: fork exclusion for Dependabot and secret scanning, private-repo skip for `AllowAutoMerge`, `secret_scanning`, branch protection, and the ruleset, `DesiredWikiAllowlist` for legitimate wiki repos, and the tri-state distinction between `dependabot_alerts=unknown` and `dependabot_alerts=off`

