# ADR-0007: Validate flows against the registry and reject cycles

**Status:** Accepted
**Date:** 2026-11-08
**Deciders:** maintainer

## Context

Flow validation accepted duplicate or empty node IDs (the last one silently
won), self-loops and cycles (an `A→A` node that emits twice per input burns CPU
forever), and decided whether a flow had a trigger from the client-supplied
`Category` field. A second, Category-based `Validate()` coexisted with the
runtime's own checks.

## Options considered

### Option A: Allow cycles with a hop limit
**Pros:** enables loops. **Cons:** runs never finish; run accounting and
history become unreliable; no current node needs loops.

### Option B: Reject cycles; one registry-based validation path
| Dimension | Assessment |
|---|---|
| Complexity | Low (iterative DFS) |
| Security | Client cannot mislabel node categories |
| Maintenance | One function to keep correct |

## Decision

`Flow.validate` (`internal/flow/model.go`) is the single structural check;
other deploy-time checks (config typing, tool risk rules, ADR-0013) are called
from the same path so every flow problem is reported consistently. Deploy calls
it with a server-side trigger predicate (the instance implements
`node.TriggerNode` or the registered category is `input`); `Flow.Validate(reg)`
is the registry-only wrapper. Neither uses the client's `Category`. It rejects
empty/duplicate node IDs, duplicate non-empty connection IDs, connections to
missing nodes or ports, self-loops and cycles, and requires at least one
enabled trigger in any non-empty flow (an empty flow is accepted as a draft).
Invalid flows map to HTTP 400 (ADR-0006).

## Consequences

- Easier: every deployed flow is a DAG; runs always terminate structurally.
- Harder: iterative patterns need explicit nodes (e.g. a future loop node with a
  hard limit, which would need its own ADR).
- Revisit when: a real use case for bounded loops appears.
