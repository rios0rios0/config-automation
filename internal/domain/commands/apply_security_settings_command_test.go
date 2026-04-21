//go:build unit

package commands_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/fleet-maintenance/internal/domain/commands"
	"github.com/rios0rios0/fleet-maintenance/internal/domain/entities"
	"github.com/rios0rios0/fleet-maintenance/test/domain/builders"
	doubles "github.com/rios0rios0/fleet-maintenance/test/domain/doubles/repositories"
)

func TestApplySecuritySettingsCommand(t *testing.T) {
	t.Parallel()

	t.Run("should enable secret scanning and Dependabot when both are disabled on a public repo", func(t *testing.T) {
		t.Parallel()
		// given
		disabled := false
		audit := builders.NewAuditResultBuilder().
			WithSecurity(entities.SecuritySettings{
				SecretScanning:    "disabled",
				PushProtection:    "disabled",
				DependabotAlerts:  &disabled,
				DependabotUpdates: false,
			}).
			Build()
		securityRepo := doubles.NewInMemorySecuritySettingsRepository()
		command := commands.NewApplySecuritySettingsCommand(securityRepo)

		// when
		var changes []commands.ApplySecuritySettingsChange
		command.Execute(context.TODO(), commands.ApplySecuritySettingsInput{
			Owner: "rios0rios0", Audits: []entities.AuditResult{audit},
		}, commands.ApplySecuritySettingsListeners{
			OnChange:  func(c commands.ApplySecuritySettingsChange) { changes = append(changes, c) },
			OnSuccess: func(_, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Contains(t, securityRepo.SecretScanningEnabled, audit.Repository.Name)
		assert.Contains(t, securityRepo.VulnerabilityAlertsEnabled, audit.Repository.Name)
		assert.Contains(t, securityRepo.AutomatedSecurityFixesEnabled, audit.Repository.Name)
		require.Len(t, changes, 3)
		for _, c := range changes {
			assert.True(t, c.Applied)
		}
	})

	t.Run("should skip forks entirely", func(t *testing.T) {
		t.Parallel()
		// given
		forkRepo := builders.NewRepositoryBuilder().WithName("forked").AsFork().Build()
		audit := builders.NewAuditResultBuilder().
			WithRepository(forkRepo).
			WithSecurity(entities.SecuritySettings{}).
			Build()
		securityRepo := doubles.NewInMemorySecuritySettingsRepository()
		command := commands.NewApplySecuritySettingsCommand(securityRepo)

		// when
		var skipped []string
		command.Execute(context.TODO(), commands.ApplySecuritySettingsInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}}, commands.ApplySecuritySettingsListeners{
			OnSkip:    func(name, _ string) { skipped = append(skipped, name) },
			OnSuccess: func(_, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Empty(t, securityRepo.SecretScanningEnabled)
		assert.Empty(t, securityRepo.VulnerabilityAlertsEnabled)
		assert.Empty(t, securityRepo.AutomatedSecurityFixesEnabled)
		assert.Equal(t, []string{"forked"}, skipped)
	})

	t.Run("should skip secret scanning on private repos but still handle Dependabot", func(t *testing.T) {
		t.Parallel()
		// given
		disabled := false
		privateRepo := builders.NewRepositoryBuilder().WithName("secret").AsPrivate().Build()
		audit := builders.NewAuditResultBuilder().
			WithRepository(privateRepo).
			WithSecurity(entities.SecuritySettings{
				DependabotAlerts:  &disabled,
				DependabotUpdates: false,
			}).
			Build()
		securityRepo := doubles.NewInMemorySecuritySettingsRepository()
		command := commands.NewApplySecuritySettingsCommand(securityRepo)

		// when
		command.Execute(context.TODO(), commands.ApplySecuritySettingsInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}}, commands.ApplySecuritySettingsListeners{
			OnSuccess: func(_, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Empty(t, securityRepo.SecretScanningEnabled)
		assert.Contains(t, securityRepo.VulnerabilityAlertsEnabled, "secret")
		assert.Contains(t, securityRepo.AutomatedSecurityFixesEnabled, "secret")
	})

	t.Run("should not mutate when DryRun is set", func(t *testing.T) {
		t.Parallel()
		// given
		disabled := false
		audit := builders.NewAuditResultBuilder().
			WithSecurity(entities.SecuritySettings{
				SecretScanning:   "disabled",
				PushProtection:   "disabled",
				DependabotAlerts: &disabled,
			}).
			Build()
		securityRepo := doubles.NewInMemorySecuritySettingsRepository()
		command := commands.NewApplySecuritySettingsCommand(securityRepo)

		// when
		command.Execute(context.TODO(), commands.ApplySecuritySettingsInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}, DryRun: true}, commands.ApplySecuritySettingsListeners{
			OnSuccess: func(_, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Empty(t, securityRepo.SecretScanningEnabled)
		assert.Empty(t, securityRepo.VulnerabilityAlertsEnabled)
		assert.Empty(t, securityRepo.AutomatedSecurityFixesEnabled)
	})
}
