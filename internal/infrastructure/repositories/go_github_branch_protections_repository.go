package repositories

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/go-github/v75/github"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
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
	list, _, err := r.client.Repositories.GetAllRulesets(ctx, owner, name, nil)
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
	_, _, err := r.client.Repositories.CreateRuleset(ctx, owner, name, buildRulesetRequest(ruleset))
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

func mapRulesetToEntity(rs *github.RepositoryRuleset) entities.Ruleset {
	if rs == nil {
		return entities.Ruleset{}
	}
	entity := entities.Ruleset{
		Name:        rs.Name,
		Enforcement: string(rs.Enforcement),
	}
	if rs.ID != nil {
		entity.ID = *rs.ID
	}
	entity.AdminBypass = hasAdminBypass(rs.BypassActors)
	entity.HasNonFastForward = hasNonFastForwardRule(rs.Rules)
	entity.AllowedMergeMethods = allowedMergeMethods(rs.Rules)
	entity.TargetsMain = targetsMain(rs.Conditions)
	return entity
}

func hasAdminBypass(actors []*github.BypassActor) bool {
	for _, actor := range actors {
		if actor == nil || actor.ActorType == nil || actor.ActorID == nil {
			continue
		}
		if string(*actor.ActorType) == entities.RepositoryAdminActorType &&
			*actor.ActorID == entities.RepositoryAdminActorID {
			return true
		}
	}
	return false
}

// hasNonFastForwardRule reports the `non_fast_forward` rule, which blocks
// FORCE PUSHES. It is unrelated to how pull requests merge — that is
// allowedMergeMethods below.
func hasNonFastForwardRule(rules *github.RepositoryRulesetRules) bool {
	return rules != nil && rules.NonFastForward != nil
}

// allowedMergeMethods reads the pull_request rule's allowed_merge_methods.
// Returns nil when the ruleset carries no pull_request rule at all, so the
// audit can tell "no rule" apart from "rule allows nothing".
func allowedMergeMethods(rules *github.RepositoryRulesetRules) []string {
	if rules == nil || rules.PullRequest == nil {
		return nil
	}
	methods := make([]string, 0, len(rules.PullRequest.AllowedMergeMethods))
	for _, method := range rules.PullRequest.AllowedMergeMethods {
		methods = append(methods, string(method))
	}
	return methods
}

func targetsMain(conditions *github.RepositoryRulesetConditions) bool {
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

func buildRulesetRequest(ruleset entities.Ruleset) github.RepositoryRuleset {
	actorType := github.BypassActorTypeRepositoryRole
	actorID := int64(entities.RepositoryAdminActorID)
	bypassMode := github.BypassModeAlways
	target := github.RulesetTargetBranch

	return github.RepositoryRuleset{
		Name:        ruleset.Name,
		Target:      &target,
		Enforcement: github.RulesetEnforcement(ruleset.Enforcement),
		BypassActors: []*github.BypassActor{
			{
				ActorID:    &actorID,
				ActorType:  &actorType,
				BypassMode: &bypassMode,
			},
		},
		Conditions: &github.RepositoryRulesetConditions{
			RefName: &github.RepositoryRulesetRefConditionParameters{
				Include: []string{"refs/heads/" + entities.DesiredDefaultBranch},
				Exclude: []string{},
			},
		},
		Rules: buildRulesetRules(ruleset),
	}
}

// buildRulesetRules assembles the two rules the policy carries:
// `non_fast_forward` (blocks force pushes) and `pull_request` (pins the
// merge methods). The pull_request rule's review settings mirror
// DesiredBranchProtection so the ruleset and classic branch protection
// state the same review policy instead of stacking two different ones —
// GitHub applies the strictest of the two, so a mismatch here would
// silently tighten the effective policy.
func buildRulesetRules(ruleset entities.Ruleset) *github.RepositoryRulesetRules {
	rules := &github.RepositoryRulesetRules{}
	if ruleset.HasNonFastForward {
		rules.NonFastForward = &github.EmptyRuleParameters{}
	}
	if ruleset.AllowedMergeMethods != nil {
		methods := make([]github.PullRequestMergeMethod, 0, len(ruleset.AllowedMergeMethods))
		for _, method := range ruleset.AllowedMergeMethods {
			methods = append(methods, github.PullRequestMergeMethod(method))
		}
		rules.PullRequest = &github.PullRequestRuleParameters{
			AllowedMergeMethods:            methods,
			DismissStaleReviewsOnPush:      true,
			RequireCodeOwnerReview:         false,
			RequireLastPushApproval:        false,
			RequiredApprovingReviewCount:   entities.DesiredReviewCount,
			RequiredReviewThreadResolution: true,
		}
	}
	return rules
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
