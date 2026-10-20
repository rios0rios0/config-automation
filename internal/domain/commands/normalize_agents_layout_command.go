package commands

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// layoutHandler settles one pair and returns what it did.
type layoutHandler func(ctx context.Context, root string, pair entities.LayoutPair) (entities.LayoutOutcome, error)

// NormalizeAgentsLayoutCommand keeps AGENTS.md as the instruction file and
// every CLAUDE.md beside one as the import of it, deterministically, before
// any model runs. A pair it cannot change safely is reported and left alone.
type NormalizeAgentsLayoutCommand struct {
	workingTree repositories.WorkingTreeRepository
	handlers    map[entities.LayoutPairState]layoutHandler
}

// NewNormalizeAgentsLayoutCommand is the constructor the composition root calls. A pair
// whose state has no handler is skipped.
func NewNormalizeAgentsLayoutCommand(workingTree repositories.WorkingTreeRepository) *NormalizeAgentsLayoutCommand {
	command := &NormalizeAgentsLayoutCommand{workingTree: workingTree}
	command.handlers = map[entities.LayoutPairState]layoutHandler{
		{Claude: entities.SideFile, Agents: entities.SideFile}:      command.merge,
		{Claude: entities.SideAbsent, Agents: entities.SideFile}:    command.point,
		{Claude: entities.SideLink, Agents: entities.SideFile}:      command.point,
		{Claude: entities.SidePointer, Agents: entities.SideAbsent}: command.dangle,
		{Claude: entities.SidePointer, Agents: entities.SideFile}:   command.keep,
		{Claude: entities.SideAbsent, Agents: entities.SideAbsent}:  command.keep,
	}
	return command
}

// NormalizeAgentsLayoutInput names the working tree to normalize.
type NormalizeAgentsLayoutInput struct {
	Root string
}

// NormalizeAgentsLayoutListeners receives the report or the failure.
type NormalizeAgentsLayoutListeners struct {
	OnSuccess func(report entities.AgentsLayoutReport)
	OnError   func(err error)
}

// Execute brings the working tree into the AGENTS.md layout. A rename is
// committed on its own; every other change is staged for the refresh
// commit.
func (c *NormalizeAgentsLayoutCommand) Execute(
	ctx context.Context,
	input NormalizeAgentsLayoutInput,
	listeners NormalizeAgentsLayoutListeners,
) {
	report, err := c.normalize(ctx, input.Root)
	if err != nil {
		listeners.OnError(err)
		return
	}
	listeners.OnSuccess(report)
}

func (c *NormalizeAgentsLayoutCommand) normalize(
	ctx context.Context,
	root string,
) (entities.AgentsLayoutReport, error) {
	report := entities.NewAgentsLayoutReport()
	if err := c.renameLegacyFiles(ctx, root, &report); err != nil {
		return report, err
	}
	touched, err := c.settlePairs(ctx, root, &report)
	if err != nil {
		return report, err
	}
	if len(touched) > 0 {
		if err = c.workingTree.Stage(ctx, root, sortedPaths(touched)); err != nil {
			return report, fmt.Errorf("staging the layout: %w", err)
		}
	}
	report.Changed = len(report.Renamed) > 0 || len(touched) > 0
	report.Note = entities.DescribeAgentsLayout(report)
	return report, nil
}

// renameLegacyFiles moves every CLAUDE.md that has no AGENTS.md, or only a
// symlink to itself, into AGENTS.md and commits the renames alone, so
// `git log --follow AGENTS.md` reaches the history CLAUDE.md had. A pair
// with an ignored or unsafe side never matches.
func (c *NormalizeAgentsLayoutCommand) renameLegacyFiles(
	ctx context.Context,
	root string,
	report *entities.AgentsLayoutReport,
) error {
	pairs, err := c.pairs(ctx, root)
	if err != nil {
		return err
	}
	for _, pair := range pairs {
		if pair.State.Claude != entities.SideFile ||
			(pair.State.Agents != entities.SideAbsent && pair.State.Agents != entities.SideLink) {
			continue
		}
		if pair.State.Agents == entities.SideLink {
			if err = c.workingTree.Untrack(ctx, root, pair.Agents); err != nil {
				return fmt.Errorf("removing the %s symlink: %w", pair.Agents, err)
			}
		}
		if err = c.workingTree.Move(ctx, root, pair.Claude, pair.Agents); err != nil {
			return fmt.Errorf("renaming %s: %w", pair.Claude, err)
		}
		report.Record(entities.OutcomeRenamed, pair.Claude)
	}
	if len(report.Renamed) == 0 {
		return nil
	}
	if err = c.workingTree.Commit(ctx, root, entities.RenameMessage); err != nil {
		return fmt.Errorf("committing the renames: %w", err)
	}
	return nil
}

// settlePairs runs every pair through the handler of its state, replaces a
// legacy banner, and returns the files it changed. The import a rename
// leaves behind is reported as part of the rename.
func (c *NormalizeAgentsLayoutCommand) settlePairs(
	ctx context.Context,
	root string,
	report *entities.AgentsLayoutReport,
) (map[string]bool, error) {
	pairs, err := c.pairs(ctx, root)
	if err != nil {
		return nil, err
	}
	touched := map[string]bool{}
	for _, pair := range pairs {
		handler, found := c.handlers[pair.State]
		if !found {
			handler = c.skip
		}
		outcome, handlerErr := handler(ctx, root, pair)
		if handlerErr != nil {
			return nil, handlerErr
		}
		if outcome != entities.OutcomePointed || !slices.Contains(report.Renamed, pair.Claude) {
			report.Record(outcome, pair.Claude)
		}
		if outcome == entities.OutcomeSkipped {
			continue
		}
		if outcome == entities.OutcomeMerged || outcome == entities.OutcomePointed {
			touched[pair.Claude], touched[pair.Agents] = true, true
		}
		rebannered, rebannerErr := c.rebanner(ctx, root, pair)
		if rebannerErr != nil {
			return nil, rebannerErr
		}
		if rebannered {
			touched[pair.Agents] = true
			report.Record(entities.OutcomeRebannered, pair.Agents)
		}
	}
	return touched, nil
}

// pairs lists every directory holding a tracked or new CLAUDE.md or
// AGENTS.md outside .claude and the excluded directories, in path order.
// Untracked files count, so the pass after the refresh sees an AGENTS.md
// the refresh created.
func (c *NormalizeAgentsLayoutCommand) pairs(ctx context.Context, root string) ([]entities.LayoutPair, error) {
	files, err := c.workingTree.FindFiles(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("listing files: %w", err)
	}
	directories := map[string]bool{}
	for _, file := range files {
		if entities.IsPairPath(file) && entities.IsRefreshPath(file) {
			directories[path.Dir(file)] = true
		}
	}
	ordered := sortedPaths(directories)
	slices.SortFunc(ordered, func(left, right string) int {
		return slices.Compare(directoryParts(left), directoryParts(right))
	})
	pairs := make([]entities.LayoutPair, 0, len(ordered))
	for _, directory := range ordered {
		pair, pairErr := c.pair(ctx, root, directory)
		if pairErr != nil {
			return nil, pairErr
		}
		pairs = append(pairs, pair)
	}
	return pairs, nil
}

// pair classifies the CLAUDE.md and AGENTS.md of one directory.
func (c *NormalizeAgentsLayoutCommand) pair(
	ctx context.Context,
	root, directory string,
) (entities.LayoutPair, error) {
	claude, agents := path.Join(directory, entities.ClaudeFile), path.Join(directory, entities.AgentsFile)
	claudeSide, err := c.side(ctx, root, claude, entities.AgentsFile)
	if err != nil {
		return entities.LayoutPair{}, err
	}
	agentsSide, err := c.side(ctx, root, agents, entities.ClaudeFile)
	if err != nil {
		return entities.LayoutPair{}, err
	}
	return entities.LayoutPair{
		Claude: claude,
		Agents: agents,
		State:  entities.LayoutPairState{Claude: claudeSide, Agents: agentsSide},
	}, nil
}

// side classifies one file of a pair without following a symlink.
func (c *NormalizeAgentsLayoutCommand) side(
	ctx context.Context,
	root, file, sibling string,
) (entities.PairSide, error) {
	safe, err := hasSafeParents(ctx, c.workingTree, root, file)
	if err != nil || !safe {
		return entities.SideUnsafe, err
	}
	ignored, err := c.workingTree.IsIgnored(ctx, root, file)
	if err != nil {
		return entities.SideUnsafe, fmt.Errorf("checking the ignore rules for %s: %w", file, err)
	}
	if ignored {
		return entities.SideIgnored, nil
	}
	kind, err := c.workingTree.FindKind(ctx, root, file)
	if err != nil {
		return entities.SideUnsafe, fmt.Errorf("inspecting %s: %w", file, err)
	}
	if kind == entities.PathSymlink {
		return c.linkSide(ctx, root, file, sibling)
	}
	if kind == entities.PathAbsent {
		return entities.SideAbsent, nil
	}
	if kind != entities.PathFile {
		return entities.SideUnsafe, nil
	}
	content, err := c.workingTree.FindContent(ctx, root, file)
	if err != nil {
		return entities.SideUnsafe, fmt.Errorf("reading %s: %w", file, err)
	}
	if !utf8.Valid(content) {
		return entities.SideUnsafe, nil
	}
	if sibling == entities.AgentsFile && entities.IsAgentsImport(string(content)) {
		return entities.SidePointer, nil
	}
	return entities.SideFile, nil
}

// linkSide classifies a symlink: one to the other file of its pair can be
// replaced, any other is left alone.
func (c *NormalizeAgentsLayoutCommand) linkSide(
	ctx context.Context,
	root, file, sibling string,
) (entities.PairSide, error) {
	target, err := c.workingTree.FindLinkTarget(ctx, root, file)
	if err != nil {
		return entities.SideUnsafe, fmt.Errorf("reading the %s symlink: %w", file, err)
	}
	if entities.SameLinkTarget(target, sibling) {
		return entities.SideLink, nil
	}
	return entities.SideUnsafe, nil
}

// merge keeps what a CLAUDE.md adds beside an existing AGENTS.md, for the
// refresh to integrate, and reduces CLAUDE.md to the import.
func (c *NormalizeAgentsLayoutCommand) merge(
	ctx context.Context,
	root string,
	pair entities.LayoutPair,
) (entities.LayoutOutcome, error) {
	claude, err := c.workingTree.FindContent(ctx, root, pair.Claude)
	if err != nil {
		return entities.OutcomeKept, fmt.Errorf("reading %s: %w", pair.Claude, err)
	}
	agents, err := c.workingTree.FindContent(ctx, root, pair.Agents)
	if err != nil {
		return entities.OutcomeKept, fmt.Errorf("reading %s: %w", pair.Agents, err)
	}
	if merged, changed := entities.MergedAgentsGuidance(string(agents), string(claude)); changed {
		if err = c.workingTree.SaveContent(ctx, root, pair.Agents, []byte(merged)); err != nil {
			return entities.OutcomeKept, fmt.Errorf("writing %s: %w", pair.Agents, err)
		}
	}
	return entities.OutcomeMerged, c.writeImport(ctx, root, pair.Claude)
}

// point writes the import beside an AGENTS.md, replacing a symlink itself
// rather than writing through it.
func (c *NormalizeAgentsLayoutCommand) point(
	ctx context.Context,
	root string,
	pair entities.LayoutPair,
) (entities.LayoutOutcome, error) {
	if pair.State.Claude == entities.SideLink {
		if err := c.workingTree.Delete(ctx, root, pair.Claude); err != nil {
			return entities.OutcomeKept, fmt.Errorf("removing the %s symlink: %w", pair.Claude, err)
		}
	}
	return entities.OutcomePointed, c.writeImport(ctx, root, pair.Claude)
}

// dangle reports an import whose AGENTS.md is missing; the refresh may
// create it.
func (c *NormalizeAgentsLayoutCommand) dangle(
	context.Context,
	string,
	entities.LayoutPair,
) (entities.LayoutOutcome, error) {
	return entities.OutcomeDangling, nil
}

// keep leaves a compliant pair as it is.
func (c *NormalizeAgentsLayoutCommand) keep(
	context.Context,
	string,
	entities.LayoutPair,
) (entities.LayoutOutcome, error) {
	return entities.OutcomeKept, nil
}

// skip leaves a pair it cannot change safely for a human to review.
func (c *NormalizeAgentsLayoutCommand) skip(
	context.Context,
	string,
	entities.LayoutPair,
) (entities.LayoutOutcome, error) {
	return entities.OutcomeSkipped, nil
}

// rebanner replaces the legacy CLAUDE.md banner of an AGENTS.md file.
func (c *NormalizeAgentsLayoutCommand) rebanner(
	ctx context.Context,
	root string,
	pair entities.LayoutPair,
) (bool, error) {
	if pair.State.Agents != entities.SideFile {
		return false, nil
	}
	content, err := c.workingTree.FindContent(ctx, root, pair.Agents)
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", pair.Agents, err)
	}
	text, changed := entities.WithAgentsBanner(string(content))
	if !changed {
		return false, nil
	}
	if err = c.workingTree.SaveContent(ctx, root, pair.Agents, []byte(text)); err != nil {
		return false, fmt.Errorf("writing %s: %w", pair.Agents, err)
	}
	return true, nil
}

// writeImport reduces a CLAUDE.md to the import of its AGENTS.md.
func (c *NormalizeAgentsLayoutCommand) writeImport(ctx context.Context, root, claude string) error {
	if err := c.workingTree.SaveContent(ctx, root, claude, []byte(entities.AgentsImport)); err != nil {
		return fmt.Errorf("writing %s: %w", claude, err)
	}
	return nil
}

// directoryParts splits a directory into its path parts, the root being
// none, so directories sort the way paths compare: the root first and
// "a/b" before "a-b".
func directoryParts(directory string) []string {
	if directory == "." {
		return []string{}
	}
	return strings.Split(directory, "/")
}
