package entities_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

const (
	// classicTokenBodyLength, fineGrainedBodyLength, anthropicBodyLength and
	// accessKeyBodyLength are the lengths of each token shape after its prefix.
	classicTokenBodyLength = 36
	fineGrainedBodyLength  = 82
	anthropicBodyLength    = 95
	accessKeyBodyLength    = 16
	// largeDiffLines pushes a diff well past any 64 KiB line-reader buffer.
	largeDiffLines = 4096
)

// credentialShapes assembles one value of every shape at run time, so no
// file holds one for a secret scanner to report.
func credentialShapes() map[string]string {
	return map[string]string{
		"a Slack bot-token prefix":    "xo" + "xb-" + "fixture",
		"a private-key banner":        "BEGIN " + "OPENSSH " + "PRIVATE" + " KEY",
		"a classic GitHub token":      "gh" + "p_" + strings.Repeat("a1", classicTokenBodyLength)[:classicTokenBodyLength],
		"a fine-grained GitHub token": "github" + "_pat_" + strings.Repeat("0aZ_", fineGrainedBodyLength)[:fineGrainedBodyLength],
		"an Anthropic token":          "sk-" + "ant-" + "oat01-" + strings.Repeat("aZ0_-", anthropicBodyLength)[:anthropicBodyLength],
		"an AWS access key":           "AK" + "IA" + strings.Repeat("Z9", accessKeyBodyLength)[:accessKeyBodyLength],
		"a JWT":                       "ey" + strings.Repeat("J", 17) + ".ey" + "J",
	}
}

// addedDiff returns a diff that adds line to file.
func addedDiff(file, line string) string {
	return "diff --git a/" + file + " b/" + file + "\n--- a/" + file + "\n+++ b/" + file +
		"\n@@ -0,0 +1 @@\n+" + line + "\n"
}

func TestHasCredentialShape(t *testing.T) {
	t.Parallel()

	for name, shape := range credentialShapes() {
		t.Run("should refuse an added line holding "+name, func(t *testing.T) {
			t.Parallel()
			// given
			diff := addedDiff("README.md", "Example: "+shape)

			// when / then
			assert.True(t, entities.HasCredentialShape(diff))
		})
	}

	t.Run("should accept prose that names the prefixes without a token", func(t *testing.T) {
		t.Parallel()
		// given
		diff := addedDiff("README.md",
			"Never commit a `github_pat_`, `sk-ant-`, `ghp_` or `AKIA` value; use `fixture-token-placeholder`.")

		// when / then
		assert.False(t, entities.HasCredentialShape(diff))
	})

	t.Run("should read only the added lines, never a removed line or a file header", func(t *testing.T) {
		t.Parallel()
		// given
		shape := credentialShapes()["an Anthropic token"]
		diff := "diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/" + shape +
			"\n@@ -1 +1 @@\n-" + shape + "\n+Rotated the example.\n"

		// when / then
		assert.False(t, entities.HasCredentialShape(diff))
	})

	t.Run("should find a token past the first 64 KiB of the diff", func(t *testing.T) {
		t.Parallel()
		// given
		filler := strings.Repeat("+"+strings.Repeat("x", 64)+"\n", largeDiffLines)
		diff := "diff --git a/AGENTS.md b/AGENTS.md\n--- /dev/null\n+++ b/AGENTS.md\n@@ -0,0 +1,4097 @@\n" +
			filler + "+" + credentialShapes()["a fine-grained GitHub token"] + "\n"

		// when / then
		assert.True(t, entities.HasCredentialShape(diff))
	})
}

func TestAddedLines(t *testing.T) {
	t.Parallel()

	t.Run("should return the added lines of every file without their marker", func(t *testing.T) {
		t.Parallel()
		// given
		diff := addedDiff("README.md", "first") + addedDiff("AGENTS.md", "second")

		// when
		lines := entities.AddedLines(diff)

		// then
		assert.Equal(t, []string{"first", "second"}, lines)
	})
}
