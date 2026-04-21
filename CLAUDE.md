# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Repository Is

This repo houses **scheduled, cross-repo maintenance workflows** that run from a central location and act on every `rios0rios0` repository. There are two of them, and the Python script they share:

1. **`.github/workflows/repo-compliance-audit.yaml`** — daily (06:00 UTC) audit that runs `scripts/harden_repos.py --phase 1 --fail-on-noncompliant` against every `rios0rios0` repo. Fails if any repo drifts from the compliance policy. Uploads `/tmp/gh_hardening_audit_before.json` as an artifact.

2. **`.github/workflows/ai-docs-refresh.yaml`** — weekly (Mondays 07:00 UTC) matrix workflow. The `discover` job calls `scripts/harden_repos.py --list-json` to enumerate non-fork non-archived repos. The `refresh` job self-checks-out this repo to load `scripts/refresh_ai_docs_prompt.md`, then checks out each target repo and invokes `anthropics/claude-code-action@v1` against it. Drift detection uses `git add -N` (intent-to-add) followed by `git diff -w --quiet` so modified and newly-created `CLAUDE.md` / `.github/copilot-instructions.md` both count; whitespace-only diffs are ignored. `CHANGELOG.md` is staged alongside the AI-docs files (so Claude's release-note entry lands in the same PR) but is intentionally **not** part of the drift gate — a stray CHANGELOG-only edit cannot open a spurious PR. Branch name is stable (`chore/ai-docs-refresh`) and force-pushed so repeated runs update a single open PR.

3. **`scripts/harden_repos.py`** — the heart of the compliance system. Understanding its phase model is required before editing.

## The `harden_repos.py` Script

Phase model:

- **Phase 1 (`--phase 1`)** — read-only audit. Builds an `audits` list of dicts (one per repo) and writes `/tmp/gh_hardening_audit_before.json`. With `--fail-on-noncompliant` (used by CI) it exits 1 when `compute_issues()` reports any violations.
- **Phases 2/3/4** — mutations. Each first re-runs phase 1 to get fresh audit data, then applies only the diffs. Phase 4 (branch protection + `main-protection` ruleset) skips private repos and repos where `protection_available=False`.
- **Phase 5** — re-audits and diffs against the phase-1 snapshot on disk.
- **`--dry-run`** — runs phases 1–4 with no side effects; `--phase` is ignored.
- **`--list-json`** — emits a JSON array of `{name, default_branch}` filtered to non-fork non-archived repos, consumed by the `ai-docs-refresh` matrix.
- **`--repo <name>`** — target a single repo.

Key invariants to preserve when modifying:

- `list_repos()` branches on authenticated user vs `HARDEN_OWNER` and owner account type (`User`/`Organization`). Keep all three code paths (`/user/repos`, `/users/{owner}/repos`, `/orgs/{owner}/repos`) in sync.
- `check_vulnerability_alerts()` is tri-state (`True`/`False`/`None` for unknown). `compute_issues()` distinguishes `unknown` from `off` — don't collapse them.
- Ruleset compliance checks three things together: the ruleset name exists, the `non_fast_forward` rule is present, and `conditions.ref_name.include` targets `refs/heads/main`. A name-only match is not compliant.
- `RULESET_BODY` bypasses `actor_type=RepositoryRole, actor_id=5` (Repository Admin) so the owner can force-push. `BRANCH_PROTECTION_BODY` sets `enforce_admins=False` for the same reason. Don't tighten either without intent.
- `WIKI_ALLOWLIST` holds the small set of repos (currently only `guide`) that legitimately use the wiki feature; phase 2 won't flip `has_wiki=False` on those.
- Forks skip `secret_scanning`, `push_protection`, `dependabot_alerts`, and `dependabot_updates` because every upstream sync wipes Dependabot work and secret scanning belongs to the upstream owner. Private repos skip `allow_auto_merge` because GitHub Free silently ignores that `PATCH`.

Environment variables:

- `HARDEN_OWNER` (default: `rios0rios0`) — GitHub owner/org to audit.
- `GH_BIN` (default: `gh`) — path to the `gh` CLI binary.
- `GH_TOKEN` — consumed by `gh` for authentication; both workflows set this from `secrets.COMPLIANCE_AUDIT_TOKEN` (audit) or `secrets.CLAUDE_MD_REFRESH_TOKEN` (refresh discover job).

## Build / Test / Lint

There is no build or test suite. The only runnable code is `scripts/harden_repos.py` (pure stdlib Python 3.12 + the `gh` CLI). To validate a change to the script:

```bash
python3 -c "import ast; ast.parse(open('scripts/harden_repos.py').read())"
python3 scripts/harden_repos.py --dry-run                # exercises phases 1-4 without mutating anything
python3 scripts/harden_repos.py --phase 1 --repo <name>  # single-repo audit
```

## Conventions Specific to This Repo

- **YAML files use `.yaml`** (not `.yml`), with string values single-quoted except where interpolation requires double quotes.
- **Actions pins:** keep every workflow on the same latest major (currently `actions/checkout@v6`, `actions/upload-artifact@v7`, `actions/setup-python@v6`, `anthropics/claude-code-action@v1`). When bumping, bump across both workflows in the same commit.
- **Changelog discipline:** every change goes under `[Unreleased]` in `CHANGELOG.md` in the same commit. Keep a Changelog format, simple past tense, backticks around code identifiers.
- **Commits:** `type(SCOPE): message` in simple past tense, no trailing period. See `.claude/rules/git-flow.md` in the user's global rules.
- **Ruleset/branch-protection changes are load-bearing.** Every `rios0rios0` repo inherits the same policy; a change here propagates to all of them on the next audit run.

## Related Repositories

- [`rios0rios0/.github`](https://github.com/rios0rios0/.github) — community health fallback files, workflow templates, and reusable Claude Code workflows. Community health changes belong there, not here.
- [`rios0rios0/pipelines`](https://github.com/rios0rios0/pipelines) — reusable workflows consumed by the workflow templates in `.github`.
- [`rios0rios0/autobump`](https://github.com/rios0rios0/autobump) — releases `CHANGELOG.md` entries into versioned sections.
- [`rios0rios0/guide`](https://github.com/rios0rios0/guide/wiki) — canonical development standards.
