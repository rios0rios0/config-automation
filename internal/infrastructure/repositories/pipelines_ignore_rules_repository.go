package repositories

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

const (
	// ignoreRulesScript is where a checkout of rios0rios0/pipelines keeps the
	// generator `make gitignore` runs.
	ignoreRulesScript = "global/scripts/tools/gitignore/run.sh"
	// ignoreRulesTimeout bounds one run of the generator, which only reads a
	// Makefile and rewrites one file, so a hang cannot hold the batch.
	ignoreRulesTimeout = 2 * time.Minute
	// scriptsDirectoryVariable is how the generator finds its fragments, the
	// variable the pipelines Makefiles set to the same checkout.
	scriptsDirectoryVariable = "SCRIPTS_DIR"
)

// errNoIgnoreRulesScript reports a directory that holds no generator.
var errNoIgnoreRulesScript = errors.New("not a checkout of rios0rios0/pipelines")

// PipelinesIgnoreRulesRepository implements repositories.IgnoreRulesRepository
// with the generator script of a rios0rios0/pipelines checkout, so the
// fragments and the rules that pick them have a single source. The script
// starts with nothing of this process's environment but the tools' search
// path, the temporary directory, and the locale, so no token reaches it.
type PipelinesIgnoreRulesRepository struct {
	scriptsDirectory string
	environ          func() []string
}

// Ensure the port is satisfied at compile time.
var _ repositories.IgnoreRulesRepository = (*PipelinesIgnoreRulesRepository)(nil)

// NewPipelinesIgnoreRulesRepository is the constructor the composition root
// calls with the pipelines checkout, refusing a directory that holds no
// generator so a misconfigured run stops before cloning anything. environ
// supplies the environment the generator's variables are kept from.
func NewPipelinesIgnoreRulesRepository(
	scriptsDirectory string,
	environ func() []string,
) (*PipelinesIgnoreRulesRepository, error) {
	absolute, err := filepath.Abs(scriptsDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", scriptsDirectory, err)
	}
	info, err := os.Stat(filepath.Join(absolute, filepath.FromSlash(ignoreRulesScript)))
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w: it holds no %s", scriptsDirectory, errNoIgnoreRulesScript, ignoreRulesScript)
	}
	return &PipelinesIgnoreRulesRepository{scriptsDirectory: absolute, environ: environ}, nil
}

// Generate runs the generator in root, the way `make gitignore` does there.
// The output is what the script printed, trimmed; a failure carries it too.
func (r *PipelinesIgnoreRulesRepository) Generate(ctx context.Context, root string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, ignoreRulesTimeout)
	defer cancel()
	script := filepath.Join(r.scriptsDirectory, filepath.FromSlash(ignoreRulesScript))
	// #nosec G204 -- sh runs the pipelines checkout's own generator, and each argument is one argv entry
	command := exec.CommandContext(ctx, "sh", script, ".")
	command.Dir = root
	command.Env = r.environment()
	output, err := command.CombinedOutput()
	printed := strings.TrimSpace(string(output))
	if err != nil {
		return printed, fmt.Errorf("the shared ignore-rules generator failed: %w: %s", err, printed)
	}
	return printed, nil
}

// environment keeps the variables the generator's POSIX tools read and adds
// the checkout it reads its fragments from.
func (r *PipelinesIgnoreRulesRepository) environment() []string {
	kept := []string{}
	for _, entry := range r.environ() {
		name, _, _ := strings.Cut(entry, "=")
		if slices.Contains(generatorVariables(), name) {
			kept = append(kept, entry)
		}
	}
	return append(kept, scriptsDirectoryVariable+"="+r.scriptsDirectory)
}

// generatorVariables are the only variables the generator inherits: where
// its tools are, where `mktemp` writes, and the locale `sort` orders by.
func generatorVariables() []string {
	return []string{"PATH", "TMPDIR", "LANG", "LC_ALL", "LC_COLLATE", "LC_CTYPE"}
}
