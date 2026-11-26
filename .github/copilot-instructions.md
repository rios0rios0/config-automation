# GitHub Copilot Instructions — config-automation

This file gives AI assistants (GitHub Copilot, Cursor, Claude Code) the minimum context needed to work in this repository without first reading the whole codebase. Keep it in sync with `AGENTS.md` and `README.md`; any change to the compliance policy, CLI flags, workflows, or build commands must be reflected here in the same commit.

## Project Purpose

`config-automation` runs scheduled, cross-repo maintenance against every repository of [`rios0rios0`](https://github.com/rios0rios0), [`medhub-life`](https://github.com/medhub-life), and [`prefy`](https://github.com/prefy). A fine-grained PAT is bound to a single resource owner, so each GitHub-facing workflow fans out with `strategy.matrix.owner` and passes each leg one owner (via `HARDEN_OWNER`) and that owner's own token; the SonarQube Cloud workflow has no matrix, because only `rios0rios0` has a SonarQube Cloud organization. Repositories are identified by their `owner/name` slug (`entities.Repository.QualifiedName()`), since bare names collide across owners:

1. **Daily compliance audit** — `.github/workflows/repo-compliance-audit.yaml` runs `go run ./cmd/harden-repos --phase 1 --fail-on-noncompliant` and fails CI when any repo drifts from the hardening policy. Uploads `${TMPDIR:-/tmp}/gh_hardening_audit_before.json` as an artifact.
2. **Weekly config and docs refresh** — `.github/workflows/config-and-docs-refresh.yaml` runs the `refresh-docs` CLI (`cmd/refresh-docs`), which also runs on Azure DevOps through `azure-devops/jobs/refresh-config-and-docs.yaml`. `refresh-docs discover` lists every owner of `.config-automation.yaml` with that owner's own token and batches each owner's non-fork, non-archived, non-disabled repos (`batch_size`, default `10`), so a matrix leg carries one owner and the name of its secret (`credential`); an unreachable owner is recorded, the rest still run, and the terminal `verify` job turns the run red. A single-repo dispatch refuses a repo the schedule leaves out. GitHub listing reuses the go-github adapter through `commands.FindOwnedRepositories`; Azure DevOps listing and both providers' PRs go through gitforge. Each leg runs `scripts/install-refresh-tools.sh` (the one pin list for the Claude CLI range and the checksum-verified chlog), clones `rios0rios0/pipelines`, and then runs `refresh-docs run --pipelines-dir`, whose `RefreshRepositoriesCommand` clones each repo, applies the layout, writes the shared ignore rules into its `.gitignore` with the pipelines `make gitignore` generator (`SyncIgnoreRulesCommand`: the host's file, never granted, drift on its own, a symlink skipped with a warning, a generator failure recorded as `gitignore:`), runs the read-only audit and the scoped refresh, guards the changelog and the added lines, and opens or updates the PR on the force-pushed `chore/config-and-docs-refresh` branch. Every failure is recorded under its reason and never aborts the batch. git gets the token only from a credential helper scoped to the provider's host, so it is never in a URL, `argv` or `.git/config`, and Claude Code starts with every hosting token withheld, every `${NAME}` a configured provider token reads included, and may not read under `/proc`, where its parents' environments hold the token (only `CLAUDE_CODE_OAUTH_TOKEN` remains). The usage-limit wordings (`monthly usage limit`, `monthly spend limit`, `hit your weekly limit`, `hit your 5-hour limit`, the `usage_limit_reached` code) record `claude-quota:` and skip the rest of the batch as `quota_skipped`; a safeguard refusal (`flagged this message for a cybersecurity topic`, the phrase its old and current wordings share, or either era's `cyber-use-case` / `real-time-cyber-safeguards` link) is `safeguard_skipped` plus a warning annotation and does not fail the leg (`SucceededWithIssues` on Azure Pipelines). The changelog target is enforced by the grant: `Bash(chlog new:*)` in a chlog repo (the `pipelines` basic-checks predicate), `Edit(/CHANGELOG.md)` otherwise, none without a changelog; in chlog mode a `CHANGELOG.md` or existing-fragment edit is restored and a fragment `chlog new` did not write fails the repo as `fragment:`. Every file grant is an `Edit(...)` rule, never `Write(...)`. The secret-shape guard reads the added lines of the drift and the staged changelog entry, taking the diff with `--text --no-ext-diff --no-textconv` so a repository's `.gitattributes` cannot hide them, fails closed, and refuses a credential shape as `secret-shape:`; the prompt template `internal/domain/entities/prompts/refresh_config_and_docs.md` carries the matching secret-hygiene wording, so the two move together. Repository-controlled output never acts as a CI command (`::stop-commands::` on GitHub Actions, `##vso[` defused on Azure Pipelines). Every run refreshes every target repository; there is no activity gate. Each leg prints `processed / no_drift / prs_created / prs_updated / quota_skipped / safeguard_skipped / failed` and exits non-zero only on `failed`. `workflow_dispatch` exposes `repo`, `batch_size`, `max_parallel`, and `max_turns` (defaults: `10`, `2`, `50`).
3. **Weekly release reconciliation** — `.github/workflows/release-reconcile.yaml` diffs every repo's released `CHANGELOG.md` versions against its git tags and re-pushes any missing tag at its bump commit, re-triggering the `pipelines` tag-push delivery path so a "bumped but never released" gap (a bump whose `main` run failed the quality gate) is recovered. Enumerates repos via `go run ./cmd/harden-repos --list-json`, delegates detection to the single-sourced `pipelines` primitive `global/scripts/shared/reconcile-releases.sh` (cloned at run time), and orchestrates via `scripts/reconcile-repos.sh`. Pushes with that owner's PAT (a `GITHUB_TOKEN`-pushed tag would not start delivery). `workflow_dispatch` exposes `repo`, `dry_run`, and `fail_on_gap` inputs; results go to `$GITHUB_STEP_SUMMARY`. Only a version above the repository's highest SemVer tag is pushed, since a tag push deploys and a lower gap is a bump a later release superseded.
4. **Daily Sonar analysis policy** — `.github/workflows/sonar-analysis-policy.yaml` runs `go run ./cmd/harden-repos --sonar-policy` against every project of the `rios0rios0` SonarQube Cloud organization (no owner matrix: `medhub-life` and `prefy` have no SonarQube Cloud organization). Per project it adds the missing `sonar.issue.ignore.multicriteria` pairs from `entities.DesiredSonarIssueExclusions()` (`POST api/settings/set`), accepts the still-open issues of those rules (`POST api/issues/bulk_change`, `do_transition=accept`), and marks their `TO_REVIEW` hotspots `REVIEWED`/`SAFE` (`POST api/hotspots/change_status`). The setting cannot be committed to the analyzed repo: automatic analysis reads only `sonar.sources`, `sonar.exclusions`, `sonar.inclusions`, `sonar.tests`, `sonar.test.exclusions`, `sonar.test.inclusions`, `sonar.sourceEncoding`, `sonar.cpd.exclusions`, `sonar.python.version` and `sonar.cfamily.reportingCppStandardOverride` from `.sonarcloud.properties`, with no issue-exclusion key among them. Exclusion and triage are two halves of one fix — the exclusion only binds the next analysis, the triage only clears what is already recorded. `workflow_dispatch` exposes `project` and `dry_run`; the secret is `SONAR_TOKEN` (a SonarQube Cloud *user* token whose user holds `Administer`, `Administer Issues` and `Administer Security Hotspots` on the projects).
5. **`cmd/harden-repos/`** — the Go CLI that implements the compliance policy and all phase commands.

## Prompt audit and refresh model

Before either phase, `refresh-docs layout` (`cmd/refresh-docs`) moves each repository into the
`AGENTS.md` layout: `AGENTS.md` holds the instructions and each `CLAUDE.md` beside
one contains only `@AGENTS.md`. A lone `CLAUDE.md` is renamed in its own commit so
history follows the file; text a `CLAUDE.md` adds beside an existing `AGENTS.md` is
appended under `## Migrated from CLAUDE.md` for the refresh to integrate; a missing
import or a symlink between the two becomes the import file. Other symlinks, ignored
paths, and non-UTF-8 files are reported, never changed. A second pass after the
refresh adds the import for an `AGENTS.md` the refresh created. Layout failures are
recorded as `layout:` and skip only that repository. The `agents`, `claude` and
`copilot` instruction-file settings (defaults per provider, Copilot off on Azure
DevOps; a flag beats the configuration) decide what may be created, and the layout
pass removes the files of a disabled kind, which the manifest lists under `removed`.

`refresh-docs run` runs the bundled `/checkup prompt-audit .` skill (an
alias of `/doctor prompt-audit`) in print mode before editing. Its audit sees
repository instruction files, `AGENTS.md` first, including nested guidance, rules,
skills, agents, and commands, with only `Read,Grep,Glob` available and user/project
settings and MCP servers disabled. It emits the report and proposed diff to the job
log; a failed, empty, or unsuccessful JSON result stops that repository before
editing and enters the existing batch failure handler. Git provenance is unavailable
to the audit, so ambiguous conflicts remain findings.

The second invocation verifies audit findings and factual documentation drift in
the exact files listed by the scope manifest. It covers root/nested `AGENTS.md`,
`.claude/CLAUDE.md`, `.claude/rules/`, Markdown skills and their references under
`.claude/skills/` and `.github/skills/`, agent definitions under `.claude/agents/`
and `.github/agents/`, `.claude/commands/`, Copilot instructions/prompts, root
README, and CONTRIBUTING at the root, `.github/`, `.azuredevops/`, or `docs/`.
Existing files only, plus the optional creation targets the instruction-file settings
allow; a one-line `CLAUDE.md` is never granted.
It holds `AGENTS.md` to Claude Code's under-200-lines guidance. Preserve frontmatter,
permissions, model pins, generated sections, and deliberate directory overrides.
Other findings stay in the log for human review. Both calls use
`CLAUDE_MODEL` (default `claude-opus-5-5`) and `CLAUDE_EFFORT` (default
`high`; Opus 5.5 otherwise runs at `medium`), detach stdin, skip the target
repository's settings, hooks, and MCP servers, run with `--permission-mode dontAsk`,
and have their own `MAX_TURNS` limit. The audit's JSON is read from stdout alone and
must be a successful, non-empty result before the refresh runs. The refresh lists its tools explicitly because
the default Linux tool set lacks `Grep` and `Glob`.

## Architecture

Clean Architecture with `domain` (contracts) / `infrastructure` (implementations) split. Dependencies always point inward; the domain layer never imports `github.com/google/go-github`.

```
config-automation/
├── cmd/
│   ├── harden-repos/               # CLI entry point + composition root (`main.go`, `container.go`)
│   └── refresh-docs/               # `discover`, `run`, `layout`, `scope`: the refresh, its config, CI presenters
├── internal/
│   ├── domain/
│   │   ├── commands/               # one command per phase + `--list-json` + `--dry-run`; refresh discovery, batch, scope, layout
│   │   ├── entities/               # `Repository`, `SecuritySettings`, `BranchProtection`, `Ruleset`, `AuditResult`, `SonarProject`, `compliance_policy.go`, `sonar_analysis_policy.go`
│   │   └── repositories/           # ports: repos, security, branch protection, Sonar, git, agent, pull requests, refresh targets
│   └── infrastructure/
│       └── repositories/           # `GoGithub…Repository` adapters over `github.com/google/go-github/v93`, gitforge adapters, git and Claude Code CLI adapters, plus `SonarCloudSonarProjectsRepository` over the SonarQube Cloud Web API
├── test/
│   ├── domain/
│   │   ├── builders/               # `RepositoryBuilder`, `AuditResultBuilder`
│   │   ├── doubles/repositories/   # in-memory doubles preferred over `testify/mock`
│   │   └── fixtures/               # refresh paths shared by the scope and batch tests
│   └── scripts/                    # Go tests that run `scripts/reconcile-repos.sh` against local repositories
├── .github/workflows/              # `repo-compliance-audit.yaml`, `config-and-docs-refresh.yaml`, `release-reconcile.yaml`, `sonar-analysis-policy.yaml`, `default.yaml`, `claude-mention.yaml`, `claude-review.yaml`
├── azure-devops/                   # jobs template that runs the refresh on Azure Pipelines
├── .config-automation.yaml         # the owners the GitHub refresh walks, with their token variables
└── scripts/
    ├── install-refresh-tools.sh    # Claude Code CLI range, checksum-verified chlog, refresh-docs build
    └── reconcile-repos.sh          # the release reconciliation's per-repository loop
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
- **`api/issues/bulk_change` reports a refusal in its 200 body, not in the status line.** It answers with a `total`/`success`/`ignored`/`failures` tally, and an issue the caller may not transition (the token's user lacks `Administer Issues` there) lands in `ignored`. `SonarCloudSonarProjectsRepository.AcceptIssues` therefore decodes the tally, returns how many actually moved, and errors on any ignored or failed one; `ApplySonarPolicyCommand` logs that number rather than `len(keys)`. Treating a 2xx as success would log "26 issues accepted", exit 0, and leave all 26 open — the sibling write paths (`api/settings/set`, `api/hotspots/change_status`) only avoid that hole because they answer 403.
- **The Sonar policy is scoped by rule key, never by file.** `entities.DesiredSonarIssueExclusions()` excludes `githubactions:S7637` on `**/*.y*ml` (the pattern also covers composite `action.yaml` files, analysed by the same language). Every other rule keeps firing on those files — `S7634`, `S7630` and the rest are real findings that must stay reported — which is why this is an issue exclusion and not a `sonar.exclusions` entry or a deactivated quality-profile rule. `entities.DesiredSonarTriagedRuleKeys()` is *derived* from that list so the two cannot drift, `MergeSonarIssueExclusions` writes the union with whatever the project already had (`api/settings/set` replaces a property set wholesale), and the three sub-steps are attempted independently because `Administer`, `Administer Issues` and `Administer Security Hotspots` are three separate SonarQube Cloud permissions.

## Build / Test / Lint / Run

```bash
make build                          # compile bin/harden-repos
make test                           # go test -race over cmd/ and internal/ via rios0rios0/pipelines
make lint                           # golangci-lint run ./...
make sast                           # full SAST suite via rios0rios0/pipelines
go test -run TestAuditRepositoriesCommand ./internal/domain/commands/
go test -count=1 ./cmd/refresh-docs/...  # the refresh end to end against local repositories and a fake Claude CLI
go test -count=1 ./test/scripts/...      # the release reconciliation's shell
```

CLI phases:

```bash
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --phase 1   # read-only audit
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --phase 2   # repo settings
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --phase 3   # security settings
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --phase 4   # branch protection + ruleset
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --phase 5   # re-audit + diff snapshot
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --dry-run   # phases 1-4, no mutations
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --list-json # matrix input for release-reconcile

SONAR_TOKEN=<user-token> go run ./cmd/harden-repos --sonar-policy               # SonarQube Cloud analysis policy
go run ./cmd/harden-repos --sonar-policy --dry-run                              # preview; needs no credential
```

## Environment Variables

| Variable                         | Purpose                                                                 |
|----------------------------------|-------------------------------------------------------------------------|
| `HARDEN_OWNER`                   | Comma-separated GitHub owners/orgs to audit, in order (default: `rios0rios0`). The workflows set it to a **single** owner per matrix leg, because a fine-grained PAT is bound to one resource owner. |
| `GH_TOKEN` / `GITHUB_TOKEN`      | Bearer token for `github.com/google/go-github`.                         |
| `TMPDIR`                         | Honored by `os.TempDir()` for `gh_hardening_audit_before.json` output.  |
| `SONAR_TOKEN`                    | SonarQube Cloud **user** token for `--sonar-policy` (not an analysis token). Absent, reads still answer for public projects and every mutation fails with the API's own denial, which differs per endpoint: uncredentialed, `api/settings/set` answers `404 Project doesn't exist` (SonarQube Cloud hides a project from a caller that cannot administer it) while `api/issues/bulk_change` and `api/hotspots/change_status` answer `401 Authentication is required`; an under-permissioned token gets `403 Insufficient privileges`. |
| `SONAR_ORGANIZATION`             | Single SonarQube Cloud organization walked by `--sonar-policy` (default `rios0rios0`). Deliberately not `HARDEN_OWNER`: the other owners have no SonarQube Cloud organization. |
| `SONAR_HOST_URL`                 | SonarQube Cloud base URL (default `https://sonarcloud.io`).             |

Workflow secrets, one fine-grained PAT per owner shared by the three GitHub workflows (a fine-grained token is bound to a single resource owner, and each token's lifetime must be 366 days or less): `PERSONAL_ACCESS_TOKEN` (`rios0rios0`), `MEDHUB_ACCESS_TOKEN` (`medhub-life`), `PREFY_ACCESS_TOKEN` (`prefy`) — the same names `autobump-automation` and `autoupdate-automation` use — plus `CLAUDE_CODE_OAUTH_TOKEN` (refresh Claude Code CLI, shared) and `SONAR_TOKEN` (SonarQube Cloud user token for the analysis policy; a single organization, so not per-owner).

## Conventions

- **Go style** — `snake_case` file names, one-letter receiver names (`c` for `Command`, `r` for `Repository`), manual constructor injection in each `cmd/<app>/container.go` (no Dig), Logrus for logging, testify for assertions, no framework tags on entities.
- **Tests** — no build tag on unit tests (`go test ./...` runs them all), `_test` package suffix, `t.Parallel()` on every top-level test function, BDD `// given` / `// when` / `// then` blocks. Prefer in-memory doubles over `testify/mock`. Builders live under `test/domain/builders/`.
- **YAML files** — `.yaml` (never `.yml`); single-quote string values except where variable interpolation requires double quotes; never quote booleans or numbers.
- **Commits** — `type(SCOPE): message` in simple past tense, no trailing period. See `.claude/rules/git-flow.md` in the user's global rules.
- **Changelog** — every change writes its own fragment under `.changes/unreleased/` with `chlog new --kind <Kind> --body '...'`, in the same commit; `CHANGELOG.md` is generated from them and is never edited by hand. Keep a Changelog kinds. Proper nouns capitalized (GitHub, Go, Docker), code identifiers in backticks, versions in backticks.
- **Actions pins** — every step-level `actions/*` use is pinned to a full commit SHA with a trailing `# vX.Y.Z` comment, never a bare tag (a security decision from `0.3.8`; do not revert a SHA to `@v6`). Keep every workflow on the same latest major: `actions/checkout` v6, `actions/upload-artifact` v7, `actions/setup-go` v6, `actions/setup-node` v6. When bumping, update the SHA and its comment across all four scheduled workflows (`repo-compliance-audit.yaml`, `config-and-docs-refresh.yaml`, `release-reconcile.yaml`, `sonar-analysis-policy.yaml`) in the same commit. The `@anthropic-ai/claude-code` npm package is the exception — installed from the open npm range `>=2.1.283`, so the `setup-node` major has to keep up with the CLI's `engines` floor (`>=22` as of `2.1.316`); npm only warns (`EBADENGINE`) on an older Node.js.
- **Go toolchain** — every `actions/setup-go` step uses `go-version-file: 'go.mod'`, never a hardcoded `go-version`. `setup-go` exports `GOTOOLCHAIN=local`, so a loose spec like `'1.26'` resolves to whatever patch release the runner cached and then hard-fails every `go run` once `go.mod` requires a newer patch. Bumping `go.mod` is enough; the workflows follow.

## When Editing the Policy

Ruleset and branch-protection changes propagate to every `rios0rios0` repo on the next audit run. When touching `compliance_policy.go` or `ComputeIssues()`:

1. Update the policy tests under `internal/domain/entities/` and the command tests under `internal/domain/commands/`.
2. Run `HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --dry-run` and confirm the non-compliant set matches expectations.
3. Update `AGENTS.md`, `README.md`, and this file together.
4. Record the change in a fragment: `chlog new --kind Changed --body '...'`.

The same applies to `sonar_analysis_policy.go`: a change there rewrites a project setting on every project of the SonarQube Cloud organization on the next daily run, and every rule it lists stops being reported. Preview it with `go run ./cmd/harden-repos --sonar-policy --dry-run` (no credential needed) before merging, and never widen the exclusion from a rule key to a file pattern.

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
  `chlog new --kind <Kind> --body '<past-tense description>'`
- Write an apostrophe inside the single-quoted body as `'\''`.
- Valid kinds: Added, Changed, Deprecated, Removed, Fixed, Security
- Choose the kind that best matches the change (e.g., new feature → Added,
  bug fix → Fixed, behavior change → Changed, removal → Removed, security fix → Security).
- If the change is backward-INCOMPATIBLE with the public API (a breaking
  change), you MUST add the `--breaking` flag:
  `chlog new --kind <Kind> --breaking --body '<past-tense description>'`.
  This is the ONLY thing that triggers a major version bump — the kind alone
  never does (per SemVer, major = incompatible change). When unsure whether a
  change breaks compatibility, ask the user instead of guessing.
- Fragments are YAML files in `.changes/unreleased/`; stage them with your commit.
- `chlog check` fails the build when a fragment is missing — never skip it.
<!-- chlog:end -->
