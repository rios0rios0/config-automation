package commands

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// hasSafeParents reports whether path can become a permission rule and
// every directory above it is a real directory or is still absent, never a
// symlink or a file, so writing to it cannot escape the working tree.
func hasSafeParents(
	ctx context.Context,
	workingTree repositories.WorkingTreeRepository,
	root, path string,
) (bool, error) {
	if !entities.HasSafePathCharacters(path) {
		return false, nil
	}
	parts := strings.Split(path, "/")
	for index := range len(parts) - 1 {
		parent := strings.Join(parts[:index+1], "/")
		kind, err := workingTree.FindKind(ctx, root, parent)
		if err != nil {
			return false, fmt.Errorf("inspecting %s: %w", parent, err)
		}
		if kind != entities.PathDirectory && kind != entities.PathAbsent {
			return false, nil
		}
	}
	return true, nil
}

// isSafeFile reports whether path is a regular file, or could be created
// as one, under safe parents.
func isSafeFile(
	ctx context.Context,
	workingTree repositories.WorkingTreeRepository,
	root, path string,
) (bool, error) {
	safe, err := hasSafeParents(ctx, workingTree, root, path)
	if err != nil || !safe {
		return false, err
	}
	kind, err := workingTree.FindKind(ctx, root, path)
	if err != nil {
		return false, fmt.Errorf("inspecting %s: %w", path, err)
	}
	return kind == entities.PathFile || kind == entities.PathAbsent, nil
}

// sortedPaths returns the members of set in byte order, as a list that
// encodes as an empty array rather than null.
func sortedPaths(set map[string]bool) []string {
	paths := make([]string, 0, len(set))
	for path := range set {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}
