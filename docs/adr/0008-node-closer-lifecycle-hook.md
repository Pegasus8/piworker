# ADR-0008: `node.Closer` lifecycle hook

**Status:** Accepted
**Date:** 2026-11-08
**Deciders:** maintainer

## Context

Only triggers had a lifecycle (`Start`/`Stop`). Action nodes holding resources
(MQTT publish clients, database pools) were never released: every redeploy
leaked a broker connection that kept auto-reconnecting, and fixed client IDs
made old and new clients kick each other off the broker.

## Options considered

### Option A: Make every node implement Start/Stop
**Pros:** symmetric. **Cons:** churn across all nodes for a few that need it.

### Option B: Optional `Closer` interface called by the runtime
| Dimension | Assessment |
|---|---|
| Complexity | Low |
| Pi resources | Connections released on stop |
| Maintenance | Opt-in per node |

## Decision

`internal/node/lifecycle.go` defines `type Closer interface { Close() error }`.
The runtime calls `Close` on every node implementing it (triggers included)
after executors drain on `Stop`, and when a trigger fails to start (the start
is rolled back); errors are logged. `Close` is not called when deploy fails
before triggers start (node creation, validation), so nodes must acquire
resources lazily (on first use or in `Start`), never in their factory. If
stopping times out, `Close` waits until leftover goroutines exit, and redeploy
of the same flow is refused (`ErrFlowStillStopping`) until then.
Resource-holding nodes add a compile-time assertion (`var _ node.Closer = …`).
Shared resources use reference counting (database pools per DSN).

## Consequences

- Easier: adding nodes with network or file resources without leaks.
- Harder: node authors must remember to implement `Close` and keep it
  idempotent.
- Revisit when: nodes need a pre-start hook (e.g. warm-up).
