# ADR-0034: A lifecycle service owns deploy, stop and restore

**Status:** Accepted
**Date:** 2026-11-15
**Deciders:** maintainer

## Context

Every deploy and stop has to hold one lock, persist the restart intent and
record the deployment marker (ADR-0018, ADR-0022, ADR-0029). That path lived
in `api.FlowsHandler`, an HTTP handler: agent authoring received the handler
as its `FlowLifecycle`, the connections and stores-restore handlers received
it to restart flows, and `gracefulShutdown` received it to refuse new
deploys. The restore at startup was a second path in `main.go` that deployed
through the runtime manager without the lock. Anything that needed the
lifecycle had to depend on the REST layer, and the handler held its own copy
of the runtime manager.

## Options considered

### Option A: Keep the path in the handler

**Pros:** nothing to move. **Cons:** the domain's central invariant is owned
by a transport; a second transport (MCP, a CLI) must import `internal/api`;
startup goes around the lock.

### Option B: A `service.Lifecycle` that owns lock, store and runtimes

| Dimension | Assessment |
|---|---|
| Complexity | Low: the code moves unchanged |
| Security | Same statuses and refusals; fewer ways around the lock |
| Pi resources | None |
| Maintenance | One place to read for "what happens on deploy" |

**Pros:** handlers are thin; the restore at startup shares the lock.
**Cons:** `internal/service` now holds HTTP statuses for the editor
(`LifecycleError.Status` already did).

## Decision

`service.Lifecycle` (`internal/service/lifecycle.go`) is the only lifecycle
path. It owns the lock, the shutdown flag, the lifecycle storage calls and
the runtime manager. `Hold()` returns a `HeldLifecycle` (`Start`, `Halt`,
`Discard`, and the `LockedLifecycle` interface agent deploys use);
`WithLock`, `RestartFlowsUsing`, `UnlessRunningFlowsUse`, `BeginShutdown`
and `RestoreRunning` take the lock themselves. `api.FlowsHandler` holds the
lock for definition edits through `Hold()` and maps `*LifecycleError` to
responses; it keeps no runtime manager of its own (reads go through
`Lifecycle.Runtimes()`). Nothing outside `Lifecycle` calls
`RuntimeManager.Deploy`, `Restore` or `Undeploy` in production code.

REST and MCP contracts, statuses and messages are unchanged.

## Consequences

- Easier: adding a caller of the lifecycle (it needs `internal/service`
  only); reasoning about startup, which now cannot interleave with a request.
- Harder: nothing known.
- Revisit when: flows need per-flow locks (a slow stop blocks every other
  lifecycle request today, as before).

## Action items

1. [x] `service.Lifecycle` with the handler's tests moved to it.
2. [x] `FlowsHandler`, `main.go` (`buildApp`, shutdown, restore) use it.
