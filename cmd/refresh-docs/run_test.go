package main

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/test/domain/fixtures"
)

const (
	// fineGrainedBodyLength and anthropicBodyLength are the lengths of each
	// token shape after its prefix.
	fineGrainedBodyLength = 82
	anthropicBodyLength   = 95
)

// fineGrainedTokenShape and anthropicTokenShape assemble a value of each
// shape at run time, so no file holds one for a secret scanner to report.
func fineGrainedTokenShape() string {
	return "github" + "_pat_" + strings.Repeat("0aZ_", fineGrainedBodyLength)[:fineGrainedBodyLength]
}

func anthropicTokenShape() string {
	return "sk-" + "ant-" + "oat01-" + strings.Repeat("aZ0_-", anthropicBodyLength)[:anthropicBodyLength]
}

// changelogEntry returns, for a changelog convention, the files that select
// it and the path and content of an entry recording body. A chlog fragment
// carries the name and `time` that `chlog new` writes.
func changelogEntry(mode, body string) (map[string]string, string, string) {
	if mode == "chlog" {
		now := time.Now().UTC()
		return map[string]string{".changes/unreleased/.gitkeep": ""},
			fmt.Sprintf(".changes/unreleased/%d-security.yaml", now.UnixNano()),
			"kind: Security\nbody: " + body + "\ntime: " + now.Format(time.RFC3339Nano) + "\n"
	}
	return map[string]string{"CHANGELOG.md": "# Changelog\n"}, "CHANGELOG.md", "# Changelog\n\n- " + body + "\n"
}

func TestRefreshBatch(t *testing.T) {
	t.Parallel()

	t.Run("should bring every category into one PR without staging unrelated files", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		paths := slices.Concat(fixtures.SupportedRefreshPaths(), slices.DeleteFunc(
			fixtures.ExcludedRefreshPaths(), func(path string) bool { return path == "CHANGELOG.md" }))
		remote := fixture.repository("complete", withContent(paths, original))
		writes := map[string]any{"source.txt": "Unexpected out-of-scope change\n"}
		for _, path := range fixtures.SupportedRefreshPaths() {
			if !slices.Contains(fixtures.PairedImports(), path) {
				writes[path] = "Updated documentation\n"
			}
		}

		// when
		run := fixture.run(writes)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		changed := strings.Split(fixture.git(remote, "diff", "--name-only", "main", refreshBranch), "\n")
		expected := fixtures.SupportedRefreshPaths()
		for _, path := range fixtures.PairedImports() {
			if !slices.Contains(expected, path) {
				expected = append(expected, path)
			}
		}
		assert.ElementsMatch(t, expected, changed)
		for _, path := range fixtures.PairedImports() {
			assert.Equal(t, strings.TrimSpace(entities.AgentsImport), fixture.show(remote, path))
		}
		require.Len(t, fixture.pullRequests.Created, 1)
		created := fixture.pullRequests.Created[0]
		assert.Equal(t, entities.PullRequestDraft{
			SourceBranch: refreshBranch, TargetBranch: "main", Title: defaultCommitMessage,
			Description: created.Description,
		}, created)
		assert.True(t, strings.HasPrefix(created.Description, "Automated refresh by the fixture.\n\n"))
		assert.Equal(t, "fixture-bot <fixture-bot@example.invalid>",
			fixture.git(remote, "log", "-1", "--format=%an <%ae>", refreshBranch))
		assert.Contains(
			t,
			run.stdout,
			"prs_created (1): fixture/complete (https://example.invalid/fixture/complete/pull/1)",
		)
	})

	t.Run("should keep processing the batch when an audit fails", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		fixture.repository("first", map[string]string{"README.md": original})
		remote := fixture.repository("second", map[string]string{"README.md": original})
		fixture.agent["FAIL_AUDIT_REPO"] = "fixture__first"

		// when
		run := fixture.run(map[string]any{"README.md": "Updated\n"})

		// then
		assert.Equal(t, exitFailure, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "  - claude: fixture/first")
		assert.Contains(t, run.stdout, "processed (2/2)")
		assert.Equal(t, "Updated", fixture.show(remote, "README.md"))
	})

	t.Run("should keep processing the batch when a clone or a push fails", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		fixture.missing("absent")
		rejected := fixture.repository("rejected", map[string]string{"README.md": original})
		remote := fixture.repository("last", map[string]string{"README.md": original})
		fixture.clones.failPush = []string{"fixture__rejected"}

		// when
		run := fixture.run(map[string]any{"README.md": "Updated\n"})

		// then
		assert.Equal(t, exitFailure, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "  - clone: fixture/absent")
		assert.Contains(t, run.stdout, "  - push: fixture/rejected")
		assert.Contains(t, run.stdout, "processed (3/3)")
		assert.Empty(t, fixture.branches(rejected))
		assert.Equal(t, "Updated", fixture.show(remote, "README.md"))
	})

	t.Run("should not open a PR when nothing changes", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		fixture.repository("quiet", compliant(map[string]string{"README.md": original}))

		// when
		run := fixture.run(nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "no_drift (1): fixture/quiet")
		assert.Empty(t, fixture.pullRequests.Created)
	})

	t.Run("should not open a PR for a changelog-only or a whitespace-only edit", func(t *testing.T) {
		t.Parallel()
		for name, writes := range map[string]map[string]any{
			"changelog":  {"CHANGELOG.md": "Stray changelog entry\n"},
			"whitespace": {"README.md": "Original   documentation   \n"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				// given
				fixture := newRefreshFixture(t)
				fixture.repository(name, compliant(map[string]string{"README.md": original, "CHANGELOG.md": original}))

				// when
				run := fixture.run(writes)

				// then
				require.Equal(t, 0, run.code, run.stdout+run.stderr)
				assert.Contains(t, run.stdout, "no_drift (1)")
				assert.Empty(t, fixture.pullRequests.Created)
			})
		}
	})

	t.Run("should give a README-only repository initial guidance", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		remote := fixture.repository("bootstrap", map[string]string{"README.md": original})

		// when
		run := fixture.run(map[string]any{"AGENTS.md": "New useful guidance\n"})

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Equal(t, "New useful guidance", fixture.show(remote, "AGENTS.md"))
		assert.Equal(t, strings.TrimSpace(entities.AgentsImport), fixture.show(remote, "CLAUDE.md"))
	})

	t.Run("should detect and stage a tracked deletion", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		remote := fixture.repository("deletion", compliant(map[string]string{"README.md": original}))

		// when
		run := fixture.run(map[string]any{"README.md": nil})

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Equal(t, "D\tREADME.md", fixture.git(remote, "diff", "--name-status", "main", refreshBranch))
	})

	t.Run("should move a legacy CLAUDE.md to AGENTS.md when the refresh edits nothing", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		remote := fixture.repository("legacy", map[string]string{"CLAUDE.md": legacyClaude, "README.md": "Read me\n"})

		// when
		run := fixture.run(nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Len(t, fixture.pullRequests.Created, 1)
		assert.Equal(t, defaultCommitMessage+"\n"+entities.RenameMessage,
			fixture.git(remote, "log", "--format=%s", "main.."+refreshBranch))
		assert.Contains(t, fixture.git(remote, "log", "--follow", "--format=%s", refreshBranch, "--", "AGENTS.md"),
			"Initial fixture")
		assert.Equal(t, "fixture-bot", fixture.git(remote, "log", "-1", "--format=%an", refreshBranch+"~1"))
		assert.Equal(t, strings.TrimSpace(entities.AgentsImport), fixture.show(remote, "CLAUDE.md"))
		agents := fixture.show(remote, "AGENTS.md")
		assert.True(t, strings.HasPrefix(agents, "# AGENTS.md\n"))
		assert.True(t, strings.HasSuffix(agents, "## Build\n\nmake test"))
		prompt := fixture.record("refresh-prompt")
		assert.Contains(t, prompt, "## Guidance layout for this repository")
		assert.Contains(t, prompt, "Renamed `CLAUDE.md` to `AGENTS.md`")
	})

	t.Run("should update the open PR rather than open another", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		remote := fixture.repository("open", compliant(map[string]string{"README.md": original}))
		fixture.pullRequests.WithOpen(entities.RefreshTarget{Organization: fixtureOwner, Name: "open"}, refreshBranch,
			entities.PullRequest{Number: 9})

		// when
		run := fixture.run(map[string]any{"README.md": "Updated\n"})

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "PR #9 already exists; force-push updated its diff.")
		assert.Contains(t, run.stdout, "prs_updated (1): fixture/open#9")
		assert.Empty(t, fixture.pullRequests.Created)
		assert.Equal(t, "Updated", fixture.show(remote, "README.md"))
	})

	t.Run("should open no PR when the lookup of an open one fails", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		fixture.repository("lookup", compliant(map[string]string{"README.md": original}))
		fixture.pullRequests.ErrorOnFind = errors.New("503 Service Unavailable")

		// when
		run := fixture.run(map[string]any{"README.md": "Updated\n"})

		// then
		assert.Equal(t, exitFailure, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "  - pr-list: fixture/lookup")
		assert.Contains(t, run.stdout, "503 Service Unavailable")
		assert.Empty(t, fixture.pullRequests.Created)
	})

	t.Run("should record a PR the provider refuses to open", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		fixture.repository("refused", compliant(map[string]string{"README.md": original}))
		fixture.pullRequests.ErrorOnInsert = errors.New("422 Validation Failed")

		// when
		run := fixture.run(map[string]any{"README.md": "Updated\n"})

		// then
		assert.Equal(t, exitFailure, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "  - pr-create: fixture/refused")
	})

	t.Run("should record a defect in an adapter as that repository's failure", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		fixture.repository("first", compliant(map[string]string{"README.md": original}))
		fixture.repository("second", compliant(map[string]string{"README.md": original}))
		fixture.pullRequests.PanicOnFind = true

		// when
		run := fixture.run(map[string]any{"README.md": "Updated\n"})

		// then
		assert.Equal(t, exitFailure, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "  - internal: fixture/first")
		assert.Contains(t, run.stdout, "  - internal: fixture/second")
		assert.Contains(t, run.stdout, "processed (2/2)")
	})

	t.Run("should stop workflow commands while Claude's output is printed", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		fixture.repository("echo", compliant(map[string]string{"README.md": original}))
		echoed := "::warning::injected ##[add-mask]value"
		fixture.agent["CLAUDE_ECHO"] = echoed

		// when
		run := fixture.run(nil, "--ci", dialectGitHubActions)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		stop := regexp.MustCompile(`(?m)^::stop-commands::([0-9a-f]{32})$`).FindStringSubmatch(run.stdout)
		require.Len(t, stop, 2, run.stdout)
		resume := "\n::" + stop[1] + "::\n"
		require.Contains(t, run.stdout, resume)
		assert.Less(t, strings.Index(run.stdout, stop[0]), strings.Index(run.stdout, echoed))
		assert.Less(t, strings.Index(run.stdout, echoed), strings.Index(run.stdout, resume))
	})

	t.Run("should defuse logging commands in Claude's output on Azure Pipelines", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		fixture.repository("echo", compliant(map[string]string{"README.md": original}))
		fixture.agent["CLAUDE_ECHO"] = "##vso[task.setvariable variable=x]y ##VSO[task.complete result=Failed;]"

		// when
		run := fixture.run(nil, "--ci", dialectAzurePipelines)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "##_vso[task.setvariable variable=x]y ##_VSO[task.complete result=Failed;]")
		assert.NotRegexp(t, `(?i)##vso\[task\.(setvariable variable=x|complete result=Failed)`, run.stdout)
	})
}

func TestRefreshBatchArguments(t *testing.T) {
	t.Parallel()

	t.Run("should refuse to start without the organization's token", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		fixture.repository("untouched", map[string]string{"README.md": original})
		delete(fixture.variables, "GH_TOKEN")

		// when
		run := fixture.run(nil)

		// then
		assert.Equal(t, exitFailure, run.code)
		assert.Contains(t, run.stderr, "GH_TOKEN is empty or not set")
		assert.Empty(t, fixture.clones.urls)
	})

	t.Run("should read the token from the variable --token-env names", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		fixture.repository("named", compliant(map[string]string{"README.md": original}))
		delete(fixture.variables, "GH_TOKEN")
		fixture.variables["FIXTURE_OWNER_TOKEN"] = fixtureToken

		// when
		run := fixture.run(map[string]any{"README.md": "Updated\n"}, "--token-env", "FIXTURE_OWNER_TOKEN")

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Equal(t, []string{fixtureToken}, fixture.tokens)
	})

	t.Run("should reject a usage error", func(t *testing.T) {
		t.Parallel()
		for _, arguments := range [][]string{{"--max-turns", "0"}, {"--copilot", "maybe"}, {"--ci", "jenkins"}} {
			// given
			fixture := newRefreshFixture(t)
			fixture.repository("usage", map[string]string{"README.md": original})

			// when
			run := fixture.run(nil, arguments...)

			// then
			assert.Equal(t, exitUsageError, run.code, arguments)
		}
	})
}

func TestRefreshBatchUsageLimit(t *testing.T) {
	t.Parallel()

	for _, message := range []string{
		"You have hit your weekly limit",
		"Claude AI monthly usage limit reached",
		"You've hit your org's monthly spend limit · ask your admin to raise it",
		"You've hit your 5-hour limit · resets 9am (UTC)",
		`{"type":"result","is_error":true,"api_error":"usage_limit_reached","result":"reworded"}`,
	} {
		t.Run("should skip the rest of the batch when Claude reports: "+message, func(t *testing.T) {
			t.Parallel()
			// given
			fixture := newRefreshFixture(t)
			fixture.repository("first", map[string]string{"README.md": original})
			fixture.repository("second", map[string]string{"README.md": original})
			fixture.agent["FAIL_AUDIT_REPO"], fixture.agent["FAIL_AUDIT_MESSAGE"] = "fixture__first", message

			// when
			run := fixture.run(nil)

			// then
			assert.Equal(t, exitFailure, run.code, run.stdout+run.stderr)
			assert.Contains(t, run.stdout, "  - claude-quota: fixture/first")
			assert.Contains(t, run.stdout, "quota_skipped (1): fixture/second")
			assert.Contains(t, run.stdout, "fixture/second (SKIPPED: Claude usage limit hit earlier in batch)")
			assert.Equal(t, "fixture__first\n", fixture.record("claude-calls"))
		})
	}
}

func TestRefreshBatchSafeguard(t *testing.T) {
	t.Parallel()

	for _, message := range []string{
		"API Error: Claude Code's safeguards flagged this request",
		"API Error: Opus has safety measures that flagged this message for a cybersecurity topic.",
	} {
		t.Run("should skip a refused repository without failing the batch: "+message, func(t *testing.T) {
			t.Parallel()
			// given
			fixture := newRefreshFixture(t)
			fixture.repository("refused", map[string]string{"README.md": original})
			remote := fixture.repository("next", map[string]string{"README.md": original})
			fixture.agent["FAIL_AUDIT_REPO"] = "fixture__refused"
			fixture.agent["FAIL_AUDIT_MESSAGE"] = message

			// when
			run := fixture.run(map[string]any{"README.md": "Updated\n"}, "--ci", dialectGitHubActions)

			// then
			require.Equal(t, 0, run.code, run.stdout+run.stderr)
			assert.Contains(t, run.stdout, "safeguard_skipped (1): fixture/refused")
			assert.Contains(t, run.stdout,
				"::warning::Claude's safeguards refused fixture/refused; it was skipped, not refreshed.")
			assert.Contains(t, run.stdout, "failed (0):")
			assert.Equal(t, "Updated", fixture.show(remote, "README.md"))
		})
	}

	t.Run("should end a safeguard-only batch with issues on Azure Pipelines", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t).onAzureDevOps()
		fixture.repository("refused", map[string]string{"README.md": original})
		fixture.agent["FAIL_AUDIT_REPO"] = "fixture__Fixture Project__refused"
		fixture.agent["FAIL_AUDIT_MESSAGE"] = "cyber-use-case"

		// when
		run := fixture.run(nil, "--ci", dialectAzurePipelines)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "##vso[task.logissue type=warning]Claude's safeguards refused "+
			"Fixture Project/refused; it was skipped, not refreshed.")
		assert.True(t, strings.HasSuffix(run.stdout, "##vso[task.complete result=SucceededWithIssues;]\n"))
	})
}

func TestRefreshBatchCredentials(t *testing.T) {
	t.Parallel()

	t.Run("should authenticate git with the provider's credential and start Claude without it", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		remote := fixture.repository("token", compliant(map[string]string{"README.md": original}))

		// when
		run := fixture.run(map[string]any{"README.md": "Updated\n"})

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Equal(t, "Updated", fixture.show(remote, "README.md"))
		assert.Equal(t, []string{"https://github.com/fixture/token.git"}, fixture.clones.urls)
		expected := entities.ProviderGitHub.GitCredential(fixtureToken)
		assert.Equal(t, []entities.GitCredential{expected, expected}, fixture.clones.credentials)
		agent := fixture.record("claude-environment")
		assert.NotContains(t, agent, fixtureToken)
		assert.Contains(t, agent, "CLAUDE_CODE_OAUTH_TOKEN="+fixtureOAuthToken+"\n")
		assert.Equal(t, []string{fixtureToken}, fixture.tokens, "the pull-request adapter lost the token")
	})

	t.Run("should clone an Azure DevOps repository by its percent-encoded URL", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t).onAzureDevOps()
		remote := fixture.repository("fixture repo", compliant(map[string]string{"README.md": original}))

		// when
		run := fixture.run(map[string]any{"README.md": "Updated\n"})

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Equal(t, []string{"https://dev.azure.com/fixture/Fixture%20Project/_git/fixture%20repo"},
			fixture.clones.urls)
		assert.Equal(t, entities.ProviderAzureDevOps.GitCredential(fixtureToken), fixture.clones.credentials[0])
		assert.Equal(t, "Updated", fixture.show(remote, "README.md"))
		assert.Contains(t, run.stdout, "prs_created (1): Fixture Project/fixture repo")
	})

	for name, token := range map[string]string{
		"a fine-grained GitHub token": fineGrainedTokenShape(),
		"an Anthropic token":          anthropicTokenShape(),
	} {
		t.Run("should refuse a refresh that adds "+name, func(t *testing.T) {
			t.Parallel()
			// given
			fixture := newRefreshFixture(t)
			remote := fixture.repository("leak", compliant(map[string]string{"README.md": original}))

			// when
			run := fixture.run(map[string]any{"README.md": "Example: " + token + "\n"})

			// then
			assert.Equal(t, exitFailure, run.code, run.stdout+run.stderr)
			assert.Contains(t, run.stdout, "  - secret-shape: fixture/leak")
			assert.Empty(t, fixture.branches(remote))
			assert.Empty(t, fixture.pullRequests.Created)
		})
	}

	t.Run("should accept a refresh that names those prefixes without a token", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		remote := fixture.repository("prose", compliant(map[string]string{"README.md": original}))
		prose := "Never commit a `github_pat_` or `sk-ant-` value; use `fixture-token-placeholder`.\n"

		// when
		run := fixture.run(map[string]any{"README.md": prose})

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Equal(t, strings.TrimSpace(prose), fixture.show(remote, "README.md"))
	})

	for _, mode := range []string{"chlog", "changelog"} {
		t.Run("should stage the "+mode+" entry with the refresh", func(t *testing.T) {
			t.Parallel()
			// given
			fixture := newRefreshFixture(t)
			files, path, content := changelogEntry(mode, "recorded the refresh")
			files["README.md"] = original
			remote := fixture.repository(mode, compliant(files))

			// when
			run := fixture.run(map[string]any{"README.md": "Updated\n", path: content})

			// then
			require.Equal(t, 0, run.code, run.stdout+run.stderr)
			assert.Contains(t, run.stdout, "--> changelog convention: "+mode+"\n")
			assert.Equal(t, strings.TrimSpace(content), fixture.show(remote, path))
		})

		t.Run("should refuse a token in the "+mode+" entry it stages", func(t *testing.T) {
			t.Parallel()
			// given
			fixture := newRefreshFixture(t)
			files, path, content := changelogEntry(mode, "recorded "+anthropicTokenShape())
			files["README.md"] = original
			remote := fixture.repository(mode, compliant(files))

			// when
			run := fixture.run(map[string]any{"README.md": "Updated\n", path: content})

			// then
			assert.Equal(t, exitFailure, run.code, run.stdout+run.stderr)
			assert.Contains(t, run.stdout, "  - secret-shape: fixture/"+mode)
			assert.Empty(t, fixture.branches(remote))
		})
	}
}

func TestRefreshBatchChlog(t *testing.T) {
	t.Parallel()

	t.Run("should discard a hand-edit of the generated CHANGELOG.md and keep the refresh", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		files, path, content := changelogEntry("chlog", "refreshed the guidance")
		files["README.md"], files["CHANGELOG.md"] = original, "# Changelog\n"
		remote := fixture.repository("generated", compliant(files))

		// when
		run := fixture.run(map[string]any{
			"README.md": "Updated\n", path: content, "CHANGELOG.md": "# Changelog\n\n- hand-written\n",
		})

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "--> discarded a hand-edit of CHANGELOG.md")
		assert.Equal(t, "# Changelog", fixture.show(remote, "CHANGELOG.md"))
		assert.Equal(t, strings.TrimSpace(content), fixture.show(remote, path))
	})

	t.Run("should discard an edit to a fragment an earlier PR committed", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		_, path, content := changelogEntry("chlog", "an earlier change")
		remote := fixture.repository("earlier", compliant(map[string]string{"README.md": original, path: content}))

		// when
		run := fixture.run(map[string]any{"README.md": "Updated\n", path: "kind: Removed\nbody: rewritten\n"})

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "--> discarded edits to existing chlog fragments")
		assert.Equal(t, strings.TrimSpace(content), fixture.show(remote, path))
	})

	t.Run("should not open a PR whose only change is a fragment", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		files, path, content := changelogEntry("chlog", "recorded nothing")
		fixture.repository("fragment", compliant(files))

		// when
		run := fixture.run(map[string]any{path: content})

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "no_drift (1): fixture/fragment")
		assert.Empty(t, fixture.pullRequests.Created)
	})

	t.Run("should reject a fragment chlog new did not write", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		files, _, _ := changelogEntry("chlog", "")
		files["README.md"] = original
		remote := fixture.repository("forged", compliant(files))
		forged := "kind: Changed\nbody: refreshed\ntime: 2020-01-01T00:00:00Z\n"

		// when
		run := fixture.run(map[string]any{"README.md": "Updated\n", ".changes/unreleased/refresh.yaml": forged})

		// then
		assert.Equal(t, exitFailure, run.code, run.stdout+run.stderr)
		assert.Contains(t, run.stdout, "--> rejected chlog fragment '.changes/unreleased/refresh.yaml'")
		assert.Contains(t, run.stdout, "  - fragment: fixture/forged")
		assert.Empty(t, fixture.branches(remote))
	})

	t.Run("should grant chlog new alone in a chlog repository", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		files, _, _ := changelogEntry("chlog", "")
		fixture.repository("grants", compliant(files))

		// when
		run := fixture.run(nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		prompt := fixture.record("refresh-prompt")
		assert.Contains(t, prompt, "## Changelog convention for this repository\n\nThis repository uses chlog.")
	})
}

func TestRefreshBatchInstructionFiles(t *testing.T) {
	t.Parallel()

	copilot := map[string]string{
		".github/copilot-instructions.md": "Copilot guidance\n", ".github/skills/code-review/SKILL.md": "Review\n",
		".github/skills/review/SKILL.md": "Another skill\n", "README.md": original,
	}

	t.Run("should remove the Copilot files on Azure DevOps even when Claude edits nothing", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t).onAzureDevOps()
		remote := fixture.repository("copilot", compliant(copilot))

		// when
		run := fixture.run(nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Equal(t, "D\t.github/copilot-instructions.md\nD\t.github/skills/code-review/SKILL.md",
			fixture.git(remote, "diff", "--name-status", "main", refreshBranch))
		prompt := fixture.record("refresh-prompt")
		assert.Contains(t, prompt, "Removed `.github/copilot-instructions.md`")
		assert.NotContains(t, prompt, "## Rules for `.github/copilot-instructions.md`")
		require.Len(t, fixture.pullRequests.Created, 1)
		assert.Contains(t, fixture.pullRequests.Created[0].Description, "removes GitHub Copilot's instructions")
	})

	t.Run("should keep and refresh the Copilot files when the flag turns them on", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t).onAzureDevOps()
		remote := fixture.repository("copilot", compliant(copilot))

		// when
		run := fixture.run(map[string]any{".github/copilot-instructions.md": "Updated Copilot guidance\n"},
			"--copilot=true")

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Equal(t, "M\t.github/copilot-instructions.md",
			fixture.git(remote, "diff", "--name-status", "main", refreshBranch))
	})

	t.Run("should remove the Copilot files on GitHub when the configuration turns them off", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		fixture.extraConfig = "  instruction_files:\n    copilot: false\n"
		remote := fixture.repository("copilot", compliant(copilot))

		// when
		run := fixture.run(nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Contains(t, fixture.git(remote, "diff", "--name-status", "main", refreshBranch),
			"D\t.github/copilot-instructions.md")
	})

	t.Run("should move a legacy CLAUDE.md into AGENTS.md and remove it when Claude is off", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newRefreshFixture(t)
		remote := fixture.repository("claude", map[string]string{"CLAUDE.md": legacyClaude, "README.md": original})

		// when
		run := fixture.run(nil, "--claude=false")

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.True(t, strings.HasSuffix(fixture.show(remote, "AGENTS.md"), "## Build\n\nmake test"))
		assert.Empty(t, fixture.git(remote, "ls-tree", "--name-only", refreshBranch, "CLAUDE.md"))
		assert.NotContains(t, fixture.record("refresh-prompt"), "Claude Code reads it through the `CLAUDE.md` import")
	})
}
