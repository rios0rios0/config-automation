package commands

import (
	"context"
	"fmt"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// ApplySecuritySettingsCommand runs phase 3: enable secret scanning,
// push protection, Dependabot alerts, and automated security fixes on
// every eligible repo (public, non-fork), and disable GitHub Actions on
// every fork. Forks are exempt from the first three and private repos
// from secret scanning, per the policy carve-outs.
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
// "dependabot_updates", "actions_disabled").
type ApplySecuritySettingsChange struct {
	RepositoryName string
	Action         string
	Applied        bool
}

// ApplySecuritySettingsListeners mirrors the phase-2 listener shape.
// OnSkip fires for a fork that needs nothing: secret scanning and
// Dependabot are carved out there, so once Actions are off the fork has
// no rule left to enforce.
type ApplySecuritySettingsListeners struct {
	OnChange  func(change ApplySecuritySettingsChange)
	OnSkip    func(repoName, reason string)
	OnSuccess func(secretScanningChanges, dependabotChanges, actionsChanges int)
	OnError   func(repoName string, err error)
}

// Execute enables the missing security features per audit, honoring
// the fork and private-repo carve-outs, and disables Actions on forks.
func (c ApplySecuritySettingsCommand) Execute(
	ctx context.Context,
	input ApplySecuritySettingsInput,
	listeners ApplySecuritySettingsListeners,
) {
	secretScanningChanges := 0
	dependabotChanges := 0
	actionsChanges := 0

	for _, audit := range input.Audits {
		if audit.AuditError != "" {
			continue
		}
		if audit.Repository.Fork {
			actionsChanges += c.applyFork(ctx, input, audit, listeners)
			continue
		}
		secret, dependabot := c.applyOne(ctx, input, audit, listeners)
		secretScanningChanges += secret
		dependabotChanges += dependabot
	}

	listeners.OnSuccess(secretScanningChanges, dependabotChanges, actionsChanges)
}

// applyFork enforces the one security rule a fork is subject to: GitHub
// Actions must be off (entities.DesiredForkActionsEnabled). Secret
// scanning and Dependabot are left alone, matching the carve-out in
// AuditResult.ComputeIssues — an upstream sync wipes them, so enforcing
// them there only churns. An unknown switch is enforced, not trusted,
// the same way an unknown dependabot_alerts is. Returns the number of
// Actions changes made (0 or 1).
func (c ApplySecuritySettingsCommand) applyFork(
	ctx context.Context,
	input ApplySecuritySettingsInput,
	audit entities.AuditResult,
	listeners ApplySecuritySettingsListeners,
) int {
	if audit.Security.IsActionsDisabled() {
		if listeners.OnSkip != nil {
			listeners.OnSkip(audit.Repository.Name, "fork")
		}
		return 0
	}
	if !c.applyActionsDisabled(ctx, input, audit, listeners) {
		return 0
	}
	return 1
}

// applyOne runs the three security sub-applications for one non-fork
// audit. Matching the original `continue`-on-error semantics, any
// sub-step that errors aborts the remaining sub-steps for this repo.
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

func (c ApplySecuritySettingsCommand) applyActionsDisabled(
	ctx context.Context,
	input ApplySecuritySettingsInput,
	audit entities.AuditResult,
	listeners ApplySecuritySettingsListeners,
) bool {
	change := ApplySecuritySettingsChange{RepositoryName: audit.Repository.Name, Action: "actions_disabled"}
	if input.DryRun {
		emitSecurityChange(listeners.OnChange, change)
		return true
	}
	if err := c.securityRepo.DisableActions(ctx, input.Owner, audit.Repository.Name); err != nil {
		listeners.OnError(audit.Repository.Name, fmt.Errorf("disabling actions: %w", err))
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
