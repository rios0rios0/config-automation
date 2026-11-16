package repositories

import "context"

// ChangesRepository is the port to what a refresh does with a clone's
// history: switching branches, measuring drift against HEAD, restoring files,
// and telling whether the branch has anything to publish. Every path is
// relative to root, slash-separated, and taken literally, never as a pattern.
type ChangesRepository interface {
	// Checkout switches the working tree to an existing branch.
	Checkout(ctx context.Context, root, branch string) error

	// CreateBranch starts branch at HEAD, replacing a branch of that name.
	CreateBranch(ctx context.Context, root, branch string) error

	// MarkIntentToAdd records untracked paths in the index as about to be
	// added, so a new file shows in a diff against HEAD.
	MarkIntentToAdd(ctx context.Context, root string, paths []string) error

	// HasChanges reports whether paths differ from HEAD, whitespace aside.
	HasChanges(ctx context.Context, root string, paths []string) (bool, error)

	// FindDiff returns the zero-context diff of paths against HEAD.
	FindDiff(ctx context.Context, root string, paths []string) (string, error)

	// HasUnstagedChanges reports whether path differs between the index and
	// the working tree.
	HasUnstagedChanges(ctx context.Context, root, path string) (bool, error)

	// Restore replaces path in the working tree with its index version.
	Restore(ctx context.Context, root, path string) error

	// FindUntracked lists the untracked files below directory that the ignore
	// rules do not match.
	FindUntracked(ctx context.Context, root, directory string) ([]string, error)

	// HasStagedChanges reports whether the index differs from HEAD.
	HasStagedChanges(ctx context.Context, root string) (bool, error)

	// IsAhead reports whether HEAD holds commits that base does not.
	IsAhead(ctx context.Context, root, base string) (bool, error)
}
