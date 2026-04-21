package repositories

import (
	"context"

	"github.com/rios0rios0/fleet-maintenance/internal/domain/entities"
	"github.com/rios0rios0/fleet-maintenance/internal/domain/repositories"
)

// InMemorySecuritySettingsRepository records every mutation call so
// tests can assert the command invoked the right endpoints in the right
// order. FindByRepositoryName returns whatever was seeded via
// WithSettings, keyed by repo name.
type InMemorySecuritySettingsRepository struct {
	SettingsByName                map[string]entities.SecuritySettings
	VulnerabilityAlertsEnabled    []string
	AutomatedSecurityFixesEnabled []string
	SecretScanningEnabled         []string

	ErrorOnFind                  error
	ErrorOnEnableAlerts          error
	ErrorOnEnableFixes           error
	ErrorOnEnableSecretScanning  error
}

// NewInMemorySecuritySettingsRepository builds the double.
func NewInMemorySecuritySettingsRepository() *InMemorySecuritySettingsRepository {
	return &InMemorySecuritySettingsRepository{
		SettingsByName: map[string]entities.SecuritySettings{},
	}
}

// WithSettings seeds the tri-state security snapshot for one repo.
func (r *InMemorySecuritySettingsRepository) WithSettings(name string, settings entities.SecuritySettings) *InMemorySecuritySettingsRepository {
	r.SettingsByName[name] = settings
	return r
}

func (r *InMemorySecuritySettingsRepository) FindByRepositoryName(_ context.Context, repo entities.Repository) (entities.SecuritySettings, error) {
	if r.ErrorOnFind != nil {
		return entities.SecuritySettings{}, r.ErrorOnFind
	}
	return r.SettingsByName[repo.Name], nil
}

func (r *InMemorySecuritySettingsRepository) EnableVulnerabilityAlerts(_ context.Context, _, name string) error {
	if r.ErrorOnEnableAlerts != nil {
		return r.ErrorOnEnableAlerts
	}
	r.VulnerabilityAlertsEnabled = append(r.VulnerabilityAlertsEnabled, name)
	return nil
}

func (r *InMemorySecuritySettingsRepository) EnableAutomatedSecurityFixes(_ context.Context, _, name string) error {
	if r.ErrorOnEnableFixes != nil {
		return r.ErrorOnEnableFixes
	}
	r.AutomatedSecurityFixesEnabled = append(r.AutomatedSecurityFixesEnabled, name)
	return nil
}

func (r *InMemorySecuritySettingsRepository) EnableSecretScanning(_ context.Context, _, name string) error {
	if r.ErrorOnEnableSecretScanning != nil {
		return r.ErrorOnEnableSecretScanning
	}
	r.SecretScanningEnabled = append(r.SecretScanningEnabled, name)
	return nil
}

// Ensure interface compliance.
var _ repositories.SecuritySettingsRepository = (*InMemorySecuritySettingsRepository)(nil)
