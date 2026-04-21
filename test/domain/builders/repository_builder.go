package builders

import "github.com/rios0rios0/fleet-maintenance/internal/domain/entities"

// RepositoryBuilder constructs entities.Repository values for tests with
// a fluent API. Defaults mimic a fresh public repo in the rios0rios0
// account (no settings applied yet).
type RepositoryBuilder struct {
	repo entities.Repository
}

// NewRepositoryBuilder returns a builder seeded with sensible defaults.
func NewRepositoryBuilder() *RepositoryBuilder {
	return &RepositoryBuilder{
		repo: entities.Repository{
			Name:          "example",
			Owner:         "rios0rios0",
			Visibility:    "public",
			Private:       false,
			Fork:          false,
			Archived:      false,
			DefaultBranch: "main",
			Settings: entities.RepositorySettings{
				AllowSquashMerge: true,
				AllowRebaseMerge: true,
				AllowMergeCommit: true,
			},
		},
	}
}

func (b *RepositoryBuilder) WithName(name string) *RepositoryBuilder {
	b.repo.Name = name
	return b
}

func (b *RepositoryBuilder) WithOwner(owner string) *RepositoryBuilder {
	b.repo.Owner = owner
	return b
}

func (b *RepositoryBuilder) AsPrivate() *RepositoryBuilder {
	b.repo.Private = true
	b.repo.Visibility = "private"
	return b
}

func (b *RepositoryBuilder) AsFork() *RepositoryBuilder {
	b.repo.Fork = true
	return b
}

func (b *RepositoryBuilder) AsArchived() *RepositoryBuilder {
	b.repo.Archived = true
	return b
}

func (b *RepositoryBuilder) WithSettings(settings entities.RepositorySettings) *RepositoryBuilder {
	b.repo.Settings = settings
	return b
}

func (b *RepositoryBuilder) WithCompliantSettings() *RepositoryBuilder {
	b.repo.Settings = entities.DesiredRepoSettings
	return b
}

func (b *RepositoryBuilder) Build() entities.Repository {
	return b.repo
}
