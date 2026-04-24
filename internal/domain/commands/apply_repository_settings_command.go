package commands

import (
	"context"
	"fmt"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// ApplyRepositorySettingsCommand runs phase 2: for every audit with
// non-compliant repo settings, patch the repo to match the policy. Fork
// and allowlist carve-outs mirror AuditResult.ComputeIssues().
type ApplyRepositorySettingsCommand struct {
	reposRepo repositories.Repository
}

// NewApplyRepositorySettingsCommand is the Dig-injectable constructor.
func NewApplyRepositorySettingsCommand(reposRepo repositories.Repository) *ApplyRepositorySettingsCommand {
	return &ApplyRepositorySettingsCommand{reposRepo: reposRepo}
}

// ApplyRepositorySettingsInput is passed a pre-computed audit slice so
// the command does not re-hit the API. DryRun means "print the deltas
// but don't PATCH".
type ApplyRepositorySettingsInput struct {
	Owner  string
	Audits []entities.AuditResult
	DryRun bool
}

// ApplyRepositorySettingsChange describes one intended or applied edit.
// It is emitted through the listener so the CLI can print deltas.
type ApplyRepositorySettingsChange struct {
	RepositoryName string
	OldSettings    entities.RepositorySettings
	NewSettings    entities.RepositorySettings
	Applied        bool
}

// ApplyRepositorySettingsListeners notifies the CLI of per-repo
// outcomes and final totals. OnChange fires for each repo that needed
// a patch; OnSuccess fires at the end with the totals.
type ApplyRepositorySettingsListeners struct {
	OnChange  func(change ApplyRepositorySettingsChange)
	OnSuccess func(changed, compliant int)
	OnError   func(repoName string, err error)
}

// Execute walks the audits and applies the policy to every repo whose
// settings drift from it, honoring allowlist and private-repo carve-outs.
func (c ApplyRepositorySettingsCommand) Execute(
	ctx context.Context,
	input ApplyRepositorySettingsInput,
	listeners ApplyRepositorySettingsListeners,
) {
	changed := 0
	compliant := 0

	for _, audit := range input.Audits {
		if audit.AuditError != "" {
			continue
		}

		desired := buildDesiredSettings(audit)
		if desired == audit.Repository.Settings {
			compliant++
			continue
		}

		change := ApplyRepositorySettingsChange{
			RepositoryName: audit.Repository.Name,
			OldSettings:    audit.Repository.Settings,
			NewSettings:    desired,
			Applied:        false,
		}

		if !input.DryRun {
			target := audit.Repository
			target.Settings = desired
			if err := c.reposRepo.Save(ctx, target); err != nil {
				listeners.OnError(audit.Repository.Name, fmt.Errorf("patching settings: %w", err))
				continue
			}
			change.Applied = true
		}

		if listeners.OnChange != nil {
			listeners.OnChange(change)
		}
		changed++
	}

	listeners.OnSuccess(changed, compliant)
}

func buildDesiredSettings(audit entities.AuditResult) entities.RepositorySettings {
	desired := entities.DesiredRepoSettings()

	// allow_auto_merge is a GitHub Team feature. The API silently ignores
	// PATCH attempts on GitHub Free for private repos, so leave the
	// current value in place when the target is true and the repo is
	// private — otherwise we'd PATCH and still be flagged next audit.
	if audit.Repository.Private && desired.AllowAutoMerge && !audit.Repository.Settings.AllowAutoMerge {
		desired.AllowAutoMerge = audit.Repository.Settings.AllowAutoMerge
	}

	if _, ok := entities.DesiredWikiAllowlist()[audit.Repository.Name]; ok {
		desired.HasWiki = audit.Repository.Settings.HasWiki
	}

	return desired
}
