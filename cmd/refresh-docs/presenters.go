package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

const (
	// stopTokenBytes sizes the random token that ends a stop-commands block.
	stopTokenBytes = 16
	// dialectGitHubActions, dialectAzurePipelines and dialectPlain name the
	// output dialects `--ci` accepts.
	dialectGitHubActions  = "github-actions"
	dialectAzurePipelines = "azure-pipelines"
	dialectPlain          = "plain"
	// dialectAuto detects the dialect from the environment.
	dialectAuto = "auto"
)

// output is one value a step hands to the jobs that follow it.
type output struct {
	name  string
	value string
}

// presenter renders a refresh in one CI's dialect. Untrusted text is
// repository-controlled and must never act as a CI command; a warning or an
// error becomes an annotation.
type presenter interface {
	Notice(message string)
	UntrustedNotice(message string)
	Warning(message string)
	Error(message string)
	AgentStarted()
	AgentOutput() io.Writer
	AgentFinished()
	Outputs(values []output) error
	Finish(summary entities.RefreshSummary) int
}

// presenters maps each dialect to its constructor.
func presenters() map[string]func(stdout io.Writer, getenv func(string) string) presenter {
	return map[string]func(io.Writer, func(string) string) presenter{
		dialectGitHubActions: func(stdout io.Writer, getenv func(string) string) presenter {
			return &githubActionsPresenter{stdout: stdout, outputFile: getenv("GITHUB_OUTPUT")}
		},
		dialectAzurePipelines: func(stdout io.Writer, _ func(string) string) presenter {
			return &azurePipelinesPresenter{stdout: stdout, agent: &defusingWriter{target: stdout}}
		},
		dialectPlain: func(stdout io.Writer, _ func(string) string) presenter {
			return &plainPresenter{stdout: stdout}
		},
	}
}

// newPresenter returns the presenter of dialect, detecting it from the
// environment when it is auto.
func newPresenter(dialect string, stdout io.Writer, getenv func(string) string) (presenter, error) {
	if dialect == dialectAuto {
		dialect = detectDialect(getenv)
	}
	constructor, found := presenters()[dialect]
	if !found {
		return nil, fmt.Errorf("unknown --ci dialect %q (use %s, %s, %s, or %s)",
			dialect, dialectAuto, dialectGitHubActions, dialectAzurePipelines, dialectPlain)
	}
	return constructor(stdout, getenv), nil
}

// detectDialect reads the variables each CI sets on its runners.
func detectDialect(getenv func(string) string) string {
	if getenv("GITHUB_ACTIONS") == "true" {
		return dialectGitHubActions
	}
	if strings.EqualFold(getenv("TF_BUILD"), "true") {
		return dialectAzurePipelines
	}
	return dialectPlain
}

// githubActionsPresenter writes GitHub Actions workflow commands. The runner
// acts on a command anywhere in a line of output, so untrusted text runs
// between `::stop-commands::` and its random resume token, and annotations
// escape their data and defuse the legacy `##[` form.
type githubActionsPresenter struct {
	stdout     io.Writer
	outputFile string
	agentToken string
}

func (p *githubActionsPresenter) Notice(message string) {
	writeLine(p.stdout, message)
}

func (p *githubActionsPresenter) UntrustedNotice(message string) {
	token := stopToken()
	writeLine(p.stdout, "::stop-commands::"+token)
	writeLine(p.stdout, message)
	writeLine(p.stdout, "::"+token+"::")
}

func (p *githubActionsPresenter) Warning(message string) {
	writeLine(p.stdout, "::warning::"+githubEscape(message))
}

func (p *githubActionsPresenter) Error(message string) {
	writeLine(p.stdout, "::error::"+githubEscape(message))
}

func (p *githubActionsPresenter) AgentStarted() {
	p.agentToken = stopToken()
	writeLine(p.stdout, "::stop-commands::"+p.agentToken)
}

func (p *githubActionsPresenter) AgentOutput() io.Writer {
	return p.stdout
}

func (p *githubActionsPresenter) AgentFinished() {
	writeLine(p.stdout, "::"+p.agentToken+"::")
}

// Outputs appends name=value lines to the step's output file.
func (p *githubActionsPresenter) Outputs(values []output) error {
	if p.outputFile == "" {
		return writePlainOutputs(p.stdout, values)
	}
	file, err := os.OpenFile(p.outputFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening GITHUB_OUTPUT: %w", err)
	}
	written := writePlainOutputs(file, values)
	if closed := file.Close(); closed != nil && written == nil {
		return fmt.Errorf("closing GITHUB_OUTPUT: %w", closed)
	}
	return written
}

func (p *githubActionsPresenter) Finish(summary entities.RefreshSummary) int {
	return writeSummary(p.stdout, summary)
}

// azurePipelinesPresenter writes Azure Pipelines logging commands. The agent
// runs a `##vso[` command found anywhere in a line, so every line of untrusted
// text has it defused, the streamed agent output included.
type azurePipelinesPresenter struct {
	stdout io.Writer
	agent  *defusingWriter
}

func (p *azurePipelinesPresenter) Notice(message string) {
	writeLine(p.stdout, defuseAzure(message))
}

func (p *azurePipelinesPresenter) UntrustedNotice(message string) {
	writeLine(p.stdout, defuseAzure(message))
}

func (p *azurePipelinesPresenter) Warning(message string) {
	writeLine(p.stdout, "##vso[task.logissue type=warning]"+azureEscape(defuseAzure(message)))
}

func (p *azurePipelinesPresenter) Error(message string) {
	writeLine(p.stdout, "##vso[task.logissue type=error]"+azureEscape(defuseAzure(message)))
}

func (p *azurePipelinesPresenter) AgentStarted() {}

func (p *azurePipelinesPresenter) AgentOutput() io.Writer {
	return p.agent
}

func (p *azurePipelinesPresenter) AgentFinished() {
	p.agent.Flush()
}

// Outputs sets output variables a later job reads as dependencies.
func (p *azurePipelinesPresenter) Outputs(values []output) error {
	for _, value := range values {
		writeLine(p.stdout, "##vso[task.setvariable variable="+value.name+";isoutput=true]"+azureEscape(value.value))
	}
	return nil
}

// Finish prints the summary. A batch whose only trouble is a safeguard refusal
// ends as SucceededWithIssues: visible in the run, without a red leg every
// week over a repository nothing here can fix.
func (p *azurePipelinesPresenter) Finish(summary entities.RefreshSummary) int {
	code := writeSummary(p.stdout, summary)
	if code == 0 && len(summary.SafeguardSkipped) > 0 {
		writeLine(p.stdout, "##vso[task.complete result=SucceededWithIssues;]")
	}
	return code
}

// plainPresenter writes plain text, for a run outside any CI.
type plainPresenter struct {
	stdout io.Writer
}

func (p *plainPresenter) Notice(message string)          { writeLine(p.stdout, message) }
func (p *plainPresenter) UntrustedNotice(message string) { writeLine(p.stdout, message) }
func (p *plainPresenter) Warning(message string)         { writeLine(p.stdout, "warning: "+message) }
func (p *plainPresenter) Error(message string)           { writeLine(p.stdout, "error: "+message) }
func (p *plainPresenter) AgentStarted()                  {}
func (p *plainPresenter) AgentOutput() io.Writer         { return p.stdout }
func (p *plainPresenter) AgentFinished()                 {}

func (p *plainPresenter) Outputs(values []output) error {
	return writePlainOutputs(p.stdout, values)
}

func (p *plainPresenter) Finish(summary entities.RefreshSummary) int {
	return writeSummary(p.stdout, summary)
}

// defusingWriter defuses `##vso[` in each complete line it passes on, holding
// a partial line until its end arrives, so a command split across writes is
// still caught.
type defusingWriter struct {
	target  io.Writer
	pending bytes.Buffer
}

func (w *defusingWriter) Write(data []byte) (int, error) {
	w.pending.Write(data)
	for {
		end := bytes.IndexByte(w.pending.Bytes(), '\n')
		if end < 0 {
			return len(data), nil
		}
		line := string(w.pending.Next(end + 1))
		if _, err := io.WriteString(w.target, defuseAzure(line)); err != nil {
			return len(data), err
		}
	}
}

// Flush passes on a final line that never ended.
func (w *defusingWriter) Flush() {
	if w.pending.Len() > 0 {
		_, _ = io.WriteString(w.target, defuseAzure(w.pending.String())+"\n")
		w.pending.Reset()
	}
}

// defuseAzure breaks every `##vso[` in text, whatever its case.
func defuseAzure(text string) string {
	return regexp.MustCompile(`(?i)##vso\[`).ReplaceAllStringFunc(text, func(match string) string {
		return match[:2] + "_" + match[2:]
	})
}

// githubEscape escapes annotation data and defuses the legacy command form.
func githubEscape(text string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", "##[", "##_[").Replace(text)
}

// azureEscape escapes the data of a logging command.
func azureEscape(text string) string {
	return strings.NewReplacer("%", "%AZP25", "\r", "%0D", "\n", "%0A").Replace(text)
}

// stopToken returns a fresh random token for one stop-commands block.
func stopToken() string {
	random := make([]byte, stopTokenBytes)
	_, _ = rand.Read(random)
	return hex.EncodeToString(random)
}

// writeLine writes one line of output.
func writeLine(stdout io.Writer, line string) {
	_, _ = fmt.Fprintln(stdout, line)
}

// writePlainOutputs writes name=value lines.
func writePlainOutputs(target io.Writer, values []output) error {
	for _, value := range values {
		if _, err := fmt.Fprintf(target, "%s=%s\n", value.name, value.value); err != nil {
			return fmt.Errorf("writing output %s: %w", value.name, err)
		}
	}
	return nil
}

// writeSummary prints the batch summary and returns the exit code: failure
// only when a repository failed.
func writeSummary(stdout io.Writer, summary entities.RefreshSummary) int {
	writeLine(stdout, "")
	for _, line := range summary.Lines() {
		writeLine(stdout, line)
	}
	if summary.HasFailures() {
		return exitFailure
	}
	return 0
}
