package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/config-automation/internal/domain/commands"
	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

const (
	phaseAudit             = 1
	phaseApplyRepo         = 2
	phaseApplySecurity     = 3
	phaseApplyProtection   = 4
	phaseReport            = 5
	exitUsageError         = 2
	secretColumnWidth      = 7
	mergeMethodColumnWidth = 8
	tableWidth             = 178
	repoColumnWidth        = 48
)

// ownerSeparator splits the HARDEN_OWNER environment variable into the
// list of owners every phase iterates over.
const ownerSeparator = ","

// defaultOwner is the owner used when HARDEN_OWNER is unset or lists
// nothing usable.
const defaultOwner = "rios0rios0"

// Logrus structured-logging field keys reused across phases.
const (
	fieldRepo    = "repo"
	fieldOwner   = "owner"
	fieldApplied = "applied"
	fieldAction  = "action"
	fieldPhase   = "phase"
)

// ownerAudits pairs an owner with the audits collected for it. Phases 2-4
// mutate through per-owner command inputs, so the grouping has to survive
// the audit step even though phase 1 and the snapshots consume one flat
// list.
type ownerAudits struct {
	Owner  string
	Audits []entities.AuditResult
}

func main() {
	var (
		phase              int
		repoFilter         string
		listJSON           bool
		dryRun             bool
		failOnNonCompliant bool
	)

	flag.IntVar(
		&phase,
		"phase",
		0,
		"audit/apply phase (1-5); 0 means 'all audit phases when --dry-run, otherwise required'",
	)
	flag.StringVar(&repoFilter, "repo", "", "target a single repository by name, in every configured owner")
	flag.BoolVar(
		&listJSON,
		"list-json",
		false,
		"emit a JSON array of non-fork non-archived repos for the config-and-docs refresh matrix",
	)
	flag.BoolVar(&dryRun, "dry-run", false, "run phases 1-4 without mutating anything")
	flag.BoolVar(
		&failOnNonCompliant,
		"fail-on-noncompliant",
		false,
		"exit 1 when phase 1 detects any non-compliant repo",
	)
	flag.Parse()

	owners := parseOwners(os.Getenv("HARDEN_OWNER"))

	set := injectCommands()
	ctx := context.Background()

	switch {
	case listJSON:
		runListJSON(ctx, set, owners)
	case dryRun:
		runDryRun(ctx, set, owners, repoFilter)
	case phase == phaseAudit:
		runPhase1(ctx, set, owners, repoFilter, failOnNonCompliant)
	case phase == phaseApplyRepo:
		runPhase2(ctx, set, owners, repoFilter)
	case phase == phaseApplySecurity:
		runPhase3(ctx, set, owners, repoFilter)
	case phase == phaseApplyProtection:
		runPhase4(ctx, set, owners, repoFilter)
	case phase == phaseReport:
		runPhase5(ctx, set, owners)
	default:
		logger.Error("must specify --phase 1..5, --list-json, or --dry-run")
		flag.Usage()
		os.Exit(exitUsageError)
	}
}

// parseOwners splits the comma-separated HARDEN_OWNER value into the
// owners every phase walks. Blank entries are dropped and duplicates are
// collapsed (a repeated owner would audit and mutate the same repos
// twice), while the caller's ordering is preserved so the audit table
// and the --list-json matrix stay stable across runs. An empty result
// falls back to defaultOwner, matching the single-owner behaviour this
// replaced.
func parseOwners(raw string) []string {
	seen := make(map[string]struct{})
	owners := make([]string, 0, 1)

	for part := range strings.SplitSeq(raw, ownerSeparator) {
		owner := strings.TrimSpace(part)
		if owner == "" {
			continue
		}
		if _, duplicate := seen[owner]; duplicate {
			continue
		}
		seen[owner] = struct{}{}
		owners = append(owners, owner)
	}

	if len(owners) == 0 {
		return []string{defaultOwner}
	}
	return owners
}

// runListJSON emits the JSON array consumed by the config-and-docs
// refresh matrix and the release-reconcile script. Every entry carries
// its `owner`: once the list spans several owners, a bare name no longer
// identifies a repository, and the consumers clone `owner/name`.
func runListJSON(ctx context.Context, set commandSet, owners []string) {
	payload := make([]map[string]string, 0)

	for _, owner := range owners {
		var result []entities.Repository
		set.ListTargets.Execute(
			ctx,
			commands.ListTargetRepositoriesInput{Owner: owner},
			commands.ListTargetRepositoriesListeners{
				OnSuccess: func(repos []entities.Repository) {
					result = repos
				},
				OnError: func(err error) {
					logger.WithError(err).WithField("owner", owner).Fatal("listing target repos")
				},
			},
		)

		for _, r := range result {
			payload = append(payload, map[string]string{
				// Taken from the loop variable rather than r.Owner so the
				// value is always the owner actually queried, even if a
				// listing response omits the owner object.
				"owner":          owner,
				"name":           r.Name,
				"default_branch": r.DefaultBranch,
			})
		}
	}

	encoder := json.NewEncoder(os.Stdout)
	if err := encoder.Encode(payload); err != nil {
		logger.WithError(err).Fatal("encoding JSON")
	}
}

// runPhase1 audits every repo of every owner, prints the table, and
// writes the before snapshot. When --fail-on-noncompliant is set,
// non-zero exit on any issue.
func runPhase1(ctx context.Context, set commandSet, owners []string, repoFilter string, failOnNonCompliant bool) {
	audits := flattenAudits(executeAuditPerOwner(ctx, set, owners, repoFilter))
	printAuditTable(audits)
	saveSnapshot(audits, auditBeforePath())

	nonCompliant := countNonCompliant(audits)
	if failOnNonCompliant && nonCompliant > 0 {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "Non-compliant repos: %d/%d\n", nonCompliant, len(audits))
		os.Exit(1)
	}
}

func runPhase2(ctx context.Context, set commandSet, owners []string, repoFilter string) {
	grouped := executeAuditPerOwner(ctx, set, owners, repoFilter)
	saveSnapshot(flattenAudits(grouped), auditBeforePath())

	for _, group := range grouped {
		set.ApplyRepo.Execute(ctx, commands.ApplyRepositorySettingsInput{
			Owner:  group.Owner,
			Audits: group.Audits,
		}, commands.ApplyRepositorySettingsListeners{
			OnChange: func(change commands.ApplyRepositorySettingsChange) {
				logger.WithFields(logger.Fields{
					fieldOwner:   group.Owner,
					fieldRepo:    change.RepositoryName,
					fieldApplied: change.Applied,
					"new_wiki":   change.NewSettings.HasWiki,
				}).Info("applied repo settings")
			},
			OnSuccess: func(changed, compliant int) {
				logger.WithFields(logger.Fields{
					fieldOwner:  group.Owner,
					"changed":   changed,
					"compliant": compliant,
				}).Info("phase 2 complete")
			},
			OnError: func(name string, err error) {
				logger.WithError(err).WithFields(logger.Fields{
					fieldOwner: group.Owner,
					fieldRepo:  name,
				}).Error("phase 2 error")
			},
		})
	}
}

// runPhase3 mirrors runPhase4's shape but dispatches a different
// command with a distinct listener type, so the duplication is intrinsic.
//
//nolint:dupl // distinct listener/input types prevent a generic extraction
func runPhase3(ctx context.Context, set commandSet, owners []string, repoFilter string) {
	grouped := executeAuditPerOwner(ctx, set, owners, repoFilter)
	saveSnapshot(flattenAudits(grouped), auditBeforePath())

	for _, group := range grouped {
		set.ApplySecurity.Execute(ctx, commands.ApplySecuritySettingsInput{
			Owner:  group.Owner,
			Audits: group.Audits,
		}, commands.ApplySecuritySettingsListeners{
			OnChange: func(change commands.ApplySecuritySettingsChange) {
				logger.WithFields(logger.Fields{
					fieldOwner:   group.Owner,
					fieldRepo:    change.RepositoryName,
					fieldAction:  change.Action,
					fieldApplied: change.Applied,
				}).Info("applied security setting")
			},
			OnSkip: func(name, reason string) {
				logger.WithFields(logger.Fields{
					fieldOwner: group.Owner,
					fieldRepo:  name,
					"reason":   reason,
				}).Info("skipped")
			},
			OnSuccess: func(secretScanning, dependabot int) {
				logger.WithFields(logger.Fields{
					fieldOwner:        group.Owner,
					"secret_scanning": secretScanning,
					"dependabot":      dependabot,
				}).Info("phase 3 complete")
			},
			OnError: func(name string, err error) {
				logger.WithError(err).WithFields(logger.Fields{
					fieldOwner: group.Owner,
					fieldRepo:  name,
				}).Error("phase 3 error")
			},
		})
	}
}

// runPhase4 mirrors runPhase3's shape but dispatches a different
// command with a distinct listener type, so the duplication is intrinsic.
//
//nolint:dupl // distinct listener/input types prevent a generic extraction
func runPhase4(ctx context.Context, set commandSet, owners []string, repoFilter string) {
	grouped := executeAuditPerOwner(ctx, set, owners, repoFilter)
	saveSnapshot(flattenAudits(grouped), auditBeforePath())

	for _, group := range grouped {
		set.ApplyProtection.Execute(ctx, commands.ApplyBranchProtectionInput{
			Owner:  group.Owner,
			Audits: group.Audits,
		}, commands.ApplyBranchProtectionListeners{
			OnChange: func(change commands.ApplyBranchProtectionChange) {
				logger.WithFields(logger.Fields{
					fieldOwner:   group.Owner,
					fieldRepo:    change.RepositoryName,
					fieldAction:  change.Action,
					fieldApplied: change.Applied,
				}).Info("applied branch protection")
			},
			OnSkip: func(name, reason string) {
				logger.WithFields(logger.Fields{
					fieldOwner: group.Owner,
					fieldRepo:  name,
					"reason":   reason,
				}).Info("skipped")
			},
			OnSuccess: func(changed, skipped int) {
				logger.WithFields(logger.Fields{
					fieldOwner: group.Owner,
					"changed":  changed,
					"skipped":  skipped,
				}).Info("phase 4 complete")
			},
			OnError: func(name string, err error) {
				logger.WithError(err).WithFields(logger.Fields{
					fieldOwner: group.Owner,
					fieldRepo:  name,
				}).Error("phase 4 error")
			},
		})
	}
}

func runPhase5(ctx context.Context, set commandSet, owners []string) {
	before, err := loadSnapshot(auditBeforePath())
	if err != nil {
		logger.WithError(err).Fatal("loading before snapshot; run --phase 1 first")
	}
	after := flattenAudits(executeAuditPerOwner(ctx, set, owners, ""))
	saveSnapshot(after, auditAfterPath())

	set.Report.Execute(commands.ReportComplianceChangesInput{
		Before: before,
		After:  after,
	}, commands.ReportComplianceChangesListeners{
		OnSuccess: func(diffs []commands.ComplianceDiff, reposChanged int) {
			for _, d := range diffs {
				logger.WithFields(logger.Fields{
					fieldRepo: d.Repository,
					"field":   d.Field,
					"before":  d.Before,
					"after":   d.After,
				}).Info("changed")
			}
			logger.WithField("repos_changed", reposChanged).Info("phase 5 complete")
		},
	})
}

func runDryRun(ctx context.Context, set commandSet, owners []string, repoFilter string) {
	grouped := executeAuditPerOwner(ctx, set, owners, repoFilter)
	saveSnapshot(flattenAudits(grouped), auditBeforePath())

	for _, group := range grouped {
		set.ApplyRepo.Execute(ctx, commands.ApplyRepositorySettingsInput{
			Owner:  group.Owner,
			Audits: group.Audits,
			DryRun: true,
		}, commands.ApplyRepositorySettingsListeners{
			OnChange: func(change commands.ApplyRepositorySettingsChange) {
				logger.WithFields(logger.Fields{
					fieldOwner:  group.Owner,
					fieldRepo:   change.RepositoryName,
					fieldPhase:  phaseApplyRepo,
					fieldAction: "repo_settings",
				}).Info("would apply")
			},
			OnSuccess: func(_, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		set.ApplySecurity.Execute(ctx, commands.ApplySecuritySettingsInput{
			Owner:  group.Owner,
			Audits: group.Audits,
			DryRun: true,
		}, commands.ApplySecuritySettingsListeners{
			OnChange: func(change commands.ApplySecuritySettingsChange) {
				logger.WithFields(logger.Fields{
					fieldOwner:  group.Owner,
					fieldRepo:   change.RepositoryName,
					fieldPhase:  phaseApplySecurity,
					fieldAction: change.Action,
				}).Info("would apply")
			},
			OnSuccess: func(_, _ int) {},
			OnError:   func(_ string, _ error) {},
		})

		set.ApplyProtection.Execute(ctx, commands.ApplyBranchProtectionInput{
			Owner:  group.Owner,
			Audits: group.Audits,
			DryRun: true,
		}, commands.ApplyBranchProtectionListeners{
			OnChange: func(change commands.ApplyBranchProtectionChange) {
				logger.WithFields(logger.Fields{
					fieldOwner:  group.Owner,
					fieldRepo:   change.RepositoryName,
					fieldPhase:  phaseApplyProtection,
					fieldAction: change.Action,
				}).Info("would apply")
			},
			OnSuccess: func(_, _ int) {},
			OnError:   func(_ string, _ error) {},
		})
	}
}

// executeAuditPerOwner audits every owner in turn and keeps the results
// grouped by owner. A failure on one owner is fatal, same as before —
// a partially-audited fleet must not be mistaken for a compliant one.
func executeAuditPerOwner(
	ctx context.Context,
	set commandSet,
	owners []string,
	repoFilter string,
) []ownerAudits {
	grouped := make([]ownerAudits, 0, len(owners))
	for _, owner := range owners {
		grouped = append(grouped, ownerAudits{
			Owner:  owner,
			Audits: executeAudit(ctx, set, owner, repoFilter),
		})
	}
	return grouped
}

// flattenAudits concatenates the per-owner groups in owner order, which
// is what the table, the snapshots, and the compliance count consume.
func flattenAudits(grouped []ownerAudits) []entities.AuditResult {
	total := 0
	for _, group := range grouped {
		total += len(group.Audits)
	}

	flat := make([]entities.AuditResult, 0, total)
	for _, group := range grouped {
		flat = append(flat, group.Audits...)
	}
	return flat
}

func executeAudit(ctx context.Context, set commandSet, owner, repoFilter string) []entities.AuditResult {
	var out []entities.AuditResult
	set.Audit.Execute(ctx, commands.AuditRepositoriesInput{
		Owner:      owner,
		RepoFilter: repoFilter,
	}, commands.AuditRepositoriesListeners{
		OnProgress: func(i, total int, name string) {
			fmt.Fprintf(os.Stderr, "\r  Auditing %s %d/%d: %-40s", owner, i+1, total, name)
		},
		OnSuccess: func(audits []entities.AuditResult) {
			out = audits
		},
		OnError: func(err error) {
			logger.WithError(err).WithField(fieldOwner, owner).Fatal("auditing")
		},
	})
	fmt.Fprintln(os.Stderr)
	return out
}

func printAuditTable(audits []entities.AuditResult) {
	// Sorted by the qualified name so the table groups by owner and stays
	// stable when two owners host a repo of the same name.
	sort.Slice(audits, func(i, j int) bool {
		return audits[i].Repository.QualifiedName() < audits[j].Repository.QualifiedName()
	})

	printTableHeader()
	nonCompliant := printTableRows(audits)
	printSummary(audits)
	printNonComplianceReport(audits, nonCompliant)
}

func printTableHeader() {
	fmt.Fprintf(
		os.Stdout,
		"\n%-*s %-8s %-7s %-7s %-7s %-5s %-5s %-7s %-7s %-7s %-7s %-5s %-6s %-8s %-6s %-5s\n",
		repoColumnWidth,
		"REPO",
		"VIS",
		"DEL-BR",
		"AUTO-M",
		"UPD-BR",
		"WIKI",
		"PROJ",
		"SEC-SC",
		"PUSH-P",
		"DEP-AL",
		"DEP-UP",
		"PROT",
		"NO-FP",
		"MERGE-M",
		"STALE",
		"SIGS",
	)
	fmt.Fprintln(os.Stdout, stringOfChar('-', tableWidth))
}

func printTableRows(audits []entities.AuditResult) int {
	nonCompliant := 0
	for _, a := range audits {
		repo := a.Repository
		if a.AuditError != "" {
			fmt.Fprintf(os.Stdout, "%-*s ERROR: %s\n", repoColumnWidth, repo.QualifiedName(), a.AuditError)
			continue
		}

		fmt.Fprintf(os.Stdout, "%-*s %-8s %-7s %-7s %-7s %-5s %-5s %-7s %-7s %-7s %-7s %-5s %-6s %-8s %-6s %-5s\n",
			repoColumnWidth,
			repo.QualifiedName(),
			repo.Visibility,
			yesNo(repo.Settings.DeleteBranchOnMerge),
			yesNo(repo.Settings.AllowAutoMerge),
			yesNo(repo.Settings.AllowUpdateBranch),
			yesNo(repo.Settings.HasWiki),
			yesNo(repo.Settings.HasProjects),
			truncate(defaultIfEmpty(a.Security.SecretScanning, "N/A"), secretColumnWidth),
			truncate(defaultIfEmpty(a.Security.PushProtection, "N/A"), secretColumnWidth),
			yesNoTri(a.Security.DependabotAlerts),
			yesNo(a.Security.DependabotUpdates),
			protectionLabel(a),
			forcePushLabel(a),
			mergeMethodsLabel(a),
			yesNo(a.BranchProtection.DismissStaleReviews),
			yesNoTri(a.BranchProtection.Signatures),
		)

		if len(a.ComputeIssues()) > 0 {
			nonCompliant++
		}
	}
	return nonCompliant
}

func protectionLabel(a entities.AuditResult) string {
	switch {
	case a.BranchProtection.Enabled:
		return "Y"
	case !a.BranchProtection.Available:
		return "N/A"
	default:
		return "N"
	}
}

func forcePushLabel(a entities.AuditResult) string {
	if a.HasForcePushRuleset() {
		return "Y"
	}
	return "N"
}

// mergeMethodsLabel renders the ruleset's allowed merge methods, which is
// where the no-fast-forward-merge policy lives. NO-FP next to it is the
// force-push rule and a different thing entirely. The three empty-ish
// states are distinct and must not collapse: "-" is no ruleset at all,
// "missing" is a ruleset without a pull_request rule, and "none" is a
// pull_request rule that permits no method — only the last would block
// every merge outright.
func mergeMethodsLabel(a entities.AuditResult) string {
	switch {
	case a.Ruleset == nil:
		return "-"
	case a.Ruleset.AllowedMergeMethods == nil:
		return "missing"
	case len(a.Ruleset.AllowedMergeMethods) == 0:
		return "none"
	default:
		return truncate(strings.Join(a.Ruleset.AllowedMergeMethods, "+"), mergeMethodColumnWidth)
	}
}

func printSummary(audits []entities.AuditResult) {
	total := len(audits)
	public := 0
	private := 0
	forks := 0
	protected := 0
	unavailable := 0
	for _, a := range audits {
		if a.Repository.Private {
			private++
		} else {
			public++
		}
		if a.Repository.Fork {
			forks++
		}
		if a.BranchProtection.Enabled {
			protected++
		}
		if !a.BranchProtection.Available {
			unavailable++
		}
	}
	fmt.Fprintf(os.Stdout, "\nSummary: %d repos (%d public, %d private, %d forks)\n", total, public, private, forks)
	fmt.Fprintf(os.Stdout, "Branch protection: %d enabled, %d unavailable\n", protected, unavailable)
}

func printNonComplianceReport(audits []entities.AuditResult, nonCompliant int) {
	fmt.Fprintln(os.Stdout, "\n=== NON-COMPLIANCE REPORT ===")
	if nonCompliant == 0 {
		fmt.Fprintln(os.Stdout, "\nAll repos are compliant.")
		return
	}
	for _, a := range audits {
		issues := a.ComputeIssues()
		if len(issues) == 0 {
			continue
		}
		fmt.Fprintf(os.Stdout, "\n  %s (%d):\n", a.Repository.QualifiedName(), len(issues))
		for _, issue := range issues {
			fmt.Fprintf(os.Stdout, "    - %s\n", issue)
		}
	}
	fmt.Fprintf(os.Stdout, "\nTotal non-compliant: %d/%d\n", nonCompliant, len(audits))
}

func countNonCompliant(audits []entities.AuditResult) int {
	n := 0
	for _, a := range audits {
		if len(a.ComputeIssues()) > 0 {
			n++
		}
	}
	return n
}

func auditBeforePath() string {
	return filepath.Join(os.TempDir(), "gh_hardening_audit_before.json")
}

func auditAfterPath() string {
	return filepath.Join(os.TempDir(), "gh_hardening_audit_after.json")
}

func saveSnapshot(audits []entities.AuditResult, path string) {
	f, err := os.Create(path)
	if err != nil {
		logger.WithError(err).WithField("path", path).Warn("saving snapshot")
		return
	}
	defer func() { _ = f.Close() }()

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	//nolint:musttag // AuditResult is a framework-agnostic entity; json tags belong on infrastructure DTOs only
	if encodeErr := encoder.Encode(audits); encodeErr != nil {
		logger.WithError(encodeErr).WithField("path", path).Warn("encoding snapshot")
		return
	}
	logger.WithField("path", path).Info("audit saved")
}

func loadSnapshot(path string) ([]entities.AuditResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var audits []entities.AuditResult
	//nolint:musttag // AuditResult is a framework-agnostic entity; json tags belong on infrastructure DTOs only
	if decodeErr := json.NewDecoder(f).Decode(&audits); decodeErr != nil {
		return nil, decodeErr
	}
	return audits, nil
}

func yesNo(b bool) string {
	if b {
		return "Y"
	}
	return "N"
}

func yesNoTri(b *bool) string {
	if b == nil {
		return "-"
	}
	return yesNo(*b)
}

func defaultIfEmpty(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func stringOfChar(c byte, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = c
	}
	return string(out)
}
