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
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
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
	// differencesExitCode is how `git diff --quiet` says the sides differ;
	// any higher code is an error, never a difference.
	differencesExitCode = 1
	// literalPathspec makes git take a path as written, never as a pattern.
	literalPathspec = ":(literal)"
	// gitDiff is the subcommand every change question asks.
	gitDiff = "diff"
	// noExternalDiff and noTextConversion keep the repository's own
	// `.gitattributes` from replacing a diff with an external driver or a
	// text conversion.
	noExternalDiff   = "--no-ext-diff"
	noTextConversion = "--no-textconv"
)

// GitWorkingTreeRepository implements repositories.WorkingTreeRepository and
// repositories.ChangesRepository with the git CLI and the local filesystem.
// A non-empty identity authors its commits through `-c` options, so nothing
// is written to any git configuration.
type GitWorkingTreeRepository struct {
	identity entities.GitIdentity
}

// Ensure both ports are satisfied at compile time.
var (
	_ repositories.WorkingTreeRepository = (*GitWorkingTreeRepository)(nil)
	_ repositories.ChangesRepository     = (*GitWorkingTreeRepository)(nil)
)

// NewGitWorkingTreeRepository is the constructor the composition root calls.
// The zero identity leaves git's own configuration in charge.
func NewGitWorkingTreeRepository(identity entities.GitIdentity) *GitWorkingTreeRepository {
	return &GitWorkingTreeRepository{identity: identity}
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
	_, err := r.git(ctx, root, append([]string{"add", "--"}, literal(paths...)...)...)
	return err
}

// Commit records the index with `git commit`, as the configured identity.
func (r *GitWorkingTreeRepository) Commit(ctx context.Context, root, message string) error {
	arguments := []string{}
	if r.identity.Name != "" {
		arguments = append(arguments, "-c", "user.name="+r.identity.Name)
	}
	if r.identity.Email != "" {
		arguments = append(arguments, "-c", "user.email="+r.identity.Email)
	}
	_, err := r.git(ctx, root, append(arguments, "commit", "-q", "-m", message)...)
	return err
}

// Checkout switches to an existing branch with `git checkout`.
func (r *GitWorkingTreeRepository) Checkout(ctx context.Context, root, branch string) error {
	_, err := r.git(ctx, root, "checkout", "--quiet", branch, "--")
	return err
}

// CreateBranch starts branch at HEAD with `git checkout -B`.
func (r *GitWorkingTreeRepository) CreateBranch(ctx context.Context, root, branch string) error {
	_, err := r.git(ctx, root, "checkout", "--quiet", "-B", branch)
	return err
}

// MarkIntentToAdd runs `git add --intent-to-add` on paths.
func (r *GitWorkingTreeRepository) MarkIntentToAdd(ctx context.Context, root string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	_, err := r.git(ctx, root, append([]string{"add", "--intent-to-add", "--"}, literal(paths...)...)...)
	return err
}

// HasChanges asks `git diff HEAD --quiet -w` about paths.
func (r *GitWorkingTreeRepository) HasChanges(ctx context.Context, root string, paths []string) (bool, error) {
	return r.differs(ctx, root, append(
		[]string{gitDiff, "HEAD", "--quiet", noExternalDiff, noTextConversion, "-w", "--"}, literal(paths...)...)...)
}

// FindDiff returns `git diff HEAD -U0` of paths, every file read as text,
// so a repository that marks a path `-diff` or `binary` cannot turn its added
// lines into "Binary files differ"; no paths is no diff.
func (r *GitWorkingTreeRepository) FindDiff(ctx context.Context, root string, paths []string) (string, error) {
	if len(paths) == 0 {
		return "", nil
	}
	return r.git(ctx, root, append(
		[]string{gitDiff, "HEAD", "--text", noExternalDiff, noTextConversion, "-U0", "--"}, literal(paths...)...)...)
}

// HasUnstagedChanges asks `git diff --quiet` whether path differs between
// the index and the working tree.
func (r *GitWorkingTreeRepository) HasUnstagedChanges(ctx context.Context, root, path string) (bool, error) {
	return r.differs(ctx, root, append(
		[]string{gitDiff, "--quiet", noExternalDiff, noTextConversion, "--"}, literal(path)...)...)
}

// Restore puts the index version of path back with `git checkout --`.
func (r *GitWorkingTreeRepository) Restore(ctx context.Context, root, path string) error {
	_, err := r.git(ctx, root, append([]string{"checkout", "--"}, literal(path)...)...)
	return err
}

// FindUntracked lists the untracked, unignored files below directory.
func (r *GitWorkingTreeRepository) FindUntracked(ctx context.Context, root, directory string) ([]string, error) {
	output, err := r.git(
		ctx, root, append([]string{"ls-files", "-z", "--others", "--exclude-standard", "--"}, literal(directory)...)...)
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

// HasStagedChanges asks `git diff --cached --quiet` whether the index
// differs from HEAD.
func (r *GitWorkingTreeRepository) HasStagedChanges(ctx context.Context, root string) (bool, error) {
	return r.differs(ctx, root, gitDiff, "--cached", "--quiet")
}

// HasRevision asks `git rev-parse --verify --quiet` whether revision names a
// commit; an exit of 1 means it does not, any other failure is an error.
func (r *GitWorkingTreeRepository) HasRevision(ctx context.Context, root, revision string) (bool, error) {
	_, err := r.git(ctx, root, "rev-parse", "--verify", "--quiet", revision+"^{commit}")
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == differencesExitCode {
		return false, nil
	}
	return false, err
}

// IsAhead counts the commits of HEAD that base lacks.
func (r *GitWorkingTreeRepository) IsAhead(ctx context.Context, root, base string) (bool, error) {
	output, err := r.git(ctx, root, "rev-list", "--count", base+"..HEAD")
	if err != nil {
		return false, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil {
		return false, fmt.Errorf("counting the commits ahead of %s: %w", base, err)
	}
	return count > 0, nil
}

// differs runs a `git diff --quiet` and turns its exit code into an answer.
func (r *GitWorkingTreeRepository) differs(ctx context.Context, root string, arguments ...string) (bool, error) {
	_, err := r.git(ctx, root, arguments...)
	if err == nil {
		return false, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == differencesExitCode {
		return true, nil
	}
	return false, err
}

// literal marks paths as literal pathspecs.
func literal(paths ...string) []string {
	marked := make([]string, 0, len(paths))
	for _, path := range paths {
		marked = append(marked, literalPathspec+path)
	}
	return marked
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
