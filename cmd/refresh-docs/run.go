package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/rios0rios0/config-automation/internal/domain/commands"
	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

// errNoToken reports a run started without the organization's token, which
// could clone nothing private and open no pull request.
var errNoToken = errors.New("no token")

// tokenVariables are the variables that hold a hosting token, whichever
// provider a run is for. None of them reaches the agent.
func tokenVariables() []string {
	return []string{"GH_TOKEN", "GITHUB_TOKEN", "AZURE_DEVOPS_PAT", "AZURE_DEVOPS_EXT_PAT", "SYSTEM_ACCESSTOKEN"}
}

// defaultTokenVariables maps each provider to the variable its token is read
// from when `--token-env` is absent. There is no fallback chain: a token read
// from an unexpected variable could be a narrower one, such as a pipeline's
// own project-scoped token, which fails later and more confusingly.
func defaultTokenVariables() map[entities.Provider]string {
	return map[entities.Provider]string{
		entities.ProviderGitHub:      "GH_TOKEN",
		entities.ProviderAzureDevOps: "AZURE_DEVOPS_PAT",
	}
}

// runOptions are the flags of `refresh-docs run`.
type runOptions struct {
	config    string
	batch     string
	tokenEnv  string
	model     string
	effort    string
	maxTurns  int
	workDir   string
	dialect   string
	overrides instructionFlags
}

// refreshBatch refreshes the repositories of one matrix leg. The batch is
// the `--batch` JSON or, when absent, the REPOS variable the CI sets from the
// matrix.
func refreshBatch(ctx context.Context, arguments []string, output streams, wiring dependencies) int {
	options, err := parseRunOptions(arguments, wiring.getenv)
	if err != nil {
		return usageFailure(output, "run", err)
	}
	loaded, err := loadConfiguration(options.config)
	if err != nil {
		return failure(output, "run", err)
	}
	provider, _ := loaded.provider()
	files, err := options.overrides.apply(loaded.instructionFiles(provider))
	if err != nil {
		return usageFailure(output, "run", err)
	}
	targets, err := parseBatch(options.batch)
	if err != nil {
		return usageFailure(output, "run", err)
	}
	printer, err := newPresenter(options.dialect, output.stdout, wiring.getenv)
	if err != nil {
		return usageFailure(output, "run", err)
	}
	tokenEnv := valueOr(options.tokenEnv, defaultTokenVariables()[provider])
	token := wiring.getenv(tokenEnv)
	if token == "" {
		return failure(output, "run", fmt.Errorf("%w: %s is empty or not set", errNoToken, tokenEnv))
	}
	workDirectory, cleanup, err := scratchDirectory(options.workDir)
	if err != nil {
		return failure(output, "run", err)
	}
	defer cleanup()
	command := newRefreshCommand(wiring, provider, token, loaded.identity(),
		printer.AgentOutput(), append(tokenVariables(), tokenEnv))
	code := 0
	command.Execute(ctx, commands.RefreshRepositoriesInput{
		Targets: targets, WorkDirectory: workDirectory, Token: token,
		Settings: commands.RefreshSettings{
			Provider: provider, InstructionFiles: files, GuideURL: loaded.guideURL(),
			Branch: loaded.branch(), CommitMessage: defaultCommitMessage,
			PullRequestTitle: loaded.pullRequestTitle(), PullRequestBody: loaded.pullRequestBody(files),
			Model: options.model, Effort: options.effort, MaxTurns: options.maxTurns,
		},
	}, refreshListeners(printer, &code))
	return code
}

// refreshListeners renders the batch's progress through the presenter and
// records the exit code.
func refreshListeners(printer presenter, code *int) commands.RefreshRepositoriesListeners {
	return commands.RefreshRepositoriesListeners{
		OnStarted: func(index, total int, target string) {
			printer.Notice(fmt.Sprintf("=========== [%d/%d] %s ===========", index, total, target))
		},
		OnQuotaSkipped: func(index, total int, target string) {
			printer.Notice(fmt.Sprintf(
				"=========== [%d/%d] %s (SKIPPED: Claude usage limit hit earlier in batch) ===========",
				index, total, target))
		},
		OnNotice:          printer.Notice,
		OnUntrustedNotice: printer.UntrustedNotice,
		OnWarning:         printer.Warning,
		OnAgentStarted:    printer.AgentStarted,
		OnAgentFinished:   printer.AgentFinished,
		OnFinished:        func(summary entities.RefreshSummary) { *code = printer.Finish(summary) },
		OnError: func(err error) {
			printer.Error(err.Error())
			*code = exitFailure
		},
	}
}

// parseRunOptions reads the flags of `run`, defaulting the model, the effort,
// and the turn limit from the environment the CI sets.
func parseRunOptions(arguments []string, getenv func(string) string) (runOptions, error) {
	options := runOptions{}
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	maxTurns, err := strconv.Atoi(environmentOr(getenv, "MAX_TURNS", strconv.Itoa(defaultMaxTurns)))
	if err != nil {
		return options, fmt.Errorf("MAX_TURNS must be a whole number: %w", err)
	}
	flags.StringVar(&options.config, "config", "", "configuration file")
	flags.StringVar(&options.batch, "batch", getenv("REPOS"), "the batch's repositories as JSON")
	flags.StringVar(&options.tokenEnv, "token-env", "", "variable holding the organization's token")
	flags.StringVar(&options.model, "model", environmentOr(getenv, "CLAUDE_MODEL", defaultModel), "Claude model")
	flags.StringVar(&options.effort, "effort", environmentOr(getenv, "CLAUDE_EFFORT", defaultEffort), "effort")
	flags.IntVar(&options.maxTurns, "max-turns", maxTurns, "turn limit of each Claude phase")
	flags.StringVar(&options.workDir, "work-dir", "", "directory for the scratch clones")
	flags.StringVar(&options.dialect, "ci", dialectAuto, "output dialect")
	options.overrides.register(flags)
	if err = flags.Parse(arguments); err != nil {
		return options, err
	}
	if options.config == "" || flags.NArg() != 0 {
		return options, errUsage
	}
	if options.maxTurns < 1 {
		return options, fmt.Errorf("--max-turns must be positive, not %d", options.maxTurns)
	}
	return options, nil
}

// parseBatch reads the batch's repositories.
func parseBatch(batch string) ([]entities.RefreshTarget, error) {
	if batch == "" {
		return nil, fmt.Errorf("no batch: pass --batch or set REPOS: %w", errUsage)
	}
	var responses []refreshTargetResponse
	if err := json.Unmarshal([]byte(batch), &responses); err != nil {
		return nil, fmt.Errorf("reading the batch: %w", err)
	}
	return mapToRefreshTargets(responses), nil
}

// scratchDirectory returns the directory the clones go in and how to remove
// it. A directory the operator named is kept; a temporary one is removed.
func scratchDirectory(named string) (string, func(), error) {
	if named != "" {
		// A directory needs its execute bit; the rule below is written for files.
		// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
		return named, func() {}, os.MkdirAll(named, 0o700)
	}
	directory, err := os.MkdirTemp("", "refresh-docs-")
	if err != nil {
		return "", nil, fmt.Errorf("creating the scratch directory: %w", err)
	}
	return directory, func() { _ = os.RemoveAll(directory) }, nil
}
