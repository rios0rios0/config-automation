# Contributing

Contributions are welcome. By participating, you agree to maintain a respectful and constructive environment.

For coding standards, testing patterns, architecture guidelines, commit conventions, and all
development practices, refer to the **[Development Guide](https://github.com/rios0rios0/guide/wiki)**.

## Prerequisites

- Go 1.26+
- [Make](https://www.gnu.org/software/make/)
- [GitHub CLI (`gh`)](https://cli.github.com/) authenticated against an account with read access to `rios0rios0` repos
- A `GH_TOKEN` (env var) or `gh auth login` session whose token has the scopes described in `README.md`

## Development Workflow

1. Fork and clone the repository.
2. Create a branch: `git checkout -b feat/my-change`
3. Install dependencies:
   ```bash
   go mod download
   ```
4. Validate before every push:
   ```bash
   make lint
   make test
   make sast
   ```
5. If your change touches `cmd/harden-repos/`, also run a single-repo audit against a safe target:
   ```bash
   HARDEN_OWNER=rios0rios0 go run ./cmd/harden-repos --phase 1 --repo autobump
   ```
6. Update `CHANGELOG.md` under `[Unreleased]` in the same commit that introduces the change.
7. If your change alters the compliance policy (branch protection, rulesets, repo settings), update `CLAUDE.md` and `README.md` to match.
8. Commit following the [commit conventions](https://github.com/rios0rios0/guide/wiki/Git-Flow).
9. Open a pull request against `main`.

## Testing

Unit tests use `//go:build unit`, BDD-style `// given / // when / // then` blocks, and in-memory doubles (no `testify/mock`). Place new tests next to the production code in a `_test` package (external tests):

```bash
go test -tags=unit ./...
go test -tags=unit -run TestAuditRepositoriesCommand ./internal/domain/commands/
```

Test data builders live under `test/domain/builders/`; in-memory doubles live under `test/domain/doubles/repositories/`.

## Hardening Policy Changes

Constants in `internal/domain/entities/compliance_policy.go` and the policy carve-outs in `AuditResult.ComputeIssues()` propagate to every `rios0rios0` repo on the next audit run. Treat policy edits carefully:

1. Call out the intent in the PR description.
2. Verify via `--dry-run` that the set of repos flagged as non-compliant matches expectations.
3. After merge, manually apply phases 2-4 to surface unintended non-compliance early.

## Testing Workflow Changes

GitHub Actions workflows can only be fully exercised by running them. For the AI docs refresh, trigger a single-repo dispatch against a low-risk target after merge:

```bash
gh workflow run ai-docs-refresh.yaml -R rios0rios0/fleet-maintenance -f repo=<safe-repo>
```
