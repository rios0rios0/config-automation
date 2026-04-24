package repositories

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/go-github/v66/github"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// GoGithubSecuritySettingsRepository implements
// repositories.SecuritySettingsRepository by wrapping go-github.
//
// A nil DependabotAlerts pointer means "unknown" (API failure or
// permission denial) — the compliance policy distinguishes unknown
// from disabled, so we preserve the tri-state semantics of the
// Python original.
type GoGithubSecuritySettingsRepository struct {
	client *github.Client
}

// NewGoGithubSecuritySettingsRepository is the Dig-injectable constructor.
func NewGoGithubSecuritySettingsRepository(client *github.Client) *GoGithubSecuritySettingsRepository {
	return &GoGithubSecuritySettingsRepository{client: client}
}

var _ repositories.SecuritySettingsRepository = (*GoGithubSecuritySettingsRepository)(nil)

// FindByRepositoryName builds the full SecuritySettings for a repo:
// secret scanning + push protection from the detail payload, plus
// Dependabot alerts and automated security fixes from their endpoints.
func (r GoGithubSecuritySettingsRepository) FindByRepositoryName(
	ctx context.Context,
	repo entities.Repository,
) (entities.SecuritySettings, error) {
	detail, _, err := r.client.Repositories.Get(ctx, repo.Owner, repo.Name)
	if err != nil {
		return entities.SecuritySettings{}, err
	}

	settings := entities.SecuritySettings{}
	if detail != nil && detail.SecurityAndAnalysis != nil {
		if detail.SecurityAndAnalysis.SecretScanning != nil && detail.SecurityAndAnalysis.SecretScanning.Status != nil {
			settings.SecretScanning = *detail.SecurityAndAnalysis.SecretScanning.Status
		}
		if detail.SecurityAndAnalysis.SecretScanningPushProtection != nil &&
			detail.SecurityAndAnalysis.SecretScanningPushProtection.Status != nil {
			settings.PushProtection = *detail.SecurityAndAnalysis.SecretScanningPushProtection.Status
		}
	}

	alerts, alertsErr := r.findVulnerabilityAlerts(ctx, repo.Owner, repo.Name)
	if alertsErr == nil {
		settings.DependabotAlerts = &alerts
	}

	fixes, fixesErr := r.findAutomatedSecurityFixes(ctx, repo.Owner, repo.Name)
	if fixesErr != nil {
		settings.DependabotUpdates = false
	} else {
		settings.DependabotUpdates = fixes
	}

	return settings, nil
}

// EnableVulnerabilityAlerts turns on Dependabot alerts.
func (r GoGithubSecuritySettingsRepository) EnableVulnerabilityAlerts(ctx context.Context, owner, name string) error {
	_, err := r.client.Repositories.EnableVulnerabilityAlerts(ctx, owner, name)
	return err
}

// EnableAutomatedSecurityFixes turns on automated security fixes.
func (r GoGithubSecuritySettingsRepository) EnableAutomatedSecurityFixes(
	ctx context.Context,
	owner, name string,
) error {
	_, err := r.client.Repositories.EnableAutomatedSecurityFixes(ctx, owner, name)
	return err
}

// EnableSecretScanning flips secret_scanning and push_protection on via
// PATCH /repos/{owner}/{name}.
func (r GoGithubSecuritySettingsRepository) EnableSecretScanning(ctx context.Context, owner, name string) error {
	enabled := "enabled"
	patch := &github.Repository{
		SecurityAndAnalysis: &github.SecurityAndAnalysis{
			SecretScanning:               &github.SecretScanning{Status: &enabled},
			SecretScanningPushProtection: &github.SecretScanningPushProtection{Status: &enabled},
		},
	}
	_, _, err := r.client.Repositories.Edit(ctx, owner, name, patch)
	return err
}

// findVulnerabilityAlerts distinguishes enabled (204) from disabled
// (404). Any other response is surfaced as an error so the caller can
// treat DependabotAlerts as nil (unknown).
func (r GoGithubSecuritySettingsRepository) findVulnerabilityAlerts(
	ctx context.Context,
	owner, name string,
) (bool, error) {
	enabled, resp, err := r.client.Repositories.GetVulnerabilityAlerts(ctx, owner, name)
	if err != nil {
		var ghErr *github.ErrorResponse
		if errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, err
	}
	if resp != nil && resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return enabled, nil
}

// findAutomatedSecurityFixes reads the boolean via the dedicated
// endpoint.
func (r GoGithubSecuritySettingsRepository) findAutomatedSecurityFixes(
	ctx context.Context,
	owner, name string,
) (bool, error) {
	fixes, _, err := r.client.Repositories.GetAutomatedSecurityFixes(ctx, owner, name)
	if err != nil {
		return false, err
	}
	if fixes == nil || fixes.Enabled == nil {
		return false, nil
	}
	return *fixes.Enabled, nil
}
