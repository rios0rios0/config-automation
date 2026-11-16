package entities

import (
	"fmt"
	"slices"
	"strings"
)

const (
	// batchNumberWidth pads a batch number so batch names sort in order.
	batchNumberWidth = 2
	// workKeySeparator joins the parts of a target's scratch directory name.
	workKeySeparator = "__"
)

// RefreshTarget is one repository a refresh visits, in the same shape for
// every provider: on GitHub the organization is the owner and there is no
// project; on Azure DevOps there are both. The flags carry what discovery
// learned, so the policy of which repositories to skip stays in the domain.
type RefreshTarget struct {
	Organization  string
	Project       string
	Name          string
	ID            string
	DefaultBranch string
	Fork          bool
	Archived      bool
	Disabled      bool
}

// RefreshTargetReference names one repository asked for by name.
type RefreshTargetReference struct {
	Organization string
	Project      string
	Name         string
}

// QualifiedName identifies the target in reports: `owner/name` on GitHub and
// `project/name` on Azure DevOps.
func (t RefreshTarget) QualifiedName() string {
	if t.Project != "" {
		return t.Project + "/" + t.Name
	}
	return t.Organization + "/" + t.Name
}

// WorkKey names the target's scratch clone. Every part of its identity is
// kept, so two organizations or projects holding a repository of the same
// name never share a directory.
func (t RefreshTarget) WorkKey() string {
	parts := []string{}
	for _, part := range []string{t.Organization, t.Project, t.Name} {
		if part != "" {
			parts = append(parts, strings.NewReplacer("/", "_", "\\", "_").Replace(part))
		}
	}
	return strings.Join(parts, workKeySeparator)
}

// ExclusionReasons lists why the refresh leaves the target out, in the order
// a refusal names them: a fork's guidance is its upstream's, an archived or
// disabled repository cannot take a pull request, and an empty one has no
// default branch to refresh. None means the target is refreshed.
func (t RefreshTarget) ExclusionReasons() []string {
	reasons := []string{}
	if t.Fork {
		reasons = append(reasons, "a fork")
	}
	if t.Archived {
		reasons = append(reasons, "archived")
	}
	if t.Disabled {
		reasons = append(reasons, "disabled")
	}
	if t.DefaultBranch == "" {
		reasons = append(reasons, "empty")
	}
	return reasons
}

// RefreshBatch is one matrix leg: targets of a single organization that one
// job refreshes in sequence with that organization's credential. Credential
// is an opaque label the CI maps to its secret store.
type RefreshBatch struct {
	Name         string
	Organization string
	Credential   string
	Targets      []RefreshTarget
}

// ChunkRefreshTargets sorts one organization's targets by qualified name and
// splits them into batches of at most size, named "<organization> batch_NN".
func ChunkRefreshTargets(organization, credential string, targets []RefreshTarget, size int) []RefreshBatch {
	ordered := slices.Clone(targets)
	slices.SortFunc(ordered, func(left, right RefreshTarget) int {
		return strings.Compare(left.QualifiedName(), right.QualifiedName())
	})
	batches := []RefreshBatch{}
	for start := 0; start < len(ordered); start += size {
		end := min(start+size, len(ordered))
		batches = append(batches, RefreshBatch{
			Name:         fmt.Sprintf("%s batch_%0*d", organization, batchNumberWidth, len(batches)+1),
			Organization: organization,
			Credential:   credential,
			Targets:      ordered[start:end],
		})
	}
	return batches
}
