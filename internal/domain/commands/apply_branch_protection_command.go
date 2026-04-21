package commands

import (
	"context"
	"fmt"

	"github.com/rios0rios0/fleet-maintenance/internal/domain/entities"
	"github.com/rios0rios0/fleet-maintenance/internal/domain/repositories"
)

// ApplyBranchProtectionCommand runs phase 4: apply the classic branch
// protection policy and the `main-protection` ruleset to every public
// repo whose endpoints are available. Private repos and those where
// protection is unavailable are skipped.
type ApplyBranchProtectionCommand struct {
	branchProtectionRepo repositories.BranchProtectionsRepository
}

// NewApplyBranchProtectionCommand is the Dig-injectable constructor.
func NewApplyBranchProtectionCommand(branchProtectionRepo repositories.BranchProtectionsRepository) *ApplyBranchProtectionCommand {
	return &ApplyBranchProtectionCommand{branchProtectionRepo: branchProtectionRepo}
}

// ApplyBranchProtectionInput is the command input.
type ApplyBranchProtectionInput struct {
	Owner  string
	Audits []entities.AuditResult
	DryRun bool
}

// ApplyBranchProtectionChange describes one branch-protection-level
// mutation (branch protection body, required signatures, or ruleset).
type ApplyBranchProtectionChange struct {
	RepositoryName string
	Action         string
	Applied        bool
}

// ApplyBranchProtectionListeners is the listener shape for phase 4.
type ApplyBranchProtectionListeners struct {
	OnChange  func(change ApplyBranchProtectionChange)
	OnSkip    func(repoName, reason string)
	OnSuccess func(changed, skipped int)
	OnError   func(repoName string, err error)
}

// Execute walks the audits and, for each eligible repo, applies
// protection + ruleset. Private and protection-unavailable repos skip.
func (c ApplyBranchProtectionCommand) Execute(
	ctx context.Context,
	input ApplyBranchProtectionInput,
	listeners ApplyBranchProtectionListeners,
) {
	changed := 0
	skipped := 0

	for _, audit := range input.Audits {
		if audit.AuditError != "" {
			continue
		}

		repo := audit.Repository
		if repo.Private {
			if listeners.OnSkip != nil {
				listeners.OnSkip(repo.Name, "private")
			}
			skipped++
			continue
		}
		if !audit.BranchProtection.Available {
			if listeners.OnSkip != nil {
				listeners.OnSkip(repo.Name, "protection_unavailable")
			}
			skipped++
			continue
		}

		mutated := false

		// Classic branch protection: always write the desired body; the
		// repository implementation PUTs the full struct idempotently.
		if !audit.BranchProtection.Enabled || !isProtectionCompliant(audit.BranchProtection) {
			change := ApplyBranchProtectionChange{RepositoryName: repo.Name, Action: "branch_protection"}
			if input.DryRun {
				if listeners.OnChange != nil {
					listeners.OnChange(change)
				}
			} else {
				if err := c.branchProtectionRepo.SaveProtection(ctx, input.Owner, repo.Name, entities.DesiredDefaultBranch, DesiredBranchProtection()); err != nil {
					listeners.OnError(repo.Name, fmt.Errorf("saving protection: %w", err))
					continue
				}
				change.Applied = true
				if listeners.OnChange != nil {
					listeners.OnChange(change)
				}
			}
			mutated = true
		}

		// Required signatures: separate endpoint.
		if audit.BranchProtection.Signatures == nil || !*audit.BranchProtection.Signatures {
			change := ApplyBranchProtectionChange{RepositoryName: repo.Name, Action: "required_signatures"}
			if input.DryRun {
				if listeners.OnChange != nil {
					listeners.OnChange(change)
				}
			} else {
				if err := c.branchProtectionRepo.EnableRequiredSignatures(ctx, input.Owner, repo.Name, entities.DesiredDefaultBranch); err != nil {
					listeners.OnError(repo.Name, fmt.Errorf("enabling required signatures: %w", err))
					continue
				}
				change.Applied = true
				if listeners.OnChange != nil {
					listeners.OnChange(change)
				}
			}
			mutated = true
		}

		// Ruleset: create if missing or non-compliant.
		if audit.Ruleset == nil || !audit.Ruleset.IsCompliant() {
			change := ApplyBranchProtectionChange{RepositoryName: repo.Name, Action: "ruleset"}
			if input.DryRun {
				if listeners.OnChange != nil {
					listeners.OnChange(change)
				}
			} else {
				if err := c.branchProtectionRepo.CreateRuleset(ctx, input.Owner, repo.Name, DesiredRuleset()); err != nil {
					listeners.OnError(repo.Name, fmt.Errorf("creating ruleset: %w", err))
					continue
				}
				change.Applied = true
				if listeners.OnChange != nil {
					listeners.OnChange(change)
				}
			}
			mutated = true
		}

		if mutated {
			changed++
		}
	}

	listeners.OnSuccess(changed, skipped)
}

// DesiredBranchProtection returns the canonical branch protection body
// that phase 4 PUTs. Kept as a function (not a var) because the struct
// contains slices/maps that should not be shared across calls.
func DesiredBranchProtection() entities.BranchProtection {
	sig := true
	return entities.BranchProtection{
		Available:              true,
		Enabled:                true,
		ReviewCount:            entities.DesiredReviewCount,
		DismissStaleReviews:    true,
		RequireCodeOwners:      false,
		EnforceAdmins:          false,
		LinearHistory:          false,
		AllowForcePushes:       false,
		AllowDeletions:         false,
		ConversationResolution: true,
		Signatures:             &sig,
	}
}

// DesiredRuleset returns the canonical ruleset body. Admin bypass is
// always included so the owner can force-push when they need to.
func DesiredRuleset() entities.Ruleset {
	return entities.Ruleset{
		Name:              entities.DesiredRulesetName,
		Enforcement:       "active",
		HasNonFastForward: true,
		TargetsMain:       true,
		AdminBypass:       true,
	}
}

func isProtectionCompliant(p entities.BranchProtection) bool {
	if !p.Enabled {
		return false
	}
	if p.ReviewCount != entities.DesiredReviewCount {
		return false
	}
	if !p.DismissStaleReviews {
		return false
	}
	if !p.ConversationResolution {
		return false
	}
	return true
}
