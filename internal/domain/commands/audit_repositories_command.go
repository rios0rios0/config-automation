package commands

import (
	"context"
	"fmt"

	"github.com/rios0rios0/fleet-maintenance/internal/domain/entities"
	"github.com/rios0rios0/fleet-maintenance/internal/domain/repositories"
)

// AuditRepositoriesCommand runs phase 1: walks every repo for a given
// owner and produces an AuditResult per repo. Read-only.
type AuditRepositoriesCommand struct {
	reposRepo            repositories.RepositoriesRepository
	securityRepo         repositories.SecuritySettingsRepository
	branchProtectionRepo repositories.BranchProtectionsRepository
}

// NewAuditRepositoriesCommand is the Dig-injectable constructor.
func NewAuditRepositoriesCommand(
	reposRepo repositories.RepositoriesRepository,
	securityRepo repositories.SecuritySettingsRepository,
	branchProtectionRepo repositories.BranchProtectionsRepository,
) *AuditRepositoriesCommand {
	return &AuditRepositoriesCommand{
		reposRepo:            reposRepo,
		securityRepo:         securityRepo,
		branchProtectionRepo: branchProtectionRepo,
	}
}

// AuditRepositoriesInput narrows the audit to one repo when RepoFilter
// is set. Owner is required.
type AuditRepositoriesInput struct {
	Owner      string
	RepoFilter string
}

// AuditRepositoriesListeners covers the three outcomes the CLI cares
// about. OnProgress is optional; it fires after every repo so the
// terminal can show "auditing N/M" style progress.
type AuditRepositoriesListeners struct {
	OnProgress func(index, total int, name string)
	OnSuccess  func(audits []entities.AuditResult)
	OnError    func(err error)
}

// Execute performs the audit and calls OnSuccess with every audit
// result (compliant or not). It does not exit on non-compliance; the
// CLI layer decides based on --fail-on-noncompliant.
func (c AuditRepositoriesCommand) Execute(
	ctx context.Context,
	input AuditRepositoriesInput,
	listeners AuditRepositoriesListeners,
) {
	authenticated, err := c.reposRepo.FindAuthenticatedLogin(ctx)
	if err != nil {
		listeners.OnError(fmt.Errorf("finding authenticated login: %w", err))
		return
	}

	kind, err := c.reposRepo.FindOwnerKind(ctx, input.Owner)
	if err != nil {
		listeners.OnError(fmt.Errorf("finding owner kind for %s: %w", input.Owner, err))
		return
	}

	repos, err := c.reposRepo.FindAllByOwner(ctx, input.Owner, authenticated == input.Owner, kind)
	if err != nil {
		listeners.OnError(fmt.Errorf("listing repos for %s: %w", input.Owner, err))
		return
	}

	if input.RepoFilter != "" {
		filtered := make([]entities.Repository, 0, 1)
		for _, r := range repos {
			if r.Name == input.RepoFilter {
				filtered = append(filtered, r)
				break
			}
		}
		repos = filtered
	}

	audits := make([]entities.AuditResult, 0, len(repos))
	for index, repo := range repos {
		if listeners.OnProgress != nil {
			listeners.OnProgress(index, len(repos), repo.Name)
		}
		audits = append(audits, c.auditOne(ctx, input.Owner, repo))
	}

	listeners.OnSuccess(audits)
}

func (c AuditRepositoriesCommand) auditOne(ctx context.Context, owner string, repo entities.Repository) entities.AuditResult {
	// Always refetch individual details since the list endpoint omits
	// security_and_analysis and secret-scanning fields.
	detailed, err := c.reposRepo.FindByName(ctx, owner, repo.Name)
	if err != nil {
		return entities.AuditResult{Repository: repo, AuditError: err.Error()}
	}

	security, err := c.securityRepo.FindByRepositoryName(ctx, detailed)
	if err != nil {
		return entities.AuditResult{Repository: detailed, AuditError: fmt.Sprintf("security: %s", err.Error())}
	}

	protection, err := c.branchProtectionRepo.FindProtectionByBranch(ctx, owner, detailed.Name, entities.DesiredDefaultBranch)
	if err != nil {
		return entities.AuditResult{Repository: detailed, Security: security, AuditError: fmt.Sprintf("protection: %s", err.Error())}
	}

	ruleset, err := c.branchProtectionRepo.FindRulesetByName(ctx, owner, detailed.Name, entities.DesiredRulesetName)
	if err != nil {
		return entities.AuditResult{Repository: detailed, Security: security, BranchProtection: protection, AuditError: fmt.Sprintf("ruleset: %s", err.Error())}
	}

	return entities.AuditResult{
		Repository:       detailed,
		Security:         security,
		BranchProtection: protection,
		Ruleset:          ruleset,
	}
}
