package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
	"github.com/rios0rios0/config-automation/internal/domain/repositories"
)

// RefreshRepositoriesCommand refreshes one batch of repositories in
// sequence: clone, instruction-file layout, scope, the read-only audit and the
// scoped refresh, the guards, drift, and the pull request. Every failure is
// local to the repository that caused it; only a usage limit stops the batch
// from calling Claude again.
type RefreshRepositoriesCommand struct {
	workingTree  repositories.WorkingTreeRepository
	changes      repositories.ChangesRepository
	clones       repositories.ClonesRepository
	agent        repositories.AgentRepository
	pullRequests repositories.PullRequestsRepository
	layout       *NormalizeAgentsLayoutCommand
	scope        *DiscoverRefreshScopeCommand
	now          func() time.Time
}

// RefreshRepositoriesDependencies are the ports and commands the refresh
// composes, wired by the composition root.
type RefreshRepositoriesDependencies struct {
	WorkingTree  repositories.WorkingTreeRepository
	Changes      repositories.ChangesRepository
	Clones       repositories.ClonesRepository
	Agent        repositories.AgentRepository
	PullRequests repositories.PullRequestsRepository
	Now          func() time.Time
}

// NewRefreshRepositoriesCommand is the constructor the composition root calls.
func NewRefreshRepositoriesCommand(dependencies RefreshRepositoriesDependencies) *RefreshRepositoriesCommand {
	return &RefreshRepositoriesCommand{
		workingTree:  dependencies.WorkingTree,
		changes:      dependencies.Changes,
		clones:       dependencies.Clones,
		agent:        dependencies.Agent,
		pullRequests: dependencies.PullRequests,
		layout:       NewNormalizeAgentsLayoutCommand(dependencies.WorkingTree),
		scope:        NewDiscoverRefreshScopeCommand(dependencies.WorkingTree),
		now:          dependencies.Now,
	}
}

// RefreshSettings are the choices every repository of a batch shares.
type RefreshSettings struct {
	Provider         entities.Provider
	InstructionFiles entities.InstructionFiles
	GuideURL         string
	Branch           string
	CommitMessage    string
	PullRequestTitle string
	PullRequestBody  string
	Model            string
	Effort           string
	MaxTurns         int
}

// RefreshRepositoriesInput is one batch: its repositories, the directory the
// scratch clones go in, the organization's token, and the shared settings.
type RefreshRepositoriesInput struct {
	Targets       []entities.RefreshTarget
	WorkDirectory string
	Token         string
	Settings      RefreshSettings
}

// RefreshRepositoriesListeners receives the progress of a batch. Notices are
// text the host wrote; untrusted notices carry repository-controlled text,
// such as a path or git's own output, which the caller must not let act as a
// CI command. A warning is shown as an annotation. The agent's own output
// streams between OnAgentStarted and OnAgentFinished.
type RefreshRepositoriesListeners struct {
	OnStarted         func(index, total int, target string)
	OnQuotaSkipped    func(index, total int, target string)
	OnNotice          func(message string)
	OnUntrustedNotice func(message string)
	OnWarning         func(message string)
	OnAgentStarted    func()
	OnAgentFinished   func()
	OnFinished        func(summary entities.RefreshSummary)
	OnError           func(err error)
}

// Execute refreshes every repository of the batch and reports the summary.
func (c *RefreshRepositoriesCommand) Execute(
	ctx context.Context,
	input RefreshRepositoriesInput,
	listeners RefreshRepositoriesListeners,
) {
	settings := input.Settings
	base, err := entities.RenderRefreshPrompt(entities.PromptParameters{
		Claude: settings.InstructionFiles.Claude, Copilot: settings.InstructionFiles.Copilot,
		GuideURL: settings.GuideURL,
	})
	if err != nil {
		listeners.OnError(err)
		return
	}
	batch := &refreshBatch{
		command: c, input: input, listeners: listeners, basePrompt: base,
		credential: settings.Provider.GitCredential(input.Token),
		summary:    entities.NewRefreshSummary(len(input.Targets)),
	}
	for index, target := range input.Targets {
		batch.summary.Processed = index + 1
		name := target.QualifiedName()
		if batch.quotaHit {
			batch.summary.QuotaSkipped = append(batch.summary.QuotaSkipped, name)
			listeners.OnQuotaSkipped(index+1, len(input.Targets), name)
			continue
		}
		listeners.OnStarted(index+1, len(input.Targets), name)
		batch.refresh(ctx, target)
	}
	listeners.OnFinished(batch.summary)
}

// refreshBatch carries what the repositories of one batch share.
type refreshBatch struct {
	command    *RefreshRepositoriesCommand
	input      RefreshRepositoriesInput
	listeners  RefreshRepositoriesListeners
	basePrompt string
	credential entities.GitCredential
	summary    entities.RefreshSummary
	quotaHit   bool
}

// refresh runs one repository and always removes its scratch clone. A panic
// is recorded as that repository's failure, so it never takes the rest of the
// batch down with it.
func (b *refreshBatch) refresh(ctx context.Context, target entities.RefreshTarget) {
	run := &repositoryRefresh{
		batch: b, target: target, name: target.QualifiedName(),
		root: filepath.Join(b.input.WorkDirectory, target.WorkKey()),
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			run.fail(entities.FailureInternal, fmt.Errorf("%v", recovered))
		}
		if err := b.command.clones.Discard(ctx, run.root); err != nil {
			b.listeners.OnUntrustedNotice("--> could not remove the clone: " + err.Error())
		}
	}()
	run.execute(ctx)
}

// repositoryRefresh is the state of one repository's refresh.
type repositoryRefresh struct {
	batch  *refreshBatch
	target entities.RefreshTarget
	name   string
	root   string
	layout entities.AgentsLayoutReport
	scope  entities.RefreshScope
	mode   entities.ChangelogMode
}

// execute runs the steps in order; each returns false once it has recorded
// the repository's outcome.
func (r *repositoryRefresh) execute(ctx context.Context) {
	steps := []func(context.Context) bool{
		r.prepare, r.normalize, r.discoverScope, r.detectChangelog, r.runAgent,
		r.normalizeAgain, r.guardChangelog, r.publish,
	}
	for _, step := range steps {
		if !step(ctx) {
			return
		}
	}
}

// fail records the repository under reason, with what went wrong.
func (r *repositoryRefresh) fail(reason entities.FailureReason, err error) bool {
	if err != nil {
		r.batch.listeners.OnUntrustedNotice(fmt.Sprintf("--> %s failed: %v", reason, err))
	}
	r.batch.summary.Failed = append(r.batch.summary.Failed, entities.RefreshFailure{Reason: reason, Target: r.name})
	return false
}

// prepare clones the repository and checks out its default branch.
func (r *repositoryRefresh) prepare(ctx context.Context) bool {
	command := r.batch.command
	if err := command.clones.Discard(ctx, r.root); err != nil {
		return r.fail(entities.FailureClone, err)
	}
	url := r.batch.input.Settings.Provider.CloneURL(r.target)
	if err := command.clones.Clone(ctx, url, r.root, r.batch.credential); err != nil {
		return r.fail(entities.FailureClone, err)
	}
	if err := command.changes.Checkout(ctx, r.root, r.target.DefaultBranch); err != nil {
		return r.fail(entities.FailureCheckout, err)
	}
	return true
}

// normalize moves the clone into the instruction-file layout before scope
// discovery, so the audit and the refresh work on the layout the pull request
// proposes. A rename is committed alone so history follows the file.
func (r *repositoryRefresh) normalize(ctx context.Context) bool {
	report, err := r.runLayout(ctx)
	if err != nil {
		return r.fail(entities.FailureLayout, err)
	}
	r.layout = report
	for _, skipped := range report.Skipped {
		r.batch.listeners.OnWarning("Left out of the AGENTS.md layout: " + quoted(skipped))
	}
	return true
}

// normalizeAgain adds the import for an AGENTS.md the refresh created and
// removes any instruction file of a disabled kind the refresh wrote anyway.
func (r *repositoryRefresh) normalizeAgain(ctx context.Context) bool {
	if _, err := r.runLayout(ctx); err != nil {
		return r.fail(entities.FailureLayout, err)
	}
	return true
}

// runLayout runs the layout command and returns its report.
func (r *repositoryRefresh) runLayout(ctx context.Context) (entities.AgentsLayoutReport, error) {
	var report entities.AgentsLayoutReport
	var failure error
	r.batch.command.layout.Execute(
		ctx,
		NormalizeAgentsLayoutInput{Root: r.root, InstructionFiles: r.batch.input.Settings.InstructionFiles},
		NormalizeAgentsLayoutListeners{
			OnSuccess: func(result entities.AgentsLayoutReport) { report = result },
			OnError:   func(err error) { failure = err },
		},
	)
	return report, failure
}

// discoverScope builds the manifest every later step works from.
func (r *repositoryRefresh) discoverScope(ctx context.Context) bool {
	var failure error
	r.batch.command.scope.Execute(
		ctx,
		DiscoverRefreshScopeInput{Root: r.root, InstructionFiles: r.batch.input.Settings.InstructionFiles},
		DiscoverRefreshScopeListeners{
			OnSuccess: func(result entities.RefreshScope) { r.scope = result },
			OnError:   func(err error) { failure = err },
		},
	)
	if failure != nil {
		return r.fail(entities.FailureScope, failure)
	}
	for _, skipped := range r.scope.Skipped {
		r.batch.listeners.OnWarning("Excluded symlink or unsupported refresh path: " + quoted(skipped))
	}
	return true
}

// detectChangelog decides the repository's changelog convention, which the
// grants enforce rather than the prompt alone.
func (r *repositoryRefresh) detectChangelog(ctx context.Context) bool {
	hasConfig := false
	for _, file := range entities.ChlogConfigFiles() {
		kind, err := r.batch.command.workingTree.FindKind(ctx, r.root, file)
		if err != nil {
			return r.fail(entities.FailureScope, err)
		}
		hasConfig = hasConfig || kind == entities.PathFile
	}
	fragments, err := r.batch.command.workingTree.FindKind(ctx, r.root, entities.ChlogFragmentDirectory)
	if err != nil {
		return r.fail(entities.FailureScope, err)
	}
	changelog, err := r.batch.command.workingTree.FindKind(ctx, r.root, entities.ChangelogFile)
	if err != nil {
		return r.fail(entities.FailureScope, err)
	}
	r.mode = entities.DetectChangelogMode(
		hasConfig, fragments == entities.PathDirectory, changelog == entities.PathFile)
	r.batch.listeners.OnNotice("--> changelog convention: " + string(r.mode))
	return true
}

// runAgent runs the read-only audit and then the scoped refresh. Their output
// streams between the agent listeners; a failure is classified only after the
// stream ends, so its annotation is never swallowed by a CI's stop-commands.
func (r *repositoryRefresh) runAgent(ctx context.Context) bool {
	settings := r.batch.input.Settings
	listeners := r.batch.listeners
	audit := entities.AgentRun{
		Directory: r.root, Prompt: entities.AuditPrompt(settings.Model, settings.InstructionFiles),
		Model: settings.Model, Effort: settings.Effort, MaxTurns: settings.MaxTurns,
		Tools: entities.AuditTools(), AllowedTools: entities.AuditTools(),
	}
	listeners.OnAgentStarted()
	listeners.OnNotice("--> prompt audit (read-only)")
	result, err := r.batch.command.agent.Audit(ctx, audit)
	if err == nil {
		listeners.OnNotice("--> refresh guidance from verified findings")
		prompt := entities.RepositoryPrompt(r.batch.basePrompt, r.mode, r.layout.Note)
		grant := r.mode.Grant()
		result, err = r.batch.command.agent.Refresh(ctx, entities.AgentRun{
			Directory: r.root, Prompt: entities.RefreshPrompt(prompt, r.scope, result.Report),
			Model: settings.Model, Effort: settings.Effort, MaxTurns: settings.MaxTurns,
			Tools: entities.RefreshTools(grant), AllowedTools: entities.RefreshAllowedTools(r.scope, grant),
		})
	}
	listeners.OnAgentFinished()
	if err == nil {
		return true
	}
	return r.agentFailed(result.Output, err)
}

// agentFailed records a failed Claude phase by what its output says.
func (r *repositoryRefresh) agentFailed(output string, err error) bool {
	listeners := r.batch.listeners
	switch entities.ClassifyAgentFailure(output + "\n" + err.Error()) {
	case entities.AgentFailureUsageLimit:
		listeners.OnNotice("--> Claude usage limit reached; skipping the rest of this batch.")
		r.batch.quotaHit = true
		return r.fail(entities.FailureClaudeQuota, nil)
	case entities.AgentFailureSafeguard:
		listeners.OnNotice("--> Claude's safeguards refused this repo; moving on to the next one.")
		listeners.OnWarning("Claude's safeguards refused " + r.name + "; it was skipped, not refreshed.")
		r.batch.summary.SafeguardSkipped = append(r.batch.summary.SafeguardSkipped, r.name)
		return false
	case entities.AgentFailureOther:
		return r.fail(entities.FailureClaude, err)
	}
	return r.fail(entities.FailureClaude, err)
}

// guardChangelog enforces the chlog convention after the refresh. The grants
// already deny every tool that could reach CHANGELOG.md in a chlog
// repository, so neither restore should ever fire; they exist because the
// grant is the CLI's promise rather than this host's. Discarding is always
// right: the next `chlog batch` discards a hand-edit anyway, and a fragment an
// earlier pull request committed must not be rewritten by this one.
func (r *repositoryRefresh) guardChangelog(ctx context.Context) bool {
	if r.mode != entities.ChangelogChlog {
		return true
	}
	guards := []struct{ path, message string }{
		{
			entities.ChangelogFile,
			"--> discarded a hand-edit of CHANGELOG.md: this repo generates it from chlog fragments.",
		},
		{entities.ChlogDirectory, "--> discarded edits to existing chlog fragments: only new fragments are allowed."},
	}
	for _, guard := range guards {
		changed, err := r.batch.command.changes.HasUnstagedChanges(ctx, r.root, guard.path)
		if err == nil && changed {
			r.batch.listeners.OnNotice(guard.message)
			err = r.batch.command.changes.Restore(ctx, r.root, guard.path)
		}
		if err != nil {
			r.batch.listeners.OnNotice("--> could not restore the changelog target; skipping this repo.")
			return r.fail(entities.FailureChangelogGuard, err)
		}
	}
	return r.checkFragments(ctx)
}

// checkFragments rejects a new fragment that `chlog new` did not write.
func (r *repositoryRefresh) checkFragments(ctx context.Context) bool {
	command := r.batch.command
	fragments, err := command.changes.FindUntracked(ctx, r.root, entities.ChlogFragmentDirectory)
	if err != nil {
		return r.fail(entities.FailureFragment, err)
	}
	for _, fragment := range fragments {
		content, readErr := command.workingTree.FindContent(ctx, r.root, fragment)
		if readErr != nil {
			return r.fail(entities.FailureFragment, readErr)
		}
		if invalid := entities.ValidateChlogFragment(fragment, string(content), command.now()); invalid != nil {
			r.batch.listeners.OnUntrustedNotice(fmt.Sprintf(
				"--> rejected chlog fragment '%s' (%v); skipping this repo.", fragment, invalid))
			return r.fail(entities.FailureFragment, nil)
		}
	}
	return true
}

// publish measures drift, refuses a credential-shaped diff, and commits,
// pushes, and opens or updates the pull request.
func (r *repositoryRefresh) publish(ctx context.Context) bool {
	drift, ok := r.measureDrift(ctx)
	if !ok {
		return false
	}
	ahead, err := r.batch.command.changes.IsAhead(ctx, r.root, "refs/remotes/origin/"+r.target.DefaultBranch)
	if err != nil {
		return r.fail(entities.FailureDrift, err)
	}
	if len(drift) == 0 && !ahead {
		r.batch.summary.NoDrift = append(r.batch.summary.NoDrift, r.name)
		return false
	}
	changelog, ok := r.changelogPaths(ctx)
	if !ok || !r.guardSecrets(ctx, slices.Concat(drift, changelog)) {
		return false
	}
	if !r.commit(ctx, slices.Concat(drift, changelog)) {
		return false
	}
	return r.openPullRequest(ctx)
}

// measureDrift returns the manifest paths that differ from HEAD, whitespace
// aside. A new file counts once it is marked intent-to-add; a tracked file
// that is gone, removed by the host or deleted by the refresh, counts as a
// deletion. The changelog is never part of drift, so a stray changelog-only
// edit cannot open a pull request.
func (r *repositoryRefresh) measureDrift(ctx context.Context) ([]string, bool) {
	command := r.batch.command
	tracked := slices.Concat(r.scope.Existing, r.scope.Removed)
	present, candidates := []string{}, []string{}
	for _, path := range r.scope.Paths {
		kind, err := command.workingTree.FindKind(ctx, r.root, path)
		if err != nil {
			return nil, r.fail(entities.FailureStage, err)
		}
		if kind == entities.PathFile {
			present = append(present, path)
			candidates = append(candidates, path)
		} else if slices.Contains(tracked, path) {
			candidates = append(candidates, path)
		}
	}
	if err := command.changes.MarkIntentToAdd(ctx, r.root, present); err != nil {
		return nil, r.fail(entities.FailureStage, err)
	}
	if len(candidates) == 0 {
		return []string{}, true
	}
	changed, err := command.changes.HasChanges(ctx, r.root, candidates)
	if err != nil {
		return nil, r.fail(entities.FailureDrift, err)
	}
	if !changed {
		return []string{}, true
	}
	return candidates, true
}

// changelogPaths returns where this repository's changelog entry lands,
// marking a new chlog fragment intent-to-add so the secret guard reads it in
// full. A chlog repository without a fragment directory stages nothing.
func (r *repositoryRefresh) changelogPaths(ctx context.Context) ([]string, bool) {
	paths := []string{}
	for _, path := range r.mode.StagePaths() {
		kind, err := r.batch.command.workingTree.FindKind(ctx, r.root, path)
		if err != nil {
			return nil, r.fail(entities.FailureStage, err)
		}
		if kind == entities.PathFile || kind == entities.PathDirectory {
			paths = append(paths, path)
		}
	}
	if slices.Contains(paths, entities.ChlogFragmentDirectory) {
		if err := r.batch.command.changes.MarkIntentToAdd(
			ctx, r.root, []string{entities.ChlogFragmentDirectory}); err != nil {
			return nil, r.fail(entities.FailureStage, err)
		}
	}
	return paths, true
}

// guardSecrets refuses a diff that adds a credential shape, and fails closed:
// a diff that cannot be read is a refusal, never a pass.
func (r *repositoryRefresh) guardSecrets(ctx context.Context, paths []string) bool {
	diff, err := r.batch.command.changes.FindDiff(ctx, r.root, paths)
	if err != nil {
		r.batch.listeners.OnNotice("--> could not read the refresh diff to check it for credential-shaped " +
			"literals; skipping this repo.")
		return r.fail(entities.FailureSecretShape, err)
	}
	if entities.HasCredentialShape(diff) {
		r.batch.listeners.OnNotice("--> the refresh added a credential-shaped literal, which must not reach " +
			"a commit; skipping this repo.")
		return r.fail(entities.FailureSecretShape, nil)
	}
	return true
}

// commit stages the refresh on the refresh branch and pushes it, forced, so
// repeated runs keep updating one pull request. A branch whose only change is
// the layout's rename commit has nothing more to commit.
func (r *repositoryRefresh) commit(ctx context.Context, paths []string) bool {
	command := r.batch.command
	settings := r.batch.input.Settings
	if err := command.changes.CreateBranch(ctx, r.root, settings.Branch); err != nil {
		return r.fail(entities.FailureBranch, err)
	}
	if len(paths) > 0 {
		if err := command.workingTree.Stage(ctx, r.root, paths); err != nil {
			return r.fail(entities.FailureStage, err)
		}
	}
	staged, err := command.changes.HasStagedChanges(ctx, r.root)
	if err != nil {
		return r.fail(entities.FailureCommit, err)
	}
	if staged {
		if err = command.workingTree.Commit(ctx, r.root, settings.CommitMessage); err != nil {
			return r.fail(entities.FailureCommit, err)
		}
	}
	if err = command.clones.Push(ctx, r.root, settings.Branch, r.batch.credential); err != nil {
		return r.fail(entities.FailurePush, err)
	}
	return true
}

// openPullRequest updates the open pull request through the push above, or
// opens one. A failed lookup opens none, so a transient error never doubles
// a pull request.
func (r *repositoryRefresh) openPullRequest(ctx context.Context) bool {
	command := r.batch.command
	settings := r.batch.input.Settings
	existing, found, err := command.pullRequests.FindOpenBySourceBranch(ctx, r.target, settings.Branch)
	if err != nil {
		return r.fail(entities.FailurePullRequestList, err)
	}
	if found {
		r.batch.listeners.OnNotice(fmt.Sprintf("PR #%d already exists; force-push updated its diff.", existing.Number))
		r.batch.summary.PRsUpdated = append(r.batch.summary.PRsUpdated, fmt.Sprintf("%s#%d", r.name, existing.Number))
		return true
	}
	created, err := command.pullRequests.Insert(ctx, r.target, entities.PullRequestDraft{
		SourceBranch: settings.Branch, TargetBranch: r.target.DefaultBranch,
		Title: settings.PullRequestTitle, Description: settings.PullRequestBody,
	})
	if err != nil {
		return r.fail(entities.FailurePullRequestCreate, err)
	}
	r.batch.listeners.OnNotice("Created " + created.URL)
	r.batch.summary.PRsCreated = append(r.batch.summary.PRsCreated, fmt.Sprintf("%s (%s)", r.name, created.URL))
	return true
}

// quoted renders a repository-controlled path as a JSON string, so a line
// break or a quote in it cannot change the shape of the message.
func quoted(path string) string {
	encoded, err := json.Marshal(path)
	if err != nil {
		return fmt.Sprintf("%q", path)
	}
	return string(encoded)
}
