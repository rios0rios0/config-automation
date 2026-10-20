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

// DiscoverRefreshScopeInput names the working tree to inventory.
type DiscoverRefreshScopeInput struct {
	Root string
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
	scope, err := c.discover(ctx, input.Root)
	if err != nil {
		listeners.OnError(err)
		return
	}
	listeners.OnSuccess(scope)
}

func (c *DiscoverRefreshScopeCommand) discover(ctx context.Context, root string) (entities.RefreshScope, error) {
	existing, skipped, err := c.trackedDocuments(ctx, root)
	if err != nil {
		return entities.RefreshScope{}, err
	}
	// An import stays in the manifest for drift and staging, but only the
	// host writes it, so it never receives an edit grant. The root one is
	// listed even while absent: the host adds it once the refresh creates
	// AGENTS.md.
	managed, err := c.managedImports(ctx, root, existing)
	if err != nil {
		return entities.RefreshScope{}, err
	}
	if !existing[entities.ClaudeFile] {
		safe, safeErr := isSafeFile(ctx, c.workingTree, root, entities.ClaudeFile)
		if safeErr != nil {
			return entities.RefreshScope{}, safeErr
		}
		if safe {
			managed[entities.ClaudeFile] = true
		}
	}
	creatable, err := c.creatableDocuments(ctx, root, existing, managed)
	if err != nil {
		return entities.RefreshScope{}, err
	}
	editable := union(existing, creatable)
	tools := entities.ReadOnlyTools()
	for _, document := range sortedPaths(editable) {
		tools = append(tools, "Edit(/"+document+")")
	}
	return entities.RefreshScope{
		Paths:        sortedPaths(union(editable, managed)),
		Existing:     sortedPaths(existing),
		Creatable:    sortedPaths(creatable),
		Managed:      sortedPaths(managed),
		HasGuidance:  hasAgentGuidance(existing),
		AllowedTools: tools,
		Skipped:      sortedPaths(skipped),
	}, nil
}

// trackedDocuments splits the tracked refresh paths into regular files the
// refresh may edit and the rest: symlinks, submodules, unsafe paths.
func (c *DiscoverRefreshScopeCommand) trackedDocuments(
	ctx context.Context,
	root string,
) (map[string]bool, map[string]bool, error) {
	entries, err := c.workingTree.FindIndexEntries(ctx, root)
	if err != nil {
		return nil, nil, fmt.Errorf("listing the index: %w", err)
	}
	existing, skipped := map[string]bool{}, map[string]bool{}
	for _, entry := range entries {
		if !entities.IsRefreshPath(entry.Path) {
			continue
		}
		if entry.Stage != 0 {
			return nil, nil, fmt.Errorf("unmerged refresh path: %s", entry.Path)
		}
		safe, safeErr := isSafeFile(ctx, c.workingTree, root, entry.Path)
		if safeErr != nil {
			return nil, nil, safeErr
		}
		if !safe || !slices.Contains(entities.RegularFileModes(), entry.Mode) {
			skipped[entry.Path] = true
			continue
		}
		existing[entry.Path] = true
	}
	return existing, skipped, nil
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

// creatableDocuments returns the files the refresh may create: the
// platform's targets and the AGENTS.md every import points at, when absent,
// safe to write, and not ignored by the repository.
func (c *DiscoverRefreshScopeCommand) creatableDocuments(
	ctx context.Context,
	root string,
	existing, managed map[string]bool,
) (map[string]bool, error) {
	candidates := entities.RefreshCreatablePaths()
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

// hasAgentGuidance reports whether existing holds agent guidance beyond the
// documents written for people.
func hasAgentGuidance(existing map[string]bool) bool {
	for document := range existing {
		if !entities.IsHumanDocument(document) {
			return true
		}
	}
	return false
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
