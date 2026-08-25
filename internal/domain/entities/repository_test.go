package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/config-automation/test/domain/builders"
)

func TestRepositoryQualifiedName(t *testing.T) {
	t.Parallel()

	t.Run("should join owner and name when the owner is known", func(t *testing.T) {
		t.Parallel()
		// given
		repo := builders.NewRepositoryBuilder().WithOwner("medhub-life").WithName("guide").Build()

		// when
		qualified := repo.QualifiedName()

		// then
		assert.Equal(t, "medhub-life/guide", qualified)
	})

	t.Run("should fall back to the bare name when the owner is unknown", func(t *testing.T) {
		t.Parallel()
		// given
		repo := builders.NewRepositoryBuilder().WithOwner("").WithName("guide").Build()

		// when
		qualified := repo.QualifiedName()

		// then
		assert.Equal(t, "guide", qualified)
	})

	t.Run("should differ between owners when two owners host the same name", func(t *testing.T) {
		t.Parallel()
		// given
		mine := builders.NewRepositoryBuilder().WithOwner("rios0rios0").WithName("guide").Build()
		theirs := builders.NewRepositoryBuilder().WithOwner("prefy").WithName("guide").Build()

		// when
		mineQualified := mine.QualifiedName()
		theirsQualified := theirs.QualifiedName()

		// then
		assert.NotEqual(t, mineQualified, theirsQualified)
	})
}
