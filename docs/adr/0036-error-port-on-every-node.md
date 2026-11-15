# ADR-0036: Every node has an error port

**Status:** Proposed
**Date:** 2026-11-15
**Deciders:** maintainer

## Context

When a node failed (after its retries) the run's branch ended there. The
failure was logged, recorded in the run history and counted, but the flow
itself could not react: no notification when a sensor read fails, no
fallback when a request is refused, no error answer for a tool call other
than the raw node error. Users had to watch the Activity panel.

## Options considered

### Option A: A flow-level "catch" trigger node

**Pros:** one handler for the whole flow. **Cons:** a second kind of
trigger with scoping rules (which nodes it catches); the handler is detached
from the node on the canvas; cycles between a catch and what it catches need
their own rule.

### Option B: An `error` output declared by each node type

**Pros:** nothing implicit. **Cons:** every builtin and its tests change;
single-output nodes become multi-output, which breaks the automatic
`SourcePort` (ADR-0005 notes, the fail-loud routing rule) and the editor's
auto-connect.

### Option C: A reserved `error` port the runtime gives every processing and action node

| Dimension | Assessment |
|---|---|
| Complexity | Low: one routing call in the executor, one rule in validation |
| Security | The failure text is treated as the admin's data (below) |
| Pi resources | None when the port is not connected |
| Maintenance | Node types do not change; new nodes get it for free |

## Decision

Every node that is not a trigger has, besides the outputs its type declares,
the output port `error` (`types.ErrorPort`). A node type cannot declare an
output with that ID (the registry refuses it). A connection from it is valid
wherever a connection from a declared output is (Lint, deploy, agent `connect`
ops); from a trigger it is an unknown port.

When a node's execution fails, after its retries or by panicking, and its
error port is connected, the runtime routes a copy of the message the node
failed on, with its metadata (the same run), topic `error` and payload
`{"message", "nodeId", "nodeType", "input"}`: the error text as it is
logged, the node, and the payload it failed on. The handlers are counted
into the run before they are routed, so the run ends after them. Cycles are
refused as for any connection (ADR-0007).

Handling a failure does not hide it: the node's error event, the run's
error count, the metrics and the log are unchanged. One thing changes: a
called run (a tool call) is not ended by a handled failure, so the handler
decides the answer, for example with a Reply node in error mode; unhandled,
the call ends with the node error as before.

The failure text can quote the failed node's settings, which agents may not
read (ADR-0021, ADR-0022). A node that was added or changed, or gained an
input, and receives what leaves an error port raises `data_read_changed`,
which `agent_deploy = low` refuses.

## Consequences

- Easier: notifying, falling back and answering on failure, inside the flow.
- Harder: the port is implicit, so the editor draws it (a red handle under
  the node) and the agents' authoring prompt says it exists; it is not in a
  node type's `outputs`.
- Revisit when: flows need one handler for many nodes (Option A can be added
  on top: a catch node is a node every error port feeds).

## Action items

1. [x] Runtime routing, validation, registry rule, agent ops.
2. [x] Editor handle and connection rules; authoring prompt; user guide.
3. [x] `data_read_changed` for nodes fed by an error port.
