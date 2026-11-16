package entities

import (
	"fmt"
	"net/url"
	"slices"
)

// Provider names a Git hosting platform the refresh runs against. The values
// match the provider names of the library that talks to their APIs.
type Provider string

const (
	// ProviderGitHub is github.com.
	ProviderGitHub Provider = "github"
	// ProviderAzureDevOps is Azure DevOps Services, dev.azure.com.
	ProviderAzureDevOps Provider = "azuredevops"
)

// providerTraits are the facts about one provider that the refresh needs:
// where git talks to it, which user name carries a token over HTTPS, how a
// repository's clone URL is spelled, and which instruction files its
// repositories keep unless told otherwise.
type providerTraits struct {
	gitHost          string
	gitUsername      string
	cloneURL         func(target RefreshTarget) string
	pullRequestURL   func(target RefreshTarget, number int) string
	instructionFiles InstructionFiles
}

// providers maps every supported provider to its traits.
func providers() map[Provider]providerTraits {
	return map[Provider]providerTraits{
		ProviderGitHub: {
			gitHost:     "github.com",
			gitUsername: "x-access-token",
			cloneURL: func(target RefreshTarget) string {
				return "https://github.com/" + url.PathEscape(target.Organization) + "/" +
					url.PathEscape(target.Name) + ".git"
			},
			pullRequestURL: func(target RefreshTarget, number int) string {
				return fmt.Sprintf("https://github.com/%s/%s/pull/%d",
					url.PathEscape(target.Organization), url.PathEscape(target.Name), number)
			},
			instructionFiles: InstructionFiles{Agents: true, Claude: true, Copilot: true},
		},
		// Copilot's repository features (custom instructions, code-review
		// skills) are GitHub's, so an Azure DevOps repository keeps no Copilot
		// files unless a configuration asks for them.
		ProviderAzureDevOps: {
			gitHost:     "dev.azure.com",
			gitUsername: "pat",
			cloneURL: func(target RefreshTarget) string {
				// Project and repository names may hold spaces, which git's
				// HTTPS transport rejects unencoded, so each segment is escaped.
				return "https://dev.azure.com/" + url.PathEscape(target.Organization) + "/" +
					url.PathEscape(target.Project) + "/_git/" + url.PathEscape(target.Name)
			},
			pullRequestURL: func(target RefreshTarget, number int) string {
				return fmt.Sprintf("https://dev.azure.com/%s/%s/_git/%s/pullrequest/%d",
					url.PathEscape(target.Organization), url.PathEscape(target.Project),
					url.PathEscape(target.Name), number)
			},
			instructionFiles: InstructionFiles{Agents: true, Claude: true, Copilot: false},
		},
	}
}

// ParseProvider returns the provider name, or an error naming the supported
// ones.
func ParseProvider(name string) (Provider, error) {
	provider := Provider(name)
	if _, found := providers()[provider]; !found {
		return "", fmt.Errorf("unsupported provider %q (supported: %v)", name, SupportedProviders())
	}
	return provider, nil
}

// SupportedProviders lists every provider name in sorted order.
func SupportedProviders() []string {
	names := []string{}
	for provider := range providers() {
		names = append(names, string(provider))
	}
	slices.Sort(names)
	return names
}

// CloneURL returns the HTTPS URL a target is cloned from. It never carries a
// credential: git asks a credential helper for one when the host requests it.
func (p Provider) CloneURL(target RefreshTarget) string {
	return providers()[p].cloneURL(target)
}

// PullRequestURL returns the web page of a target's pull request.
func (p Provider) PullRequestURL(target RefreshTarget, number int) string {
	return providers()[p].pullRequestURL(target, number)
}

// GitCredential pairs a token with the host it is for and the user name the
// provider expects beside it.
func (p Provider) GitCredential(token string) GitCredential {
	traits := providers()[p]
	return GitCredential{Host: traits.gitHost, Username: traits.gitUsername, Token: token}
}

// DefaultInstructionFiles returns the instruction files a provider's
// repositories keep when no configuration says otherwise.
func (p Provider) DefaultInstructionFiles() InstructionFiles {
	return providers()[p].instructionFiles
}

// GitCredential is what git sends to one host when it asks for credentials.
type GitCredential struct {
	Host     string
	Username string
	Token    string
}

// GitIdentity is the author and committer of the commits a refresh makes.
// The zero value leaves git's own configuration in charge.
type GitIdentity struct {
	Name  string
	Email string
}
