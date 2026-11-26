package entities

// IgnoreFile is the root ignore file whose shared block the refresh keeps in
// step with the generator of rios0rios0/pipelines.
const IgnoreFile = ".gitignore"

// IgnoreRulesOutcome is what the host did to a repository's root `.gitignore`.
type IgnoreRulesOutcome string

const (
	// IgnoreRulesOff means the run named no generator, so `.gitignore` was
	// not looked at.
	IgnoreRulesOff IgnoreRulesOutcome = "off"
	// IgnoreRulesCurrent means the shared block was already current.
	IgnoreRulesCurrent IgnoreRulesOutcome = "current"
	// IgnoreRulesCreated means the repository had no `.gitignore`, and the
	// generator created one holding the shared block.
	IgnoreRulesCreated IgnoreRulesOutcome = "created"
	// IgnoreRulesUpdated means the generator wrote or rewrote the shared block
	// of an existing `.gitignore`.
	IgnoreRulesUpdated IgnoreRulesOutcome = "updated"
	// IgnoreRulesSkipped means `.gitignore` is a symlink or not a regular
	// file, which the host never writes through.
	IgnoreRulesSkipped IgnoreRulesOutcome = "skipped"
)

// IgnoreRulesReport is the outcome of one repository's ignore-rules pass,
// with what the generator printed.
type IgnoreRulesReport struct {
	Outcome IgnoreRulesOutcome
	Output  string
}

// Changed reports whether the host changed `.gitignore`, which makes the
// file part of the refresh's drift.
func (r IgnoreRulesReport) Changed() bool {
	return r.Outcome == IgnoreRulesCreated || r.Outcome == IgnoreRulesUpdated
}

// DescribeIgnoreRules renders a report for the refresh prompt, or "" when
// the host did not look at `.gitignore`, which leaves the prompt without
// that section.
func DescribeIgnoreRules(report IgnoreRulesReport) string {
	return ignoreRulesNotes()[report.Outcome]
}

// ignoreRulesNotes maps each outcome to what the prompt says about it.
func ignoreRulesNotes() map[IgnoreRulesOutcome]string {
	block := ": the delimited block `make gitignore` generates, listing the files the CI pipeline " +
		"writes into this working tree."
	return map[IgnoreRulesOutcome]string{
		IgnoreRulesOff:     "",
		IgnoreRulesCurrent: "No changes: `.gitignore` already carries the current shared ignore rules.",
		IgnoreRulesCreated: "- Created `.gitignore` with the shared ignore rules of `rios0rios0/pipelines`" +
			block + " This is part of the refresh: record it under `Added`, for example " +
			"`added \\`.gitignore\\` with the shared ignore rules of \\`rios0rios0/pipelines\\``.",
		IgnoreRulesUpdated: "- Updated the shared ignore rules of `rios0rios0/pipelines` in `.gitignore`" +
			block + " Every line outside the block is unchanged. This is part of the refresh: record it " +
			"under `Changed`, for example " +
			"`updated the shared ignore rules of \\`rios0rios0/pipelines\\` in \\`.gitignore\\``.",
		IgnoreRulesSkipped: "- Left `.gitignore` unchanged because it is not a regular file (a symlink, " +
			"a directory, or another file type); report it for human review.",
	}
}
