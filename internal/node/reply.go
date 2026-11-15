package node

import (
	"context"
	"errors"
)

// ErrNotRunning is what a callable trigger returns when its flow is not
// running (flow.ErrFlowNotRunning is this error).
var ErrNotRunning = errors.New("flow is not running")

// Replier delivers the reply of a called run (a tool call). Reply reports
// whether the payload became the call's result: only the first reply of a
// run that is still waiting wins; replies for unknown or finished
// correlation IDs (e.g. a run not started by a call) return false. The
// payload is serialized inside the call, so the caller may keep mutating it
// afterwards. A payload that cannot be serialized, or whose JSON exceeds the
// runtime's maximum result size, is not a result: Reply returns false and,
// unless a later reply wins, the call ends as an error explaining why.
type Replier interface {
	Reply(correlationID string, payload any, isError bool) bool
}

type replierKey struct{}

// WithReplier returns ctx carrying the Replier of the runtime that runs the
// node. The flow runtime puts its own on every node's context.
func WithReplier(ctx context.Context, r Replier) context.Context {
	return context.WithValue(ctx, replierKey{}, r)
}

// ReplierFromContext returns the Replier of the flow runtime that owns ctx
// (the context nodes receive in Process). It is never nil: outside a
// runtime it returns a Replier whose Reply always reports false.
func ReplierFromContext(ctx context.Context) Replier {
	if ctx != nil {
		if r, ok := ctx.Value(replierKey{}).(Replier); ok && r != nil {
			return r
		}
	}
	return noReplier{}
}

type noReplier struct{}

func (noReplier) Reply(string, any, bool) bool { return false }
