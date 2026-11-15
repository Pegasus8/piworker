package service

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"sync"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/rs/zerolog"
)

// LifecycleStore is the storage the lifecycle path reads and writes.
type LifecycleStore interface {
	GetFlow(id string) (*flow.Flow, error)
	GetRunningFlows() ([]*flow.Flow, error)
	UpdateFlowState(id string, state flow.FlowState) error
	RecordDeployment(flowID string, d storage.Deployment) error
	GetDeployment(flowID string) (*storage.Deployment, error)
	GetFlowVersion(flowID string, version int) (*storage.FlowVersion, error)
}

// Lifecycle is the one lifecycle path (docs/design/mcp.md §10, ADR-0034):
// every deploy, stop and restart, the editor's requests, approved drafts,
// rollbacks, agent deploys, connection changes and the restore at startup,
// runs here under one lock, persists the restart intent and records the
// deployment marker. It owns the lock, so storage and the runtime manager
// agree; handlers and the MCP server only call it.
type Lifecycle struct {
	// mu serializes definition and lifecycle mutations.
	mu sync.Mutex
	// shuttingDown (guarded by mu) refuses new deployments once the process
	// has begun stopping, so nothing starts after StopAll.
	shuttingDown bool
	store        LifecycleStore
	manager      *flow.RuntimeManager
	logger       zerolog.Logger
}

// NewLifecycle creates the lifecycle path over the store and the runtimes.
func NewLifecycle(store LifecycleStore, manager *flow.RuntimeManager, logger zerolog.Logger) *Lifecycle {
	return &Lifecycle{store: store, manager: manager, logger: logger}
}

// Runtimes is the runtime manager the lifecycle deploys to, for reads
// (a runtime's warnings, tools, injecting a trigger). Deploying or
// undeploying through it directly goes around the lock: use the Lifecycle.
func (l *Lifecycle) Runtimes() *flow.RuntimeManager { return l.manager }

// Running reports whether flow id is deployed and running, without the
// lock (FlowLifecycle): an advisory read for views; what acts on the flow
// reads it again under the lock.
func (l *Lifecycle) Running(id string) bool {
	rt, ok := l.manager.GetRuntime(id)
	return ok && rt.IsRunning()
}

// BeginShutdown makes every later deploy fail with 503. It waits for any
// lifecycle change already in progress, so once it returns no new flow can
// start.
func (l *Lifecycle) BeginShutdown() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.shuttingDown = true
}

// Hold takes the lifecycle lock: until Release no save, deploy, stop,
// toggle or delete runs elsewhere. The lock is not reentrant.
func (l *Lifecycle) Hold() *HeldLifecycle {
	l.mu.Lock()
	return &HeldLifecycle{l: l}
}

// WithLock runs fn holding the lifecycle lock (FlowLifecycle). The
// LockedLifecycle fn gets is valid only while fn runs.
func (l *Lifecycle) WithLock(ctx context.Context, fn func(LockedLifecycle) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	held := l.Hold()
	defer held.Release()
	return fn(held)
}

// HeldLifecycle is the lifecycle while its lock is held (Lifecycle.Hold);
// it is the LockedLifecycle of a WithLock call. After Release it refuses to
// act.
type HeldLifecycle struct{ l *Lifecycle }

// errLifecycleExpired refuses a HeldLifecycle used after its Release (a
// caller bug: the lock is no longer held).
var errLifecycleExpired = errors.New("lifecycle used outside its lock")

// Release frees the lock. Calling it again does nothing.
func (h *HeldLifecycle) Release() {
	if h.l == nil {
		return
	}
	l := h.l
	h.l = nil
	l.mu.Unlock()
}

func expired() *LifecycleError {
	return &LifecycleError{Status: http.StatusInternalServerError, Message: errLifecycleExpired.Error(), Err: errLifecycleExpired}
}

// Running reports whether flowID is deployed and running.
func (h *HeldLifecycle) Running(flowID string) bool {
	return h.l != nil && h.l.Running(flowID)
}

// Deploy implements LockedLifecycle.
func (h *HeldLifecycle) Deploy(ctx context.Context, flowID string, by DeployActor, restart bool) (int, error) {
	f, le := h.Start(ctx, flowID, StartOptions{Strict: true, Restart: restart, By: by})
	if le != nil {
		return 0, le
	}
	return f.Version, nil
}

// Stop implements LockedLifecycle.
func (h *HeldLifecycle) Stop(_ context.Context, flowID string, _ DeployActor) error {
	if _, le := h.Halt(flowID, true); le != nil {
		return le
	}
	return nil
}

// StartOptions say how Start deploys.
type StartOptions struct {
	// Expected, when not 0, is the version the client shows: another
	// stored one is a 409 carrying the current version.
	Expected int
	// Strict makes a running flow a 409 (unless Restart); not strict
	// (toggling on a running flow) only persists the intent.
	Strict bool
	// Restart stops a running instance and deploys the stored version.
	Restart bool
	// By is who deploys (the deployment marker); the zero value is the
	// editor.
	By DeployActor
}

func refuse(status int, message string, err error) *LifecycleError {
	return &LifecycleError{Status: status, Message: message, Err: err}
}

// load reads the stored flow id for a deploy or stop.
func (l *Lifecycle) load(id string) (*flow.Flow, *LifecycleError) {
	if id == "" {
		return nil, refuse(http.StatusBadRequest, "Flow ID is required", nil)
	}
	f, err := l.store.GetFlow(id)
	if errors.Is(err, storage.ErrFlowNotFound) {
		return nil, refuse(http.StatusNotFound, "Flow not found", err)
	}
	if err != nil {
		l.logger.Error().Err(err).Str("flowID", id).Msg("Failed to get flow")
		return nil, refuse(http.StatusInternalServerError, "Failed to get flow", err)
	}
	return f, nil
}

// Start deploys the stored current version of flow id and persists its
// restart intent. A redeploy failing after the old instance stopped leaves
// the flow stopped with its running intent. Every deployment it makes is
// recorded with opts.By (the marker names the deployed version); a marker
// that cannot be recorded undoes the deployment, so what runs is always
// attributed. After BeginShutdown it refuses with 503.
func (h *HeldLifecycle) Start(ctx context.Context, id string, opts StartOptions) (*flow.Flow, *LifecycleError) {
	l := h.l
	if l == nil {
		return nil, expired()
	}
	if l.shuttingDown {
		return nil, refuse(http.StatusServiceUnavailable, "PiWorker is shutting down", nil)
	}
	f, ref := l.load(id)
	if ref != nil {
		return nil, ref
	}
	if opts.Expected != 0 && f.Version != opts.Expected {
		return nil, &LifecycleError{
			Status:         http.StatusConflict,
			Message:        "The flow was changed since you loaded it; reload it to see the version you would deploy",
			CurrentVersion: f.Version,
			Err:            storage.ErrVersionConflict,
		}
	}
	by := opts.By
	if by.Kind == "" {
		by.Kind = ActorEditor
	}
	running := l.Running(id)
	if running && opts.Strict && !opts.Restart {
		return nil, refuse(http.StatusConflict, "Flow is already running", flow.ErrFlowAlreadyRunning)
	}
	deploy := !running || opts.Restart
	if running && opts.Restart {
		if err := l.manager.Undeploy(id); err != nil {
			l.logger.Error().Err(err).Str("flowID", id).Msg("Failed to stop flow for a redeploy")
			return nil, refuse(http.StatusInternalServerError, "Failed to stop flow", err)
		}
	}
	if deploy {
		if err := l.manager.Deploy(ctx, f); err != nil {
			return nil, refuse(DeployErrorStatus(err), "Failed to deploy flow: "+err.Error(), err)
		}
	}
	undo := func() {
		if !deploy {
			return
		}
		if err := l.manager.Undeploy(id); err != nil {
			l.logger.Error().Err(err).Str("flowID", id).Msg("Failed to roll back deployment")
		}
	}
	// State changes are not new versions of the definition.
	oldState := f.State
	f.State = flow.FlowStateRunning
	if err := l.store.UpdateFlowState(id, f.State); err != nil {
		// A newly started flow must not continue after a failed deployment response.
		undo()
		return nil, refuse(http.StatusInternalServerError, "Failed to persist flow state", err)
	}
	if deploy {
		if err := l.store.RecordDeployment(id, by.Deployment(f.Version)); err != nil {
			l.logger.Error().Err(err).Str("flowID", id).Msg("Failed to record the deployment")
			undo()
			// A redeploy's flow was meant to run before; a new one was not.
			if !running && oldState != f.State {
				if err := l.store.UpdateFlowState(id, oldState); err != nil {
					l.logger.Error().Err(err).Str("flowID", id).Msg("Failed to restore flow state")
				}
			}
			return nil, refuse(http.StatusInternalServerError, "Failed to record the deployment", err)
		}
	}
	return f, nil
}

// Halt stops flow id and persists its stop intent. A flow that is not
// running is a 409 when strict. The deployment marker is kept (it names the
// last deployment). Allowed during shutdown.
func (h *HeldLifecycle) Halt(id string, strict bool) (*flow.Flow, *LifecycleError) {
	l := h.l
	if l == nil {
		return nil, expired()
	}
	f, ref := l.load(id)
	if ref != nil {
		return nil, ref
	}
	rt, exists := l.manager.GetRuntime(id)
	running := exists && rt.IsRunning()
	if strict && !running {
		return nil, refuse(http.StatusConflict, "Flow is not running", flow.ErrFlowNotRunning)
	}
	oldState := f.State
	f.State = flow.FlowStateInactive
	// Persist stop intent first: if storage is unavailable, leave execution alone.
	if err := l.store.UpdateFlowState(id, f.State); err != nil {
		return nil, refuse(http.StatusInternalServerError, "Failed to persist flow state", err)
	}
	if exists {
		if err := l.manager.Undeploy(id); err != nil {
			f.State = oldState
			if rollbackErr := l.store.UpdateFlowState(id, f.State); rollbackErr != nil {
				l.logger.Error().Err(rollbackErr).Msg("Failed to restore flow state")
			}
			return nil, refuse(http.StatusInternalServerError, "Failed to stop flow", err)
		}
	}
	return f, nil
}

// Discard stops flow id if it runs, without touching storage: its
// definition is about to be deleted.
func (h *HeldLifecycle) Discard(id string) {
	l := h.l
	if l == nil || !l.Running(id) {
		return
	}
	if err := l.manager.Undeploy(id); err != nil {
		l.logger.Warn().Err(err).Str("flowID", id).Msg("Failed to stop flow before deletion")
	}
}

// DeployErrorStatus maps a runtime deploy error to an HTTP status: problems
// with the flow definition are client errors (400), a previous instance
// that is still stopping or already running is a conflict (409), anything
// else is a 500.
func DeployErrorStatus(err error) int {
	var verr *flow.ValidationError
	switch {
	case errors.As(err, &verr),
		errors.Is(err, flow.ErrNoTriggerNodes),
		errors.Is(err, flow.ErrInvalidNodeType),
		errors.Is(err, flow.ErrInvalidNodeConfig),
		errors.Is(err, flow.ErrInvalidConnection):
		return http.StatusBadRequest
	case errors.Is(err, flow.ErrFlowStillStopping),
		errors.Is(err, flow.ErrFlowAlreadyRunning),
		errors.Is(err, flow.ErrResourceConflict):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// Broker connections and the flows that use them (ADR-0029): a changed
// connection restarts the running flows that use it, and a connection a
// running flow uses cannot be deleted. Both hold the lifecycle lock, so
// they never interleave with a deploy or a stop.

// FlowRef names a flow in a response.
type FlowRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func flowRef(f *flow.Flow) FlowRef { return FlowRef{ID: f.ID, Name: f.Name} }

// FlowRestartFailure is a flow that a restart stopped and could not deploy
// again (it is now failed), with the reason.
type FlowRestartFailure struct {
	FlowRef
	Error string `json:"error"`
}

// runningUsers returns the definitions of the running flows whose nodes
// name any of the connections, sorted by name then ID. It asks the running
// instances (FlowRuntime.ConnectionsUsed): no storage read, no node built,
// and it is the definition that runs. Callers hold mu.
func (l *Lifecycle) runningUsers(names []string) []*flow.Flow {
	var out []*flow.Flow
	for _, id := range l.manager.ListRunning() {
		rt, ok := l.manager.GetRuntime(id)
		if !ok {
			continue
		}
		if slices.ContainsFunc(rt.ConnectionsUsed(), func(c string) bool { return slices.Contains(names, c) }) {
			out = append(out, rt.Flow())
		}
	}
	slices.SortFunc(out, func(a, b *flow.Flow) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
	})
	return out
}

// RestartFlowsUsing restarts, once each, every running flow that uses any
// of the broker connections names, so it runs with their current settings:
// each is stopped and deployed again (the definition that ran; the
// deployment marker is kept). A flow that cannot be deployed again is
// marked failed and reported. After BeginShutdown nothing is restarted.
func (l *Lifecycle) RestartFlowsUsing(ctx context.Context, names []string) ([]FlowRef, []FlowRestartFailure) {
	l.mu.Lock()
	defer l.mu.Unlock()
	restarted, failed := []FlowRef{}, []FlowRestartFailure{}
	if l.shuttingDown || len(names) == 0 {
		return restarted, failed
	}
	for _, f := range l.runningUsers(names) {
		if err := l.restart(ctx, f); err != nil {
			failed = append(failed, FlowRestartFailure{FlowRef: flowRef(f), Error: err.Error()})
			continue
		}
		restarted = append(restarted, flowRef(f))
	}
	return restarted, failed
}

// restart stops f and deploys it again; callers hold mu. A failed deploy
// leaves the flow stopped and stored as failed.
func (l *Lifecycle) restart(ctx context.Context, f *flow.Flow) error {
	if err := l.manager.Undeploy(f.ID); err != nil {
		l.logger.Error().Err(err).Str("flowID", f.ID).Msg("Failed to stop a flow for a connection change")
		return err
	}
	if err := l.manager.Deploy(ctx, f); err != nil {
		l.logger.Error().Err(err).Str("flowID", f.ID).Msg("Flow could not restart after a connection change")
		if serr := l.store.UpdateFlowState(f.ID, flow.FlowStateFailed); serr != nil {
			l.logger.Error().Err(serr).Str("flowID", f.ID).Msg("Failed to mark the flow failed")
		}
		return err
	}
	l.logger.Info().Str("flowID", f.ID).Str("flowName", f.Name).Msg("Flow restarted after a connection change")
	return nil
}

// UnlessRunningFlowsUse runs fn (the deletion of the connection name)
// under the lifecycle lock unless a running flow uses the connection; then
// it returns those flows and fn does not run.
func (l *Lifecycle) UnlessRunningFlowsUse(name string, fn func() error) ([]FlowRef, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	users := l.runningUsers([]string{name})
	if len(users) == 0 {
		return nil, fn()
	}
	refs := make([]FlowRef, 0, len(users))
	for _, f := range users {
		refs = append(refs, flowRef(f))
	}
	return refs, nil
}

// RestoreRunning starts the flows that were running before the process
// stopped (startup) and reports how many started and how many could not
// (those are stored as failed). Settings that no longer pass the
// deploy-time schema check (values saved before it existed) are logged, not
// fatal: the flow keeps running as it did, and the next explicit deploy
// asks for a fix. After BeginShutdown nothing is restored.
func (l *Lifecycle) RestoreRunning(ctx context.Context) (restored, failed int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shuttingDown {
		return 0, 0
	}
	flows, err := l.store.GetRunningFlows()
	if err != nil {
		l.logger.Error().Err(err).Msg("Failed to get previously running flows")
		return 0, 0
	}
	if len(flows) == 0 {
		l.logger.Debug().Msg("No flows to restore")
		return 0, 0
	}
	l.logger.Info().Int("count", len(flows)).Msg("Restoring previously running flows")
	for _, f := range flows {
		issues, err := l.manager.Restore(ctx, f)
		for _, issue := range issues {
			l.logger.Warn().Str("flowID", f.ID).Str("flowName", f.Name).Str("path", issue.Path).Str("problem", issue.Message).
				Msg("Restored flow has a setting that the next deploy will refuse; the node clamps or defaults it as before")
		}
		if err != nil {
			l.logger.Error().Err(err).Str("flowID", f.ID).Str("flowName", f.Name).Msg("Failed to restore flow")
			failed++
			if updateErr := l.store.UpdateFlowState(f.ID, flow.FlowStateFailed); updateErr != nil {
				l.logger.Error().Err(updateErr).Str("flowID", f.ID).Msg("Failed to update flow state to failed")
			}
			continue
		}
		l.logger.Info().Str("flowID", f.ID).Str("flowName", f.Name).Msg("Flow restored successfully")
		restored++
		// What an editor deploy would answer as warnings (an undefined
		// secret an optional credential references, ...): logged, as
		// nobody sees a response at startup.
		if rt, ok := l.manager.GetRuntime(f.ID); ok {
			for _, is := range rt.Warnings() {
				l.logger.Warn().Str("flowID", f.ID).Str("flowName", f.Name).Str("code", is.Code).Str("path", is.Path).
					Str("problem", is.Message).Msg("Restored flow has a warning")
			}
		}
		if err := l.markRestored(f); err != nil {
			l.logger.Error().Err(err).Str("flowID", f.ID).Msg("Failed to record who deployed the restored version")
		}
	}
	l.logger.Info().Int("restored", restored).Int("failed", failed).Msg("Flow restoration complete")
	return restored, failed
}

// markRestored keeps the deployment marker of a restored flow true (H6): a
// restore is not a new deployment, so a marker that names the restored
// version is kept; one that names another version (a redeploy that failed
// after a save left the running intent on the newer version, such as an
// agent's draft) is replaced by the version's own author, who saved it to
// run. A flow without stored versions keeps its marker.
func (l *Lifecycle) markRestored(f *flow.Flow) error {
	d, err := l.store.GetDeployment(f.ID)
	if err != nil || (d != nil && d.Version == f.Version) {
		return err
	}
	v, err := l.store.GetFlowVersion(f.ID, f.Version)
	if errors.Is(err, storage.ErrFlowVersionNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return l.store.RecordDeployment(f.ID, storage.Deployment{Version: f.Version, ActorKind: v.ActorKind, TokenID: v.TokenID, TokenName: v.TokenName})
}
