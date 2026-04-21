package repositories

import (
	"context"
	"errors"

	"github.com/rios0rios0/fleet-maintenance/internal/domain/entities"
)

// ErrRulesetNotFound is returned by FindRulesetByName when no ruleset
// with the requested name exists on the repository. Callers treat it as
// "no ruleset configured" rather than as a propagated failure.
var ErrRulesetNotFound = errors.New("ruleset not found")

// BranchProtectionsRepository is the port for classic branch protection
// and the `main-protection` ruleset. They live together because they
// cover the same "protect the default branch" concern and are always
// enforced as a pair by phase 4.
type BranchProtectionsRepository interface {
	// FindProtectionByBranch returns the protection state for
	// owner/name@branch. BranchProtection.Available=false signals that
	// the endpoint returned 403/404 for plan or permission reasons and
	// phase 4 should skip the repo.
	FindProtectionByBranch(ctx context.Context, owner, name, branch string) (entities.BranchProtection, error)

	// SaveProtection applies the policy-aligned branch protection body
	// (PUT /repos/{owner}/{name}/branches/{branch}/protection).
	SaveProtection(ctx context.Context, owner, name, branch string, protection entities.BranchProtection) error

	// EnableRequiredSignatures turns on the required-signatures switch
	// for the branch
	// (POST /repos/{owner}/{name}/branches/{branch}/protection/required_signatures).
	EnableRequiredSignatures(ctx context.Context, owner, name, branch string) error

	// FindRulesetByName returns the ruleset with the given name, or nil
	// when it does not exist. Detail (bypass actors, rules, conditions)
	// is populated so callers can judge compliance without a second call.
	FindRulesetByName(ctx context.Context, owner, name, rulesetName string) (*entities.Ruleset, error)

	// CreateRuleset creates the `main-protection` ruleset with the
	// policy-aligned body: enforcement=active, non_fast_forward rule,
	// targets refs/heads/main, RepositoryAdmin bypass.
	CreateRuleset(ctx context.Context, owner, name string, ruleset entities.Ruleset) error
}
