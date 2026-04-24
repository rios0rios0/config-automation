package commands

import (
	"context"
	"fmt"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// ApplySecuritySettingsCommand runs phase 3: enable secret scanning,
// push protection, Dependabot alerts, and automated security fixes on
// every eligible repo (public, non-fork). Forks and private repos are
// skipped per the policy carve-out.
type ApplySecuritySettingsCommand struct {
	securityRepo repositories.SecuritySettingsRepository
}

// NewApplySecuritySettingsCommand is the Dig-injectable constructor.
func NewApplySecuritySettingsCommand(
	securityRepo repositories.SecuritySettingsRepository,
) *ApplySecuritySettingsCommand {
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
		if audit.Repository.Fork {
			if listeners.OnSkip != nil {
				listeners.OnSkip(audit.Repository.Name, "fork")
			}
			continue
		}
		secret, dependabot := c.applyOne(ctx, input, audit, listeners)
		secretScanningChanges += secret
		dependabotChanges += dependabot
	}

	listeners.OnSuccess(secretScanningChanges, dependabotChanges)
}

// applyOne runs the three security sub-applications for one audit.
// Matching the original `continue`-on-error semantics, any sub-step
// that errors aborts the remaining sub-steps for this repo.
func (c ApplySecuritySettingsCommand) applyOne(
	ctx context.Context,
	input ApplySecuritySettingsInput,
	audit entities.AuditResult,
	listeners ApplySecuritySettingsListeners,
) (int, int) {
	secret := 0
	dependabot := 0
	if !audit.Repository.Private && !audit.Security.IsSecretScanningEnabled() {
		if !c.applySecretScanning(ctx, input, audit, listeners) {
			return secret, dependabot
		}
		secret++
	}
	alerts := audit.Security.DependabotAlerts
	if alerts == nil || !*alerts {
		if !c.applyDependabotAlerts(ctx, input, audit, listeners) {
			return secret, dependabot
		}
		dependabot++
	}
	if !audit.Security.DependabotUpdates {
		if !c.applyDependabotUpdates(ctx, input, audit, listeners) {
			return secret, dependabot
		}
		dependabot++
	}
	return secret, dependabot
}

func (c ApplySecuritySettingsCommand) applySecretScanning(
	ctx context.Context,
	input ApplySecuritySettingsInput,
	audit entities.AuditResult,
	listeners ApplySecuritySettingsListeners,
) bool {
	change := ApplySecuritySettingsChange{RepositoryName: audit.Repository.Name, Action: "secret_scanning"}
	if input.DryRun {
		emitSecurityChange(listeners.OnChange, change)
		return true
	}
	if err := c.securityRepo.EnableSecretScanning(ctx, input.Owner, audit.Repository.Name); err != nil {
		listeners.OnError(audit.Repository.Name, fmt.Errorf("enabling secret scanning: %w", err))
		return false
	}
	change.Applied = true
	emitSecurityChange(listeners.OnChange, change)
	return true
}

func (c ApplySecuritySettingsCommand) applyDependabotAlerts(
	ctx context.Context,
	input ApplySecuritySettingsInput,
	audit entities.AuditResult,
	listeners ApplySecuritySettingsListeners,
) bool {
	change := ApplySecuritySettingsChange{RepositoryName: audit.Repository.Name, Action: "dependabot_alerts"}
	if input.DryRun {
		emitSecurityChange(listeners.OnChange, change)
		return true
	}
	if err := c.securityRepo.EnableVulnerabilityAlerts(ctx, input.Owner, audit.Repository.Name); err != nil {
		listeners.OnError(audit.Repository.Name, fmt.Errorf("enabling vulnerability alerts: %w", err))
		return false
	}
	change.Applied = true
	emitSecurityChange(listeners.OnChange, change)
	return true
}

func (c ApplySecuritySettingsCommand) applyDependabotUpdates(
	ctx context.Context,
	input ApplySecuritySettingsInput,
	audit entities.AuditResult,
	listeners ApplySecuritySettingsListeners,
) bool {
	change := ApplySecuritySettingsChange{RepositoryName: audit.Repository.Name, Action: "dependabot_updates"}
	if input.DryRun {
		emitSecurityChange(listeners.OnChange, change)
		return true
	}
	if err := c.securityRepo.EnableAutomatedSecurityFixes(ctx, input.Owner, audit.Repository.Name); err != nil {
		listeners.OnError(audit.Repository.Name, fmt.Errorf("enabling automated security fixes: %w", err))
		return false
	}
	change.Applied = true
	emitSecurityChange(listeners.OnChange, change)
	return true
}

func emitSecurityChange(cb func(change ApplySecuritySettingsChange), change ApplySecuritySettingsChange) {
	if cb != nil {
		cb(change)
	}
}
