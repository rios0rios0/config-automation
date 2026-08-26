//go:build unit

package commands_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/config-automation/internal/domain/commands"
	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/test/domain/builders"
	doubles "github.com/rios0rios0/config-automation/test/domain/doubles/repositories"
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

	t.Run("should update the ruleset in place when a drifted one already exists", func(t *testing.T) {
		t.Parallel()
		// given — the shape every already-hardened repo is in after a policy
		// change: a `main-protection` ruleset exists but predates the
		// pull_request rule. Creating would hit `422 Name must be unique`.
		drifted := &entities.Ruleset{
			ID:                4242,
			Name:              entities.DesiredRulesetName,
			Enforcement:       "active",
			HasNonFastForward: true,
			TargetsMain:       true,
			AdminBypass:       true,
		}
		audit := builders.NewAuditResultBuilder().WithRuleset(drifted).Build()
		protectionRepo := doubles.NewInMemoryBranchProtectionsRepository()
		command := commands.NewApplyBranchProtectionCommand(protectionRepo)

		// when
		command.Execute(context.TODO(), commands.ApplyBranchProtectionInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}}, commands.ApplyBranchProtectionListeners{
			OnSuccess: func(_, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Contains(t, protectionRepo.RulesetsUpdated, audit.Repository.Name)
		assert.Empty(t, protectionRepo.RulesetsCreated, "must not POST over an existing ruleset name")
	})

	t.Run("should create rather than update when no ruleset exists", func(t *testing.T) {
		t.Parallel()
		// given
		audit := builders.NewAuditResultBuilder().WithoutRuleset().Build()
		protectionRepo := doubles.NewInMemoryBranchProtectionsRepository()
		command := commands.NewApplyBranchProtectionCommand(protectionRepo)

		// when
		command.Execute(context.TODO(), commands.ApplyBranchProtectionInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}}, commands.ApplyBranchProtectionListeners{
			OnSuccess: func(_, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		// then
		assert.Contains(t, protectionRepo.RulesetsCreated, audit.Repository.Name)
		assert.Empty(t, protectionRepo.RulesetsUpdated)
	})

	t.Run("should report an update failure without falling back to create", func(t *testing.T) {
		t.Parallel()
		// given
		drifted := &entities.Ruleset{ID: 7, Name: entities.DesiredRulesetName, Enforcement: "active"}
		audit := builders.NewAuditResultBuilder().WithRuleset(drifted).Build()
		protectionRepo := doubles.NewInMemoryBranchProtectionsRepository()
		protectionRepo.ErrorOnUpdateRuleset = errors.New("boom")
		command := commands.NewApplyBranchProtectionCommand(protectionRepo)

		// when
		var failures []string
		command.Execute(context.TODO(), commands.ApplyBranchProtectionInput{Owner: "rios0rios0", Audits: []entities.AuditResult{audit}}, commands.ApplyBranchProtectionListeners{
			OnSuccess: func(_, _ int) {},
			OnError:   func(name string, _ error) { failures = append(failures, name) },
		})

		// then
		assert.Equal(t, []string{audit.Repository.Name}, failures)
		assert.Empty(t, protectionRepo.RulesetsCreated)
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
