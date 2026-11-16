package repositories

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// agentWaitDelay bounds how long a finished or cancelled Claude run may keep
// its output open through a process it started, so a lingering child cannot
// hang the batch.
const agentWaitDelay = 10 * time.Second

// errAuditUnsuccessful reports an audit that exited cleanly without a
// successful, non-empty report, which must never feed a refresh.
var errAuditUnsuccessful = errors.New("prompt audit did not return a successful, non-empty report")

// ClaudeCodeAgentRepository implements repositories.AgentRepository with the
// Claude Code CLI in print mode. Both phases skip the target repository's
// settings, hooks, and MCP servers, run under dontAsk so anything the grants
// do not cover is denied, read nothing from stdin, and start without the
// tokens this process holds for the hosting provider: the CLI's own OAuth
// token is the only secret the agent's environment keeps.
type ClaudeCodeAgentRepository struct {
	binary   string
	environ  func() []string
	live     io.Writer
	withheld []string
}

// Ensure the port is satisfied at compile time.
var _ repositories.AgentRepository = (*ClaudeCodeAgentRepository)(nil)

// NewClaudeCodeAgentRepository is the constructor the composition root calls.
// environ supplies the environment the agent starts from, live receives the
// output as it streams, and withheld names the variables the agent must not
// inherit.
func NewClaudeCodeAgentRepository(
	binary string,
	environ func() []string,
	live io.Writer,
	withheld []string,
) *ClaudeCodeAgentRepository {
	return &ClaudeCodeAgentRepository{binary: binary, environ: environ, live: live, withheld: withheld}
}

// Audit runs the read-only prompt audit. Its JSON result, which is the whole
// of stdout, is printed only once read: its report on success, the whole
// result otherwise. A zero exit is not enough, because print mode can return
// an error result, such as a turn limit.
func (r *ClaudeCodeAgentRepository) Audit(ctx context.Context, run entities.AgentRun) (entities.AgentResult, error) {
	arguments := append(r.arguments(run), "--output-format", "json")
	stdout, combined, err := r.execute(ctx, run, arguments, false)
	if err != nil {
		r.print(stdout)
		return entities.AgentResult{Output: combined}, fmt.Errorf("the prompt audit failed: %w", err)
	}
	report, found := successfulReport(stdout)
	if !found {
		r.print(stdout)
		r.print(errAuditUnsuccessful.Error())
		return entities.AgentResult{Output: combined}, errAuditUnsuccessful
	}
	r.print(report)
	return entities.AgentResult{Output: combined, Report: report}, nil
}

// Refresh runs the scoped refresh, streaming its output as it comes.
func (r *ClaudeCodeAgentRepository) Refresh(ctx context.Context, run entities.AgentRun) (entities.AgentResult, error) {
	_, combined, err := r.execute(ctx, run, r.arguments(run), true)
	if err != nil {
		return entities.AgentResult{Output: combined}, fmt.Errorf("the refresh failed: %w", err)
	}
	return entities.AgentResult{Output: combined}, nil
}

// arguments are the flags both phases share.
func (r *ClaudeCodeAgentRepository) arguments(run entities.AgentRun) []string {
	return []string{
		"-p", run.Prompt,
		"--model", run.Model, "--effort", run.Effort, "--max-turns", strconv.Itoa(run.MaxTurns),
		"--setting-sources", "", "--strict-mcp-config",
		"--tools", run.Tools, "--allowedTools", run.AllowedTools,
		"--permission-mode", "dontAsk",
	}
}

// execute runs the CLI in the run's directory. It returns stdout alone and
// the combined output. stderr always streams live, and stdout too when
// streamStdout is set; one lock serializes the two, since each is copied by
// its own goroutine. Cancelling asks the CLI to stop with SIGTERM, and a run
// ended by a signal counts as a failure.
func (r *ClaudeCodeAgentRepository) execute(
	ctx context.Context,
	run entities.AgentRun,
	arguments []string,
	streamStdout bool,
) (string, string, error) {
	// #nosec G204 -- the binary is the composition root's, and each argument is one argv entry, never shell text
	command := exec.CommandContext(ctx, r.binary, arguments...)
	command.Dir = run.Directory
	command.Env = r.environment()
	command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
	command.WaitDelay = agentWaitDelay
	live := &lockedWriter{target: r.live}
	var stdout bytes.Buffer
	combined := &lockedBuffer{}
	stdoutTargets := []io.Writer{&stdout, combined}
	if streamStdout {
		stdoutTargets = append(stdoutTargets, live)
	}
	command.Stdout = io.MultiWriter(stdoutTargets...)
	command.Stderr = io.MultiWriter(combined, live)
	err := command.Run()
	return stdout.String(), combined.String(), err
}

// environment is the configured environment without the withheld variables.
func (r *ClaudeCodeAgentRepository) environment() []string {
	kept := []string{}
	for _, entry := range r.environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !slices.Contains(r.withheld, name) {
			kept = append(kept, entry)
		}
	}
	return kept
}

// print writes text and a final line break to the live output.
func (r *ClaudeCodeAgentRepository) print(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	_, _ = io.WriteString(r.live, strings.TrimRight(text, "\n")+"\n")
}

// successfulReport returns the report of a successful audit result: a result
// message whose subtype is success, that is not an error, and whose text is
// not blank.
func successfulReport(output string) (string, bool) {
	var result struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		IsError *bool  `json:"is_error"`
		Result  any    `json:"result"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return "", false
	}
	report, isText := result.Result.(string)
	if result.Type != "result" || result.Subtype != "success" || result.IsError == nil || *result.IsError ||
		!isText || strings.TrimSpace(report) == "" {
		return "", false
	}
	return report, true
}
