package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// errNoIgnoreFile reports a generator that exited cleanly but left no
// regular `.gitignore` behind.
var errNoIgnoreFile = errors.New("the generator left no regular file at " + entities.IgnoreFile)

// SyncIgnoreRulesCommand keeps the shared ignore rules of rios0rios0/pipelines
// in a working tree's root `.gitignore`. The generator owns the delimited
// block and the rest of the file stays the repository's own; this command
// decides whether the file may be written at all and tells what changed.
type SyncIgnoreRulesCommand struct {
	workingTree repositories.WorkingTreeRepository
	ignoreRules repositories.IgnoreRulesRepository
}

// NewSyncIgnoreRulesCommand is the constructor the composition root calls. A
// nil generator leaves every `.gitignore` alone.
func NewSyncIgnoreRulesCommand(
	workingTree repositories.WorkingTreeRepository,
	ignoreRules repositories.IgnoreRulesRepository,
) *SyncIgnoreRulesCommand {
	return &SyncIgnoreRulesCommand{workingTree: workingTree, ignoreRules: ignoreRules}
}

// SyncIgnoreRulesInput names the working tree whose `.gitignore` to sync.
type SyncIgnoreRulesInput struct {
	Root string
}

// SyncIgnoreRulesListeners receives the report or the failure.
type SyncIgnoreRulesListeners struct {
	OnSuccess func(report entities.IgnoreRulesReport)
	OnError   func(err error)
}

// Execute writes the shared ignore rules into the root `.gitignore`.
func (c *SyncIgnoreRulesCommand) Execute(
	ctx context.Context,
	input SyncIgnoreRulesInput,
	listeners SyncIgnoreRulesListeners,
) {
	report, err := c.sync(ctx, input.Root)
	if err != nil {
		listeners.OnError(err)
		return
	}
	listeners.OnSuccess(report)
}

// sync runs the generator on a regular or absent `.gitignore` and compares
// the file before and after. A symlink is never handed to the generator: it
// replaces the file by copying over the path, which would follow the link
// out of the working tree.
func (c *SyncIgnoreRulesCommand) sync(ctx context.Context, root string) (entities.IgnoreRulesReport, error) {
	if c.ignoreRules == nil {
		return entities.IgnoreRulesReport{Outcome: entities.IgnoreRulesOff}, nil
	}
	kind, err := c.workingTree.FindKind(ctx, root, entities.IgnoreFile)
	if err != nil {
		return entities.IgnoreRulesReport{}, fmt.Errorf("inspecting %s: %w", entities.IgnoreFile, err)
	}
	if kind != entities.PathFile && kind != entities.PathAbsent {
		return entities.IgnoreRulesReport{Outcome: entities.IgnoreRulesSkipped}, nil
	}
	before := ""
	if kind == entities.PathFile {
		if before, err = c.content(ctx, root); err != nil {
			return entities.IgnoreRulesReport{}, err
		}
	}
	output, err := c.ignoreRules.Generate(ctx, root)
	if err != nil {
		return entities.IgnoreRulesReport{}, err
	}
	after, err := c.generated(ctx, root)
	if err != nil {
		return entities.IgnoreRulesReport{}, err
	}
	outcome := entities.IgnoreRulesUpdated
	if kind == entities.PathAbsent {
		outcome = entities.IgnoreRulesCreated
	} else if after == before {
		outcome = entities.IgnoreRulesCurrent
	}
	return entities.IgnoreRulesReport{Outcome: outcome, Output: output}, nil
}

// generated reads the `.gitignore` the generator left, which must be a
// regular file.
func (c *SyncIgnoreRulesCommand) generated(ctx context.Context, root string) (string, error) {
	kind, err := c.workingTree.FindKind(ctx, root, entities.IgnoreFile)
	if err != nil {
		return "", fmt.Errorf("inspecting %s: %w", entities.IgnoreFile, err)
	}
	if kind != entities.PathFile {
		return "", errNoIgnoreFile
	}
	return c.content(ctx, root)
}

// content reads `.gitignore`.
func (c *SyncIgnoreRulesCommand) content(ctx context.Context, root string) (string, error) {
	content, err := c.workingTree.FindContent(ctx, root, entities.IgnoreFile)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", entities.IgnoreFile, err)
	}
	return string(content), nil
}
