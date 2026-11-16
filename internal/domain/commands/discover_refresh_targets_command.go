package commands

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// errNoBatchSize rejects a batch size below one, which would make no batch
// at all.
var errNoBatchSize = errors.New("the batch size must be a positive whole number")

// RefreshTargetSource is one organization to enumerate. Targets is bound to
// that organization's credential, because a fine-grained token reaches one
// resource owner only; it is nil when there is no credential, and then
// Unavailable says why.
type RefreshTargetSource struct {
	Organization string
	Credential   string
	Targets      repositories.RefreshTargetsRepository
	Unavailable  string
}

// UnreachableOrganization is an organization the discovery could not
// enumerate, and why. The other organizations are still refreshed, and the
// caller turns the run red so the gap cannot pass as success.
type UnreachableOrganization struct {
	Organization string
	Reason       string
}

// DiscoverRefreshTargetsCommand turns organizations into the matrix of
// batches the refresh runs, or one named repository into a single batch.
type DiscoverRefreshTargetsCommand struct{}

// NewDiscoverRefreshTargetsCommand is the constructor the composition root calls.
func NewDiscoverRefreshTargetsCommand() *DiscoverRefreshTargetsCommand {
	return &DiscoverRefreshTargetsCommand{}
}

// DiscoverRefreshTargetsInput lists the organizations, the repositories the
// configuration leaves out by qualified name, the batch size, and an
// optional single target.
type DiscoverRefreshTargetsInput struct {
	Sources   []RefreshTargetSource
	Exclude   []string
	BatchSize int
	Target    *entities.RefreshTargetReference
}

// DiscoverRefreshTargetsListeners receives the batches with the organizations
// that could not be enumerated, a refused single target, or a failure.
type DiscoverRefreshTargetsListeners struct {
	OnSuccess func(batches []entities.RefreshBatch, unreachable []UnreachableOrganization)
	OnRefused func(target string, reasons []string)
	OnError   func(err error)
}

// Execute enumerates every organization, or resolves the single target.
func (c *DiscoverRefreshTargetsCommand) Execute(
	ctx context.Context,
	input DiscoverRefreshTargetsInput,
	listeners DiscoverRefreshTargetsListeners,
) {
	if input.BatchSize < 1 {
		listeners.OnError(errNoBatchSize)
		return
	}
	if input.Target != nil {
		c.resolveTarget(ctx, input, listeners)
		return
	}
	batches, unreachable := []entities.RefreshBatch{}, []UnreachableOrganization{}
	for _, source := range input.Sources {
		if source.Targets == nil {
			unreachable = append(unreachable, UnreachableOrganization{source.Organization, source.Unavailable})
			continue
		}
		targets, err := source.Targets.FindAllByOrganization(ctx, source.Organization)
		if err != nil {
			unreachable = append(unreachable, UnreachableOrganization{source.Organization, err.Error()})
			continue
		}
		kept := []entities.RefreshTarget{}
		for _, target := range targets {
			if len(target.ExclusionReasons()) == 0 && !slices.Contains(input.Exclude, target.QualifiedName()) {
				kept = append(kept, target)
			}
		}
		batches = append(batches, entities.ChunkRefreshTargets(
			source.Organization, source.Credential, kept, input.BatchSize)...)
	}
	listeners.OnSuccess(batches, unreachable)
}

// resolveTarget fetches the named repository with the state the schedule
// filters on, so naming a repository cannot reach one the schedule leaves
// out. Such a repository is refused rather than skipped, so the dispatch
// fails visibly instead of finishing green with nothing refreshed.
func (c *DiscoverRefreshTargetsCommand) resolveTarget(
	ctx context.Context,
	input DiscoverRefreshTargetsInput,
	listeners DiscoverRefreshTargetsListeners,
) {
	reference := *input.Target
	source, found := targetSource(input.Sources, reference.Organization)
	if !found {
		listeners.OnError(fmt.Errorf("the organization %q is not configured", reference.Organization))
		return
	}
	if source.Targets == nil {
		listeners.OnError(fmt.Errorf("cannot reach %s: %s", source.Organization, source.Unavailable))
		return
	}
	reference.Organization = source.Organization
	target, err := source.Targets.FindByName(ctx, reference)
	if err != nil {
		listeners.OnError(fmt.Errorf("fetching %s: %w", reference.Name, err))
		return
	}
	reasons := target.ExclusionReasons()
	if slices.Contains(input.Exclude, target.QualifiedName()) {
		reasons = append(reasons, "excluded by the configuration")
	}
	if len(reasons) > 0 {
		listeners.OnRefused(target.QualifiedName(), reasons)
		return
	}
	listeners.OnSuccess(
		entities.ChunkRefreshTargets(source.Organization, source.Credential,
			[]entities.RefreshTarget{target}, input.BatchSize),
		[]UnreachableOrganization{},
	)
}

// targetSource finds the organization a single target belongs to; an empty
// organization means the first configured one.
func targetSource(sources []RefreshTargetSource, organization string) (RefreshTargetSource, bool) {
	for _, source := range sources {
		if organization == "" || source.Organization == organization {
			return source, true
		}
	}
	return RefreshTargetSource{}, false
}
