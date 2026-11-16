package repositories

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

const (
	// credentialHostVariable, credentialUserVariable and
	// credentialTokenVariable carry one credential to one git process. They
	// are set on that child only, so the token is never in a URL, on a command
	// line, or in the clone's configuration.
	credentialHostVariable  = "CONFIG_AUTOMATION_GIT_HOST"     // #nosec G101 -- a variable's name, not a credential
	credentialUserVariable  = "CONFIG_AUTOMATION_GIT_USERNAME" // #nosec G101 -- a variable's name, not a credential
	credentialTokenVariable = "CONFIG_AUTOMATION_GIT_TOKEN"    // #nosec G101 -- a variable's name, not a credential
)

// GitClonesRepository implements repositories.ClonesRepository with the git
// CLI. git asks the helper below for a credential only when the host requests
// one, and the helper answers for the expected host alone, so a redirect can
// never carry the token somewhere else. The empty `credential.helper=` first
// clears every helper configured on the machine, so none is asked and none
// stores the token.
type GitClonesRepository struct{}

// Ensure the port is satisfied at compile time.
var _ repositories.ClonesRepository = (*GitClonesRepository)(nil)

// NewGitClonesRepository is the constructor the composition root calls.
func NewGitClonesRepository() *GitClonesRepository {
	return &GitClonesRepository{}
}

// Clone copies the repository at url into directory.
func (r *GitClonesRepository) Clone(
	ctx context.Context,
	url, directory string,
	credential entities.GitCredential,
) error {
	return r.authenticated(ctx, "", credential, "clone", "--quiet", "--", url, directory)
}

// Push force-pushes branch to origin and tracks it there.
func (r *GitClonesRepository) Push(
	ctx context.Context,
	root, branch string,
	credential entities.GitCredential,
) error {
	return r.authenticated(ctx, root, credential, "push", "--quiet", "--force", "--set-upstream", "origin", branch)
}

// Discard removes a scratch clone; an absent one is nothing to remove.
func (r *GitClonesRepository) Discard(_ context.Context, directory string) error {
	if err := os.RemoveAll(directory); err != nil {
		return fmt.Errorf("removing %s: %w", directory, err)
	}
	return nil
}

// authenticated runs git with the credential helper and the credential in the
// child's environment, optionally in root.
func (r *GitClonesRepository) authenticated(
	ctx context.Context,
	root string,
	credential entities.GitCredential,
	arguments ...string,
) error {
	vector := CredentialHelperArguments()
	if root != "" {
		vector = append([]string{"-C", root}, vector...)
	}
	vector = append(vector, arguments...)
	command := exec.CommandContext(ctx, "git", vector...)
	command.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), CredentialEnvironment(credential)...)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Run(); err != nil {
		return fmt.Errorf("git %s: %w: %s", arguments[0], err, strings.TrimSpace(output.String()))
	}
	return nil
}

// CredentialHelperArguments returns the `-c` options that clear the machine's
// credential helpers and install one that answers from the environment, for
// the expected host only. The snippet is passed as written: the shell git
// starts for it expands the variables, where `printf` is a builtin.
func CredentialHelperArguments() []string {
	helper := `!f() { test "$1" = get || exit 0; host=; ` +
		`while IFS= read -r line; do case "$line" in host=*) host="${line#host=}";; esac; done; ` +
		`test -n "$host" && test "$host" = "$` + credentialHostVariable + `" || exit 0; ` +
		`printf 'username=%s\npassword=%s\n' "$` + credentialUserVariable + `" "$` +
		credentialTokenVariable + `"; }; f`
	return []string{"-c", "credential.helper=", "-c", "credential.helper=" + helper}
}

// CredentialEnvironment returns the variables that hand credential to the
// helper of CredentialHelperArguments.
func CredentialEnvironment(credential entities.GitCredential) []string {
	return []string{
		credentialHostVariable + "=" + credential.Host,
		credentialUserVariable + "=" + credential.Username,
		credentialTokenVariable + "=" + credential.Token,
	}
}
