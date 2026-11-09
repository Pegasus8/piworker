package service

import (
	"context"
	"net/http"
	"slices"
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
// contract's eight plus credentialed_request_changed (hand-off H1).
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
func (a *Authoring) Deploy(ctx context.Context, actor Actor, req DeployRequest) (DeployResult, error) {
	return DeployResult{}, notImplemented("deploy")
}

// Stop stops a running flow for an agent with the deploy scope
// (pw_stop_flow), under the same level as Deploy, evaluated on the current
// version (low: risk at most external, none of LowDeployRefusedAlerts
// against the last admin-approved version), through the lifecycle lock with
// the same token re-check. A flow that is not running is a conflict.
// Audited.
func (a *Authoring) Stop(ctx context.Context, actor Actor, flowID string) (DeployResult, error) {
	return DeployResult{}, notImplemented("stop")
}

// Rollback restores, for the editor, the last admin-approved version of a
// flow as a new version (VersionMeta{editor, admin-approved, note "rollback
// to vN"}), under the lifecycle lock, and redeploys it when the flow is
// running (a failed redeploy is DeployError; the version is saved).
// Conflict when the current version is already admin-approved or there is
// none to restore. Agents: forbidden.
func (a *Authoring) Rollback(ctx context.Context, actor Actor, flowID string) (ApplyResult, error) {
	return ApplyResult{}, notImplemented("rollback")
}

// Versions lists, for the editor, a flow's versions newest first (limit 1 to
// MaxVersionListLimit, 0: DefaultVersionListLimit; metadata only), its
// current and last admin-approved version, and which one the deployment
// marker names. Agents: forbidden; an unknown flow: flow_not_found.
func (a *Authoring) Versions(ctx context.Context, actor Actor, flowID string, limit int) (VersionList, error) {
	return VersionList{}, notImplemented("versions")
}

// Marks returns, for the editor, the marks of a stored flow: its deployment
// marker and whether its current version (f.Version) is not admin-approved.
// Agents: forbidden.
func (a *Authoring) Marks(ctx context.Context, actor Actor, f *flow.Flow) (FlowMarks, error) {
	return FlowMarks{}, notImplemented("marks")
}
