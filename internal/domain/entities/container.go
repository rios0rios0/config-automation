package entities

import "go.uber.org/dig"

// RegisterProviders is a no-op for the entities layer (plain data types
// with no dependencies to wire). Kept for architectural consistency with
// the Dig orchestrator in internal/container.go.
func RegisterProviders(_ *dig.Container) error {
	return nil
}
