package repositories

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/go-github/v66/github"

	"github.com/rios0rios0/fleet-maintenance/internal/domain/entities"
	"github.com/rios0rios0/fleet-maintenance/internal/domain/repositories"
)

// GoGithubBranchProtectionsRepository implements
// repositories.BranchProtectionsRepository by wrapping go-github.
// Classic branch protection and the policy ruleset both live here
// because phase 4 always operates on them as a pair.
type GoGithubBranchProtectionsRepository struct {
	client *github.Client
}

// NewGoGithubBranchProtectionsRepository is the Dig-injectable constructor.
func NewGoGithubBranchProtectionsRepository(client *github.Client) *GoGithubBranchProtectionsRepository {
	return &GoGithubBranchProtectionsRepository{client: client}
}

var _ repositories.BranchProtectionsRepository = (*GoGithubBranchProtectionsRepository)(nil)

// FindProtectionByBranch returns the protection state. Available=false
// signals that the endpoint returned 403 (plan or permission) or 404
// (no branch, no protection) — the command layer uses this to skip the
// repo rather than error out.
func (r GoGithubBranchProtectionsRepository) FindProtectionByBranch(
	ctx context.Context,
	owner, name, branch string,
) (entities.BranchProtection, error) {
	protection, _, err := r.client.Repositories.GetBranchProtection(ctx, owner, name, branch)
	if err != nil {
		if isStatusCode(err, http.StatusForbidden) || isUpgradeRequired(err) {
			return entities.BranchProtection{Available: false}, nil
		}
		if isStatusCode(err, http.StatusNotFound) || strings.Contains(err.Error(), "Branch not protected") {
			return entities.BranchProtection{Available: true, Enabled: false}, nil
		}
		return entities.BranchProtection{}, err
	}

	state := entities.BranchProtection{
		Available: true,
		Enabled:   true,
	}
	applyProtectionFields(&state, protection)
	state.Signatures = r.findRequiredSignatures(ctx, owner, name, branch)
	return state, nil
}

func applyProtectionFields(state *entities.BranchProtection, protection *github.Protection) {
	if protection == nil {
		return
	}
	if protection.RequiredPullRequestReviews != nil {
		state.ReviewCount = protection.RequiredPullRequestReviews.RequiredApprovingReviewCount
		state.DismissStaleReviews = protection.RequiredPullRequestReviews.DismissStaleReviews
		state.RequireCodeOwners = protection.RequiredPullRequestReviews.RequireCodeOwnerReviews
	}
	if protection.EnforceAdmins != nil {
		state.EnforceAdmins = protection.EnforceAdmins.Enabled
	}
	if protection.RequireLinearHistory != nil {
		state.LinearHistory = protection.RequireLinearHistory.Enabled
	}
	if protection.AllowForcePushes != nil {
		state.AllowForcePushes = protection.AllowForcePushes.Enabled
	}
	if protection.AllowDeletions != nil {
		state.AllowDeletions = protection.AllowDeletions.Enabled
	}
	if protection.RequiredConversationResolution != nil {
		state.ConversationResolution = protection.RequiredConversationResolution.Enabled
	}
}

// SaveProtection PUTs the classic branch protection body.
func (r GoGithubBranchProtectionsRepository) SaveProtection(
	ctx context.Context,
	owner, name, branch string,
	state entities.BranchProtection,
) error {
	request := &github.ProtectionRequest{
		RequiredPullRequestReviews: &github.PullRequestReviewsEnforcementRequest{
			DismissStaleReviews:          state.DismissStaleReviews,
			RequireCodeOwnerReviews:      state.RequireCodeOwners,
			RequiredApprovingReviewCount: state.ReviewCount,
		},
		EnforceAdmins:                  state.EnforceAdmins,
		RequiredConversationResolution: &state.ConversationResolution,
		AllowForcePushes:               &state.AllowForcePushes,
		AllowDeletions:                 &state.AllowDeletions,
		RequireLinearHistory:           &state.LinearHistory,
	}
	_, _, err := r.client.Repositories.UpdateBranchProtection(ctx, owner, name, branch, request)
	return err
}

// EnableRequiredSignatures calls the dedicated endpoint.
func (r GoGithubBranchProtectionsRepository) EnableRequiredSignatures(
	ctx context.Context,
	owner, name, branch string,
) error {
	_, _, err := r.client.Repositories.RequireSignaturesOnProtectedBranch(ctx, owner, name, branch)
	return err
}

// FindRulesetByName paginates rulesets for the repo and returns the one
// matching `rulesetName`. When no match exists, or when the rulesets
// endpoint returns an upgrade-required 403 (private repos on GitHub
// Free), the function returns `repositories.ErrRulesetNotFound` so the
// audit command's private-repo carve-out applies instead of failing
// the run. Any other 403 is returned unchanged so auth/scope issues
// are surfaced rather than hidden as a missing ruleset.
func (r GoGithubBranchProtectionsRepository) FindRulesetByName(
	ctx context.Context,
	owner, name, rulesetName string,
) (*entities.Ruleset, error) {
	list, _, err := r.client.Repositories.GetAllRulesets(ctx, owner, name, false)
	if err != nil {
		if isUpgradeRequired(err) {
			return nil, repositories.ErrRulesetNotFound
		}
		return nil, err
	}

	for _, rs := range list {
		if rs == nil || rs.Name != rulesetName {
			continue
		}
		id := int64(0)
		if rs.ID != nil {
			id = *rs.ID
		}
		detail, _, detailErr := r.client.Repositories.GetRuleset(ctx, owner, name, id, false)
		if detailErr != nil {
			return nil, detailErr
		}
		entity := mapRulesetToEntity(detail)
		return &entity, nil
	}
	return nil, repositories.ErrRulesetNotFound
}

// CreateRuleset posts the canonical policy ruleset.
func (r GoGithubBranchProtectionsRepository) CreateRuleset(
	ctx context.Context,
	owner, name string,
	ruleset entities.Ruleset,
) error {
	body := buildRulesetRequest(ruleset)
	_, _, err := r.client.Repositories.CreateRuleset(ctx, owner, name, body)
	return err
}

func (r GoGithubBranchProtectionsRepository) findRequiredSignatures(
	ctx context.Context,
	owner, name, branch string,
) *bool {
	sig, _, err := r.client.Repositories.GetSignaturesProtectedBranch(ctx, owner, name, branch)
	if err != nil {
		return nil
	}
	if sig == nil || sig.Enabled == nil {
		return nil
	}
	return sig.Enabled
}

func mapRulesetToEntity(rs *github.Ruleset) entities.Ruleset {
	if rs == nil {
		return entities.Ruleset{}
	}
	entity := entities.Ruleset{
		Name:        rs.Name,
		Enforcement: rs.Enforcement,
	}
	if rs.ID != nil {
		entity.ID = *rs.ID
	}
	entity.AdminBypass = hasAdminBypass(rs.BypassActors)
	entity.HasNonFastForward = hasNonFastForwardRule(rs.Rules)
	entity.TargetsMain = targetsMain(rs.Conditions)
	return entity
}

func hasAdminBypass(actors []*github.BypassActor) bool {
	for _, actor := range actors {
		if actor == nil || actor.ActorType == nil || actor.ActorID == nil {
			continue
		}
		if *actor.ActorType == entities.RepositoryAdminActorType && *actor.ActorID == entities.RepositoryAdminActorID {
			return true
		}
	}
	return false
}

func hasNonFastForwardRule(rules []*github.RepositoryRule) bool {
	for _, rule := range rules {
		if rule != nil && rule.Type == "non_fast_forward" {
			return true
		}
	}
	return false
}

func targetsMain(conditions *github.RulesetConditions) bool {
	if conditions == nil || conditions.RefName == nil {
		return false
	}
	for _, include := range conditions.RefName.Include {
		if include == "refs/heads/main" || include == "~DEFAULT_BRANCH" {
			return true
		}
	}
	return false
}

func buildRulesetRequest(ruleset entities.Ruleset) *github.Ruleset {
	actorType := entities.RepositoryAdminActorType
	actorID := int64(entities.RepositoryAdminActorID)
	bypassMode := "always"
	target := "branch"

	return &github.Ruleset{
		Name:        ruleset.Name,
		Target:      &target,
		Enforcement: ruleset.Enforcement,
		BypassActors: []*github.BypassActor{
			{
				ActorID:    &actorID,
				ActorType:  &actorType,
				BypassMode: &bypassMode,
			},
		},
		Conditions: &github.RulesetConditions{
			RefName: &github.RulesetRefConditionParameters{
				Include: []string{"refs/heads/main"},
				Exclude: []string{},
			},
		},
		Rules: []*github.RepositoryRule{github.NewNonFastForwardRule()},
	}
}

func isStatusCode(err error, status int) bool {
	var ghErr *github.ErrorResponse
	if !errors.As(err, &ghErr) {
		return false
	}
	return ghErr.Response != nil && ghErr.Response.StatusCode == status
}

func isUpgradeRequired(err error) bool {
	return strings.Contains(err.Error(), "Upgrade to GitHub Pro")
}
