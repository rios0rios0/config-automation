package entities

// Stable string tokens used across the codebase for the tri-state
// secret-scanning and Dependabot-alerts fields.
const (
	SecurityStateEnabled  = "enabled"
	SecurityStateDisabled = "disabled"
	SecurityStateUnknown  = "unknown"
)

// SecuritySettings groups the security toggles audited and enforced by
// phase 3: secret scanning, push protection, Dependabot, and the
// repository-level GitHub Actions switch.
//
// DependabotAlerts is a *bool so callers can distinguish "unknown" (API
// failure or insufficient permission) from "disabled" (API returned 404).
// compute_issues() in the Python original flagged this as `unknown` and the
// Go port preserves the distinction. ActionsEnabled carries the same
// tri-state for the same reason: a fork whose switch could not be read is
// reported as unknown, never assumed off.
type SecuritySettings struct {
	SecretScanning    string
	PushProtection    string
	DependabotAlerts  *bool
	DependabotUpdates bool
	ActionsEnabled    *bool
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
	return triStateLabel(s.DependabotAlerts)
}

// ActionsState returns a stable string representation of the
// repository-level GitHub Actions switch for reports: "enabled",
// "disabled", or "unknown".
func (s SecuritySettings) ActionsState() string {
	return triStateLabel(s.ActionsEnabled)
}

// IsActionsDisabled reports whether GitHub Actions are known to be off.
// Unknown is not disabled: a fork whose switch could not be read is still
// enforced by phase 3 rather than assumed compliant.
func (s SecuritySettings) IsActionsDisabled() bool {
	return s.ActionsEnabled != nil && !*s.ActionsEnabled
}

// triStateLabel renders a nil-means-unknown boolean with the stable
// SecurityState tokens.
func triStateLabel(value *bool) string {
	if value == nil {
		return SecurityStateUnknown
	}
	if *value {
		return SecurityStateEnabled
	}
	return SecurityStateDisabled
}
