package entities

// Stable string tokens used across the codebase for the tri-state
// secret-scanning and Dependabot-alerts fields.
const (
	SecurityStateEnabled  = "enabled"
	SecurityStateDisabled = "disabled"
	SecurityStateUnknown  = "unknown"
)

// SecuritySettings groups the security toggles audited and enforced by
// phase 3: secret scanning, push protection, and Dependabot.
//
// DependabotAlerts is a *bool so callers can distinguish "unknown" (API
// failure or insufficient permission) from "disabled" (API returned 404).
// compute_issues() in the Python original flagged this as `unknown` and the
// Go port preserves the distinction.
type SecuritySettings struct {
	SecretScanning    string
	PushProtection    string
	DependabotAlerts  *bool
	DependabotUpdates bool
}

// IsSecretScanningEnabled is a small helper used by compliance checks.
func (s SecuritySettings) IsSecretScanningEnabled() bool {
	return s.SecretScanning == SecurityStateEnabled
}

// IsPushProtectionEnabled is a small helper used by compliance checks.
func (s SecuritySettings) IsPushProtectionEnabled() bool {
	return s.PushProtection == SecurityStateEnabled
}

// DependabotAlertsState returns a stable string representation for reports:
// "enabled", "disabled", or "unknown".
func (s SecuritySettings) DependabotAlertsState() string {
	if s.DependabotAlerts == nil {
		return SecurityStateUnknown
	}
	if *s.DependabotAlerts {
		return SecurityStateEnabled
	}
	return SecurityStateDisabled
}
