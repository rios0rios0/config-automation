package repositories

import (
	"context"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

// ClonesRepository is the port to the scratch clones a refresh works in. A
// credential reaches git only when the host asks for one: it is never part of
// a URL, a command line, or the clone's configuration, which sits in the
// directory the agent works in.
type ClonesRepository interface {
	// Clone copies the repository at url into directory.
	Clone(ctx context.Context, url, directory string, credential entities.GitCredential) error

	// Push force-pushes branch to the clone's origin and tracks it there.
	Push(ctx context.Context, root, branch string, credential entities.GitCredential) error

	// Discard removes a scratch clone, and does nothing when it is absent.
	Discard(ctx context.Context, directory string) error
}
