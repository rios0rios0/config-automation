package scripts_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// fineGrainedBodyLength is the length of a fine-grained GitHub token
	// after its prefix.
	fineGrainedBodyLength = 82
	// anthropicBodyLength is about the length of an Anthropic token after
	// its version segment.
	anthropicBodyLength = 95
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
// carries the name and `time` that `chlog new` writes, so the batch's
// provenance check accepts it.
func changelogEntry(mode, body string) (map[string]string, string, string) {
	if mode == "chlog" {
		now := time.Now().UTC()
		return map[string]string{".changes/unreleased/.gitkeep": ""},
			fmt.Sprintf(".changes/unreleased/%d-security.yaml", now.UnixNano()),
			"kind: Security\nbody: " + body + "\ntime: " + now.Format(time.RFC3339Nano) + "\n"
	}
	return map[string]string{"CHANGELOG.md": "# Changelog\n"}, "CHANGELOG.md", "# Changelog\n\n- " + body + "\n"
}

func TestRefreshBatchCredentials(t *testing.T) {
	t.Parallel()

	t.Run(
		"should authenticate git from the environment and start Claude without the owner's token",
		func(t *testing.T) {
			t.Parallel()
			// given
			fixture := newBatchFixture(t)
			remote, _ := fixture.repository("token", compliant(map[string]string{"README.md": original}), false)
			token := fixture.env["GH_TOKEN"]

			// when
			run := fixture.run(map[string]any{"README.md": "Updated\n"}, nil)

			// then
			require.Equal(t, 0, run.code, run.stdout+run.stderr)
			assert.Equal(t, "Updated", fixture.show(remote, "README.md"))
			assert.Equal(t, "https://github.com/fixture/token.git\n", fixture.record("clone-urls"))
			assert.Equal(t, "clone "+token+"\npush "+token+"\n", fixture.record("credentials"))
			assert.NotContains(t, fixture.record("git-arguments"), token)
			agent := fixture.record("claude-environment")
			assert.NotContains(t, agent, token)
			assert.Contains(t, agent, "CLAUDE_CODE_OAUTH_TOKEN="+fixture.env["CLAUDE_CODE_OAUTH_TOKEN"]+"\n")
			assert.Equal(t, token+"\n"+token+"\n", fixture.record("gh-tokens"), "the PR calls lost the token")
		},
	)

	for name, token := range map[string]string{
		"a fine-grained GitHub token": fineGrainedTokenShape(),
		"an Anthropic token":          anthropicTokenShape(),
	} {
		t.Run("should refuse a refresh that adds "+name, func(t *testing.T) {
			t.Parallel()
			// given
			fixture := newBatchFixture(t)
			remote, _ := fixture.repository("leak", compliant(map[string]string{"README.md": original}), false)

			// when
			run := fixture.run(map[string]any{"README.md": "Example: " + token + "\n"}, nil)

			// then
			assert.Equal(t, 1, run.code, run.stdout+run.stderr)
			assert.Contains(t, run.stdout, "secret-shape: fixture/leak")
			assert.Empty(t, fixture.git(remote, "branch", "--list", refreshBranch))
			assert.NoFileExists(t, filepath.Join(fixture.root, "prs"))
		})
	}

	t.Run("should accept a refresh that names those prefixes without a token", func(t *testing.T) {
		t.Parallel()
		// given
		fixture := newBatchFixture(t)
		remote, _ := fixture.repository("prose", compliant(map[string]string{"README.md": original}), false)
		prose := "Never commit a `github_pat_` or `sk-ant-` value; use `fixture-token-placeholder`.\n"

		// when
		run := fixture.run(map[string]any{"README.md": prose}, nil)

		// then
		require.Equal(t, 0, run.code, run.stdout+run.stderr)
		assert.Equal(t, strings.TrimSpace(prose), fixture.show(remote, "README.md"))
	})

	for _, mode := range []string{"chlog", "changelog"} {
		t.Run("should stage the "+mode+" entry with the refresh", func(t *testing.T) {
			t.Parallel()
			// given
			fixture := newBatchFixture(t)
			files, path, content := changelogEntry(mode, "recorded the refresh")
			files["README.md"] = original
			remote, _ := fixture.repository(mode, compliant(files), false)

			// when
			run := fixture.run(map[string]any{"README.md": "Updated\n", path: content}, nil)

			// then
			require.Equal(t, 0, run.code, run.stdout+run.stderr)
			assert.Contains(t, run.stdout, "--> changelog convention: "+mode+"\n")
			assert.Equal(t, strings.TrimSpace(content), fixture.show(remote, path))
		})

		t.Run("should refuse a token in the "+mode+" entry it stages", func(t *testing.T) {
			t.Parallel()
			// given
			fixture := newBatchFixture(t)
			files, path, content := changelogEntry(mode, "recorded "+anthropicTokenShape())
			files["README.md"] = original
			remote, _ := fixture.repository(mode, compliant(files), false)

			// when
			run := fixture.run(map[string]any{"README.md": "Updated\n", path: content}, nil)

			// then
			assert.Equal(t, 1, run.code, run.stdout+run.stderr)
			assert.Contains(t, run.stdout, "secret-shape: fixture/"+mode)
			assert.Empty(t, fixture.git(remote, "branch", "--list", refreshBranch))
		})
	}
}
