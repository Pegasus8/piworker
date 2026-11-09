package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
)

// The lifecycle path (docs/design/mcp.md §10): every deploy and stop, the
// editor's requests, approved drafts, rollbacks and agent deploys, runs here
// under lifecycleMu, persists the restart intent and records the deployment
// marker. FlowsHandler implements service.FlowLifecycle with it, so the
// service's agent deploys never go around the editor's lock and statuses.

// editorDeployer is who the editor's own requests deploy as.
var editorDeployer = service.DeployActor{Kind: service.ActorEditor}

// WithLock runs fn holding the lifecycle lock (service.FlowLifecycle): no
// editor save, deploy, stop, toggle or delete runs meanwhile. The
// LockedLifecycle fn gets is valid only while fn runs.
func (h *FlowsHandler) WithLock(ctx context.Context, fn func(service.LockedLifecycle) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	l := &lockedLifecycle{h: h}
	defer func() { l.h = nil }()
	return fn(l)
}

// Running reports whether flow id is deployed and running, without the
// lifecycle lock (service.FlowLifecycle): an advisory read for views; what
// acts on the flow reads it again under the lock. The runtime manager is
// safe for concurrent use.
func (h *FlowsHandler) Running(id string) bool {
	rt, ok := h.manager.GetRuntime(id)
	return ok && rt.IsRunning()
}

// lockedLifecycle is the service.LockedLifecycle of one WithLock call.
type lockedLifecycle struct{ h *FlowsHandler }

// errLifecycleExpired refuses a LockedLifecycle used after its WithLock
// returned (a caller bug: the lock is no longer held).
var errLifecycleExpired = errors.New("lifecycle used outside its lock")

func (l *lockedLifecycle) Running(flowID string) bool {
	if l.h == nil {
		return false
	}
	rt, ok := l.h.manager.GetRuntime(flowID)
	return ok && rt.IsRunning()
}

func (l *lockedLifecycle) Deploy(ctx context.Context, flowID string, by service.DeployActor, restart bool) (int, error) {
	if l.h == nil {
		return 0, errLifecycleExpired
	}
	f, ref := l.h.startLocked(ctx, flowID, 0, true, restart, by)
	if ref != nil {
		return 0, ref.lifecycleError()
	}
	return f.Version, nil
}

func (l *lockedLifecycle) Stop(_ context.Context, flowID string, _ service.DeployActor) error {
	if l.h == nil {
		return errLifecycleExpired
	}
	if _, ref := l.h.stopLocked(flowID, true); ref != nil {
		return ref.lifecycleError()
	}
	return nil
}

// lifecycleRefusal is why a deploy or stop did not happen: the status the
// editor answers, its message, the current version when the client's was
// stale, and the cause.
type lifecycleRefusal struct {
	status         int
	message        string
	currentVersion int
	err            error
}

func (r *lifecycleRefusal) lifecycleError() *service.LifecycleError {
	return &service.LifecycleError{Status: r.status, Message: r.message, Err: r.err}
}

// write answers the editor's request with the refusal.
func (r *lifecycleRefusal) write(w http.ResponseWriter) {
	if r.currentVersion != 0 {
		writeErrorData(w, r.status, r.message, VersionConflictData{CurrentVersion: r.currentVersion})
		return
	}
	writeError(w, r.status, r.message)
}

func refuse(status int, message string, err error) *lifecycleRefusal {
	return &lifecycleRefusal{status: status, message: message, err: err}
}

// loadForLifecycle reads the stored flow id for a deploy or stop.
func (h *FlowsHandler) loadForLifecycle(id string) (*flow.Flow, *lifecycleRefusal) {
	if id == "" {
		return nil, refuse(http.StatusBadRequest, "Flow ID is required", nil)
	}
	f, err := h.store.GetFlow(id)
	if errors.Is(err, storage.ErrFlowNotFound) {
		return nil, refuse(http.StatusNotFound, "Flow not found", err)
	}
	if err != nil {
		h.logger.Error().Err(err).Str("flowID", id).Msg("Failed to get flow")
		return nil, refuse(http.StatusInternalServerError, "Failed to get flow", err)
	}
	return f, nil
}

// startLocked deploys the stored current version of flow id and persists its
// restart intent; callers hold lifecycleMu. expected, when not 0, is the
// version the client shows (another stored one: 409 with the current
// version). A running flow is a 409 when strict and not restart; with
// restart its instance is stopped and the stored version deployed (a
// redeploy failing then leaves the flow stopped with its running intent); not
// strict (toggling on a running flow) it only persists the intent. Every
// deployment it makes is recorded with by (the marker names the deployed
// version); a marker that cannot be recorded undoes the deployment, so what
// runs is always attributed. After BeginShutdown it refuses with 503.
func (h *FlowsHandler) startLocked(ctx context.Context, id string, expected int, strict, restart bool, by service.DeployActor) (*flow.Flow, *lifecycleRefusal) {
	if h.shuttingDown {
		return nil, refuse(http.StatusServiceUnavailable, "PiWorker is shutting down", nil)
	}
	f, ref := h.loadForLifecycle(id)
	if ref != nil {
		return nil, ref
	}
	if expected != 0 && f.Version != expected {
		return nil, &lifecycleRefusal{
			status:         http.StatusConflict,
			message:        "The flow was changed since you loaded it; reload it to see the version you would deploy",
			currentVersion: f.Version,
			err:            storage.ErrVersionConflict,
		}
	}
	rt, exists := h.manager.GetRuntime(id)
	running := exists && rt.IsRunning()
	if running && strict && !restart {
		return nil, refuse(http.StatusConflict, "Flow is already running", flow.ErrFlowAlreadyRunning)
	}
	deploy := !running || restart
	if running && restart {
		if err := h.manager.Undeploy(id); err != nil {
			h.logger.Error().Err(err).Str("flowID", id).Msg("Failed to stop flow for a redeploy")
			return nil, refuse(http.StatusInternalServerError, "Failed to stop flow", err)
		}
	}
	if deploy {
		if err := h.manager.Deploy(ctx, f); err != nil {
			return nil, refuse(deployErrorStatus(err), "Failed to deploy flow: "+err.Error(), err)
		}
	}
	undo := func() {
		if !deploy {
			return
		}
		if err := h.manager.Undeploy(id); err != nil {
			h.logger.Error().Err(err).Str("flowID", id).Msg("Failed to roll back deployment")
		}
	}
	// State changes are not new versions of the definition.
	oldState := f.State
	f.State = flow.FlowStateRunning
	if err := h.store.UpdateFlowState(id, f.State); err != nil {
		// A newly started flow must not continue after a failed deployment response.
		undo()
		return nil, refuse(http.StatusInternalServerError, "Failed to persist flow state", err)
	}
	if deploy {
		if err := h.store.RecordDeployment(id, by.Deployment(f.Version)); err != nil {
			h.logger.Error().Err(err).Str("flowID", id).Msg("Failed to record the deployment")
			undo()
			// A redeploy's flow was meant to run before; a new one was not.
			if !running && oldState != f.State {
				if err := h.store.UpdateFlowState(id, oldState); err != nil {
					h.logger.Error().Err(err).Str("flowID", id).Msg("Failed to restore flow state")
				}
			}
			return nil, refuse(http.StatusInternalServerError, "Failed to record the deployment", err)
		}
	}
	return f, nil
}

// stopLocked stops flow id and persists its stop intent; callers hold
// lifecycleMu. A flow that is not running is a 409 when strict. The
// deployment marker is kept (it names the last deployment). Allowed during
// shutdown.
func (h *FlowsHandler) stopLocked(id string, strict bool) (*flow.Flow, *lifecycleRefusal) {
	f, ref := h.loadForLifecycle(id)
	if ref != nil {
		return nil, ref
	}
	rt, exists := h.manager.GetRuntime(id)
	running := exists && rt.IsRunning()
	if strict && !running {
		return nil, refuse(http.StatusConflict, "Flow is not running", flow.ErrFlowNotRunning)
	}
	oldState := f.State
	f.State = flow.FlowStateInactive
	// Persist stop intent first: if storage is unavailable, leave execution alone.
	if err := h.store.UpdateFlowState(id, f.State); err != nil {
		return nil, refuse(http.StatusInternalServerError, "Failed to persist flow state", err)
	}
	if exists {
		if err := h.manager.Undeploy(id); err != nil {
			f.State = oldState
			if rollbackErr := h.store.UpdateFlowState(id, f.State); rollbackErr != nil {
				h.logger.Error().Err(rollbackErr).Msg("Failed to restore flow state")
			}
			return nil, refuse(http.StatusInternalServerError, "Failed to stop flow", err)
		}
	}
	return f, nil
}
