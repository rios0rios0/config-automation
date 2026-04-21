# Contributing

Contributions are welcome. By participating, you agree to maintain a respectful and constructive environment.

For coding standards, testing patterns, architecture guidelines, commit conventions, and all
development practices, refer to the **[Development Guide](https://github.com/rios0rios0/guide/wiki)**.

## Prerequisites

- Python 3.12+ (standard library only — no dependencies)
- [GitHub CLI (`gh`)](https://cli.github.com/) authenticated against an account with read access to `rios0rios0` repos
- A `GH_TOKEN` or `gh auth login` session whose token has the scopes described in `README.md`

## Development Workflow

1. Fork and clone the repository.
2. Create a branch: `git checkout -b feat/my-change`
3. Validate before every push:
   ```bash
   python3 -c "import ast; ast.parse(open('scripts/harden_repos.py').read())"
   python3 scripts/harden_repos.py --dry-run
   ```
4. If your change touches `harden_repos.py`, also run a single-repo audit against a safe target:
   ```bash
   python3 scripts/harden_repos.py --phase 1 --repo autobump
   ```
5. Update `CHANGELOG.md` under `[Unreleased]` in the same commit that introduces the change.
6. If your change alters the compliance policy (branch protection, rulesets, repo settings), update `CLAUDE.md` and `README.md` to match.
7. Commit following the [commit conventions](https://github.com/rios0rios0/guide/wiki/Git-Flow).
8. Open a pull request against `main`.

## Testing Workflow Changes

GitHub Actions workflows can only be fully exercised by running them. For local validation:

- Lint workflow YAML with `actionlint` if available.
- For the AI docs refresh, trigger a single-repo dispatch against a low-risk target after merge:
  ```bash
  gh workflow run ai-docs-refresh.yaml -R rios0rios0/fleet-maintenance -f repo=<safe-repo>
  ```

## Hardening Policy Changes

Changes to `BRANCH_PROTECTION_BODY`, `RULESET_BODY`, `REPO_SETTINGS`, or `WIKI_ALLOWLIST` in `harden_repos.py` propagate to every `rios0rios0` repo on the next audit run. Treat these changes carefully:

1. Call out the intent in the PR description.
2. Verify via `--dry-run` that the set of repos flagged as non-compliant matches expectations.
3. After merge, manually apply phases 2–4 to surface unintended non-compliance early.
