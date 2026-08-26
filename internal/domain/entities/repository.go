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
	AllowUpdateBranch   bool
	HasWiki             bool
	HasProjects         bool
}

// QualifiedName returns the `owner/name` slug that identifies this repo
// across owners. Once an audit spans more than one owner a bare name is
// ambiguous — two organizations can each host a `guide` — so reports,
// snapshots, and diffs key on this instead. Falls back to the bare name
// when the owner is unknown.
func (r Repository) QualifiedName() string {
	if r.Owner == "" {
		return r.Name
	}
	return r.Owner + "/" + r.Name
}

// IsForkOrArchived reports whether this repo is effectively excluded from
// Dependabot / secret scanning enforcement because upstream syncs wipe the
// state (fork) or the repo is frozen (archived).
func (r Repository) IsForkOrArchived() bool {
	return r.Fork || r.Archived
}
