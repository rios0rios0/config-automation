package internal

import (
	"go.uber.org/dig"

	"github.com/rios0rios0/fleet-maintenance/internal/domain/commands"
	"github.com/rios0rios0/fleet-maintenance/internal/domain/entities"
	"github.com/rios0rios0/fleet-maintenance/internal/infrastructure/repositories"
)

// RegisterProviders orchestrates provider registration across every
// layer in dependency order: infrastructure first (repositories have no
// dependencies), then domain entities (no-op), then domain commands
// (which depend on repository interfaces).
func RegisterProviders(container *dig.Container) error {
	if err := repositories.RegisterProviders(container); err != nil {
		return err
	}
	if err := entities.RegisterProviders(container); err != nil {
		return err
	}
	if err := commands.RegisterProviders(container); err != nil {
		return err
	}
	return nil
}
