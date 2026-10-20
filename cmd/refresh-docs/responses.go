package main

import "github.com/rios0rios0/config-automation/internal/domain/entities"

// refreshScopeResponse is the manifest `refresh-docs scope` prints, in the
// shape the refresh workflows read with jq.
type refreshScopeResponse struct {
	Paths        []string `json:"paths"`
	Existing     []string `json:"existing"`
	Creatable    []string `json:"creatable"`
	Managed      []string `json:"managed"`
	HasGuidance  bool     `json:"has_guidance"`
	AllowedTools []string `json:"allowed_tools"`
	Skipped      []string `json:"skipped"`
}

// agentsLayoutResponse is the report `refresh-docs layout` prints, in the
// shape the refresh workflows read with jq.
type agentsLayoutResponse struct {
	Renamed    []string `json:"renamed"`
	Merged     []string `json:"merged"`
	Pointed    []string `json:"pointed"`
	Rebannered []string `json:"rebannered"`
	Dangling   []string `json:"dangling"`
	Skipped    []string `json:"skipped"`
	Changed    bool     `json:"changed"`
	Note       string   `json:"note"`
}

// mapToRefreshScopeResponse maps a refresh manifest to its JSON shape.
func mapToRefreshScopeResponse(scope entities.RefreshScope) refreshScopeResponse {
	return refreshScopeResponse{
		Paths:        scope.Paths,
		Existing:     scope.Existing,
		Creatable:    scope.Creatable,
		Managed:      scope.Managed,
		HasGuidance:  scope.HasGuidance,
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
		Changed:    report.Changed,
		Note:       report.Note,
	}
}
