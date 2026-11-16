package commands

import (
	"context"
	"fmt"
	"path"
	"slices"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// DiscoverRefreshScopeCommand builds the manifest the config-and-docs
// refresh works from. Its exact paths drive the Edit grants, the prompt's
// file list, drift detection, and staging, so every category the refresh
// maintains is added here, never in the workflows.
type DiscoverRefreshScopeCommand struct {
	workingTree repositories.WorkingTreeRepository
}

// NewDiscoverRefreshScopeCommand is the constructor the composition root calls.
func NewDiscoverRefreshScopeCommand(workingTree repositories.WorkingTreeRepository) *DiscoverRefreshScopeCommand {
	return &DiscoverRefreshScopeCommand{workingTree: workingTree}
}

// DiscoverRefreshScopeInput names the working tree to inventory and the
// instruction files it keeps.
type DiscoverRefreshScopeInput struct {
	Root             string
	InstructionFiles entities.InstructionFiles
}

// DiscoverRefreshScopeListeners receives the manifest or the failure.
type DiscoverRefreshScopeListeners struct {
	OnSuccess func(scope entities.RefreshScope)
	OnError   func(err error)
}

// Execute inventories the tracked documentation of the working tree.
func (c *DiscoverRefreshScopeCommand) Execute(
	ctx context.Context,
	input DiscoverRefreshScopeInput,
	listeners DiscoverRefreshScopeListeners,
) {
	scope, err := c.discover(ctx, input.Root, input.InstructionFiles)
	if err != nil {
		listeners.OnError(err)
		return
	}
	listeners.OnSuccess(scope)
}

func (c *DiscoverRefreshScopeCommand) discover(
	ctx context.Context,
	root string,
	files entities.InstructionFiles,
) (entities.RefreshScope, error) {
	tracked, err := c.trackedDocuments(ctx, root, files)
	if err != nil {
		return entities.RefreshScope{}, err
	}
	// An import stays in the manifest for drift and staging, but only the
	// host writes it, so it never receives an edit grant. The root one is
	// listed even while absent: the host adds it once the refresh creates
	// AGENTS.md.
	managed, err := c.managedImports(ctx, root, tracked.existing)
	if err != nil {
		return entities.RefreshScope{}, err
	}
	if files.Claude && !tracked.existing[entities.ClaudeFile] {
		safe, safeErr := isSafeFile(ctx, c.workingTree, root, entities.ClaudeFile)
		if safeErr != nil {
			return entities.RefreshScope{}, safeErr
		}
		if safe {
			managed[entities.ClaudeFile] = true
		}
	}
	creatable, err := c.creatableDocuments(ctx, root, files, tracked.existing, managed)
	if err != nil {
		return entities.RefreshScope{}, err
	}
	editable := union(tracked.existing, creatable)
	tools := entities.ReadOnlyTools()
	for _, document := range sortedPaths(editable) {
		tools = append(tools, "Edit(/"+document+")")
	}
	return entities.RefreshScope{
		Paths:        sortedPaths(union(union(editable, managed), tracked.removed)),
		Existing:     sortedPaths(tracked.existing),
		Creatable:    sortedPaths(creatable),
		Managed:      sortedPaths(managed),
		Removed:      sortedPaths(tracked.removed),
		AllowedTools: tools,
		Skipped:      sortedPaths(tracked.skipped),
	}, nil
}

// trackedSet splits the tracked refresh paths by what the refresh may do
// with them.
type trackedSet struct {
	existing map[string]bool
	removed  map[string]bool
	skipped  map[string]bool
}

// trackedDocuments splits the tracked refresh paths into regular files the
// refresh may edit, instruction files of a kind the repository does not keep,
// and the rest: symlinks, submodules, unsafe paths. A removed path stays
// tracked until the refresh commit stages its deletion, whatever is on disk.
func (c *DiscoverRefreshScopeCommand) trackedDocuments(
	ctx context.Context,
	root string,
	files entities.InstructionFiles,
) (trackedSet, error) {
	entries, err := c.workingTree.FindIndexEntries(ctx, root)
	if err != nil {
		return trackedSet{}, fmt.Errorf("listing the index: %w", err)
	}
	tracked := trackedSet{existing: map[string]bool{}, removed: map[string]bool{}, skipped: map[string]bool{}}
	for _, entry := range entries {
		if !entities.IsRefreshPath(entry.Path) {
			continue
		}
		if entry.Stage != 0 {
			return trackedSet{}, fmt.Errorf("unmerged refresh path: %s", entry.Path)
		}
		if files.IsRemovedPath(entry.Path) {
			tracked.removed[entry.Path] = true
			continue
		}
		safe, safeErr := isSafeFile(ctx, c.workingTree, root, entry.Path)
		if safeErr != nil {
			return trackedSet{}, safeErr
		}
		if !safe || !slices.Contains(entities.RegularFileModes(), entry.Mode) {
			tracked.skipped[entry.Path] = true
			continue
		}
		tracked.existing[entry.Path] = true
	}
	return tracked, nil
}

// managedImports moves every CLAUDE.md that only imports AGENTS.md out of
// existing and returns them.
func (c *DiscoverRefreshScopeCommand) managedImports(
	ctx context.Context,
	root string,
	existing map[string]bool,
) (map[string]bool, error) {
	managed := map[string]bool{}
	for document := range existing {
		if !entities.IsPairPath(document) || path.Base(document) != entities.ClaudeFile {
			continue
		}
		content, err := c.workingTree.FindContent(ctx, root, document)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", document, err)
		}
		if entities.IsAgentsImport(string(content)) {
			managed[document] = true
		}
	}
	for document := range managed {
		delete(existing, document)
	}
	return managed, nil
}

// creatableDocuments returns the files the refresh may create: the targets
// the instruction files allow and the AGENTS.md every import points at, when
// absent, safe to write, and not ignored by the repository.
func (c *DiscoverRefreshScopeCommand) creatableDocuments(
	ctx context.Context,
	root string,
	files entities.InstructionFiles,
	existing, managed map[string]bool,
) (map[string]bool, error) {
	candidates := files.CreatablePaths()
	for document := range managed {
		candidates = append(candidates, path.Join(path.Dir(document), entities.AgentsFile))
	}
	creatable := map[string]bool{}
	for _, candidate := range candidates {
		if existing[candidate] {
			continue
		}
		safe, err := isSafeFile(ctx, c.workingTree, root, candidate)
		if err != nil {
			return nil, err
		}
		if !safe {
			continue
		}
		ignored, err := c.workingTree.IsIgnored(ctx, root, candidate)
		if err != nil {
			return nil, fmt.Errorf("checking the ignore rules for %s: %w", candidate, err)
		}
		if !ignored {
			creatable[candidate] = true
		}
	}
	return creatable, nil
}

// union returns the members of both sets.
func union(left, right map[string]bool) map[string]bool {
	members := map[string]bool{}
	for _, set := range []map[string]bool{left, right} {
		for member := range set {
			members[member] = true
		}
	}
	return members
}
