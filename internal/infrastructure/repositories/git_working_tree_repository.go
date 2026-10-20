package repositories

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

const (
	// indexFields counts the mode, object name, and stage before the tab of
	// an `ls-files --stage` record.
	indexFields = 3
	// indexStageField is the position of the merge stage in those fields.
	indexStageField = 2
	// notIgnoredExitCode is how `git check-ignore -q` says no rule matched.
	notIgnoredExitCode = 1
	// createdFileMode applies only to a file the layout creates; Git
	// records a non-executable file as 100644 whatever its mode on disk.
	createdFileMode = 0o600
)

// GitWorkingTreeRepository implements repositories.WorkingTreeRepository
// with the git CLI and the local filesystem.
type GitWorkingTreeRepository struct{}

// NewGitWorkingTreeRepository is the Dig-injectable constructor.
func NewGitWorkingTreeRepository() *GitWorkingTreeRepository {
	return &GitWorkingTreeRepository{}
}

// FindIndexEntries parses `git ls-files --stage -z`.
func (r *GitWorkingTreeRepository) FindIndexEntries(ctx context.Context, root string) ([]entities.IndexEntry, error) {
	output, err := r.git(ctx, root, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, err
	}
	entries := []entities.IndexEntry{}
	for record := range strings.SplitSeq(output, "\x00") {
		if record == "" {
			continue
		}
		metadata, file, found := strings.Cut(record, "\t")
		fields := strings.Fields(metadata)
		if !found || len(fields) != indexFields {
			return nil, fmt.Errorf("unexpected index record %q", record)
		}
		stage, parseErr := strconv.Atoi(fields[indexStageField])
		if parseErr != nil {
			return nil, fmt.Errorf("unexpected index stage in %q: %w", record, parseErr)
		}
		entries = append(entries, entities.IndexEntry{Mode: fields[0], Stage: stage, Path: file})
	}
	return entries, nil
}

// FindFiles lists the tracked files and the untracked ones the standard
// ignore rules do not match.
func (r *GitWorkingTreeRepository) FindFiles(ctx context.Context, root string) ([]string, error) {
	output, err := r.git(ctx, root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	files := []string{}
	for file := range strings.SplitSeq(output, "\x00") {
		if file != "" {
			files = append(files, file)
		}
	}
	return files, nil
}

// FindKind classifies path with lstat, so a symlink is never followed. A
// path below a file does not exist.
func (r *GitWorkingTreeRepository) FindKind(_ context.Context, root, path string) (entities.PathKind, error) {
	info, err := os.Lstat(r.location(root, path))
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return entities.PathAbsent, nil
	}
	if err != nil {
		return entities.PathOther, fmt.Errorf("inspecting %s: %w", path, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return entities.PathSymlink, nil
	}
	if info.IsDir() {
		return entities.PathDirectory, nil
	}
	if info.Mode().IsRegular() {
		return entities.PathFile, nil
	}
	return entities.PathOther, nil
}

// FindLinkTarget returns the target of a symlink as written.
func (r *GitWorkingTreeRepository) FindLinkTarget(_ context.Context, root, path string) (string, error) {
	target, err := os.Readlink(r.location(root, path))
	if err != nil {
		return "", fmt.Errorf("reading the %s symlink: %w", path, err)
	}
	return target, nil
}

// FindContent reads a file of the working tree.
func (r *GitWorkingTreeRepository) FindContent(_ context.Context, root, path string) ([]byte, error) {
	content, err := os.ReadFile(r.location(root, path))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return content, nil
}

// SaveContent writes a file of the working tree, keeping the mode of an
// existing one.
func (r *GitWorkingTreeRepository) SaveContent(_ context.Context, root, path string, content []byte) error {
	if err := os.WriteFile(r.location(root, path), content, createdFileMode); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// Delete removes a file or symlink from disk.
func (r *GitWorkingTreeRepository) Delete(_ context.Context, root, path string) error {
	if err := os.Remove(r.location(root, path)); err != nil {
		return fmt.Errorf("removing %s: %w", path, err)
	}
	return nil
}

// IsIgnored asks `git check-ignore --no-index`, which matches the ignore
// rules even against a tracked path.
func (r *GitWorkingTreeRepository) IsIgnored(ctx context.Context, root, path string) (bool, error) {
	_, err := r.git(ctx, root, "check-ignore", "--no-index", "-q", "--", path)
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == notIgnoredExitCode {
		return false, nil
	}
	return false, err
}

// Move renames a tracked file with `git mv`.
func (r *GitWorkingTreeRepository) Move(ctx context.Context, root, from, to string) error {
	_, err := r.git(ctx, root, "mv", "--", from, to)
	return err
}

// Untrack removes a tracked file with `git rm`.
func (r *GitWorkingTreeRepository) Untrack(ctx context.Context, root, path string) error {
	_, err := r.git(ctx, root, "rm", "-q", "--", path)
	return err
}

// Stage adds paths to the index with `git add`.
func (r *GitWorkingTreeRepository) Stage(ctx context.Context, root string, paths []string) error {
	_, err := r.git(ctx, root, append([]string{"add", "--"}, paths...)...)
	return err
}

// Commit records the index with `git commit`.
func (r *GitWorkingTreeRepository) Commit(ctx context.Context, root, message string) error {
	_, err := r.git(ctx, root, "commit", "-q", "-m", message)
	return err
}

// git runs git in root and returns its standard output, or an error that
// carries its standard error.
func (r *GitWorkingTreeRepository) git(ctx context.Context, root string, arguments ...string) (string, error) {
	vector := append([]string{"-C", root}, arguments...)
	command := exec.CommandContext(ctx, "git", vector...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(output), nil
}

// location turns a slash-separated repository path into a filesystem one.
func (r *GitWorkingTreeRepository) location(root, path string) string {
	return filepath.Join(root, filepath.FromSlash(path))
}
