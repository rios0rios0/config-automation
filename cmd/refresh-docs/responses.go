package main

import (
	"fmt"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

// adoMatrixKeyFormat names an Azure Pipelines matrix leg; keys cannot hold
// spaces, so the organization travels in each target instead.
const adoMatrixKeyFormat = "batch_%02d"

// refreshScopeResponse is the manifest `refresh-docs scope` prints.
type refreshScopeResponse struct {
	Paths        []string `json:"paths"`
	Existing     []string `json:"existing"`
	Creatable    []string `json:"creatable"`
	Managed      []string `json:"managed"`
	Removed      []string `json:"removed"`
	AllowedTools []string `json:"allowed_tools"`
	Skipped      []string `json:"skipped"`
}

// agentsLayoutResponse is the report `refresh-docs layout` prints.
type agentsLayoutResponse struct {
	Renamed    []string `json:"renamed"`
	Merged     []string `json:"merged"`
	Pointed    []string `json:"pointed"`
	Rebannered []string `json:"rebannered"`
	Dangling   []string `json:"dangling"`
	Skipped    []string `json:"skipped"`
	Removed    []string `json:"removed"`
	Changed    bool     `json:"changed"`
	Note       string   `json:"note"`
}

// refreshTargetResponse is one repository of a batch, as `run` reads it back.
// It carries identity only: the state discovery filtered on stays behind.
type refreshTargetResponse struct {
	Organization  string `json:"organization"`
	Project       string `json:"project,omitempty"`
	Name          string `json:"name"`
	ID            string `json:"id,omitempty"`
	DefaultBranch string `json:"default_branch"`
}

// githubBatchResponse is one GitHub Actions matrix leg.
type githubBatchResponse struct {
	Name         string                  `json:"name"`
	Organization string                  `json:"organization"`
	Credential   string                  `json:"credential"`
	Repos        []refreshTargetResponse `json:"repos"`
}

// azureBatchResponse is one Azure Pipelines matrix leg. Matrix values must
// be strings, so the repositories are a JSON document in one.
type azureBatchResponse struct {
	Repos string `json:"repos"`
}

// mapToRefreshScopeResponse maps a refresh manifest to its JSON shape.
func mapToRefreshScopeResponse(scope entities.RefreshScope) refreshScopeResponse {
	return refreshScopeResponse{
		Paths:        scope.Paths,
		Existing:     scope.Existing,
		Creatable:    scope.Creatable,
		Managed:      scope.Managed,
		Removed:      nonNil(scope.Removed),
		AllowedTools: scope.AllowedTools,
		Skipped:      scope.Skipped,
	}
}

// mapToAgentsLayoutResponse maps a layout report to its JSON shape.
func mapToAgentsLayoutResponse(report entities.AgentsLayoutReport) agentsLayoutResponse {
	return agentsLayoutResponse{
		Renamed:    report.Renamed,
		Merged:     report.Merged,
		Pointed:    report.Pointed,
		Rebannered: report.Rebannered,
		Dangling:   report.Dangling,
		Skipped:    report.Skipped,
		Removed:    nonNil(report.Removed),
		Changed:    report.Changed,
		Note:       report.Note,
	}
}

// mapToRefreshTargetResponses maps targets to their batch entries.
func mapToRefreshTargetResponses(targets []entities.RefreshTarget) []refreshTargetResponse {
	responses := make([]refreshTargetResponse, 0, len(targets))
	for _, target := range targets {
		responses = append(responses, refreshTargetResponse{
			Organization: target.Organization, Project: target.Project, Name: target.Name,
			ID: target.ID, DefaultBranch: target.DefaultBranch,
		})
	}
	return responses
}

// mapToRefreshTargets maps batch entries back to targets.
func mapToRefreshTargets(responses []refreshTargetResponse) []entities.RefreshTarget {
	targets := make([]entities.RefreshTarget, 0, len(responses))
	for _, response := range responses {
		targets = append(targets, entities.RefreshTarget{
			Organization: response.Organization, Project: response.Project, Name: response.Name,
			ID: response.ID, DefaultBranch: response.DefaultBranch,
		})
	}
	return targets
}

// mapToGithubMatrix maps batches to the GitHub Actions matrix.
func mapToGithubMatrix(batches []entities.RefreshBatch) []githubBatchResponse {
	matrix := make([]githubBatchResponse, 0, len(batches))
	for _, batch := range batches {
		matrix = append(matrix, githubBatchResponse{
			Name: batch.Name, Organization: batch.Organization, Credential: batch.Credential,
			Repos: mapToRefreshTargetResponses(batch.Targets),
		})
	}
	return matrix
}

// mapToAzureMatrix maps batches to the Azure Pipelines matrix, numbering the
// legs across every organization so each key is unique.
func mapToAzureMatrix(batches []entities.RefreshBatch, encode func(any) (string, error)) (
	map[string]azureBatchResponse, error,
) {
	matrix := map[string]azureBatchResponse{}
	for index, batch := range batches {
		repos, err := encode(mapToRefreshTargetResponses(batch.Targets))
		if err != nil {
			return nil, err
		}
		matrix[fmt.Sprintf(adoMatrixKeyFormat, index+1)] = azureBatchResponse{Repos: repos}
	}
	return matrix, nil
}

// nonNil turns a nil list into an empty one, which encodes as [].
func nonNil(paths []string) []string {
	if paths == nil {
		return []string{}
	}
	return paths
}
