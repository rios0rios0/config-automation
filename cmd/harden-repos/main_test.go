package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/test/domain/builders"
)

func TestParseOwners(t *testing.T) {
	t.Parallel()

	t.Run("should return the default owner when the variable is unset", func(t *testing.T) {
		t.Parallel()
		// given
		raw := ""

		// when
		owners := parseOwners(raw)

		// then
		assert.Equal(t, []string{defaultOwner}, owners)
	})

	t.Run("should return the default owner when the variable holds only separators", func(t *testing.T) {
		t.Parallel()
		// given
		raw := " , ,, "

		// when
		owners := parseOwners(raw)

		// then
		assert.Equal(t, []string{defaultOwner}, owners)
	})

	t.Run("should return a single owner when the variable names one", func(t *testing.T) {
		t.Parallel()
		// given
		raw := "rios0rios0"

		// when
		owners := parseOwners(raw)

		// then
		assert.Equal(t, []string{"rios0rios0"}, owners)
	})

	t.Run("should split every owner when the variable names several", func(t *testing.T) {
		t.Parallel()
		// given
		raw := "rios0rios0,medhub-life,prefy"

		// when
		owners := parseOwners(raw)

		// then
		assert.Equal(t, []string{"rios0rios0", "medhub-life", "prefy"}, owners)
	})

	t.Run("should trim surrounding whitespace when the list is spaced out", func(t *testing.T) {
		t.Parallel()
		// given
		raw := " rios0rios0 , medhub-life ,\tprefy "

		// when
		owners := parseOwners(raw)

		// then
		assert.Equal(t, []string{"rios0rios0", "medhub-life", "prefy"}, owners)
	})

	t.Run("should drop blank entries when the list has empty slots", func(t *testing.T) {
		t.Parallel()
		// given
		raw := "rios0rios0,,prefy,"

		// when
		owners := parseOwners(raw)

		// then
		assert.Equal(t, []string{"rios0rios0", "prefy"}, owners)
	})

	t.Run("should collapse duplicates when the same owner is listed twice", func(t *testing.T) {
		t.Parallel()
		// given
		raw := "rios0rios0,prefy,rios0rios0"

		// when
		owners := parseOwners(raw)

		// then
		assert.Equal(t, []string{"rios0rios0", "prefy"}, owners)
	})

	t.Run("should preserve the caller's ordering when the list is not alphabetical", func(t *testing.T) {
		t.Parallel()
		// given
		raw := "prefy,rios0rios0,medhub-life"

		// when
		owners := parseOwners(raw)

		// then
		assert.Equal(t, []string{"prefy", "rios0rios0", "medhub-life"}, owners)
	})
}

func TestFlattenAudits(t *testing.T) {
	t.Parallel()

	t.Run("should concatenate every group in owner order when several owners were audited", func(t *testing.T) {
		t.Parallel()
		// given
		grouped := []ownerAudits{
			{Owner: "rios0rios0", Audits: []entities.AuditResult{
				auditFor("rios0rios0", "autobump"),
				auditFor("rios0rios0", "guide"),
			}},
			{Owner: "prefy", Audits: []entities.AuditResult{
				auditFor("prefy", "guide"),
			}},
		}

		// when
		flat := flattenAudits(grouped)

		// then
		assert.Equal(t, []string{
			"rios0rios0/autobump",
			"rios0rios0/guide",
			"prefy/guide",
		}, qualifiedNames(flat))
	})

	t.Run("should return an empty slice when no owner produced audits", func(t *testing.T) {
		t.Parallel()
		// given
		grouped := []ownerAudits{
			{Owner: "rios0rios0", Audits: nil},
			{Owner: "prefy", Audits: []entities.AuditResult{}},
		}

		// when
		flat := flattenAudits(grouped)

		// then
		assert.Empty(t, flat)
	})
}

func auditFor(owner, name string) entities.AuditResult {
	return builders.NewAuditResultBuilder().
		WithRepository(builders.NewRepositoryBuilder().WithOwner(owner).WithName(name).Build()).
		Build()
}

func qualifiedNames(audits []entities.AuditResult) []string {
	names := make([]string, 0, len(audits))
	for _, audit := range audits {
		names = append(names, audit.Repository.QualifiedName())
	}
	return names
}
