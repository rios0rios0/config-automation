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

// dependencies are what the subcommands are wired from. Tests replace the
// hosting adapters, the clones, and the agent; everything else is the
// production wiring.
type dependencies struct {
	hosting map[entities.Provider]hosting
	clones  repositories.ClonesRepository
	agent   agentFactory
	getenv  func(string) string
	now     func() time.Time
}

// claudeCodeAgent builds the agent over the claude CLI on PATH, starting from
// environ.
func claudeCodeAgent(binary string, environ func() []string) agentFactory {
	return func(live io.Writer, withheld []string) repositories.AgentRepository {
		return infrastructure.NewClaudeCodeAgentRepository(binary, environ, live, withheld)
	}
}

// defaultDependencies is the production wiring: GitHub discovery over the
// go-github listing the compliance audit uses, Azure DevOps discovery and
// every provider's pull requests through gitforge, git, and the claude CLI.
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
		clones: infrastructure.NewGitClonesRepository(),
		agent:  claudeCodeAgent("claude", os.Environ),
		getenv: os.Getenv,
		now:    time.Now,
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

// newRefreshCommand wires one batch's refresh for a provider and a token.
// agentOutput receives Claude's output as it streams; withheld names the
// variables the agent must not inherit.
func newRefreshCommand(
	wiring dependencies,
	provider entities.Provider,
	token string,
	identity entities.GitIdentity,
	agentOutput io.Writer,
	withheld []string,
) *commands.RefreshRepositoriesCommand {
	workingTree := infrastructure.NewGitWorkingTreeRepository(identity)
	return commands.NewRefreshRepositoriesCommand(commands.RefreshRepositoriesDependencies{
		WorkingTree:  workingTree,
		Changes:      workingTree,
		Clones:       wiring.clones,
		Agent:        wiring.agent(agentOutput, withheld),
		PullRequests: wiring.hosting[provider].pullRequests(token),
		Now:          wiring.now,
	})
}
