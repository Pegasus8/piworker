package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
)

// Agent deploy (docs/design/mcp.md §10, ADR-0012): with agent_deploy above
// off, tokens with the deploy scope may deploy and stop flows; the admin sees
// who deployed what and rolls back to the version they last approved. Every
// deploy and stop, the agent's and the editor's, goes through the editor's
// lifecycle path (FlowLifecycle): one lock, the restart intent, the
// deployment marker and the editor's statuses.

// FlowLifecycle is the editor's lifecycle path, implemented by internal/api's
// FlowsHandler. WithLock runs fn holding the lifecycle lock (lifecycleMu): no
// editor save, deploy, stop, toggle or delete runs meanwhile, so a check fn
// makes (the version it read, the token it re-read) still holds when fn
// deploys. The LockedLifecycle is valid only during fn; fn must not call
// WithLock again (the lock is not reentrant). WithLock returns fn's error.
type FlowLifecycle interface {
	WithLock(ctx context.Context, fn func(LockedLifecycle) error) error
}

// LockedLifecycle is what the lifecycle path does under its lock, exactly as
// the editor's deploy and stop requests do it (FlowsHandler.changeRunning).
// Its refusals are *LifecycleError with the editor's statuses; other
// failures are plain errors (500).
type LockedLifecycle interface {
	// Running reports whether flowID is deployed and running.
	Running(flowID string) bool
	// Deploy deploys the stored current version of flowID and returns
	// that version. When restart is true a running instance is stopped
	// first (a redeploy); otherwise a running flow is a 409 ("Flow is
	// already running"). After shutdown began it is a 503; an unknown
	// flow a 404; a flow deploy refuses a 400 (validation: the message is
	// deploy's and may quote config values, so agents never see it); a
	// stopping instance or an exclusive resource another flow holds a 409.
	// On success it persists the restart intent (state running) and
	// records the deployment marker (storage RecordDeployment with
	// by.Deployment(version)). A redeploy that fails after the old
	// instance stopped leaves the flow stopped with its running intent
	// (read views: failed). Deploy itself lints nothing beyond what
	// deploy checks: callers lint first.
	Deploy(ctx context.Context, flowID string, by DeployActor, restart bool) (version int, err error)
	// Stop stops flowID and persists the stop intent (state inactive),
	// as the editor's stop does (allowed during shutdown). A flow that is
	// not running is a 409 ("Flow is not running"); an unknown one a 404.
	// The deployment marker is kept (it names the last deployment).
	Stop(ctx context.Context, flowID string, by DeployActor) error
}

// LifecycleError is a lifecycle operation refused with the status the
// editor path answers (ADR-0006): 400 the flow is invalid (deploy
// validation), 404 no such flow, 409 a conflict (already running, not
// running, still stopping, an exclusive resource taken: node.
// ErrResourceConflict), 503 shutting down. Err is the cause (deploy's error)
// for errors.Is.
type LifecycleError struct {
	Status  int
	Message string
	Err     error
}

func (e *LifecycleError) Error() string { return e.Message }

// Unwrap returns the cause.
func (e *LifecycleError) Unwrap() error { return e.Err }

// ToolError maps the refusal to the tool error agents see: 400
// invalid_arguments (with a fixed message: deploy's may quote config
// values), 404 flow_not_found, 409 conflict, 503 unavailable. It is nil for
// any other status (an internal failure) and for a nil e.
func (e *LifecycleError) ToolError() *ToolError {
	if e == nil {
		return nil
	}
	switch e.Status {
	case http.StatusBadRequest:
		return toolError(ErrInvalidArguments, "the flow cannot be deployed: validate it (pw_validate_flow) to see why")
	case http.StatusNotFound:
		return toolError(ErrFlowNotFound, e.Message)
	case http.StatusConflict:
		return toolError(ErrConflict, e.Message)
	case http.StatusServiceUnavailable:
		return toolError(ErrUnavailable, e.Message)
	}
	return nil
}

// DeployActor says who deploys or stops a flow; the deployment marker
// records it.
type DeployActor struct {
	Kind      ActorKind // ActorEditor or ActorAgent
	TokenID   string    // the agent's token
	TokenName string
}

// DeployActor is who the actor deploys as: the editor, or the agent's token.
func (a Actor) DeployActor() DeployActor {
	if a.isAgent() {
		return DeployActor{Kind: ActorAgent, TokenID: a.TokenID, TokenName: a.TokenName}
	}
	return DeployActor{Kind: ActorEditor}
}

// Deployment is the marker of a deployment of version by d (DeployedAt is
// left zero: storage stamps it).
func (d DeployActor) Deployment(version int) storage.Deployment {
	kind := storage.VersionActorEditor
	if d.Kind == ActorAgent {
		kind = storage.VersionActorAgent
	}
	return storage.Deployment{Version: version, ActorKind: kind, TokenID: d.TokenID, TokenName: d.TokenName}
}

// lowDeployRefusedAlerts are the alert kinds (flow.Diff against the last
// admin-approved version) that the low agent_deploy level refuses: the
// contract's eight, credentialed_request_changed (hand-off H1) and
// global_variable_write (what a running set-var writes to a global variable,
// which other flows read for their URLs, commands and conditions, changed).
var lowDeployRefusedAlerts = []string{
	flow.AlertPrivilegedNodeAdded,
	flow.AlertDestinationChanged,
	flow.AlertDynamicDestination,
	flow.AlertNewHost,
	flow.AlertSecretAdded,
	flow.AlertSecretMoved,
	flow.AlertRiskSwitchOn,
	flow.AlertSensitiveLiteral,
	flow.AlertCredentialedRequestChanged,
	flow.AlertGlobalVariableWrite,
}

// LowDeployRefusedAlerts returns the alert kinds the low agent_deploy level
// refuses, in flow.Diff's order. risk_raised is not among them: the level
// bounds the risk itself (at most external).
func LowDeployRefusedAlerts() []string { return slices.Clone(lowDeployRefusedAlerts) }

// DeployRequest is an agent's deploy (pw_deploy_flow).
type DeployRequest struct {
	FlowID string `json:"flowId"`
	// DraftID, when set, is the agent's own pending draft of FlowID to
	// apply first (saved as an agent version, never admin-approved).
	DraftID string `json:"draftId,omitempty"`
}

// DeployResult is the outcome of an agent's Deploy or Stop.
type DeployResult struct {
	FlowID string `json:"flowId"`
	// Version is the version now running (Deploy) or stopped (Stop).
	Version int `json:"version"`
	// Running is true after Deploy, false after Stop.
	Running bool      `json:"running"`
	Risk    node.Risk `json:"risk"`
}

// ApplyResult is the outcome of saving a new version for the admin
// (ApproveDraft, Rollback), deployed or not.
type ApplyResult struct {
	FlowID  string `json:"flowId"`
	Version int    `json:"version"`
	// Deployed: the new version is running.
	Deployed bool `json:"deployed"`
	// DeployError is why the requested (re)deploy failed; the version is
	// saved all the same.
	DeployError *DeployError `json:"deployError,omitempty"`
}

// DeployError is a failed deploy as the editor sees it: the lifecycle
// path's status (400, 409, 500, 503) and its message.
type DeployError struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// VersionList is the version history of a flow (Versions).
type VersionList struct {
	// Current is the flow's current version.
	Current int `json:"current"`
	// LastAdminApproved is the newest admin-approved version (0: none).
	LastAdminApproved int            `json:"lastAdminApproved"`
	Versions          []VersionEntry `json:"versions"`
}

// VersionEntry is one stored version, newest first.
type VersionEntry struct {
	Version       int       `json:"version"`
	ActorKind     string    `json:"actorKind"` // "editor" or "agent"
	TokenID       string    `json:"tokenId,omitempty"`
	TokenName     string    `json:"tokenName,omitempty"`
	AdminApproved bool      `json:"adminApproved"`
	Note          string    `json:"note,omitempty"`
	DraftID       string    `json:"draftId,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	// Deployed: the deployment marker names this version (the last one
	// deployed).
	Deployed bool `json:"deployed"`
}

// FlowMarks are what the editor marks on a flow (H6): its last deployment
// and whether its current version was not approved by the admin.
type FlowMarks struct {
	// Deployment is the last deployment (nil: never recorded).
	Deployment *DeploymentView `json:"deployment"`
	// AgentChanged: the current version is not admin-approved (an agent
	// saved it).
	AgentChanged bool `json:"agentChanged"`
}

// DeploymentView is a deployment marker as the editor shows it.
type DeploymentView struct {
	Version    int       `json:"version"`
	ActorKind  string    `json:"actorKind"` // "editor" or "agent"
	TokenName  string    `json:"tokenName,omitempty"`
	DeployedAt time.Time `json:"deployedAt"`
}

// FlowWithMarks is a flow as GET /api/flows lists it and GET
// /api/flows/{id} returns it: the flow's own JSON fields (version included)
// plus "deployment" and "agentChanged".
type FlowWithMarks struct {
	*flow.Flow
	FlowMarks
}

// MarksStore is what FlowMarksOf reads (*storage.SQLiteStore).
type MarksStore interface {
	GetDeployment(flowID string) (*storage.Deployment, error)
	ListFlowVersions(flowID string, limit int) ([]storage.FlowVersion, error)
}

// FlowMarksOf returns the marks of a stored flow f (f.Version is its current
// version): its deployment marker, and whether that version is not
// admin-approved (an agent saved it). The editor's flow list and view
// (internal/api) and Authoring.Marks share it.
func FlowMarksOf(store MarksStore, f *flow.Flow) (FlowMarks, error) {
	var marks FlowMarks
	d, err := store.GetDeployment(f.ID)
	if err != nil {
		return FlowMarks{}, fmt.Errorf("get deployment: %w", err)
	}
	if d != nil {
		marks.Deployment = &DeploymentView{Version: d.Version, ActorKind: d.ActorKind, TokenName: d.TokenName, DeployedAt: d.DeployedAt.UTC()}
	}
	newest, err := store.ListFlowVersions(f.ID, 1)
	if err != nil {
		return FlowMarks{}, fmt.Errorf("list flow versions: %w", err)
	}
	marks.AgentChanged = len(newest) == 1 && newest[0].Version == f.Version && !newest[0].AdminApproved
	return marks, nil
}

// Deploy deploys a flow for an agent with the deploy scope (pw_deploy_flow).
// The level (storage AgentDeployLevel) decides: off refuses (forbidden; the
// tool is not even listed); low allows a flow whose flow.Risk is at most
// external and whose flow.Diff against the last admin-approved version (an
// empty flow when none) raises none of LowDeployRefusedAlerts (forbidden,
// naming the kinds, never values); all allows any flow. With DraftID the
// agent's own pending draft of FlowID (another token's: not_found; stale:
// conflict with the current version; Lint errors other than
// undefined_secret: invalid_arguments) is evaluated and, when allowed, saved
// through UpdateFlowVersioned with VersionMeta{agent, the token, not
// admin-approved, DraftID} (the save closes the draft) and deployed,
// restarting a running flow. Without it the current version is deployed (a
// running flow: conflict). The flow is linted first (FlowIssues on
// invalid_arguments; undefined secrets too, since deploy refuses them).
// Under the lifecycle lock, right before deploying, the token is re-checked
// (TokensEpoch unchanged since the policy was evaluated, or the token
// re-read active with the deploy scope: else forbidden) and the flow's
// version is the one the policy evaluated (else conflict, nothing
// deployed) (H3). Lifecycle refusals map through LifecycleError.ToolError.
// Every attempt is audited (privileged when the flow is) and a deploy is
// recorded as the agent's deployment.
func (a *Authoring) Deploy(ctx context.Context, actor Actor, req DeployRequest) (_ DeployResult, err error) {
	args := &authorArgs{FlowID: req.FlowID, DraftID: req.DraftID}
	var privileged bool
	defer a.audited(actor, DeployToolDeployFlow, args, a.now(), &privileged)(&err)
	level, err := a.authorizeDeploy(ctx, actor)
	if err != nil {
		return DeployResult{}, err
	}
	plan, err := a.evaluateDeploy(ctx, level, actor, req.FlowID, req.DraftID, false)
	if plan != nil {
		privileged = plan.risk == node.RiskPrivileged
	}
	if err != nil {
		return DeployResult{}, err
	}
	var res DeployResult
	err = a.lifecycle.WithLock(ctx, func(l LockedLifecycle) error {
		if err := a.recheckDeploy(actor, plan); err != nil {
			return err
		}
		running := l.Running(plan.flowID)
		saved := 0
		if plan.draft != nil {
			v, err := a.store.UpdateFlowVersioned(plan.candidate, plan.version, storage.VersionMeta{
				ActorKind: storage.VersionActorAgent, TokenID: actor.TokenID, TokenName: actor.TokenName,
				DraftID: plan.draft.ID, Note: fmt.Sprintf("draft %s deployed by agent %s", plan.draft.ID, actor.TokenName),
			})
			if err != nil {
				return applyError(err, plan.draft.ID)
			}
			saved = v
		} else if running {
			return toolError(ErrConflict, "the flow is already running: stop it first, or deploy a draft of it (which restarts it)")
		}
		version, err := l.Deploy(ctx, plan.flowID, actor.DeployActor(), running)
		if err != nil {
			return deployRefusal(err, saved)
		}
		res = DeployResult{FlowID: plan.flowID, Version: version, Running: true, Risk: plan.risk}
		args.Version = version
		return nil
	})
	if err != nil {
		return DeployResult{}, err
	}
	return res, nil
}

// authorizeDeploy lets agents whose token has the deploy scope deploy and
// stop flows while agent_deploy is above off, and returns the level. The
// editor deploys through its own pages.
func (a *Authoring) authorizeDeploy(ctx context.Context, actor Actor) (storage.AgentDeployLevel, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	switch {
	case actor.Kind == ActorEditor:
		return "", toolError(ErrPermissionDenied, "the editor deploys and stops flows through its own pages")
	case !actor.isAgent():
		return "", toolError(ErrPermissionDenied, "unknown caller")
	case !actor.HasScope(storage.ScopeDeploy):
		return "", toolError(ErrPermissionDenied, "this token does not have the deploy scope")
	}
	level, err := a.store.AgentDeployLevel()
	if err != nil {
		return "", fmt.Errorf("read the agent deploy level: %w", err)
	}
	if level == storage.AgentDeployOff {
		return "", toolError(ErrPermissionDenied, "agent deploy is off: the admin deploys flows in the editor")
	}
	if a.lifecycle == nil {
		return "", toolError(ErrUnavailable, "flows cannot be deployed now")
	}
	return level, nil
}

// deployPlan is what a deploy or stop evaluated: the policy's verdict holds
// for exactly this version (and draft).
type deployPlan struct {
	flowID string
	// version is the flow's current version when the policy was
	// evaluated.
	version int
	level   storage.AgentDeployLevel
	// draft and candidate: the draft to apply (nil: the current version)
	// and the flow that would run.
	draft     *storage.FlowDraft
	candidate *flow.Flow
	risk      node.Risk
}

// evaluateDeploy reads the flow that would run (the current version, or the
// agent's own pending draft of it based on that version) and applies the
// level: a deploy's flow is linted first (any error, undefined secrets
// included: invalid_arguments with the issues); low allows a flow of risk
// external at most whose changes since the version the admin last approved
// raise none of LowDeployRefusedAlerts.
func (a *Authoring) evaluateDeploy(ctx context.Context, level storage.AgentDeployLevel, actor Actor, flowID, draftID string, stop bool) (*deployPlan, error) {
	if flowID == "" {
		return nil, toolError(ErrInvalidArguments, "flowId is required")
	}
	current, err := a.loadFlow(flowID)
	if err != nil {
		return nil, err
	}
	plan := &deployPlan{flowID: flowID, version: current.Version, level: level, candidate: current}
	if draftID != "" {
		d, err := a.ownDraft(actor, draftID)
		if err != nil {
			return nil, err
		}
		switch {
		case d.FlowID != flowID:
			return nil, toolError(ErrInvalidArguments, fmt.Sprintf("draft %q is not a draft of flow %q (a new flow is approved by the admin)",
				capText(draftID, maxViewIDBytes), capText(flowID, maxViewIDBytes)))
		case d.Status != storage.DraftPending:
			return nil, toolError(ErrConflict, fmt.Sprintf("the draft is no longer pending (%s)", d.Status))
		case d.BaseVersion != current.Version:
			return nil, staleBase(current.Version)
		}
		candidate, err := flow.FromJSON(d.Data)
		if err != nil {
			return nil, fmt.Errorf("decode draft %s: %w", d.ID, err)
		}
		candidate.ID = flowID
		plan.draft, plan.candidate = d, candidate
	}
	// From here the plan is returned with a refusal too: its risk marks the
	// audit row.
	plan.risk = flow.Risk(plan.candidate, a.registry)
	if !stop {
		issues := a.lint(ctx, plan.candidate)
		if errs := errorIssues(issues); len(errs) > 0 {
			return plan, &ToolError{Kind: ErrInvalidArguments, FlowIssues: agentIssues(a.proposalRedactor(plan.candidate), issues),
				Message: fmt.Sprintf("the flow has %d error(s) deploy would refuse", len(errs))}
		}
	}
	if level == storage.AgentDeployLow {
		if err := a.lowPolicy(flowID, plan.candidate, plan.risk); err != nil {
			return plan, err
		}
	}
	return plan, nil
}

// lowPolicy is the low agent_deploy level: the flow that would run is of
// risk external at most and its diff with the version the admin last
// approved (an empty flow when none) raises none of LowDeployRefusedAlerts.
// The refusal lists the alert kinds, never values.
func (a *Authoring) lowPolicy(flowID string, candidate *flow.Flow, risk node.Risk) error {
	var reasons []string
	if risk != node.RiskSafe && risk != node.RiskExternal {
		reasons = append(reasons, fmt.Sprintf("it deploys flows of risk external at most, and this flow is %s", risk))
	}
	approved, err := a.store.LastAdminApprovedVersion(flowID)
	if err != nil {
		return fmt.Errorf("get the last approved version: %w", err)
	}
	var base *flow.Flow
	if approved != nil {
		base = approved.Flow
	}
	found := map[string]bool{}
	for _, al := range flow.Diff(base, candidate, a.registry).Alerts {
		found[al.Kind] = true
	}
	var kinds []string
	for _, kind := range lowDeployRefusedAlerts {
		if found[kind] {
			kinds = append(kinds, kind)
		}
	}
	if len(kinds) > 0 {
		reasons = append(reasons, fmt.Sprintf("since the version the admin last approved, this flow changed what only the admin may approve (%s)",
			strings.Join(kinds, ", ")))
	}
	if len(reasons) > 0 {
		return toolError(ErrPermissionDenied, "the agent deploy level is low: "+strings.Join(reasons, "; ")+"; the admin deploys it in the editor")
	}
	return nil
}

// recheckDeploy runs under the lifecycle lock right before a deploy or stop
// (H3): the token is still active with the deploy scope, the level still
// allows what was evaluated, and the flow is still at the evaluated version
// (else conflict). Nothing is deployed otherwise.
func (a *Authoring) recheckDeploy(actor Actor, plan *deployPlan) error {
	tok, err := a.store.GetAPIToken(actor.TokenID)
	if errors.Is(err, storage.ErrAPITokenNotFound) {
		return toolError(ErrPermissionDenied, "this token is no longer valid")
	}
	if err != nil {
		return fmt.Errorf("get token: %w", err)
	}
	if !tok.Active(a.now()) || !slices.Contains(tok.Scopes, storage.ScopeDeploy) {
		return toolError(ErrPermissionDenied, "this token is no longer valid for deploying")
	}
	level, err := a.store.AgentDeployLevel()
	if err != nil {
		return fmt.Errorf("read the agent deploy level: %w", err)
	}
	if level == storage.AgentDeployOff || (level == storage.AgentDeployLow && plan.level != storage.AgentDeployLow) {
		return toolError(ErrPermissionDenied, "the agent deploy level changed; ask again")
	}
	current, err := a.loadFlow(plan.flowID)
	if err != nil {
		return err
	}
	if current.Version != plan.version {
		return staleBase(current.Version)
	}
	return nil
}

// deployRefusal maps a lifecycle refusal for agents; saved is the version a
// draft was saved as before the deploy failed (0: none).
func deployRefusal(err error, saved int) error {
	var le *LifecycleError
	if !errors.As(err, &le) {
		return err
	}
	te := le.ToolError()
	if te == nil {
		return err
	}
	if saved > 0 {
		te.Message = fmt.Sprintf("the draft was saved as version %d, but deploying it failed: %s", saved, te.Message)
		te.CurrentVersion = saved
	}
	return te
}

// Stop stops a running flow for an agent with the deploy scope
// (pw_stop_flow), under the same level as Deploy, evaluated on the current
// version (low: risk at most external, none of LowDeployRefusedAlerts
// against the last admin-approved version), through the lifecycle lock with
// the same token re-check. A flow that is not running is a conflict.
// Audited.
func (a *Authoring) Stop(ctx context.Context, actor Actor, flowID string) (_ DeployResult, err error) {
	args := &authorArgs{FlowID: flowID}
	var privileged bool
	defer a.audited(actor, DeployToolStopFlow, args, a.now(), &privileged)(&err)
	level, err := a.authorizeDeploy(ctx, actor)
	if err != nil {
		return DeployResult{}, err
	}
	plan, err := a.evaluateDeploy(ctx, level, actor, flowID, "", true)
	if plan != nil {
		privileged = plan.risk == node.RiskPrivileged
	}
	if err != nil {
		return DeployResult{}, err
	}
	err = a.lifecycle.WithLock(ctx, func(l LockedLifecycle) error {
		if err := a.recheckDeploy(actor, plan); err != nil {
			return err
		}
		if err := l.Stop(ctx, flowID, actor.DeployActor()); err != nil {
			return deployRefusal(err, 0)
		}
		return nil
	})
	if err != nil {
		return DeployResult{}, err
	}
	args.Version = plan.version
	return DeployResult{FlowID: flowID, Version: plan.version, Running: false, Risk: plan.risk}, nil
}

// Rollback restores, for the editor, the last admin-approved version of a
// flow as a new version (VersionMeta{editor, admin-approved, note "rollback
// to vN"}), under the lifecycle lock, and redeploys it when the flow is
// running (a failed redeploy is DeployError; the version is saved).
// Conflict when the current version is already admin-approved or there is
// none to restore. Agents: forbidden.
func (a *Authoring) Rollback(ctx context.Context, actor Actor, flowID string) (ApplyResult, error) {
	if err := authorizeEditor(ctx, actor); err != nil {
		return ApplyResult{}, err
	}
	if a.lifecycle == nil {
		return ApplyResult{}, toolError(ErrUnavailable, "flows cannot be rolled back now")
	}
	var out ApplyResult
	err := a.lifecycle.WithLock(ctx, func(l LockedLifecycle) error {
		current, err := a.loadFlow(flowID)
		if err != nil {
			return err
		}
		approved, err := a.store.LastAdminApprovedVersion(flowID)
		if err != nil {
			return fmt.Errorf("get the last approved version: %w", err)
		}
		switch {
		case approved == nil:
			return toolError(ErrConflict, "no version of this flow was approved by the admin")
		case approved.Version == current.Version:
			return toolError(ErrConflict, "the current version is the one the admin last approved: nothing to roll back")
		}
		restored := approved.Flow
		restored.ID = flowID
		version, err := a.store.UpdateFlowVersioned(restored, current.Version, storage.VersionMeta{
			ActorKind: storage.VersionActorEditor, AdminApproved: true, Note: fmt.Sprintf("rollback to v%d", approved.Version),
		})
		var conflict *storage.VersionConflictError
		if errors.As(err, &conflict) {
			return staleBase(conflict.Current)
		}
		if err != nil {
			return fmt.Errorf("save rollback: %w", err)
		}
		out = ApplyResult{FlowID: flowID, Version: version}
		if l.Running(flowID) {
			out.Version, out.Deployed, out.DeployError = a.redeploy(ctx, l, flowID, version, true)
		}
		return nil
	})
	return out, err
}

// Versions lists, for the editor, a flow's versions newest first (limit 1 to
// MaxVersionListLimit, 0: DefaultVersionListLimit; metadata only), its
// current and last admin-approved version, and which one the deployment
// marker names. Agents: forbidden; an unknown flow: flow_not_found.
func (a *Authoring) Versions(ctx context.Context, actor Actor, flowID string, limit int) (VersionList, error) {
	if err := authorizeEditor(ctx, actor); err != nil {
		return VersionList{}, err
	}
	current, err := a.loadFlow(flowID)
	if err != nil {
		return VersionList{}, err
	}
	if limit <= 0 {
		limit = DefaultVersionListLimit
	}
	limit = min(limit, MaxVersionListLimit)
	versions, err := a.store.ListFlowVersions(flowID, limit)
	if err != nil {
		return VersionList{}, fmt.Errorf("list flow versions: %w", err)
	}
	approved, err := a.store.LastAdminApprovedVersion(flowID)
	if err != nil {
		return VersionList{}, fmt.Errorf("get the last approved version: %w", err)
	}
	deployment, err := a.store.GetDeployment(flowID)
	if err != nil {
		return VersionList{}, fmt.Errorf("get deployment: %w", err)
	}
	out := VersionList{Current: current.Version, Versions: make([]VersionEntry, 0, len(versions))}
	if approved != nil {
		out.LastAdminApproved = approved.Version
	}
	for _, v := range versions {
		out.Versions = append(out.Versions, VersionEntry{
			Version: v.Version, ActorKind: v.ActorKind, TokenID: v.TokenID, TokenName: v.TokenName,
			AdminApproved: v.AdminApproved, Note: v.Note, DraftID: v.DraftID, CreatedAt: v.CreatedAt,
			Deployed: deployment != nil && deployment.Version == v.Version,
		})
	}
	return out, nil
}

// Marks returns, for the editor, the marks of a stored flow: its deployment
// marker and whether its current version (f.Version) is not admin-approved.
// Agents: forbidden.
func (a *Authoring) Marks(ctx context.Context, actor Actor, f *flow.Flow) (FlowMarks, error) {
	if err := authorizeEditor(ctx, actor); err != nil {
		return FlowMarks{}, err
	}
	return FlowMarksOf(a.store, f)
}
