package repositories

import (
	"context"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

// SecuritySettingsRepository is the port for Dependabot and secret
// scanning operations. These endpoints return different tri-states for
// "disabled", "unavailable", and "unknown", so the interface returns
// entities.SecuritySettings rather than raw booleans.
type SecuritySettingsRepository interface {
	// FindByRepositoryName fetches the current security state for the
	// given repo: secret scanning + push protection (from the repo detail
	// payload, so callers may pass the existing detail in via
	// entities.Repository instead of re-fetching) plus the tri-state
	// Dependabot alerts and automated security fixes flags.
	FindByRepositoryName(ctx context.Context, repo entities.Repository) (entities.SecuritySettings, error)

	// EnableVulnerabilityAlerts turns on Dependabot alerts
	// (PUT /repos/{owner}/{repo}/vulnerability-alerts).
	EnableVulnerabilityAlerts(ctx context.Context, owner, name string) error

	// EnableAutomatedSecurityFixes turns on Dependabot automated
	// security fixes (PUT /repos/{owner}/{repo}/automated-security-fixes).
	EnableAutomatedSecurityFixes(ctx context.Context, owner, name string) error

	// EnableSecretScanning turns on both secret scanning and push
	// protection via PATCH /repos/{owner}/{repo}. No-op on private repos
	// (callers should already gate on visibility).
	EnableSecretScanning(ctx context.Context, owner, name string) error
}
