package main

import (
	"fmt"

	"go.uber.org/dig"

	"github.com/rios0rios0/fleet-maintenance/internal"
	"github.com/rios0rios0/fleet-maintenance/internal/domain/commands"
)

// commandSet collects the commands that main.go dispatches to. Dig
// resolves every field by matching the struct fields against
// previously-registered constructors.
type commandSet struct {
	dig.In

	Audit           *commands.AuditRepositoriesCommand
	ApplyRepo       *commands.ApplyRepositorySettingsCommand
	ApplySecurity   *commands.ApplySecuritySettingsCommand
	ApplyProtection *commands.ApplyBranchProtectionCommand
	ListTargets     *commands.ListTargetRepositoriesCommand
	Report          *commands.ReportComplianceChangesCommand
}

// injectCommands builds the container, registers providers, and
// resolves the command set for the CLI dispatcher.
func injectCommands() commandSet {
	container := dig.New()
	if err := internal.RegisterProviders(container); err != nil {
		panic(fmt.Errorf("registering providers: %w", err))
	}

	var set commandSet
	if err := container.Invoke(func(resolved commandSet) {
		set = resolved
	}); err != nil {
		panic(fmt.Errorf("resolving command set: %w", err))
	}
	return set
}
