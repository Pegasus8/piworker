package api

import (
	"context"
	"errors"
	"slices"

	"github.com/Pegasus8/piworker/internal/flow"
)

// Broker connections and the flows that use them (ADR-0029): a changed
// connection restarts the running flows that use it, and a connection a
// running flow uses cannot be deleted. Both go through the editor's
// lifecycle lock, so they never interleave with a deploy or a stop.

// FlowRef names a flow in a response.
type FlowRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// FlowRestartFailure is a flow that a restart stopped and could not deploy
// again (it is now failed), with the reason.
type FlowRestartFailure struct {
	FlowRef
	Error string `json:"error"`
}

// runningUsersLocked returns the running flows whose enabled nodes name the
// connection (flow.ConnectionsUsed), in the order the manager lists them;
// callers hold lifecycleMu.
func (h *FlowsHandler) runningUsersLocked(name string) []*flow.Flow {
	var out []*flow.Flow
	for _, id := range h.manager.ListRunning() {
		f, err := h.store.GetFlow(id)
		if err != nil {
			h.logger.Error().Err(err).Str("flowID", id).Msg("Failed to load a running flow")
			continue
		}
		if slices.Contains(flow.ConnectionsUsed(f, h.registry), name) {
			out = append(out, f)
		}
	}
	slices.SortStableFunc(out, func(a, b *flow.Flow) int {
		if a.Name != b.Name {
			if a.Name < b.Name {
				return -1
			}
			return 1
		}
		if a.ID < b.ID {
			return -1
		}
		return 1
	})
	return out
}

// RestartFlowsUsing restarts every running flow that uses the broker
// connection name, so it runs with the connection's current settings: each
// is stopped and deployed again (the same stored version; the deployment
// marker is kept). A flow that cannot be deployed again is marked failed
// and reported. After BeginShutdown nothing is restarted.
func (h *FlowsHandler) RestartFlowsUsing(ctx context.Context, name string) ([]FlowRef, []FlowRestartFailure) {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	restarted, failed := []FlowRef{}, []FlowRestartFailure{}
	if h.shuttingDown {
		return restarted, failed
	}
	for _, f := range h.runningUsersLocked(name) {
		ref := FlowRef{ID: f.ID, Name: f.Name}
		if err := h.restartLocked(ctx, f); err != nil {
			failed = append(failed, FlowRestartFailure{FlowRef: ref, Error: err.Error()})
			continue
		}
		restarted = append(restarted, ref)
	}
	return restarted, failed
}

// restartLocked stops f and deploys it again; callers hold lifecycleMu. A
// failed deploy leaves the flow stopped and stored as failed.
func (h *FlowsHandler) restartLocked(ctx context.Context, f *flow.Flow) error {
	if err := h.manager.Undeploy(f.ID); err != nil {
		h.logger.Error().Err(err).Str("flowID", f.ID).Msg("Failed to stop a flow for a connection change")
		return err
	}
	if err := h.manager.Deploy(ctx, f); err != nil {
		h.logger.Error().Err(err).Str("flowID", f.ID).Msg("Flow could not restart after a connection change")
		if serr := h.store.UpdateFlowState(f.ID, flow.FlowStateFailed); serr != nil {
			h.logger.Error().Err(serr).Str("flowID", f.ID).Msg("Failed to mark the flow failed")
		}
		return err
	}
	h.logger.Info().Str("flowID", f.ID).Str("flowName", f.Name).Msg("Flow restarted after a connection change")
	return nil
}

// ErrConnectionInUse refuses deleting a broker connection a running flow
// uses.
var ErrConnectionInUse = errors.New("the connection is used by running flows")

// UnlessRunningFlowsUse runs fn (the deletion of the connection name) under
// the lifecycle lock unless a running flow uses the connection; then it
// returns those flows and ErrConnectionInUse, and fn does not run.
func (h *FlowsHandler) UnlessRunningFlowsUse(name string, fn func() error) ([]FlowRef, error) {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	if users := h.runningUsersLocked(name); len(users) > 0 {
		refs := make([]FlowRef, 0, len(users))
		for _, f := range users {
			refs = append(refs, FlowRef{ID: f.ID, Name: f.Name})
		}
		return refs, ErrConnectionInUse
	}
	return nil, fn()
}
