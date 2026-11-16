package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	forgeconfig "github.com/rios0rios0/gitforge/v4/pkg/config/domain/entities"
	"go.yaml.in/yaml/v3"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

const (
	// defaultBranch is the branch every refresh force-pushes, so repeated
	// runs keep updating one pull request.
	defaultBranch = "chore/config-and-docs-refresh"
	// defaultCommitMessage is the refresh commit, and the pull request's title.
	defaultCommitMessage = "chore(refresh): refreshed configuration and documentation"
	// defaultSource opens the pull request description.
	defaultSource = "Automated refresh by config-automation."
	// azureDevOpsHost prefixes an organization given as a URL.
	azureDevOpsHost = "https://dev.azure.com/"
)

// errNoProviders rejects a configuration that names nothing to refresh.
var errNoProviders = errors.New("the configuration lists no providers")

// configuration is the YAML file a refresh runs from. Its `providers` list
// has the shape the other fleet automations read, plus an opaque credential
// label per entry that discovery echoes into its batches.
type configuration struct {
	Providers []providerConfiguration `yaml:"providers"`
	Refresh   refreshConfiguration    `yaml:"refresh"`
}

// providerConfiguration is one provider and the organizations its token reaches.
type providerConfiguration struct {
	Type          string   `yaml:"type"`
	Token         string   `yaml:"token"`
	Organizations []string `yaml:"organizations"`
	Credential    string   `yaml:"credential"`
}

// refreshConfiguration holds what the refresh does in every repository.
type refreshConfiguration struct {
	Branch           string                        `yaml:"branch"`
	Exclude          []string                      `yaml:"exclude"`
	InstructionFiles instructionFilesConfiguration `yaml:"instruction_files"`
	GuideURL         *string                       `yaml:"guide_url"`
	Git              gitConfiguration              `yaml:"git"`
	PullRequest      pullRequestConfiguration      `yaml:"pull_request"`
}

// instructionFilesConfiguration overrides the provider's defaults; an
// absent value keeps the default.
type instructionFilesConfiguration struct {
	Agents  *bool `yaml:"agents"`
	Claude  *bool `yaml:"claude"`
	Copilot *bool `yaml:"copilot"`
}

// gitConfiguration is the identity of the refresh commits; `${VAR}` expands.
type gitConfiguration struct {
	UserName  string `yaml:"user_name"`
	UserEmail string `yaml:"user_email"`
}

// pullRequestConfiguration is the pull request's title and the line that
// opens its description.
type pullRequestConfiguration struct {
	Title  string `yaml:"title"`
	Source string `yaml:"source"`
}

// loadConfiguration reads and validates the configuration file.
func loadConfiguration(file string) (configuration, error) {
	content, err := os.ReadFile(file) // #nosec G304 -- the operator names the configuration file
	if err != nil {
		return configuration{}, fmt.Errorf("reading %s: %w", file, err)
	}
	var loaded configuration
	if err = yaml.Unmarshal(content, &loaded); err != nil {
		return configuration{}, fmt.Errorf("parsing %s: %w", file, err)
	}
	if _, err = loaded.provider(); err != nil {
		return configuration{}, fmt.Errorf("%s: %w", file, err)
	}
	for index, provider := range loaded.Providers {
		if len(provider.Organizations) == 0 {
			return configuration{}, fmt.Errorf("%s: provider %d lists no organizations", file, index+1)
		}
	}
	return loaded, nil
}

// provider returns the one provider every entry names. A run talks to one
// platform, so mixing providers in one file is rejected.
func (c configuration) provider() (entities.Provider, error) {
	if len(c.Providers) == 0 {
		return "", errNoProviders
	}
	provider, err := entities.ParseProvider(c.Providers[0].Type)
	if err != nil {
		return "", err
	}
	for _, entry := range c.Providers[1:] {
		if entities.Provider(entry.Type) != provider {
			return "", fmt.Errorf("every provider must be %q, found %q", provider, entry.Type)
		}
	}
	return provider, nil
}

// organizations returns every configured organization with its token and
// credential label, in file order. An Azure DevOps organization may be given
// as its URL; it is kept as the bare name the APIs and URLs use.
func (c configuration) organizations(provider entities.Provider) []configuredOrganization {
	listed := []configuredOrganization{}
	for _, entry := range c.Providers {
		for _, organization := range entry.Organizations {
			if provider == entities.ProviderAzureDevOps {
				organization = strings.Trim(strings.TrimPrefix(organization, azureDevOpsHost), "/")
			}
			listed = append(listed, configuredOrganization{
				name: organization, credential: entry.Credential, token: forgeconfig.ResolveToken(entry.Token),
			})
		}
	}
	return listed
}

// referencedTokenVariables returns the names of the variables the providers'
// tokens read, `${NAME}`, which the agent must not inherit either.
func (c configuration) referencedTokenVariables() []string {
	reference := regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	names := []string{}
	for _, entry := range c.Providers {
		for _, match := range reference.FindAllStringSubmatch(entry.Token, -1) {
			names = append(names, match[1])
		}
	}
	return names
}

// instructionFiles applies the configuration's overrides to the provider's
// defaults.
func (c configuration) instructionFiles(provider entities.Provider) entities.InstructionFiles {
	files := provider.DefaultInstructionFiles()
	overrides := c.Refresh.InstructionFiles
	applyOverride(&files.Agents, overrides.Agents)
	applyOverride(&files.Claude, overrides.Claude)
	applyOverride(&files.Copilot, overrides.Copilot)
	return files
}

// guideURL returns the guide whose links the refresh keeps valid; an
// explicit empty value turns that rule off.
func (c configuration) guideURL() string {
	if c.Refresh.GuideURL == nil {
		return entities.DefaultGuideURL
	}
	return *c.Refresh.GuideURL
}

// branch returns the refresh branch.
func (c configuration) branch() string {
	return valueOr(c.Refresh.Branch, defaultBranch)
}

// identity returns the author of the refresh commits.
func (c configuration) identity() entities.GitIdentity {
	return entities.GitIdentity{
		Name: os.ExpandEnv(c.Refresh.Git.UserName), Email: os.ExpandEnv(c.Refresh.Git.UserEmail),
	}
}

// pullRequestTitle returns the pull request's title.
func (c configuration) pullRequestTitle() string {
	return valueOr(c.Refresh.PullRequest.Title, defaultCommitMessage)
}

// pullRequestBody returns the pull request's description for the
// instruction files the refresh keeps.
func (c configuration) pullRequestBody(files entities.InstructionFiles) string {
	layout := "`AGENTS.md` is the instruction file and every `CLAUDE.md` holds only the `@AGENTS.md` " +
		"import. The automation moves a legacy `CLAUDE.md` into that layout before Claude Code runs, " +
		"committing a rename on its own so history follows the file."
	if !files.Claude {
		layout = "`AGENTS.md` is the instruction file and this repository keeps no `CLAUDE.md`. The " +
			"automation moves a legacy `CLAUDE.md` into `AGENTS.md` before Claude Code runs, committing a " +
			"rename on its own so history follows the file."
	}
	if !files.Copilot {
		layout += " It also removes GitHub Copilot's instructions and code-review skill, which this " +
			"repository does not keep."
	}
	return valueOr(c.Refresh.PullRequest.Source, defaultSource) + "\n\n" + layout + "\n\n" +
		"Claude Code ran a read-only prompt audit, then reviewed `AGENTS.md`, the other discovered " +
		"instruction files, skills and their references, agents, custom commands, `README.md`, and " +
		"`CONTRIBUTING.md` against the current code, updated whichever drifted, and recorded the change " +
		"using the repository's changelog convention.\n\n" +
		"Review the diff carefully --- this PR was generated by Claude Code and may contain inaccuracies."
}

// configuredOrganization is one organization with what reaches it.
type configuredOrganization struct {
	name       string
	credential string
	token      string
}

// applyOverride replaces target with override when one is set.
func applyOverride(target *bool, override *bool) {
	if override != nil {
		*target = *override
	}
}

// valueOr returns value, or fallback when value is empty.
func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
