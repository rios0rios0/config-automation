package commands

import "go.uber.org/dig"

// RegisterProviders wires every command constructor into the Dig
// container. main.go invokes these via container.Invoke().
func RegisterProviders(container *dig.Container) error {
	providers := []any{
		NewAuditRepositoriesCommand,
		NewApplyRepositorySettingsCommand,
		NewApplySecuritySettingsCommand,
		NewApplyBranchProtectionCommand,
		NewListTargetRepositoriesCommand,
		NewReportComplianceChangesCommand,
	}
	for _, p := range providers {
		if err := container.Provide(p); err != nil {
			return err
		}
	}
	return nil
}
