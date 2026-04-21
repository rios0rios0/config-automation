package repositories

import (
	"context"

	"github.com/rios0rios0/fleet-maintenance/internal/domain/entities"
	"github.com/rios0rios0/fleet-maintenance/internal/domain/repositories"
)

// InMemoryBranchProtectionsRepository records protection and ruleset
// mutations so tests can assert the command invoked the right
// endpoints. FindProtectionByBranch and FindRulesetByName return
// seeded snapshots keyed by repo name.
type InMemoryBranchProtectionsRepository struct {
	ProtectionByName map[string]entities.BranchProtection
	RulesetsByName   map[string]*entities.Ruleset

	ProtectionSaves        []entities.Repository
	SignaturesEnabled      []string
	RulesetsCreated        []string

	ErrorOnFindProtection  error
	ErrorOnSaveProtection  error
	ErrorOnSignatures      error
	ErrorOnFindRuleset     error
	ErrorOnCreateRuleset   error
}

// NewInMemoryBranchProtectionsRepository builds the double.
func NewInMemoryBranchProtectionsRepository() *InMemoryBranchProtectionsRepository {
	return &InMemoryBranchProtectionsRepository{
		ProtectionByName: map[string]entities.BranchProtection{},
		RulesetsByName:   map[string]*entities.Ruleset{},
	}
}

// WithProtection seeds the classic branch-protection state for one repo.
func (r *InMemoryBranchProtectionsRepository) WithProtection(name string, protection entities.BranchProtection) *InMemoryBranchProtectionsRepository {
	r.ProtectionByName[name] = protection
	return r
}

// WithRuleset seeds the ruleset snapshot for one repo.
func (r *InMemoryBranchProtectionsRepository) WithRuleset(name string, ruleset *entities.Ruleset) *InMemoryBranchProtectionsRepository {
	r.RulesetsByName[name] = ruleset
	return r
}

func (r *InMemoryBranchProtectionsRepository) FindProtectionByBranch(_ context.Context, _, name, _ string) (entities.BranchProtection, error) {
	if r.ErrorOnFindProtection != nil {
		return entities.BranchProtection{}, r.ErrorOnFindProtection
	}
	return r.ProtectionByName[name], nil
}

func (r *InMemoryBranchProtectionsRepository) SaveProtection(_ context.Context, _, name, _ string, _ entities.BranchProtection) error {
	if r.ErrorOnSaveProtection != nil {
		return r.ErrorOnSaveProtection
	}
	r.ProtectionSaves = append(r.ProtectionSaves, entities.Repository{Name: name})
	return nil
}

func (r *InMemoryBranchProtectionsRepository) EnableRequiredSignatures(_ context.Context, _, name, _ string) error {
	if r.ErrorOnSignatures != nil {
		return r.ErrorOnSignatures
	}
	r.SignaturesEnabled = append(r.SignaturesEnabled, name)
	return nil
}

func (r *InMemoryBranchProtectionsRepository) FindRulesetByName(_ context.Context, _, name, _ string) (*entities.Ruleset, error) {
	if r.ErrorOnFindRuleset != nil {
		return nil, r.ErrorOnFindRuleset
	}
	return r.RulesetsByName[name], nil
}

func (r *InMemoryBranchProtectionsRepository) CreateRuleset(_ context.Context, _, name string, _ entities.Ruleset) error {
	if r.ErrorOnCreateRuleset != nil {
		return r.ErrorOnCreateRuleset
	}
	r.RulesetsCreated = append(r.RulesetsCreated, name)
	return nil
}

// Ensure interface compliance.
var _ repositories.BranchProtectionsRepository = (*InMemoryBranchProtectionsRepository)(nil)
