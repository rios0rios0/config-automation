// Command refresh-docs supplies the config-and-docs refresh with what it
// needs from each cloned repository, as JSON for the workflow's jq:
//
//	refresh-docs layout <repository>  moves the repository into the AGENTS.md layout
//	refresh-docs scope <repository>   prints the manifest of paths the refresh may touch
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/config-automation/internal/domain/commands"
	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

const (
	exitFailure    = 1
	exitUsageError = 2
	argumentCount  = 2
	usage          = "usage: refresh-docs layout|scope <repository>"
	fieldCommand   = "subcommand"
)

// subcommand produces one JSON document for the repository at root.
type subcommand func(ctx context.Context, set commandSet, root string) (any, error)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one subcommand and returns the process exit code: 0 with the
// JSON document on stdout, 1 when the subcommand fails, 2 on a usage error.
// Diagnostics go to stderr.
func run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	diagnostics := logger.New()
	diagnostics.SetOutput(stderr)
	if len(arguments) != argumentCount {
		diagnostics.Error(usage)
		return exitUsageError
	}
	produce, found := subcommands()[arguments[0]]
	if !found {
		diagnostics.Error(usage)
		return exitUsageError
	}
	failure := diagnostics.WithField(fieldCommand, arguments[0])
	root, err := repositoryRoot(arguments[1])
	if err != nil {
		failure.Error(err)
		return exitFailure
	}
	document, err := produce(ctx, initializeCommands(), root)
	if err != nil {
		failure.Error(err)
		return exitFailure
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	if err = encoder.Encode(document); err != nil {
		failure.Error(err)
		return exitFailure
	}
	return 0
}

// subcommands maps each subcommand name to what it produces.
func subcommands() map[string]subcommand {
	return map[string]subcommand{
		"layout": normalizeLayout,
		"scope":  discoverScope,
	}
}

// normalizeLayout runs the AGENTS.md layout pass and returns its report.
func normalizeLayout(ctx context.Context, set commandSet, root string) (any, error) {
	var report entities.AgentsLayoutReport
	var failure error
	set.NormalizeLayout.Execute(
		ctx,
		commands.NormalizeAgentsLayoutInput{Root: root},
		commands.NormalizeAgentsLayoutListeners{
			OnSuccess: func(result entities.AgentsLayoutReport) { report = result },
			OnError:   func(err error) { failure = err },
		},
	)
	return mapToAgentsLayoutResponse(report), failure
}

// discoverScope returns the refresh manifest of the repository.
func discoverScope(ctx context.Context, set commandSet, root string) (any, error) {
	var scope entities.RefreshScope
	var failure error
	set.DiscoverScope.Execute(
		ctx,
		commands.DiscoverRefreshScopeInput{Root: root},
		commands.DiscoverRefreshScopeListeners{
			OnSuccess: func(result entities.RefreshScope) { scope = result },
			OnError:   func(err error) { failure = err },
		},
	)
	return mapToRefreshScopeResponse(scope), failure
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
