package entities

// PullRequest is an open pull request on a target repository.
type PullRequest struct {
	Number int
	URL    string
}

// PullRequestDraft is what a refresh asks the provider to open.
type PullRequestDraft struct {
	SourceBranch string
	TargetBranch string
	Title        string
	Description  string
}
