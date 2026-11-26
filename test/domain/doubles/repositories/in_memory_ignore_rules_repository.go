package repositories

import (
	"context"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// InMemoryIgnoreRulesRepository is the in-memory double for
// repositories.IgnoreRulesRepository. It stands in for the generator by
// writing Content into the working-tree double's `.gitignore`, or leaving the
// file as it is when Content is empty, and records every root it ran in.
type InMemoryIgnoreRulesRepository struct {
	tree    *InMemoryWorkingTreeRepository
	Content string
	Output  string
	Roots   []string

	// ErrorOnGenerate is returned, with Output, when set.
	ErrorOnGenerate error
}

// Ensure the port is satisfied at compile time.
var _ repositories.IgnoreRulesRepository = (*InMemoryIgnoreRulesRepository)(nil)

// NewInMemoryIgnoreRulesRepository builds a generator that writes into tree.
func NewInMemoryIgnoreRulesRepository(tree *InMemoryWorkingTreeRepository) *InMemoryIgnoreRulesRepository {
	return &InMemoryIgnoreRulesRepository{tree: tree}
}

// WithContent makes the generator leave content in `.gitignore` and print
// output.
func (r *InMemoryIgnoreRulesRepository) WithContent(content, output string) *InMemoryIgnoreRulesRepository {
	r.Content, r.Output = content, output
	return r
}

// Generate writes Content into `.gitignore`, unless it is empty.
func (r *InMemoryIgnoreRulesRepository) Generate(ctx context.Context, root string) (string, error) {
	r.Roots = append(r.Roots, root)
	if r.ErrorOnGenerate != nil {
		return r.Output, r.ErrorOnGenerate
	}
	if r.Content == "" {
		return r.Output, nil
	}
	return r.Output, r.tree.SaveContent(ctx, root, entities.IgnoreFile, []byte(r.Content))
}
