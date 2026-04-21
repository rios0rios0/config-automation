package commands

import (
	"context"
	"fmt"

	"github.com/rios0rios0/fleet-maintenance/internal/domain/entities"
	"github.com/rios0rios0/fleet-maintenance/internal/domain/repositories"
)

// ApplySecuritySettingsCommand runs phase 3: enable secret scanning,
// push protection, Dependabot alerts, and automated security fixes on
// every eligible repo (public, non-fork). Forks and private repos are
// skipped per the policy carve-out.
type ApplySecuritySettingsCommand struct {
	securityRepo repositories.SecuritySettingsRepository
}

// NewApplySecuritySettingsCommand is the Dig-injectable constructor.
func NewApplySecuritySettingsCommand(securityRepo repositories.SecuritySettingsRepository) *ApplySecuritySettingsCommand {
	return &ApplySecuritySettingsCommand{securityRepo: securityRepo}
}

// ApplySecuritySettingsInput is the command input. See
// ApplyRepositorySettingsInput for field semantics.
type ApplySecuritySettingsInput struct {
	Owner  string
	Audits []entities.AuditResult
	DryRun bool
}

// ApplySecuritySettingsChange describes one security-related mutation.
// Action is a short stable tag ("secret_scanning", "dependabot_alerts",
// "dependabot_updates").
type ApplySecuritySettingsChange struct {
	RepositoryName string
	Action         string
	Applied        bool
}

// ApplySecuritySettingsListeners mirrors the phase-2 listener shape.
type ApplySecuritySettingsListeners struct {
	OnChange  func(change ApplySecuritySettingsChange)
	OnSkip    func(repoName, reason string)
	OnSuccess func(secretScanningChanges, dependabotChanges int)
	OnError   func(repoName string, err error)
}

// Execute enables the missing security features per audit, honoring
// the fork and private-repo carve-outs.
func (c ApplySecuritySettingsCommand) Execute(
	ctx context.Context,
	input ApplySecuritySettingsInput,
	listeners ApplySecuritySettingsListeners,
) {
	secretScanningChanges := 0
	dependabotChanges := 0

	for _, audit := range input.Audits {
		if audit.AuditError != "" {
			continue
		}

		repo := audit.Repository

		if repo.Fork {
			if listeners.OnSkip != nil {
				listeners.OnSkip(repo.Name, "fork")
			}
			continue
		}

		// Secret scanning: public-only.
		if !repo.Private && !audit.Security.IsSecretScanningEnabled() {
			change := ApplySecuritySettingsChange{RepositoryName: repo.Name, Action: "secret_scanning"}
			if input.DryRun {
				if listeners.OnChange != nil {
					listeners.OnChange(change)
				}
			} else {
				if err := c.securityRepo.EnableSecretScanning(ctx, input.Owner, repo.Name); err != nil {
					listeners.OnError(repo.Name, fmt.Errorf("enabling secret scanning: %w", err))
					continue
				}
				change.Applied = true
				if listeners.OnChange != nil {
					listeners.OnChange(change)
				}
			}
			secretScanningChanges++
		}

		// Dependabot alerts: all non-fork repos.
		alerts := audit.Security.DependabotAlerts
		if alerts == nil || !*alerts {
			change := ApplySecuritySettingsChange{RepositoryName: repo.Name, Action: "dependabot_alerts"}
			if input.DryRun {
				if listeners.OnChange != nil {
					listeners.OnChange(change)
				}
			} else {
				if err := c.securityRepo.EnableVulnerabilityAlerts(ctx, input.Owner, repo.Name); err != nil {
					listeners.OnError(repo.Name, fmt.Errorf("enabling vulnerability alerts: %w", err))
					continue
				}
				change.Applied = true
				if listeners.OnChange != nil {
					listeners.OnChange(change)
				}
			}
			dependabotChanges++
		}

		// Dependabot automated security fixes: all non-fork repos.
		if !audit.Security.DependabotUpdates {
			change := ApplySecuritySettingsChange{RepositoryName: repo.Name, Action: "dependabot_updates"}
			if input.DryRun {
				if listeners.OnChange != nil {
					listeners.OnChange(change)
				}
			} else {
				if err := c.securityRepo.EnableAutomatedSecurityFixes(ctx, input.Owner, repo.Name); err != nil {
					listeners.OnError(repo.Name, fmt.Errorf("enabling automated security fixes: %w", err))
					continue
				}
				change.Applied = true
				if listeners.OnChange != nil {
					listeners.OnChange(change)
				}
			}
			dependabotChanges++
		}
	}

	listeners.OnSuccess(secretScanningChanges, dependabotChanges)
}
