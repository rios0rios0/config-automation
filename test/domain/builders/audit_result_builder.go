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
	// A repo of our own runs its workflows, so the compliant default keeps
	// Actions on; the policy only bites on forks (see WithActionsEnabled).
	actions := true
	return &AuditResultBuilder{
		audit: entities.AuditResult{
			Repository: NewRepositoryBuilder().WithCompliantSettings().Build(),
			Security: entities.SecuritySettings{
				SecretScanning:    "enabled",
				PushProtection:    "enabled",
				DependabotAlerts:  &alerts,
				DependabotUpdates: true,
				ActionsEnabled:    &actions,
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
				ID:                  1,
				Name:                entities.DesiredRulesetName,
				Enforcement:         "active",
				HasNonFastForward:   true,
				AllowedMergeMethods: entities.DesiredAllowedMergeMethods(),
				TargetsMain:         true,
				AdminBypass:         true,
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

// WithActionsEnabled pins the repository-level GitHub Actions switch to a
// known state. Call it after WithSecurity, which replaces the whole
// security snapshot.
func (b *AuditResultBuilder) WithActionsEnabled(enabled bool) *AuditResultBuilder {
	b.audit.Security.ActionsEnabled = &enabled
	return b
}

// WithActionsUnknown mimics a permissions read that failed, which the
// audit must report rather than treat as disabled.
func (b *AuditResultBuilder) WithActionsUnknown() *AuditResultBuilder {
	b.audit.Security.ActionsEnabled = nil
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
