// Command refresh-docs runs the config-and-docs refresh on GitHub and on
// Azure DevOps:
//
//	refresh-docs discover --config <file> [--repo <name>] [--batch-size <n>] [--ci <dialect>]
//	    enumerates the configured organizations into CI matrix batches
//	refresh-docs run --config <file> [--batch <json>] [--token-env <name>] [--ci <dialect>]
//	    refreshes one batch: clone, layout, audit, refresh, guards, pull request
//	refresh-docs layout [flags] <repository>  moves a clone into the instruction-file layout
//	refresh-docs scope [flags] <repository>   prints the manifest of paths a refresh may touch
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/config-automation/internal/domain/commands"
	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

const (
	exitFailure    = 1
	exitUsageError = 2
	usage          = "usage: refresh-docs discover|run --config <file> [flags], " +
		"or refresh-docs layout|scope [flags] <repository>"
	fieldCommand = "subcommand"
	// defaultModel, defaultEffort and defaultMaxTurns apply when neither a
	// flag nor the environment sets them. Opus 5.5 runs at medium effort in
	// Claude Code unless told otherwise.
	defaultModel    = "claude-opus-5-5"
	defaultEffort   = "high"
	defaultMaxTurns = 50
	// defaultBatchSize is how many repositories one matrix leg refreshes.
	defaultBatchSize = 10
)

// errUsage marks a usage error, which exits 2 rather than 1.
var errUsage = errors.New(usage)

// streams are where a subcommand writes.
type streams struct {
	stdout io.Writer
	stderr io.Writer
}

// subcommand runs with its arguments and returns the exit code.
type subcommand func(ctx context.Context, arguments []string, output streams, wiring dependencies) int

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one subcommand with the production wiring and returns the
// process exit code: 0 on success, 1 when the subcommand fails, 2 on a usage
// error. Diagnostics go to stderr.
func run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return execute(ctx, arguments, streams{stdout: stdout, stderr: stderr}, defaultDependencies())
}

// execute dispatches to the named subcommand.
func execute(ctx context.Context, arguments []string, output streams, wiring dependencies) int {
	if len(arguments) == 0 {
		return usageFailure(output, "", errUsage)
	}
	handler, found := subcommands()[arguments[0]]
	if !found {
		return usageFailure(output, "", errUsage)
	}
	return handler(ctx, arguments[1:], output, wiring)
}

// subcommands maps each subcommand name to its handler.
func subcommands() map[string]subcommand {
	return map[string]subcommand{
		"layout":   repositoryDocument("layout", normalizeLayout),
		"scope":    repositoryDocument("scope", discoverScope),
		"discover": discoverTargets,
		"run":      refreshBatch,
	}
}

// usageFailure reports a usage error on stderr.
func usageFailure(output streams, name string, err error) int {
	diagnostics := logger.New()
	diagnostics.SetOutput(output.stderr)
	entry := diagnostics.WithError(err)
	if name != "" {
		entry = entry.WithField(fieldCommand, name)
	}
	entry.Error(usage)
	return exitUsageError
}

// failure reports a failed subcommand on stderr.
func failure(output streams, name string, err error) int {
	diagnostics := logger.New()
	diagnostics.SetOutput(output.stderr)
	diagnostics.WithField(fieldCommand, name).Error(err)
	return exitFailure
}

// instructionFlags are the overrides of the instruction files a refresh
// keeps; an empty value keeps the configured or detected default.
type instructionFlags struct {
	agents  string
	claude  string
	copilot string
}

// register adds the overrides to a flag set.
func (f *instructionFlags) register(flags *flag.FlagSet) {
	flags.StringVar(&f.agents, "agents", "", "true or false: whether Claude may create a root AGENTS.md")
	flags.StringVar(&f.claude, "claude", "", "true or false: whether every CLAUDE.md is the AGENTS.md import")
	flags.StringVar(&f.copilot, "copilot", "", "true or false: whether the repository keeps GitHub Copilot files")
}

// apply overrides files with every flag that was set.
func (f *instructionFlags) apply(files entities.InstructionFiles) (entities.InstructionFiles, error) {
	for _, override := range []struct {
		value  string
		target *bool
	}{{f.agents, &files.Agents}, {f.claude, &files.Claude}, {f.copilot, &files.Copilot}} {
		if override.value == "" {
			continue
		}
		parsed, err := strconv.ParseBool(override.value)
		if err != nil {
			return files, fmt.Errorf("an instruction-file flag must be true or false: %w", err)
		}
		*override.target = parsed
	}
	return files, nil
}

// repositoryDocument adapts a subcommand that prints one JSON document about
// the repository its single argument names.
func repositoryDocument(
	name string,
	produce func(ctx context.Context, set commandSet, root string, files entities.InstructionFiles) (any, error),
) subcommand {
	return func(ctx context.Context, arguments []string, output streams, _ dependencies) int {
		flags := flag.NewFlagSet("refresh-docs", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		provider := flags.String("provider", string(entities.ProviderGitHub), "provider whose defaults apply")
		overrides := &instructionFlags{}
		overrides.register(flags)
		if err := flags.Parse(arguments); err != nil || flags.NArg() != 1 {
			return usageFailure(output, "", errUsage)
		}
		parsed, err := entities.ParseProvider(*provider)
		if err != nil {
			return usageFailure(output, "", err)
		}
		files, err := overrides.apply(parsed.DefaultInstructionFiles())
		if err != nil {
			return usageFailure(output, "", err)
		}
		root, err := repositoryRoot(flags.Arg(0))
		if err != nil {
			return failure(output, name, err)
		}
		document, err := produce(ctx, initializeCommands(), root, files)
		if err != nil {
			return failure(output, name, err)
		}
		return writeDocument(output, document)
	}
}

// writeDocument encodes one JSON document on stdout.
func writeDocument(output streams, document any) int {
	encoder := json.NewEncoder(output.stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(document); err != nil {
		return failure(output, "encode", err)
	}
	return 0
}

// normalizeLayout runs the layout pass and returns its report.
func normalizeLayout(ctx context.Context, set commandSet, root string, files entities.InstructionFiles) (any, error) {
	var report entities.AgentsLayoutReport
	var failed error
	set.NormalizeLayout.Execute(
		ctx,
		commands.NormalizeAgentsLayoutInput{Root: root, InstructionFiles: files},
		commands.NormalizeAgentsLayoutListeners{
			OnSuccess: func(result entities.AgentsLayoutReport) { report = result },
			OnError:   func(err error) { failed = err },
		},
	)
	if failed != nil {
		return nil, fmt.Errorf("layout: %w", failed)
	}
	return mapToAgentsLayoutResponse(report), nil
}

// discoverScope returns the refresh manifest of the repository.
func discoverScope(ctx context.Context, set commandSet, root string, files entities.InstructionFiles) (any, error) {
	var scope entities.RefreshScope
	var failed error
	set.DiscoverScope.Execute(
		ctx,
		commands.DiscoverRefreshScopeInput{Root: root, InstructionFiles: files},
		commands.DiscoverRefreshScopeListeners{
			OnSuccess: func(result entities.RefreshScope) { scope = result },
			OnError:   func(err error) { failed = err },
		},
	)
	if failed != nil {
		return nil, fmt.Errorf("scope: %w", failed)
	}
	return mapToRefreshScopeResponse(scope), nil
}

// repositoryRoot resolves the repository argument to an absolute path
// without symlinks, so every path below it is stable.
func repositoryRoot(argument string) (string, error) {
	absolute, err := filepath.Abs(argument)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", argument, err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", argument, err)
	}
	return resolved, nil
}

// environmentOr returns the variable's value, or fallback when it is unset.
func environmentOr(getenv func(string) string, name, fallback string) string {
	return valueOr(getenv(name), fallback)
}

// parseTargetReference reads a `--repo` value: `owner/name` or a bare
// `name` of the first organization on GitHub, `project/name` or
// `organization/project/name` on Azure DevOps.
func parseTargetReference(provider entities.Provider, value string) (*entities.RefreshTargetReference, error) {
	if value == "" {
		return nil, nil //nolint:nilnil // no reference means the whole fleet, which is not an error
	}
	parts := strings.Split(value, "/")
	layouts := map[entities.Provider]map[int]func([]string) entities.RefreshTargetReference{
		entities.ProviderGitHub: {
			1: func(p []string) entities.RefreshTargetReference { return entities.RefreshTargetReference{Name: p[0]} },
			2: func(p []string) entities.RefreshTargetReference {
				return entities.RefreshTargetReference{Organization: p[0], Name: p[1]}
			},
		},
		entities.ProviderAzureDevOps: {
			2: func(p []string) entities.RefreshTargetReference {
				return entities.RefreshTargetReference{Project: p[0], Name: p[1]}
			},
			3: func(p []string) entities.RefreshTargetReference {
				return entities.RefreshTargetReference{Organization: p[0], Project: p[1], Name: p[2]}
			},
		},
	}
	layout, found := layouts[provider][len(parts)]
	if !found || slices.Contains(parts, "") {
		return nil, fmt.Errorf("cannot read %q as a repository of %s", value, provider)
	}
	reference := layout(parts)
	return &reference, nil
}
