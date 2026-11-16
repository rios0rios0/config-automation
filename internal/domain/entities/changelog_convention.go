package entities

const (
	// ChangelogFile is the Keep a Changelog file at a repository's root.
	ChangelogFile = "CHANGELOG.md"
	// ChlogFragmentDirectory holds the pending chlog fragments.
	ChlogFragmentDirectory = ".changes/unreleased"
	// ChlogDirectory holds chlog's released and pending fragments.
	ChlogDirectory = ".changes"
)

// ChangelogMode is the changelog convention a repository follows.
type ChangelogMode string

const (
	// ChangelogChlog means CHANGELOG.md is generated from chlog fragments.
	ChangelogChlog ChangelogMode = "chlog"
	// ChangelogFileMode means entries are written into CHANGELOG.md directly.
	ChangelogFileMode ChangelogMode = "changelog"
	// ChangelogNone means the repository records no changelog.
	ChangelogNone ChangelogMode = "none"
)

// changelogTraits are what one convention grants Claude, tells it, and
// stages beside the documentation.
type changelogTraits struct {
	grant string
	note  string
	paths []string
}

// changelogConventions maps each convention to its traits. Granting only the
// tool the applicable convention needs makes the other one impossible, not
// merely discouraged: in a chlog repository the grant is the `chlog new`
// command itself, never write access to the fragment directory, so a
// fragment's file name and time come from the tool's clock.
func changelogConventions() map[ChangelogMode]changelogTraits {
	return map[ChangelogMode]changelogTraits{
		ChangelogChlog: {
			grant: "Bash(chlog new:*)",
			note: "This repository uses chlog. `CHANGELOG.md` is generated from the fragments under " +
				"`.changes/unreleased/`, and you have been granted no tool that can edit it or write into " +
				"that directory by hand. Record the refresh by running `chlog new` from the repository root, " +
				"exactly as described under \"If the repository uses chlog\".",
			paths: []string{ChlogFragmentDirectory},
		},
		ChangelogFileMode: {
			grant: "Edit(/" + ChangelogFile + ")",
			note: "This repository does not use chlog. Record the refresh directly in `CHANGELOG.md` under " +
				"`[Unreleased]`, exactly as described under \"If the repository does not use chlog\".",
			paths: []string{ChangelogFile},
		},
		ChangelogNone: {
			note: "This repository has neither chlog nor a `CHANGELOG.md`. Do not record the refresh " +
				"anywhere --- just edit whichever in-scope files drifted.",
			paths: []string{},
		},
	}
}

// DetectChangelogMode applies the predicate of the shared pipelines'
// changelog gate, so the two cannot disagree: a chlog config file or a
// pending-fragment directory means chlog, since chlog falls back to built-in
// defaults without a config; otherwise an existing CHANGELOG.md means the
// file itself.
func DetectChangelogMode(hasChlogConfig, hasFragmentDirectory, hasChangelog bool) ChangelogMode {
	if hasChlogConfig || hasFragmentDirectory {
		return ChangelogChlog
	}
	if hasChangelog {
		return ChangelogFileMode
	}
	return ChangelogNone
}

// ChlogConfigFiles are the file names chlog reads its configuration from.
func ChlogConfigFiles() []string {
	return []string{".chlog.yaml", ".chlog.yml"}
}

// Grant returns the tool rule the convention grants Claude, or "" for none.
func (m ChangelogMode) Grant() string {
	return changelogConventions()[m].grant
}

// Note returns what the per-repository prompt says about the convention.
func (m ChangelogMode) Note() string {
	return changelogConventions()[m].note
}

// StagePaths returns where the convention's entry lands. It is staged with
// the drifted documentation but never counts as drift itself, so a stray
// changelog-only edit cannot open a pull request.
func (m ChangelogMode) StagePaths() []string {
	return changelogConventions()[m].paths
}
