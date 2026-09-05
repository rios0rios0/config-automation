//go:build unit

package commands_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/commands"
	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/test/domain/builders"
	doubles "github.com/rios0rios0/config-automation/test/domain/doubles/repositories"
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
			OnSuccess: func(_, _, _ int) {},
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

	t.Run("should call OnSkip when a fork already has Actions disabled", func(t *testing.T) {
		t.Parallel()
		// given — secret scanning off and Dependabot unknown, which on a
		// non-fork would be three mutations; on a fork they are carved out,
		// and with Actions already off nothing is left to enforce.
		forkRepo := builders.NewRepositoryBuilder().WithName("forked").AsFork().Build()
		audit := builders.NewAuditResultBuilder().
			WithRepository(forkRepo).
			WithSecurity(entities.SecuritySettings{}).
			WithActionsEnabled(false).
			Build()
		securityRepo := doubles.NewInMemorySecuritySettingsRepository()
		command := commands.NewApplySecuritySettingsCommand(securityRepo)

		// when
		var skipped []string
		var changes []commands.ApplySecuritySettingsChange
		command.Execute(context.TODO(), commands.ApplySecuritySettingsInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}}, commands.ApplySecuritySettingsListeners{
			OnChange:  func(c commands.ApplySecuritySettingsChange) { changes = append(changes, c) },
			OnSkip:    func(name, _ string) { skipped = append(skipped, name) },
			OnSuccess: func(_, _, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Empty(t, securityRepo.SecretScanningEnabled)
		assert.Empty(t, securityRepo.VulnerabilityAlertsEnabled)
		assert.Empty(t, securityRepo.AutomatedSecurityFixesEnabled)
		assert.Empty(t, securityRepo.ActionsDisabled)
		assert.Empty(t, changes)
		assert.Equal(t, []string{"forked"}, skipped)
	})

	t.Run("should call OnChange with actions_disabled when a fork still has Actions enabled", func(t *testing.T) {
		t.Parallel()
		// given — the fork is also missing secret scanning and Dependabot,
		// which must stay untouched: only the Actions switch is a fork rule.
		forkRepo := builders.NewRepositoryBuilder().WithName("forked").AsFork().Build()
		audit := builders.NewAuditResultBuilder().
			WithRepository(forkRepo).
			WithSecurity(entities.SecuritySettings{}).
			WithActionsEnabled(true).
			Build()
		securityRepo := doubles.NewInMemorySecuritySettingsRepository()
		command := commands.NewApplySecuritySettingsCommand(securityRepo)

		// when
		var skipped []string
		var changes []commands.ApplySecuritySettingsChange
		var actionsChanges int
		command.Execute(context.TODO(), commands.ApplySecuritySettingsInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}}, commands.ApplySecuritySettingsListeners{
			OnChange:  func(c commands.ApplySecuritySettingsChange) { changes = append(changes, c) },
			OnSkip:    func(name, _ string) { skipped = append(skipped, name) },
			OnSuccess: func(_, _, actions int) { actionsChanges = actions },
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Equal(t, []string{"forked"}, securityRepo.ActionsDisabled)
		assert.Empty(t, securityRepo.SecretScanningEnabled)
		assert.Empty(t, securityRepo.VulnerabilityAlertsEnabled)
		assert.Empty(t, securityRepo.AutomatedSecurityFixesEnabled)
		assert.Empty(t, skipped)
		require.Len(t, changes, 1)
		assert.Equal(t, "forked", changes[0].RepositoryName)
		assert.Equal(t, "actions_disabled", changes[0].Action)
		assert.True(t, changes[0].Applied)
		assert.Equal(t, 1, actionsChanges)
	})

	t.Run("should call OnChange with actions_disabled when a fork's Actions state is unknown", func(t *testing.T) {
		t.Parallel()
		// given — an unreadable switch is enforced rather than trusted,
		// the same way an unknown dependabot_alerts is on a non-fork.
		forkRepo := builders.NewRepositoryBuilder().WithName("forked").AsFork().Build()
		audit := builders.NewAuditResultBuilder().
			WithRepository(forkRepo).
			WithActionsUnknown().
			Build()
		securityRepo := doubles.NewInMemorySecuritySettingsRepository()
		command := commands.NewApplySecuritySettingsCommand(securityRepo)

		// when
		var changes []commands.ApplySecuritySettingsChange
		command.Execute(context.TODO(), commands.ApplySecuritySettingsInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}}, commands.ApplySecuritySettingsListeners{
			OnChange:  func(c commands.ApplySecuritySettingsChange) { changes = append(changes, c) },
			OnSuccess: func(_, _, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Equal(t, []string{"forked"}, securityRepo.ActionsDisabled)
		require.Len(t, changes, 1)
		assert.Equal(t, "actions_disabled", changes[0].Action)
	})

	t.Run("should not call DisableActions when a non-fork has Actions enabled", func(t *testing.T) {
		t.Parallel()
		// given — a compliant repo of our own: its workflows are ours and
		// must keep running.
		audit := builders.NewAuditResultBuilder().WithActionsEnabled(true).Build()
		securityRepo := doubles.NewInMemorySecuritySettingsRepository()
		command := commands.NewApplySecuritySettingsCommand(securityRepo)

		// when
		var changes []commands.ApplySecuritySettingsChange
		command.Execute(context.TODO(), commands.ApplySecuritySettingsInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}}, commands.ApplySecuritySettingsListeners{
			OnChange:  func(c commands.ApplySecuritySettingsChange) { changes = append(changes, c) },
			OnSuccess: func(_, _, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Empty(t, securityRepo.ActionsDisabled)
		assert.Empty(t, changes)
	})

	t.Run("should call OnError when disabling Actions on a fork fails", func(t *testing.T) {
		t.Parallel()
		// given
		expected := errors.New("403 Resource not accessible")
		forkRepo := builders.NewRepositoryBuilder().WithName("forked").AsFork().Build()
		audit := builders.NewAuditResultBuilder().
			WithRepository(forkRepo).
			WithActionsEnabled(true).
			Build()
		securityRepo := doubles.NewInMemorySecuritySettingsRepository()
		securityRepo.ErrorOnDisableActions = expected
		command := commands.NewApplySecuritySettingsCommand(securityRepo)

		// when
		var errored []string
		var received error
		var actionsChanges int
		command.Execute(context.TODO(), commands.ApplySecuritySettingsInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}}, commands.ApplySecuritySettingsListeners{
			OnChange:  func(_ commands.ApplySecuritySettingsChange) { t.Error("OnChange should not be called") },
			OnSuccess: func(_, _, actions int) { actionsChanges = actions },
			OnError:   func(name string, err error) { errored = append(errored, name); received = err },
		})

		// then
		assert.Equal(t, []string{"forked"}, errored)
		require.ErrorIs(t, received, expected)
		assert.Empty(t, securityRepo.ActionsDisabled)
		assert.Equal(t, 0, actionsChanges)
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
			OnSuccess: func(_, _, _ int) {},
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
			OnSuccess: func(_, _, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Empty(t, securityRepo.SecretScanningEnabled)
		assert.Empty(t, securityRepo.VulnerabilityAlertsEnabled)
		assert.Empty(t, securityRepo.AutomatedSecurityFixesEnabled)
	})

	t.Run("should report actions_disabled without mutating when DryRun is set on a fork", func(t *testing.T) {
		t.Parallel()
		// given
		forkRepo := builders.NewRepositoryBuilder().WithName("forked").AsFork().Build()
		audit := builders.NewAuditResultBuilder().
			WithRepository(forkRepo).
			WithActionsEnabled(true).
			Build()
		securityRepo := doubles.NewInMemorySecuritySettingsRepository()
		command := commands.NewApplySecuritySettingsCommand(securityRepo)

		// when
		var changes []commands.ApplySecuritySettingsChange
		command.Execute(context.TODO(), commands.ApplySecuritySettingsInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}, DryRun: true}, commands.ApplySecuritySettingsListeners{
			OnChange:  func(c commands.ApplySecuritySettingsChange) { changes = append(changes, c) },
			OnSuccess: func(_, _, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Empty(t, securityRepo.ActionsDisabled)
		require.Len(t, changes, 1)
		assert.Equal(t, "actions_disabled", changes[0].Action)
		assert.False(t, changes[0].Applied)
	})
}
