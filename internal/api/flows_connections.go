package api

import (
	"cmp"
	"context"
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

func flowRef(f *flow.Flow) FlowRef { return FlowRef{ID: f.ID, Name: f.Name} }

// compareFlowsByName orders flows by name, then ID (a total order).
func compareFlowsByName(a, b *flow.Flow) int {
	return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
}

// FlowRestartFailure is a flow that a restart stopped and could not deploy
// again (it is now failed), with the reason.
type FlowRestartFailure struct {
	FlowRef
	Error string `json:"error"`
}

// runningUsersLocked returns the definitions of the running flows whose
// nodes name any of the connections, sorted by name. It asks the running
// instances (FlowRuntime.ConnectionsUsed): no storage read, no node built,
// and it is the definition that runs. Callers hold lifecycleMu.
func (h *FlowsHandler) runningUsersLocked(names []string) []*flow.Flow {
	var out []*flow.Flow
	for _, id := range h.manager.ListRunning() {
		rt, ok := h.manager.GetRuntime(id)
		if !ok {
			continue
		}
		if slices.ContainsFunc(rt.ConnectionsUsed(), func(c string) bool { return slices.Contains(names, c) }) {
			out = append(out, rt.Flow())
		}
	}
	slices.SortFunc(out, compareFlowsByName)
	return out
}

// RestartFlowsUsing restarts, once each, every running flow that uses any
// of the broker connections names, so it runs with their current
// settings: each is stopped and deployed again (the definition that ran;
// the deployment marker is kept). A flow that cannot be deployed again is
// marked failed and reported. After BeginShutdown nothing is restarted.
func (h *FlowsHandler) RestartFlowsUsing(ctx context.Context, names []string) ([]FlowRef, []FlowRestartFailure) {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	restarted, failed := []FlowRef{}, []FlowRestartFailure{}
	if h.shuttingDown || len(names) == 0 {
		return restarted, failed
	}
	for _, f := range h.runningUsersLocked(names) {
		if err := h.restartLocked(ctx, f); err != nil {
			failed = append(failed, FlowRestartFailure{FlowRef: flowRef(f), Error: err.Error()})
			continue
		}
		restarted = append(restarted, flowRef(f))
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

// UnlessRunningFlowsUse runs fn (the deletion of the connection name) under
// the lifecycle lock unless a running flow uses the connection; then it
// returns those flows and fn does not run.
func (h *FlowsHandler) UnlessRunningFlowsUse(name string, fn func() error) ([]FlowRef, error) {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	users := h.runningUsersLocked([]string{name})
	if len(users) == 0 {
		return nil, fn()
	}
	refs := make([]FlowRef, 0, len(users))
	for _, f := range users {
		refs = append(refs, flowRef(f))
	}
	return refs, nil
}
