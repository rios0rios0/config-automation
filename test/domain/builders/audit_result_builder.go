package builders

import "github.com/rios0rios0/config-automation/internal/domain/entities"

// AuditResultBuilder constructs entities.AuditResult values for tests.
// Defaults produce a public repo with every compliance flag satisfied
// (so individual tests only toggle the one field they care about).
type AuditResultBuilder struct {
	audit entities.AuditResult
}

// NewAuditResultBuilder returns a fully compliant public-repo audit.
func NewAuditResultBuilder() *AuditResultBuilder {
	sig := true
	alerts := true
	return &AuditResultBuilder{
		audit: entities.AuditResult{
			Repository: NewRepositoryBuilder().WithCompliantSettings().Build(),
			Security: entities.SecuritySettings{
				SecretScanning:    "enabled",
				PushProtection:    "enabled",
				DependabotAlerts:  &alerts,
				DependabotUpdates: true,
			},
			BranchProtection: entities.BranchProtection{
				Available:              true,
				Enabled:                true,
				ReviewCount:            entities.DesiredReviewCount,
				DismissStaleReviews:    true,
				ConversationResolution: true,
				Signatures:             &sig,
			},
			Ruleset: &entities.Ruleset{
				ID:                1,
				Name:              entities.DesiredRulesetName,
				Enforcement:       "active",
				HasNonFastForward: true,
				TargetsMain:       true,
				AdminBypass:       true,
			},
		},
	}
}

func (b *AuditResultBuilder) WithRepository(repo entities.Repository) *AuditResultBuilder {
	b.audit.Repository = repo
	return b
}

func (b *AuditResultBuilder) WithSecurity(security entities.SecuritySettings) *AuditResultBuilder {
	b.audit.Security = security
	return b
}

func (b *AuditResultBuilder) WithBranchProtection(protection entities.BranchProtection) *AuditResultBuilder {
	b.audit.BranchProtection = protection
	return b
}

func (b *AuditResultBuilder) WithoutRuleset() *AuditResultBuilder {
	b.audit.Ruleset = nil
	return b
}

func (b *AuditResultBuilder) WithRuleset(rs *entities.Ruleset) *AuditResultBuilder {
	b.audit.Ruleset = rs
	return b
}

func (b *AuditResultBuilder) WithAuditError(msg string) *AuditResultBuilder {
	b.audit.AuditError = msg
	return b
}

func (b *AuditResultBuilder) Build() entities.AuditResult {
	return b.audit
}
