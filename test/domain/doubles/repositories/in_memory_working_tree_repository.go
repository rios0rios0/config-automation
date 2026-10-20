package repositories

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

const (
	// symlinkMode is the index mode Git records for a symlink.
	symlinkMode = "120000"
	// conflictStage is the index stage of "our" side of a merge conflict.
	conflictStage = 2
)

// InMemoryWorkingTreeRepository is the in-memory double for
// repositories.WorkingTreeRepository used by command tests. Files and
// symlinks live in maps keyed by slash-separated path, Tracked mirrors the
// index, and Ignored lists the paths, or directory prefixes ending in "/",
// that the ignore rules match. It records every commit and staged path.
type InMemoryWorkingTreeRepository struct {
	Files    map[string]string
	Links    map[string]string
	Tracked  map[string]bool
	Ignored  []string
	Modes    map[string]string
	Unmerged map[string]bool
	Commits  []string
	Staged   []string

	// Error hooks: set to surface a specific error from a method.
	ErrorOnIndex  error
	ErrorOnFiles  error
	ErrorOnStage  error
	ErrorOnCommit error
}

// NewInMemoryWorkingTreeRepository builds an empty working tree.
func NewInMemoryWorkingTreeRepository() *InMemoryWorkingTreeRepository {
	return &InMemoryWorkingTreeRepository{
		Files:    map[string]string{},
		Links:    map[string]string{},
		Tracked:  map[string]bool{},
		Modes:    map[string]string{},
		Unmerged: map[string]bool{},
	}
}

// WithTrackedFiles adds committed files with their content.
func (r *InMemoryWorkingTreeRepository) WithTrackedFiles(files map[string]string) *InMemoryWorkingTreeRepository {
	for path, content := range files {
		r.Files[path] = content
		r.Tracked[path] = true
	}
	return r
}

// WithTrackedLink adds a committed symlink naming target.
func (r *InMemoryWorkingTreeRepository) WithTrackedLink(path, target string) *InMemoryWorkingTreeRepository {
	r.Links[path] = target
	r.Tracked[path] = true
	return r
}

// WithUntrackedFile adds a file that is not in the index.
func (r *InMemoryWorkingTreeRepository) WithUntrackedFile(path, content string) *InMemoryWorkingTreeRepository {
	r.Files[path] = content
	return r
}

// WithIgnored makes the ignore rules match paths; one ending in "/" matches
// everything below that directory.
func (r *InMemoryWorkingTreeRepository) WithIgnored(paths ...string) *InMemoryWorkingTreeRepository {
	r.Ignored = append(r.Ignored, paths...)
	return r
}

// WithMode records an index mode for a tracked path, such as a submodule.
func (r *InMemoryWorkingTreeRepository) WithMode(path, mode string) *InMemoryWorkingTreeRepository {
	r.Modes[path] = mode
	return r
}

// WithUnmerged marks a tracked path as being in a merge conflict.
func (r *InMemoryWorkingTreeRepository) WithUnmerged(path string) *InMemoryWorkingTreeRepository {
	r.Unmerged[path] = true
	return r
}

// FindIndexEntries lists the tracked paths in byte order.
func (r *InMemoryWorkingTreeRepository) FindIndexEntries(
	_ context.Context,
	_ string,
) ([]entities.IndexEntry, error) {
	if r.ErrorOnIndex != nil {
		return nil, r.ErrorOnIndex
	}
	entries := []entities.IndexEntry{}
	for _, path := range r.sortedTracked() {
		mode, found := r.Modes[path]
		if !found {
			mode = "100644"
			if _, isLink := r.Links[path]; isLink {
				mode = symlinkMode
			}
		}
		stage := 0
		if r.Unmerged[path] {
			stage = conflictStage
		}
		entries = append(entries, entities.IndexEntry{Mode: mode, Stage: stage, Path: path})
	}
	return entries, nil
}

// FindFiles lists the tracked paths and the untracked ones not ignored.
func (r *InMemoryWorkingTreeRepository) FindFiles(_ context.Context, _ string) ([]string, error) {
	if r.ErrorOnFiles != nil {
		return nil, r.ErrorOnFiles
	}
	files := r.sortedTracked()
	for _, path := range r.sortedPresent() {
		if !r.Tracked[path] && !r.ignored(path) {
			files = append(files, path)
		}
	}
	slices.Sort(files)
	return files, nil
}

// FindKind classifies path; a path with files below it is a directory.
func (r *InMemoryWorkingTreeRepository) FindKind(_ context.Context, _, path string) (entities.PathKind, error) {
	if _, isLink := r.Links[path]; isLink {
		return entities.PathSymlink, nil
	}
	if _, isFile := r.Files[path]; isFile {
		return entities.PathFile, nil
	}
	for _, present := range r.sortedPresent() {
		if strings.HasPrefix(present, path+"/") {
			return entities.PathDirectory, nil
		}
	}
	return entities.PathAbsent, nil
}

// FindLinkTarget returns the target of a symlink.
func (r *InMemoryWorkingTreeRepository) FindLinkTarget(_ context.Context, _, path string) (string, error) {
	target, found := r.Links[path]
	if !found {
		return "", fmt.Errorf("%s is not a symlink", path)
	}
	return target, nil
}

// FindContent returns the content of a file.
func (r *InMemoryWorkingTreeRepository) FindContent(_ context.Context, _, path string) ([]byte, error) {
	content, found := r.Files[path]
	if !found {
		return nil, fmt.Errorf("%s does not exist", path)
	}
	return []byte(content), nil
}

// SaveContent writes a file, replacing whatever was at the path.
func (r *InMemoryWorkingTreeRepository) SaveContent(_ context.Context, _, path string, content []byte) error {
	delete(r.Links, path)
	r.Files[path] = string(content)
	return nil
}

// Delete removes a file or symlink, leaving the index alone.
func (r *InMemoryWorkingTreeRepository) Delete(_ context.Context, _, path string) error {
	delete(r.Files, path)
	delete(r.Links, path)
	return nil
}

// IsIgnored reports whether an ignore rule matches path.
func (r *InMemoryWorkingTreeRepository) IsIgnored(_ context.Context, _, path string) (bool, error) {
	return r.ignored(path), nil
}

// Move renames a tracked file or symlink.
func (r *InMemoryWorkingTreeRepository) Move(_ context.Context, _, from, to string) error {
	if !r.Tracked[from] {
		return fmt.Errorf("%s is not tracked", from)
	}
	if content, isFile := r.Files[from]; isFile {
		r.Files[to] = content
	}
	if target, isLink := r.Links[from]; isLink {
		r.Links[to] = target
	}
	delete(r.Files, from)
	delete(r.Links, from)
	delete(r.Tracked, from)
	r.Tracked[to] = true
	return nil
}

// Untrack removes a tracked file from the index and from disk.
func (r *InMemoryWorkingTreeRepository) Untrack(_ context.Context, _, path string) error {
	if !r.Tracked[path] {
		return fmt.Errorf("%s is not tracked", path)
	}
	delete(r.Files, path)
	delete(r.Links, path)
	delete(r.Tracked, path)
	return nil
}

// Stage records paths and tracks the ones that exist, refusing an ignored
// path that is not tracked yet, as Git does.
func (r *InMemoryWorkingTreeRepository) Stage(_ context.Context, _ string, paths []string) error {
	if r.ErrorOnStage != nil {
		return r.ErrorOnStage
	}
	for _, path := range paths {
		if r.ignored(path) && !r.Tracked[path] {
			return fmt.Errorf("%s is ignored", path)
		}
		_, isFile := r.Files[path]
		_, isLink := r.Links[path]
		r.Tracked[path] = isFile || isLink
	}
	r.Staged = append(r.Staged, paths...)
	return nil
}

// Commit records message.
func (r *InMemoryWorkingTreeRepository) Commit(_ context.Context, _, message string) error {
	if r.ErrorOnCommit != nil {
		return r.ErrorOnCommit
	}
	r.Commits = append(r.Commits, message)
	return nil
}

// ignored reports whether an ignore rule matches path.
func (r *InMemoryWorkingTreeRepository) ignored(path string) bool {
	for _, rule := range r.Ignored {
		if path == rule || (strings.HasSuffix(rule, "/") && strings.HasPrefix(path, rule)) {
			return true
		}
	}
	return false
}

// sortedTracked lists the tracked paths in byte order.
func (r *InMemoryWorkingTreeRepository) sortedTracked() []string {
	tracked := []string{}
	for path, isTracked := range r.Tracked {
		if isTracked {
			tracked = append(tracked, path)
		}
	}
	slices.Sort(tracked)
	return tracked
}

// sortedPresent lists every file and symlink on disk in byte order.
func (r *InMemoryWorkingTreeRepository) sortedPresent() []string {
	present := []string{}
	for path := range r.Files {
		present = append(present, path)
	}
	for path := range r.Links {
		present = append(present, path)
	}
	slices.Sort(present)
	return present
}
