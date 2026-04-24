<h1 align="center">config-automation</h1>
<p align="center">
    <a href="https://github.com/rios0rios0/config-automation/releases/latest">
        <img src="https://img.shields.io/github/release/rios0rios0/config-automation.svg?style=for-the-badge&logo=github" alt="Latest Release"/></a>
    <a href="https://github.com/rios0rios0/config-automation/blob/main/LICENSE">
        <img src="https://img.shields.io/github/license/rios0rios0/config-automation.svg?style=for-the-badge&logo=github" alt="License"/></a>
    <a href="https://github.com/rios0rios0/config-automation/actions/workflows/repo-compliance-audit.yaml">
        <img src="https://img.shields.io/github/actions/workflow/status/rios0rios0/config-automation/repo-compliance-audit.yaml?branch=main&style=for-the-badge&logo=github&label=compliance" alt="Compliance Audit Status"/></a>
</p>

Scheduled GitHub Actions workflows that keep every [`rios0rios0`](https://github.com/rios0rios0) repository compliant with shared hardening policy and in sync with the team's AI-assistant guidance files.

## Features

- **Repo compliance audit** — daily cron that fails CI if any `rios0rios0` repo drifts from the hardening policy (Dependabot, secret scanning, push protection, branch protection, `main-protection` ruleset, merge settings, wiki/projects flags).
- **AI assistant docs refresh** — weekly matrix job that runs Claude Code against every non-fork non-archived repo, updates `CLAUDE.md` and `.github/copilot-instructions.md` only when they've drifted, records the change in `CHANGELOG.md`, and opens a single PR per repo.

## Prerequisites

Three repository secrets must be set on `rios0rios0/config-automation`:

| Secret                    | Purpose                                                                                                | Scope                                                                                                                                                                                                                                       |
|---------------------------|--------------------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `COMPLIANCE_AUDIT_TOKEN`  | Lists all `rios0rios0` repos and reads security/ruleset endpoints for the daily audit.                 | Classic PAT with the `repo` scope, **or** fine-grained PAT scoped to all repositories under `rios0rios0` with read access to `Administration`, `Contents`, `Metadata`, `Webhooks`, and to `Dependabot alerts` and `Secret scanning alerts`. |
| `CLAUDE_MD_REFRESH_TOKEN` | Pushes the `chore/ai-docs-refresh` branch and opens PRs on each target repo during the weekly refresh. | Fine-grained PAT scoped to all repositories under `rios0rios0` with `Contents: write`, `Pull requests: write`, and `Metadata: read`.                                                                                                        |
| `CLAUDE_CODE_OAUTH_TOKEN` | Authenticates `anthropics/claude-code-action@v1` during the refresh.                                   | Claude Code OAuth token.                                                                                                                                                                                                                    |

Set them with:

```bash
gh secret set COMPLIANCE_AUDIT_TOKEN -R rios0rios0/config-automation
gh secret set CLAUDE_MD_REFRESH_TOKEN -R rios0rios0/config-automation
gh secret set CLAUDE_CODE_OAUTH_TOKEN -R rios0rios0/config-automation
```

## Usage

Both workflows run on cron; no manual action is needed in steady state.

Manual trigger — one-off AI docs refresh against a single repo:

```bash
gh workflow run ai-docs-refresh.yaml -R rios0rios0/config-automation -f repo=autobump
```

Manual trigger — compliance audit on demand:

```bash
gh workflow run repo-compliance-audit.yaml -R rios0rios0/config-automation
```

Locally, the CLI supports the full phase model:

```bash
# Audit-only (read-only, writes /tmp/gh_hardening_audit_before.json)
HARDEN_OWNER=rios0rios0 go run ./cmd/harden-repos --phase 1

# Apply phases locally (mutates)
HARDEN_OWNER=rios0rios0 go run ./cmd/harden-repos --phase 2 --repo <name>
HARDEN_OWNER=rios0rios0 go run ./cmd/harden-repos --phase 3 --repo <name>
HARDEN_OWNER=rios0rios0 go run ./cmd/harden-repos --phase 4 --repo <name>

# Preview every phase without mutating anything
HARDEN_OWNER=rios0rios0 go run ./cmd/harden-repos --dry-run

# List target repos for the AI docs matrix (JSON on stdout)
HARDEN_OWNER=rios0rios0 go run ./cmd/harden-repos --list-json

# Re-audit and diff against the before snapshot
HARDEN_OWNER=rios0rios0 go run ./cmd/harden-repos --phase 5
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
│   │   ├── entities/               # Repository, AuditResult, ComplianceIssue, etc.
│   │   └── repositories/           # three port interfaces (repos, security, branch protection)
│   └── infrastructure/
│       └── repositories/           # go-github adapters that implement the ports
├── test/
│   └── domain/
│       ├── builders/               # RepositoryBuilder, AuditResultBuilder
│       └── doubles/repositories/   # in-memory doubles preferred over mocks per the test rules
├── .github/workflows/              # two scheduled workflows that run this CLI
└── scripts/
    └── refresh_ai_docs_prompt.md   # prompt consumed by the AI docs refresh workflow
```

The CLI follows the 5-phase compliance model:

- **Phase 1** (`--phase 1`) — read-only audit; writes `${TMPDIR:-/tmp}/gh_hardening_audit_before.json`; with `--fail-on-noncompliant` exits non-zero when any repo drifts.
- **Phase 2** (`--phase 2`) — applies repo settings (merge flags, `delete_branch_on_merge`, wiki/projects).
- **Phase 3** (`--phase 3`) — applies security settings (Dependabot, secret scanning, push protection).
- **Phase 4** (`--phase 4`) — applies branch protection and the `main-protection` ruleset.
- **Phase 5** (`--phase 5`) — re-audits and diffs against the phase-1 snapshot.
- **`--dry-run`** — runs phases 1-4 with no side effects; prints "would apply" for every mutation.

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
