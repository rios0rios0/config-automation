package entities

// Repository is the domain entity for a GitHub repository we audit and harden.
// Framework-agnostic: no tags, no dependency on go-github types.
type Repository struct {
	Name          string
	Owner         string
	Visibility    string
	Private       bool
	Fork          bool
	Archived      bool
	DefaultBranch string
	Settings      RepositorySettings
}

// RepositorySettings holds the repo-level toggles that phase 2 enforces.
// The zero value matches GitHub's default "nothing configured yet" state,
// but the compliance policy lives in DesiredRepoSettings.
type RepositorySettings struct {
	DeleteBranchOnMerge bool
	AllowAutoMerge      bool
	AllowSquashMerge    bool
	AllowRebaseMerge    bool
	AllowMergeCommit    bool
	HasWiki             bool
	HasProjects         bool
}

// IsForkOrArchived reports whether this repo is effectively excluded from
// Dependabot / secret scanning enforcement because upstream syncs wipe the
// state (fork) or the repo is frozen (archived).
func (r Repository) IsForkOrArchived() bool {
	return r.Fork || r.Archived
}
