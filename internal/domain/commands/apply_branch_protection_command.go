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
func NewApplyBranchProtectionCommand(
	branchProtectionRepo repositories.BranchProtectionsRepository,
) *ApplyBranchProtectionCommand {
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
		if reason := skipReason(audit); reason != "" {
			if listeners.OnSkip != nil {
				listeners.OnSkip(audit.Repository.Name, reason)
			}
			skipped++
			continue
		}
		if c.applyOne(ctx, input, audit, listeners) {
			changed++
		}
	}

	listeners.OnSuccess(changed, skipped)
}

func skipReason(audit entities.AuditResult) string {
	switch {
	case audit.Repository.Private:
		return "private"
	case !audit.BranchProtection.Available:
		return "protection_unavailable"
	default:
		return ""
	}
}

// applyOne runs the three branch-protection sub-applications for one
// audit. Any sub-application returning false (error) stops further work
// for that repo and the repo is not counted as mutated — matching the
// original `continue`-on-error semantics.
func (c ApplyBranchProtectionCommand) applyOne(
	ctx context.Context,
	input ApplyBranchProtectionInput,
	audit entities.AuditResult,
	listeners ApplyBranchProtectionListeners,
) bool {
	mutated := false
	if !audit.BranchProtection.Enabled || !isProtectionCompliant(audit.BranchProtection) {
		if !c.applyProtection(ctx, input, audit, listeners) {
			return false
		}
		mutated = true
	}
	if audit.BranchProtection.Signatures == nil || !*audit.BranchProtection.Signatures {
		if !c.applySignatures(ctx, input, audit, listeners) {
			return false
		}
		mutated = true
	}
	if audit.Ruleset == nil || !audit.Ruleset.IsCompliant() {
		if !c.applyRuleset(ctx, input, audit, listeners) {
			return false
		}
		mutated = true
	}
	return mutated
}

func (c ApplyBranchProtectionCommand) applyProtection(
	ctx context.Context,
	input ApplyBranchProtectionInput,
	audit entities.AuditResult,
	listeners ApplyBranchProtectionListeners,
) bool {
	change := ApplyBranchProtectionChange{RepositoryName: audit.Repository.Name, Action: "branch_protection"}
	if input.DryRun {
		emitChange(listeners.OnChange, change)
		return true
	}
	err := c.branchProtectionRepo.SaveProtection(
		ctx,
		input.Owner,
		audit.Repository.Name,
		entities.DesiredDefaultBranch,
		DesiredBranchProtection(),
	)
	if err != nil {
		listeners.OnError(audit.Repository.Name, fmt.Errorf("saving protection: %w", err))
		return false
	}
	change.Applied = true
	emitChange(listeners.OnChange, change)
	return true
}

func (c ApplyBranchProtectionCommand) applySignatures(
	ctx context.Context,
	input ApplyBranchProtectionInput,
	audit entities.AuditResult,
	listeners ApplyBranchProtectionListeners,
) bool {
	change := ApplyBranchProtectionChange{RepositoryName: audit.Repository.Name, Action: "required_signatures"}
	if input.DryRun {
		emitChange(listeners.OnChange, change)
		return true
	}
	err := c.branchProtectionRepo.EnableRequiredSignatures(
		ctx,
		input.Owner,
		audit.Repository.Name,
		entities.DesiredDefaultBranch,
	)
	if err != nil {
		listeners.OnError(audit.Repository.Name, fmt.Errorf("enabling required signatures: %w", err))
		return false
	}
	change.Applied = true
	emitChange(listeners.OnChange, change)
	return true
}

func (c ApplyBranchProtectionCommand) applyRuleset(
	ctx context.Context,
	input ApplyBranchProtectionInput,
	audit entities.AuditResult,
	listeners ApplyBranchProtectionListeners,
) bool {
	change := ApplyBranchProtectionChange{RepositoryName: audit.Repository.Name, Action: "ruleset"}
	if input.DryRun {
		emitChange(listeners.OnChange, change)
		return true
	}
	err := c.branchProtectionRepo.CreateRuleset(
		ctx,
		input.Owner,
		audit.Repository.Name,
		DesiredRuleset(),
	)
	if err != nil {
		listeners.OnError(audit.Repository.Name, fmt.Errorf("creating ruleset: %w", err))
		return false
	}
	change.Applied = true
	emitChange(listeners.OnChange, change)
	return true
}

func emitChange(cb func(change ApplyBranchProtectionChange), change ApplyBranchProtectionChange) {
	if cb != nil {
		cb(change)
	}
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
