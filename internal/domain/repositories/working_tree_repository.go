package repositories

import (
	"context"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

// WorkingTreeRepository is the port to a local Git working tree: its index,
// the files on disk, its ignore rules, and the commands that stage and
// commit. Every path is relative to root and slash-separated, as Git
// writes it. Implementations live in internal/infrastructure/repositories.
type WorkingTreeRepository interface {
	// FindIndexEntries lists the tracked paths with their modes and stages.
	FindIndexEntries(ctx context.Context, root string) ([]entities.IndexEntry, error)

	// FindFiles lists the tracked paths and the untracked ones the ignore
	// rules do not match.
	FindFiles(ctx context.Context, root string) ([]string, error)

	// FindKind classifies path without following a symlink.
	FindKind(ctx context.Context, root, path string) (entities.PathKind, error)

	// FindLinkTarget returns the target a symlink names, as written.
	FindLinkTarget(ctx context.Context, root, path string) (string, error)

	// FindContent returns the bytes of a file.
	FindContent(ctx context.Context, root, path string) ([]byte, error)

	// SaveContent replaces the bytes of a file, creating it when absent.
	SaveContent(ctx context.Context, root, path string, content []byte) error

	// Delete removes a file or symlink from disk, leaving the index alone.
	Delete(ctx context.Context, root, path string) error

	// IsIgnored reports whether the ignore rules match path, even while
	// path is still tracked.
	IsIgnored(ctx context.Context, root, path string) (bool, error)

	// Move renames a tracked file, in the index and on disk.
	Move(ctx context.Context, root, from, to string) error

	// Untrack removes a tracked file from the index and from disk.
	Untrack(ctx context.Context, root, path string) error

	// Stage records the current state of paths in the index.
	Stage(ctx context.Context, root string, paths []string) error

	// Commit records the index as a new commit with message.
	Commit(ctx context.Context, root, message string) error
}
