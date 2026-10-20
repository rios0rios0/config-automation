package main

import (
	"github.com/rios0rios0/config-automation/internal/domain/commands"
	"github.com/rios0rios0/config-automation/internal/infrastructure/repositories"
)

// commandSet collects the commands the subcommands dispatch to.
type commandSet struct {
	DiscoverScope   *commands.DiscoverRefreshScopeCommand
	NormalizeLayout *commands.NormalizeAgentsLayoutCommand
}

// initializeCommands is the composition root: both commands share the git
// working-tree adapter, and the compiler checks every dependency.
func initializeCommands() commandSet {
	workingTree := repositories.NewGitWorkingTreeRepository()
	return commandSet{
		DiscoverScope:   commands.NewDiscoverRefreshScopeCommand(workingTree),
		NormalizeLayout: commands.NewNormalizeAgentsLayoutCommand(workingTree),
	}
}
