<h1 align="center">config-automation</h1>
<p align="center">
    <a href="https://github.com/rios0rios0/config-automation/releases/latest">
        <img src="https://img.shields.io/github/release/rios0rios0/config-automation.svg?style=for-the-badge&logo=github" alt="Latest Release"/></a>
    <a href="https://github.com/rios0rios0/config-automation/blob/main/LICENSE">
        <img src="https://img.shields.io/github/license/rios0rios0/config-automation.svg?style=for-the-badge&logo=github" alt="License"/></a>
    <a href="https://github.com/rios0rios0/config-automation/actions/workflows/repo-compliance-audit.yaml">
        <img src="https://img.shields.io/github/actions/workflow/status/rios0rios0/config-automation/repo-compliance-audit.yaml?branch=main&style=for-the-badge&logo=github&label=compliance" alt="Compliance Audit Status"/></a>
</p>

Scheduled GitHub Actions workflows that keep every repository under [`rios0rios0`](https://github.com/rios0rios0), [`medhub-life`](https://github.com/medhub-life), and [`prefy`](https://github.com/prefy) compliant with shared hardening policy and in sync with the team's configuration and documentation files.

The CLI takes its owners from the `HARDEN_OWNER` environment variable, a comma-separated list. The workflows do **not** pass all owners at once: a GitHub fine-grained PAT is bound to a single resource owner, so each workflow fans out into one job per owner and hands that job only its own owner and its own token. Adding an organization means adding a matrix entry and its secret — no code change needed. Because repository names are only unique within one owner, reports, snapshots, and diffs key on the `owner/name` slug.

## Features

- **Repo compliance audit** — daily cron that fails CI if any repo in any configured owner drifts from the hardening policy (Dependabot, secret scanning, push protection, branch protection, `main-protection` ruleset, merge settings, wiki/projects flags, and GitHub Actions disabled on forks).
- **Config and docs refresh** — weekly matrix job that runs Claude Code against every non-fork non-archived repo, updates the in-scope configuration and documentation files only when they've drifted, records the change in `CHANGELOG.md`, and opens a single PR per repo. Today the in-scope set is `CLAUDE.md`, `.github/copilot-instructions.md`, and the repository's tailored GitHub Copilot code-review skill at `.github/skills/code-review/SKILL.md`; the workflow is intentionally named for the broader scope so future targets (diagrams, additional config files) can be added without renaming.
- **Sonar analysis policy** — daily job that applies the fleet's SonarQube Cloud analysis policy to every project of the `rios0rios0` organization: it adds the `sonar.issue.ignore.multicriteria` pairs the fleet needs and triages the findings those rules already raised. Today the policy silences `githubactions:S7637` ("use a full commit SHA") on YAML, because every repository calls the shared CI as `uses: rios0rios0/pipelines/.github/workflows/<name>.yaml@main` and that floating reference is deliberate.
- **Release reconciliation** — weekly job that diffs every repo's released `CHANGELOG.md` versions against its git tags and re-pushes any missing tag at its bump commit, re-triggering the [`pipelines`](https://github.com/rios0rios0/pipelines) tag-push delivery path to recover a "bumped but never released" gap (a bump whose `main` run failed the quality gate). Delegates detection to the single-sourced pipelines primitive and reuses the same per-owner refresh PATs — no new secret required.

## Prerequisites

A GitHub fine-grained PAT is bound to a **single** resource owner, so there is one PAT **per
owner**, shared by the three GitHub workflows, plus the Claude Code token and the SonarQube Cloud
token:

| Secret                    | Owner         | Purpose                                                                                          |
|---------------------------|---------------|--------------------------------------------------------------------------------------------------|
| `PERSONAL_ACCESS_TOKEN`   | `rios0rios0`  | Audits, refreshes and reconciles every repository of this owner.                                 |
| `MEDHUB_ACCESS_TOKEN`     | `medhub-life` | Same, for `medhub-life`.                                                                         |
| `PREFY_ACCESS_TOKEN`      | `prefy`       | Same, for `prefy`.                                                                               |
| `CLAUDE_CODE_OAUTH_TOKEN` | —             | Authenticates the Claude Code CLI during the refresh.                                            |
| `SONAR_TOKEN`             | —             | SonarQube Cloud **user** token for the daily analysis policy (see below).                        |

The same three secret names are used by [`autobump-automation`](https://github.com/rios0rios0/autobump-automation)
and [`autoupdate-automation`](https://github.com/rios0rios0/autoupdate-automation), so one PAT per
owner covers the whole automation fleet.

Each owner's PAT is a fine-grained token scoped to all repositories under that owner, and needs the
union of what those three workflows do:

- `Administration: read`, `Metadata: read`, `Webhooks: read`, `Dependabot alerts: read` and
  `Secret scanning alerts: read` — the daily compliance audit. `Administration: read` also covers
  the per-repository GitHub Actions switch (`GET /repos/{owner}/{repo}/actions/permissions`) that
  the forks rule audits; flipping that switch is phase 3, which — like every other apply phase —
  only runs locally and needs `Administration: write`.
- `Contents: write` and `Pull requests: write` — the weekly config/docs refresh branch and PRs, and
  the release-recovery tag pushes. The tag push must come from a PAT: a tag pushed with
  `GITHUB_TOKEN` does not start the delivery workflow.

Every fine-grained token's lifetime must be **366 days or less**. `medhub-life` and `prefy` reject
longer-lived fine-grained tokens with `403 ... forbids access via a fine-grained personal access
tokens if the token's lifetime is greater than 366 days`.

Set them with:

```bash
gh secret set PERSONAL_ACCESS_TOKEN -R rios0rios0/config-automation
gh secret set MEDHUB_ACCESS_TOKEN -R rios0rios0/config-automation
gh secret set PREFY_ACCESS_TOKEN -R rios0rios0/config-automation
gh secret set CLAUDE_CODE_OAUTH_TOKEN -R rios0rios0/config-automation
gh secret set SONAR_TOKEN -R rios0rios0/config-automation
```

`SONAR_TOKEN` is a SonarQube Cloud **user** token (My Account > Security > Generate Tokens,
type *User Token*), **not** a project analysis token: an analysis token can only submit a report,
while this job administers projects. Its user needs three separate permissions on every project of
the organization — `Administer` to write the issue-exclusion setting, `Administer Issues` to accept
an issue, and `Administer Security Hotspots` to mark a hotspot reviewed. The organization
administrator has all three. Only `rios0rios0` has a SonarQube Cloud organization, so this secret is
not per-owner.

`sonar-analysis-policy.yaml` has no owner matrix: only `rios0rios0` has a SonarQube Cloud
organization.

To cover another owner, add a `strategy.matrix.owner` entry to each per-owner workflow (plus the matching
`OWNERS` entry and `TOKEN_*` line in `config-and-docs-refresh.yaml`) and create its secret.

## Usage

Every workflow runs on cron; no manual action is needed in steady state.

Manual trigger — one-off config-and-docs refresh against a single repo. Pass
`owner/name` to reach any configured owner; a bare name uses the first one:

```bash
gh workflow run config-and-docs-refresh.yaml -R rios0rios0/config-automation -f repo=autobump
gh workflow run config-and-docs-refresh.yaml -R rios0rios0/config-automation -f repo=medhub-life/frontend
```

Manual trigger — compliance audit on demand:

```bash
gh workflow run repo-compliance-audit.yaml -R rios0rios0/config-automation
```

Manual trigger — Sonar analysis policy, whole organization or a single project:

```bash
gh workflow run sonar-analysis-policy.yaml -R rios0rios0/config-automation
gh workflow run sonar-analysis-policy.yaml -R rios0rios0/config-automation -f dry_run=true
gh workflow run sonar-analysis-policy.yaml -R rios0rios0/config-automation -f project=autobump
```

Locally, the CLI supports the full phase model:

```bash
# Audit-only (read-only, writes /tmp/gh_hardening_audit_before.json)
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --phase 1

# Apply phases locally (mutates). --repo matches the bare name in every
# configured owner, so narrow HARDEN_OWNER when a name is not unique.
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --phase 2 --repo <name>
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --phase 3 --repo <name>
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --phase 4 --repo <name>

# Preview every phase without mutating anything
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --dry-run

# List target repos for the config-and-docs refresh matrix (JSON on stdout)
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --list-json

# Re-audit and diff against the before snapshot
HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --phase 5

# Apply the SonarQube Cloud analysis policy (needs SONAR_TOKEN to mutate;
# --dry-run reports what would change and works without a credential)
SONAR_TOKEN=<user-token> go run ./cmd/harden-repos --sonar-policy
go run ./cmd/harden-repos --sonar-policy --dry-run
go run ./cmd/harden-repos --sonar-policy --dry-run --repo autobump
```

## Architecture

```
config-automation/
├── cmd/
│   └── harden-repos/               # CLI entry point + Dig wiring
├── internal/
│   ├── container.go                # top-level DI orchestrator
│   ├── domain/
│   │   ├── commands/               # one command per phase + --list-json + --dry-run
│   │   ├── entities/               # Repository, AuditResult, ComplianceIssue, SonarProject, etc.
│   │   └── repositories/           # four port interfaces (repos, security, branch protection, Sonar projects)
│   └── infrastructure/
│       └── repositories/           # go-github and SonarQube Cloud adapters that implement the ports
├── test/
│   └── domain/
│       ├── builders/               # RepositoryBuilder, AuditResultBuilder
│       └── doubles/repositories/   # in-memory doubles preferred over mocks per the test rules
├── .github/workflows/              # four scheduled workflows that run this CLI
└── scripts/
    └── refresh_config_and_docs_prompt.md   # prompt consumed by the config-and-docs refresh workflow
```

The CLI follows the 5-phase compliance model:

- **Phase 1** (`--phase 1`) — read-only audit; writes `${TMPDIR:-/tmp}/gh_hardening_audit_before.json`; with `--fail-on-noncompliant` exits non-zero when any repo drifts.
- **Phase 2** (`--phase 2`) — applies repo settings (merge flags, `delete_branch_on_merge`, wiki/projects).

  The merge flags encode a **semi-linear history**: `allow_merge_commit` and `allow_rebase_merge` on,
  `allow_squash_merge` off. A pull request is rebased onto its base first — the *Update with rebase*
  option, which GitHub only offers while rebase merging is enabled — and then landed with *Merge pull
  request*, so `main` carries one merge commit per pull request over an otherwise linear ancestry.
  Squashing is disabled because it discards the branch's commits, and GitHub's own *Rebase and merge*
  button is left visible but must not be used: it fast-forwards and drops the merge commit. GitHub has
  no single "semi-linear merge" option the way Azure DevOps does, so the policy removes the buttons
  that break the shape instead of selecting one.
- **Phase 3** (`--phase 3`) — applies security settings (Dependabot, secret scanning, push protection) and
  disables GitHub Actions on forks.

  Forks are exempt from Dependabot and secret scanning — an upstream sync wipes them — and are instead
  required to keep the repository-level **GitHub Actions switch off**: a fork carries the upstream's
  workflows verbatim, and nothing in this account needs a fork to run any. The audit reports a fork
  whose switch is on as `actions_enabled=true(want false)` (or `actions_enabled=unknown` when the
  switch could not be read, which is drift to look at rather than compliance), `--dry-run` shows it as
  `actions_disabled`, and phase 3 turns it off. Repositories of our own are never checked: their
  workflows are ours.
- **Phase 4** (`--phase 4`) — applies branch protection and the `main-protection` ruleset.
- **Phase 5** (`--phase 5`) — re-audits and diffs against the phase-1 snapshot.
- **`--dry-run`** — runs phases 1-4 with no side effects; prints "would apply" for every mutation.

Outside the phase model, `--sonar-policy` enforces the SonarQube Cloud half of the fleet policy.
It walks every project of `$SONAR_ORGANIZATION` (default `rios0rios0`) and, per project, adds the
missing `sonar.issue.ignore.multicriteria` pairs, accepts the still-open issues of those rules, and
marks their `TO_REVIEW` hotspots reviewed/safe. `--dry-run` and `--repo` both apply.

Two details are load-bearing:

- **The setting cannot live in the analyzed repository.** Every project runs SonarQube Cloud
  [automatic analysis](https://docs.sonarsource.com/sonarqube-cloud/analyzing-source-code/automatic-analysis),
  which reads only `sonar.sources`, `sonar.exclusions`, `sonar.inclusions`, `sonar.tests`,
  `sonar.test.exclusions`, `sonar.test.inclusions`, `sonar.sourceEncoding`, `sonar.cpd.exclusions`,
  `sonar.python.version` and `sonar.cfamily.reportingCppStandardOverride` from
  `.sonarcloud.properties` — no issue-exclusion key among them. The exclusion is therefore written
  to each project through `POST api/settings/set`, and this job is what keeps that fleet-wide.
- **The exclusion and the triage are two halves of one fix.** The exclusion only takes effect on the
  *next* analysis, which under automatic analysis means the next push, so on its own it leaves every
  quality gate red in the meantime; the triage clears what is already recorded and makes the gate
  recompute now, but says nothing about the next occurrence.

Scoping by rule key is what keeps this narrow: `githubactions:S7634` (only pass required secrets),
`githubactions:S7630` (script injection) and every other rule keep firing on the very same workflow
files, which a `sonar.exclusions` entry would have silenced too. Third-party actions are still
pinned to a full commit SHA — that policy is enforced by the `pipelines` scheduled dependency job,
which fails when any pin goes stale, not by S7637.

See `CLAUDE.md` for invariants and conventions.

## Development

```bash
make build           # compile bin/harden-repos
make test            # run unit tests (// given / // when / // then BDD style)
make lint            # golangci-lint
make sast            # full SAST suite via rios0rios0/pipelines
make run ARGS='--phase 1 --repo autobump'
```

## Related Repositories

- **[.github](https://github.com/rios0rios0/.github)** — default community health files, workflow templates, and reusable Claude Code workflows for every `rios0rios0` repository.
- **[pipelines](https://github.com/rios0rios0/pipelines)** — production-ready SDLC pipelines referenced by the workflow templates.
- **[autobump](https://github.com/rios0rios0/autobump)** — automated CHANGELOG and release management enforcing Keep a Changelog + SemVer.
- **[guide](https://github.com/rios0rios0/guide/wiki)** — development standards wiki covering Git Flow, architecture, CI/CD, security, testing, and code style.

## Contributing

Contributions are welcome. See `CONTRIBUTING.md` for guidelines.

## License

See [LICENSE](LICENSE) file for details.
