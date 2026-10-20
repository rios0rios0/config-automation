package main

import (
	"github.com/rios0rios0/config-automation/internal/domain/commands"
	"github.com/rios0rios0/config-automation/internal/infrastructure/repositories"
)

// commandSet collects the commands that main.go dispatches to.
type commandSet struct {
	Audit           *commands.AuditRepositoriesCommand
	ApplyRepo       *commands.ApplyRepositorySettingsCommand
	ApplySecurity   *commands.ApplySecuritySettingsCommand
	ApplyProtection *commands.ApplyBranchProtectionCommand
	ListTargets     *commands.ListTargetRepositoriesCommand
	Report          *commands.ReportComplianceChangesCommand
	ApplySonar      *commands.ApplySonarPolicyCommand
}

// initializeCommands is the composition root: the three GitHub adapters
// share one client, and the compiler checks every dependency.
func initializeCommands() commandSet {
	githubClient := repositories.NewGithubClient()
	reposRepo := repositories.NewGoGithubRepositoriesRepository(githubClient)
	securityRepo := repositories.NewGoGithubSecuritySettingsRepository(githubClient)
	protectionsRepo := repositories.NewGoGithubBranchProtectionsRepository(githubClient)
	sonarRepo := repositories.NewSonarCloudSonarProjectsRepository(repositories.NewSonarConfig())
	return commandSet{
		Audit:           commands.NewAuditRepositoriesCommand(reposRepo, securityRepo, protectionsRepo),
		ApplyRepo:       commands.NewApplyRepositorySettingsCommand(reposRepo),
		ApplySecurity:   commands.NewApplySecuritySettingsCommand(securityRepo),
		ApplyProtection: commands.NewApplyBranchProtectionCommand(protectionsRepo),
		ListTargets:     commands.NewListTargetRepositoriesCommand(reposRepo),
		Report:          commands.NewReportComplianceChangesCommand(),
		ApplySonar:      commands.NewApplySonarPolicyCommand(sonarRepo),
	}
}
