package builders

import "github.com/rios0rios0/config-automation/internal/domain/entities"

// SonarProjectBuilder constructs entities.SonarProject values for tests
// with a fluent API. Defaults mimic a project onboarded from a repo of
// the same name in the rios0rios0 organization; WithKey covers the
// renamed case (`dev-toolkit` still analysed as `rios0rios0_versainit`).
type SonarProjectBuilder struct {
	project entities.SonarProject
}

// NewSonarProjectBuilder returns a builder seeded with sensible defaults.
func NewSonarProjectBuilder() *SonarProjectBuilder {
	return &SonarProjectBuilder{
		project: entities.SonarProject{
			Key:          "rios0rios0_example",
			Name:         "example",
			Organization: entities.DesiredSonarOrganization,
		},
	}
}

func (b *SonarProjectBuilder) WithKey(key string) *SonarProjectBuilder {
	b.project.Key = key
	return b
}

// WithName sets the display name and keeps the key in sync, which is
// what an unrenamed project looks like.
func (b *SonarProjectBuilder) WithName(name string) *SonarProjectBuilder {
	b.project.Name = name
	b.project.Key = entities.DesiredSonarOrganization + "_" + name
	return b
}

func (b *SonarProjectBuilder) WithOrganization(organization string) *SonarProjectBuilder {
	b.project.Organization = organization
	return b
}

func (b *SonarProjectBuilder) Build() entities.SonarProject {
	return b.project
}
