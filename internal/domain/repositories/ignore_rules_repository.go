package repositories

import "context"

// IgnoreRulesRepository is the port to the generator of the shared ignore
// rules of rios0rios0/pipelines, the one `make gitignore` runs: the files a
// CI pipeline writes into a working tree, kept in a delimited block of its
// root `.gitignore`. Implementations live in internal/infrastructure/repositories.
type IgnoreRulesRepository interface {
	// Generate rewrites the shared block of root's `.gitignore` from the
	// fragments the generator selects for root, the languages its Makefile and
	// its CI import from the pipeline, creating the file when absent and
	// keeping every line outside the block. It returns what the generator
	// printed, which is repository-controlled text.
	Generate(ctx context.Context, root string) (string, error)
}
