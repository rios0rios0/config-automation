//go:build unit

package commands_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/fleet-maintenance/internal/domain/commands"
	"github.com/rios0rios0/fleet-maintenance/internal/domain/entities"
	"github.com/rios0rios0/fleet-maintenance/test/domain/builders"
	doubles "github.com/rios0rios0/fleet-maintenance/test/domain/doubles/repositories"
)

func TestApplyBranchProtectionCommand(t *testing.T) {
	t.Parallel()

	t.Run("should save protection, enable signatures, and create ruleset when all are missing", func(t *testing.T) {
		t.Parallel()
		// given — public repo with no protection and no ruleset.
		audit := builders.NewAuditResultBuilder().
			WithBranchProtection(entities.BranchProtection{Available: true, Enabled: false}).
			WithoutRuleset().
			Build()
		protectionRepo := doubles.NewInMemoryBranchProtectionsRepository()
		command := commands.NewApplyBranchProtectionCommand(protectionRepo)

		// when
		command.Execute(context.TODO(), commands.ApplyBranchProtectionInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}}, commands.ApplyBranchProtectionListeners{
			OnSuccess: func(_, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Len(t, protectionRepo.ProtectionSaves, 1)
		assert.Contains(t, protectionRepo.SignaturesEnabled, audit.Repository.Name)
		assert.Contains(t, protectionRepo.RulesetsCreated, audit.Repository.Name)
	})

	t.Run("should skip private repos entirely", func(t *testing.T) {
		t.Parallel()
		// given
		privateRepo := builders.NewRepositoryBuilder().WithName("secret").AsPrivate().Build()
		audit := builders.NewAuditResultBuilder().WithRepository(privateRepo).Build()
		protectionRepo := doubles.NewInMemoryBranchProtectionsRepository()
		command := commands.NewApplyBranchProtectionCommand(protectionRepo)

		// when
		var skipped []string
		command.Execute(context.TODO(), commands.ApplyBranchProtectionInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}}, commands.ApplyBranchProtectionListeners{
			OnSkip:    func(name, _ string) { skipped = append(skipped, name) },
			OnSuccess: func(_, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Empty(t, protectionRepo.ProtectionSaves)
		assert.Empty(t, protectionRepo.RulesetsCreated)
		assert.Equal(t, []string{"secret"}, skipped)
	})

	t.Run("should skip repos where branch protection is unavailable", func(t *testing.T) {
		t.Parallel()
		// given
		audit := builders.NewAuditResultBuilder().
			WithBranchProtection(entities.BranchProtection{Available: false}).
			WithoutRuleset().
			Build()
		protectionRepo := doubles.NewInMemoryBranchProtectionsRepository()
		command := commands.NewApplyBranchProtectionCommand(protectionRepo)

		// when
		var skipReasons []string
		command.Execute(context.TODO(), commands.ApplyBranchProtectionInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}}, commands.ApplyBranchProtectionListeners{
			OnSkip:    func(_, reason string) { skipReasons = append(skipReasons, reason) },
			OnSuccess: func(_, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Empty(t, protectionRepo.ProtectionSaves)
		assert.Equal(t, []string{"protection_unavailable"}, skipReasons)
	})

	t.Run("should not mutate when DryRun is set", func(t *testing.T) {
		t.Parallel()
		// given — fully non-compliant public repo.
		audit := builders.NewAuditResultBuilder().
			WithBranchProtection(entities.BranchProtection{Available: true, Enabled: false}).
			WithoutRuleset().
			Build()
		protectionRepo := doubles.NewInMemoryBranchProtectionsRepository()
		command := commands.NewApplyBranchProtectionCommand(protectionRepo)

		// when
		var changes []commands.ApplyBranchProtectionChange
		command.Execute(context.TODO(), commands.ApplyBranchProtectionInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}, DryRun: true}, commands.ApplyBranchProtectionListeners{
			OnChange:  func(c commands.ApplyBranchProtectionChange) { changes = append(changes, c) },
			OnSuccess: func(_, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Empty(t, protectionRepo.ProtectionSaves)
		assert.Empty(t, protectionRepo.RulesetsCreated)
		assert.Len(t, changes, 3, "dry run should report three would-apply changes")
		for _, c := range changes {
			assert.False(t, c.Applied)
		}
	})
}
