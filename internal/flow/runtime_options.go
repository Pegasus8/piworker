package flow

import (
	"context"
	flowvars "github.com/Pegasus8/piworker/internal/vars"
	"time"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog"
)

// RuntimeOption is a functional option for configuring the FlowRuntime.
type RuntimeOption func(*FlowRuntime)

// withLenientConfig makes NewFlowRuntime instantiate nodes whose config does
// not match their schema instead of refusing the flow.
func withLenientConfig() RuntimeOption {
	return func(r *FlowRuntime) {
		r.lenientConfig = true
	}
}

// WithObserver sets the structured per-execution observer hook. The hook must
// not block (see NodeEventHook).
func WithObserver(hook NodeEventHook) RuntimeOption {
	return func(r *FlowRuntime) {
		r.observer = hook
	}
}

// WithNodeContext wraps the context every node of the runtime gets, trigger
// Start and Process alike: wrap adds values (an isolated secret store,
// variable stores) to the runtime's context and returns it. It must keep its
// argument as the parent (the runtime cancels it on Stop) and should only add
// values. When the result carries a flow variable store (vars.WithFlowStore),
// the runtime uses it and does not load the flow's persistent store at all.
func WithNodeContext(wrap func(context.Context) context.Context) RuntimeOption {
	return func(r *FlowRuntime) {
		r.nodeContext = wrap
	}
}

// WithFlowVariables sets the per-flow variable stores the runtime takes its
// flow's persistent store from (shared with the API that manages them).
// Without it the runtime uses flowvars.DefaultFlows.
func WithFlowVariables(stores *flowvars.FlowStores) RuntimeOption {
	return func(r *FlowRuntime) {
		if stores != nil {
			r.flowVars = stores
		}
	}
}

// withoutMetrics keeps the runtime's executions, errors, retries and drops
// out of the process-wide Prometheus metrics: a dry-run's private runtime
// must not show up on the operator's dashboards and alerts.
func withoutMetrics() RuntimeOption {
	return func(r *FlowRuntime) {
		r.noMetrics = true
	}
}

// withRouteGate asks gate before each message a trigger or a node emits is
// routed, with the number of copies routing makes of it (one per target);
// when gate returns false the message is not routed (and not counted in its
// run). The dry-run bounds its work with it. gate runs on the emitting
// node's goroutine and must not block.
func withRouteGate(gate func(msg *types.Message, copies int) bool) RuntimeOption {
	return func(r *FlowRuntime) {
		r.routeGate = gate
	}
}

// WithLogger sets a custom logger for the runtime.
func WithLogger(logger zerolog.Logger) RuntimeOption {
	return func(r *FlowRuntime) {
		r.logger = logger
	}
}

// WithRetryConfig sets the retry configuration for node execution.
func WithRetryConfig(config RetryConfig) RuntimeOption {
	return func(r *FlowRuntime) {
		r.retryConfig = config
	}
}

// WithMaxConcurrency sets the maximum number of nodes that may execute
// concurrently within the flow. Values <= 0 are ignored.
func WithMaxConcurrency(n int) RuntimeOption {
	return func(r *FlowRuntime) {
		if n > 0 {
			r.maxConcurrency = n
		}
	}
}

// WithNodeQueueLimit caps how many messages may wait for a single node before
// further ones are dropped. Values <= 0 are ignored.
func WithNodeQueueLimit(n int) RuntimeOption {
	return func(r *FlowRuntime) {
		if n > 0 {
			r.queueLimit = n
		}
	}
}

// WithStopTimeout sets how long Stop waits for the flow's goroutines to exit.
// Values <= 0 are ignored.
func WithStopTimeout(d time.Duration) RuntimeOption {
	return func(r *FlowRuntime) {
		if d > 0 {
			r.stopTimeout = d
		}
	}
}

// WithCallExpiryGrace sets how long a called run's waiter outlives the call's
// timeout before it expires (default DefaultCallExpiryGrace). Values <= 0 are
// ignored.
func WithCallExpiryGrace(d time.Duration) RuntimeOption {
	return func(r *FlowRuntime) {
		if d > 0 {
			r.calls.grace = d
		}
	}
}

// WithMaxCallWaiters caps how many called runs (see Call) the runtime tracks
// at once: runs still executing plus results not collected yet. Past it, the
// uncollected result closest to its expiry is evicted, or Call fails with
// ErrTooManyCalls. Default DefaultMaxCallWaiters; values <= 0 are ignored.
func WithMaxCallWaiters(n int) RuntimeOption {
	return func(r *FlowRuntime) {
		if n > 0 {
			r.calls.maxWaiters = n
		}
	}
}

// WithMaxCallResultBytes caps the serialized payload of a called run's reply:
// a bigger reply is not stored and the call ends as an error. Default
// DefaultMaxCallResultBytes; values <= 0 are ignored.
func WithMaxCallResultBytes(n int) RuntimeOption {
	return func(r *FlowRuntime) {
		if n > 0 {
			r.calls.maxResultBytes = n
		}
	}
}
