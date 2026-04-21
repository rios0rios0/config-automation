<h1 align="center">fleet-maintenance</h1>
<p align="center">
    <a href="https://github.com/rios0rios0/fleet-maintenance/releases/latest">
        <img src="https://img.shields.io/github/release/rios0rios0/fleet-maintenance.svg?style=for-the-badge&logo=github" alt="Latest Release"/></a>
    <a href="https://github.com/rios0rios0/fleet-maintenance/blob/main/LICENSE">
        <img src="https://img.shields.io/github/license/rios0rios0/fleet-maintenance.svg?style=for-the-badge&logo=github" alt="License"/></a>
    <a href="https://github.com/rios0rios0/fleet-maintenance/actions/workflows/repo-compliance-audit.yaml">
        <img src="https://img.shields.io/github/actions/workflow/status/rios0rios0/fleet-maintenance/repo-compliance-audit.yaml?branch=main&style=for-the-badge&logo=github&label=compliance" alt="Compliance Audit Status"/></a>
</p>

Scheduled GitHub Actions workflows that keep every [`rios0rios0`](https://github.com/rios0rios0) repository compliant with shared hardening policy and in sync with the team's AI-assistant guidance files.

## Features

- **Repo compliance audit** — daily cron that fails CI if any `rios0rios0` repo drifts from the hardening policy (Dependabot, secret scanning, push protection, branch protection, `main-protection` ruleset, merge settings, wiki/projects flags).
- **AI assistant docs refresh** — weekly matrix job that runs Claude Code against every non-fork non-archived repo, updates `CLAUDE.md` and `.github/copilot-instructions.md` only when they've drifted, records the change in `CHANGELOG.md`, and opens a single PR per repo.

## Prerequisites

Three repository secrets must be set on `rios0rios0/fleet-maintenance`:

| Secret | Purpose | Scope |
|---|---|---|
| `COMPLIANCE_AUDIT_TOKEN` | Lists all `rios0rios0` repos and reads security/ruleset endpoints for the daily audit. | Classic PAT with the `repo` scope, **or** fine-grained PAT scoped to all repositories under `rios0rios0` with read access to `Administration`, `Contents`, `Metadata`, `Webhooks`, and to `Dependabot alerts` and `Secret scanning alerts`. |
| `CLAUDE_MD_REFRESH_TOKEN` | Pushes the `chore/ai-docs-refresh` branch and opens PRs on each target repo during the weekly refresh. | Fine-grained PAT scoped to all repositories under `rios0rios0` with `Contents: write`, `Pull requests: write`, and `Metadata: read`. |
| `CLAUDE_CODE_OAUTH_TOKEN` | Authenticates `anthropics/claude-code-action@v1` during the refresh. | Claude Code OAuth token. |

Set them with:

```bash
gh secret set COMPLIANCE_AUDIT_TOKEN -R rios0rios0/fleet-maintenance
gh secret set CLAUDE_MD_REFRESH_TOKEN -R rios0rios0/fleet-maintenance
gh secret set CLAUDE_CODE_OAUTH_TOKEN -R rios0rios0/fleet-maintenance
```

## Usage

Both workflows run on cron; no manual action is needed in steady state.

To trigger a single repo refresh manually (useful for testing):

```bash
gh workflow run ai-docs-refresh.yaml -R rios0rios0/fleet-maintenance -f repo=autobump
```

To run the compliance audit on demand:

```bash
gh workflow run repo-compliance-audit.yaml -R rios0rios0/fleet-maintenance
```

To apply remediation phases 2–4 of `harden_repos.py` (repo settings, security features, branch protection) against a single repo locally:

```bash
HARDEN_OWNER=rios0rios0 python3 scripts/harden_repos.py --phase 2 --repo <repo>
HARDEN_OWNER=rios0rios0 python3 scripts/harden_repos.py --phase 3 --repo <repo>
HARDEN_OWNER=rios0rios0 python3 scripts/harden_repos.py --phase 4 --repo <repo>
```

`--dry-run` previews phases 1–4 without mutating anything.

## Architecture

```
fleet-maintenance/
├── .github/
│   └── workflows/
│       ├── repo-compliance-audit.yaml    # daily cron -> harden_repos.py --phase 1
│       └── ai-docs-refresh.yaml          # weekly cron -> anthropics/claude-code-action@v1
└── scripts/
    ├── harden_repos.py                   # pure Python 3.12 + gh CLI; 5-phase model
    └── refresh_ai_docs_prompt.md         # prompt consumed by the refresh workflow
```

`harden_repos.py` implements a 5-phase model:

- **Phase 1** — read-only audit, writes `/tmp/gh_hardening_audit_before.json`; with `--fail-on-noncompliant` exits non-zero when any repo violates policy.
- **Phase 2** — applies repo settings (merge flags, `delete_branch_on_merge`, wiki/projects).
- **Phase 3** — applies security settings (Dependabot, secret scanning, push protection).
- **Phase 4** — applies branch protection and the `main-protection` ruleset.
- **Phase 5** — re-audits and diffs against the phase-1 snapshot.

See `CLAUDE.md` for the script's invariants and conventions.

## Related Repositories

- **[.github](https://github.com/rios0rios0/.github)** — default community health files, workflow templates, and reusable Claude Code workflows for every `rios0rios0` repository.
- **[pipelines](https://github.com/rios0rios0/pipelines)** — production-ready SDLC pipelines referenced by the workflow templates.
- **[autobump](https://github.com/rios0rios0/autobump)** — automated CHANGELOG and release management enforcing Keep a Changelog + SemVer.
- **[guide](https://github.com/rios0rios0/guide/wiki)** — development standards wiki covering Git Flow, architecture, CI/CD, security, testing, and code style.

## Contributing

Contributions are welcome. See `CONTRIBUTING.md` for guidelines.

## License

See [LICENSE](LICENSE) file for details.
