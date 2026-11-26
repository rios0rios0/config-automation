# Contributing

Contributions are welcome. By participating, you agree to maintain a respectful and constructive environment.

For coding standards, testing patterns, architecture guidelines, commit conventions, and all
development practices, refer to the **[Development Guide](https://github.com/rios0rios0/guide/wiki)**.

## Prerequisites

- Go 1.26+
- [Make](https://www.gnu.org/software/make/)
- [chlog](https://github.com/luizjhonata/chlog) (`go install github.com/luizjhonata/chlog@latest`)
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
   go test -count=1 ./cmd/refresh-docs/... ./test/scripts/...
   ```
5. If your change touches `cmd/harden-repos/`, also run a single-repo audit against a safe target:
   ```bash
   HARDEN_OWNER=rios0rios0,medhub-life,prefy go run ./cmd/harden-repos --phase 1 --repo autobump
   ```
6. Add a changelog fragment — never edit `CHANGELOG.md`, which is generated from them:
   ```bash
   chlog new --kind Added --body 'added the thing that was not there before'
   ```
   Write it in the same commit that introduces the change.
7. If your change alters the compliance policy (branch protection, rulesets, repo settings), update `AGENTS.md` and `README.md` to match.
8. Commit following the [commit conventions](https://github.com/rios0rios0/guide/wiki/Git-Flow).
9. Open a pull request against `main`.

## Testing

Unit tests carry no build tag and use BDD-style `// given / // when / // then` blocks and in-memory doubles (no `testify/mock`). Place new tests next to the production code in a `_test` package (external tests):

```bash
go test ./...
go test -run TestAuditRepositoriesCommand ./internal/domain/commands/
```

Test data builders live under `test/domain/builders/`; in-memory doubles live under `test/domain/doubles/repositories/`.

`test/scripts/`, which runs the release reconciliation's shell against local repositories, sits outside the directories `make test` covers; run it with `go test -count=1 ./test/scripts/...`. The refresh's end-to-end suite in `cmd/refresh-docs/` runs real git and a fake Claude CLI in child processes, which Go's test cache does not see either, so run it with `-count=1` too.

## Hardening Policy Changes

Constants in `internal/domain/entities/compliance_policy.go` and the policy carve-outs in `AuditResult.ComputeIssues()` propagate to every `rios0rios0` repo on the next audit run, and `internal/domain/entities/sonar_analysis_policy.go` does the same to every SonarQube Cloud project (a rule added there stops being reported anywhere). Treat policy edits carefully:

1. Call out the intent in the PR description.
2. Verify via `--dry-run` that the set of repos flagged as non-compliant matches expectations.
3. After merge, manually apply phases 2-4 to surface unintended non-compliance early.

## Testing Workflow Changes

The refresh scope is defined by `refresh-docs scope` (`DiscoverRefreshScopeCommand`,
with its path rules in `internal/domain/entities/refresh_scope.go`). Its per-repository
manifest drives exact edit grants, the prompt inventory, drift detection, and
staging. Extend discovery and its tests together when adding a file
category, and update the prompt template
`internal/domain/entities/prompts/refresh_config_and_docs.md` with the relevant
editing rules; a rule that depends on an instruction-file setting goes inside its
`{{if .Claude}}` or `{{if .Copilot}}` block. Preserve frontmatter, model/tool
configuration, and generated content. `refresh-docs layout`
(`NormalizeAgentsLayoutCommand`) runs before discovery and keeps `AGENTS.md` as the
instruction file with a one-line `CLAUDE.md` importing it, and removes the files of a
disabled kind; change it together with its tests in `internal/domain/commands/`.
`SyncIgnoreRulesCommand` runs the `make gitignore` generator of `rios0rios0/pipelines`
on each clone through `PipelinesIgnoreRulesRepository`. The fragments, and which of them
a repository gets, belong to that generator: change them in `pipelines`, never here, so
the refresh and a developer's `make gitignore` keep writing the same block.

Run the offline tests before a workflow change:

```bash
go test -count=1 ./cmd/refresh-docs/... ./internal/... ./test/scripts/...
```

The `cmd/refresh-docs/` suite runs `refresh-docs run` against local bare Git
repositories with a fake Claude CLI and an in-memory pull-request host, on both
providers and in each CI dialect. It covers every supported category reaching a
single PR, the `AGENTS.md` migration and its separate rename commit, the
instruction-file removals, the shared ignore rules (with a fake generator), no-op
runs, tracked deletions, the changelog and secret-shape guards, failures that must
not abort the batch, and every usage-limit wording that short-circuits a batch.

A change to `azure-devops/` can only be exercised from an Azure DevOps pipeline that
consumes it, so run one there with its `repo` parameter set before tagging a release.

GitHub Actions workflows can only be fully exercised by running them. For the config-and-docs refresh, trigger a single-repo dispatch against a low-risk target after merge:

```bash
gh workflow run config-and-docs-refresh.yaml -R rios0rios0/config-automation -f repo=<safe-repo>
```

The Sonar analysis policy has a dry run that needs no credential, so preview it before dispatching
for real — and prefer a single project on the first pass:

```bash
go run ./cmd/harden-repos --sonar-policy --dry-run
gh workflow run sonar-analysis-policy.yaml -R rios0rios0/config-automation -f project=<safe-repo> -f dry_run=true
gh workflow run sonar-analysis-policy.yaml -R rios0rios0/config-automation -f project=<safe-repo>
```
