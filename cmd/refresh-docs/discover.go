package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/rios0rios0/config-automation/internal/domain/commands"
	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

// discoverOptions are the flags of `refresh-docs discover`.
type discoverOptions struct {
	config    string
	repo      string
	batchSize int
	dialect   string
}

// discoverTargets enumerates the configured organizations, or resolves one
// named repository, into the matrix the refresh jobs run. An organization it
// cannot reach is reported and the rest are still batched; the run fails only
// when no organization could be enumerated, and the unreachable list is an
// output so a later job can still turn a partial run red.
func discoverTargets(ctx context.Context, arguments []string, output streams, wiring dependencies) int {
	options, err := parseDiscoverOptions(arguments)
	if err != nil {
		return usageFailure(output, "discover", err)
	}
	loaded, err := loadConfiguration(options.config)
	if err != nil {
		return failure(output, "discover", err)
	}
	provider, _ := loaded.provider()
	reference, err := parseTargetReference(provider, options.repo)
	if err != nil {
		return usageFailure(output, "discover", err)
	}
	dialect := options.dialect
	if dialect == dialectAuto {
		dialect = detectDialect(wiring.getenv)
	}
	printer, err := newPresenter(dialect, output.stdout, wiring.getenv)
	if err != nil {
		return usageFailure(output, "discover", err)
	}
	sources := targetSources(loaded, provider, wiring)
	code := 0
	commands.NewDiscoverRefreshTargetsCommand().Execute(
		ctx,
		commands.DiscoverRefreshTargetsInput{
			Sources: sources, Exclude: loaded.Refresh.Exclude, BatchSize: options.batchSize, Target: reference,
		},
		commands.DiscoverRefreshTargetsListeners{
			OnSuccess: func(batches []entities.RefreshBatch, unreachable []commands.UnreachableOrganization) {
				code = reportDiscovery(printer, dialect, options.batchSize, batches, unreachable)
				if reference == nil && len(sources) > 0 && len(unreachable) == len(sources) {
					printer.Error("No organization could be enumerated, so nothing was refreshed.")
					code = exitFailure
				}
			},
			OnRefused: func(target string, reasons []string) {
				printer.Error(fmt.Sprintf("Refusing to refresh %s: it is %s, and the refresh leaves such "+
					"repositories out.", target, strings.Join(reasons, " and ")))
				code = exitFailure
			},
			OnError: func(err error) {
				printer.Error(err.Error())
				code = exitFailure
			},
		},
	)
	return code
}

// parseDiscoverOptions reads the flags of `discover`.
func parseDiscoverOptions(arguments []string) (discoverOptions, error) {
	options := discoverOptions{}
	flags := flag.NewFlagSet("discover", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.config, "config", "", "configuration file")
	flags.StringVar(&options.repo, "repo", "", "refresh only this repository")
	flags.IntVar(&options.batchSize, "batch-size", defaultBatchSize, "repositories per matrix leg")
	flags.StringVar(&options.dialect, "ci", dialectAuto, "output dialect")
	if err := flags.Parse(arguments); err != nil {
		return options, err
	}
	if options.config == "" || flags.NArg() != 0 {
		return options, errUsage
	}
	return options, nil
}

// targetSources builds one source per configured organization, bound to its
// own token. An organization without a token is a source that explains why.
func targetSources(
	loaded configuration,
	provider entities.Provider,
	wiring dependencies,
) []commands.RefreshTargetSource {
	sources := []commands.RefreshTargetSource{}
	for _, organization := range loaded.organizations(provider) {
		source := commands.RefreshTargetSource{Organization: organization.name, Credential: organization.credential}
		if isUnsetToken(organization.token) {
			source.Unavailable = "no token: its token variable is empty or not set"
			if organization.credential != "" {
				source.Unavailable = "no token: the '" + organization.credential + "' secret is empty or not set"
			}
			sources = append(sources, source)
			continue
		}
		targets, err := wiring.hosting[provider].targets(organization.token)
		if err != nil {
			source.Unavailable = err.Error()
		}
		source.Targets = targets
		sources = append(sources, source)
	}
	return sources
}

// reportDiscovery writes the unreachable organizations, the matrix in the
// dialect's shape, and a summary of the batches.
func reportDiscovery(
	printer presenter,
	dialect string,
	batchSize int,
	batches []entities.RefreshBatch,
	unreachable []commands.UnreachableOrganization,
) int {
	failed := []string{}
	for _, organization := range unreachable {
		printer.Error(fmt.Sprintf("Could not enumerate '%s', so none of its repositories will be refreshed.",
			organization.Organization))
		printer.UntrustedNotice("  " + organization.Reason)
		failed = append(failed, organization.Organization)
	}
	outputs, err := matrixOutputs(dialect, batches, failed)
	if err != nil {
		printer.Error(err.Error())
		return exitFailure
	}
	if err = printer.Outputs(outputs); err != nil {
		printer.Error(err.Error())
		return exitFailure
	}
	total := 0
	for _, batch := range batches {
		total += len(batch.Targets)
	}
	printer.Notice(fmt.Sprintf("Discovered %d target repo(s) across %d batch(es) of up to %d.",
		total, len(batches), batchSize))
	for _, batch := range batches {
		printer.Notice(fmt.Sprintf("  %s: %d repo(s)", batch.Name, len(batch.Targets)))
	}
	return 0
}

// matrixOutputs encodes the batches as the dialect's jobs read them: an Azure
// Pipelines matrix is an object of string variables keyed by leg, anything
// else an array a GitHub Actions matrix expands directly. Both carry the
// organizations that could not be listed, so a later job can fail the run.
func matrixOutputs(dialect string, batches []entities.RefreshBatch, failed []string) ([]output, error) {
	count := output{name: "count", value: strconv.Itoa(len(batches))}
	unreachable, err := compactJSON(failed)
	if err != nil {
		return nil, err
	}
	organizations := output{name: "failed_organizations", value: unreachable}
	if dialect == dialectAzurePipelines {
		matrix, mapErr := mapToAzureMatrix(batches, compactJSON)
		if mapErr != nil {
			return nil, mapErr
		}
		encoded, encodeErr := compactJSON(matrix)
		if encodeErr != nil {
			return nil, encodeErr
		}
		return []output{{name: "repoMatrix", value: encoded}, count, organizations}, nil
	}
	matrix, err := compactJSON(mapToGithubMatrix(batches))
	if err != nil {
		return nil, err
	}
	return []output{{name: "matrix", value: matrix}, count, organizations}, nil
}

// compactJSON encodes value on one line, as a step output must be.
func compactJSON(value any) (string, error) {
	var encoded strings.Builder
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", fmt.Errorf("encoding the matrix: %w", err)
	}
	return strings.TrimSuffix(encoded.String(), "\n"), nil
}
