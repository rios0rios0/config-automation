package main

import (
	"errors"
	"io"
	"os"
	"time"

	forge "github.com/rios0rios0/gitforge/v4/pkg/global/domain/entities"
	"github.com/rios0rios0/gitforge/v4/pkg/providers/infrastructure/azuredevops"
	"github.com/rios0rios0/gitforge/v4/pkg/providers/infrastructure/github"

	"github.com/rios0rios0/config-automation/internal/domain/commands"
	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
	infrastructure "github.com/rios0rios0/config-automation/internal/infrastructure/repositories"
)

// errNoOrganizationListing reports a provider build without the strict
// organization listing the refresh needs.
var errNoOrganizationListing = errors.New("the provider cannot list an organization strictly")

// commandSet collects the commands the layout and scope subcommands use.
type commandSet struct {
	DiscoverScope   *commands.DiscoverRefreshScopeCommand
	NormalizeLayout *commands.NormalizeAgentsLayoutCommand
}

// hosting builds one provider's adapters for one token.
type hosting struct {
	targets      func(token string) (repositories.RefreshTargetsRepository, error)
	pullRequests func(token string) repositories.PullRequestsRepository
}

// agentFactory builds the agent for one batch: live receives its output as it
// streams, and withheld names the variables it must not inherit.
type agentFactory func(live io.Writer, withheld []string) repositories.AgentRepository

// ignoreRulesFactory builds the generator of the shared ignore rules from a
// pipelines checkout; an empty directory builds none, which leaves every
// `.gitignore` alone.
type ignoreRulesFactory func(directory string) (repositories.IgnoreRulesRepository, error)

// dependencies are what the subcommands are wired from. Tests replace the
// hosting adapters, the clones, the agent, and the environment the ignore-rules
// generator starts from; everything else is the production wiring.
type dependencies struct {
	hosting     map[entities.Provider]hosting
	clones      repositories.ClonesRepository
	agent       agentFactory
	ignoreRules ignoreRulesFactory
	getenv      func(string) string
	now         func() time.Time
}

// claudeCodeAgent builds the agent over the claude CLI on PATH, starting from
// environ.
func claudeCodeAgent(binary string, environ func() []string) agentFactory {
	return func(live io.Writer, withheld []string) repositories.AgentRepository {
		return infrastructure.NewClaudeCodeAgentRepository(binary, environ, live, withheld)
	}
}

// pipelinesIgnoreRules builds the generator of a pipelines checkout, keeping
// its variables from environ. An empty directory returns no generator, as
// nil rather than a nil pointer, so the refresh can tell it is off.
func pipelinesIgnoreRules(environ func() []string) ignoreRulesFactory {
	return func(directory string) (repositories.IgnoreRulesRepository, error) {
		if directory == "" {
			return nil, nil
		}
		generator, err := infrastructure.NewPipelinesIgnoreRulesRepository(directory, environ)
		if err != nil {
			return nil, err
		}
		return generator, nil
	}
}

// defaultDependencies is the production wiring: GitHub discovery over the
// go-github listing the compliance audit uses, Azure DevOps discovery and
// every provider's pull requests through gitforge, git, the claude CLI, and
// the pipelines checkout's ignore-rules generator.
func defaultDependencies() dependencies {
	return dependencies{
		hosting: map[entities.Provider]hosting{
			entities.ProviderGitHub: {
				targets: func(token string) (repositories.RefreshTargetsRepository, error) {
					client, err := infrastructure.NewGithubClientWithToken(token)
					if err != nil {
						return nil, err
					}
					listing := infrastructure.NewGoGithubRepositoriesRepository(client)
					return infrastructure.NewGoGithubRefreshTargetsRepository(listing), nil
				},
				pullRequests: func(token string) repositories.PullRequestsRepository {
					return infrastructure.NewGitforgePullRequestsRepository(
						entities.ProviderGitHub,
						github.NewProvider(token),
					)
				},
			},
			entities.ProviderAzureDevOps: {
				targets: func(token string) (repositories.RefreshTargetsRepository, error) {
					lister, supported := azuredevops.NewProvider(token).(forge.OrganizationRepositoryLister)
					if !supported {
						return nil, errNoOrganizationListing
					}
					return infrastructure.NewGitforgeRefreshTargetsRepository(lister), nil
				},
				pullRequests: func(token string) repositories.PullRequestsRepository {
					return infrastructure.NewGitforgePullRequestsRepository(
						entities.ProviderAzureDevOps, azuredevops.NewProvider(token))
				},
			},
		},
		clones:      infrastructure.NewGitClonesRepository(),
		agent:       claudeCodeAgent("claude", os.Environ),
		ignoreRules: pipelinesIgnoreRules(os.Environ),
		getenv:      os.Getenv,
		now:         time.Now,
	}
}

// initializeCommands wires the layout and scope commands over git, with
// git's own identity for the rename commit.
func initializeCommands() commandSet {
	workingTree := infrastructure.NewGitWorkingTreeRepository(entities.GitIdentity{})
	return commandSet{
		DiscoverScope:   commands.NewDiscoverRefreshScopeCommand(workingTree),
		NormalizeLayout: commands.NewNormalizeAgentsLayoutCommand(workingTree),
	}
}

// refreshWiring is what one batch's refresh is built for: the provider and
// its token, the commits' identity, where Claude's output streams, the
// variables the agent must not inherit, and the ignore-rules generator, nil
// when the run keeps no shared ignore rules.
type refreshWiring struct {
	provider    entities.Provider
	token       string
	identity    entities.GitIdentity
	agentOutput io.Writer
	withheld    []string
	ignoreRules repositories.IgnoreRulesRepository
}

// newRefreshCommand wires one batch's refresh.
func newRefreshCommand(wiring dependencies, batch refreshWiring) *commands.RefreshRepositoriesCommand {
	workingTree := infrastructure.NewGitWorkingTreeRepository(batch.identity)
	return commands.NewRefreshRepositoriesCommand(commands.RefreshRepositoriesDependencies{
		WorkingTree:  workingTree,
		Changes:      workingTree,
		Clones:       wiring.clones,
		Agent:        wiring.agent(batch.agentOutput, batch.withheld),
		PullRequests: wiring.hosting[batch.provider].pullRequests(batch.token),
		IgnoreRules:  batch.ignoreRules,
		Now:          wiring.now,
	})
}
