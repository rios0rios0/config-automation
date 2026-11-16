package repositories

import (
	"context"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

// AgentRepository is the port to the coding agent that audits and refreshes
// a clone. An error comes with the run's output, so the caller can tell a
// usage limit or a safeguard refusal from any other failure.
type AgentRepository interface {
	// Audit runs the read-only prompt audit and returns its report. A run that
	// exits cleanly but returns no successful, non-empty report fails.
	Audit(ctx context.Context, run entities.AgentRun) (entities.AgentResult, error)

	// Refresh runs the scoped refresh with the run's tool grants.
	Refresh(ctx context.Context, run entities.AgentRun) (entities.AgentResult, error)
}
