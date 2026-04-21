package entities

// BranchProtection is the classic branch protection state for a single
// branch (we only care about the default branch).
//
// Available=false means the repo's plan or permissions do not expose the
// branch protection endpoints (e.g., private repos on GitHub Free). In that
// case Enabled is always false and the compliance check skips this repo.
// Enabled=false with Available=true means the branch exists but protection
// has not been configured yet.
type BranchProtection struct {
	Available              bool
	Enabled                bool
	ReviewCount            int
	DismissStaleReviews    bool
	RequireCodeOwners      bool
	EnforceAdmins          bool
	LinearHistory          bool
	AllowForcePushes       bool
	AllowDeletions         bool
	ConversationResolution bool
	Signatures             *bool
}

// SignaturesState mirrors the SecuritySettings helper: tri-state.
func (b BranchProtection) SignaturesState() string {
	if b.Signatures == nil {
		return "unknown"
	}
	if *b.Signatures {
		return "enabled"
	}
	return "disabled"
}
