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

`Flow.Validate(reg *node.Registry)` is the single validation path. It rejects
empty/duplicate node IDs, duplicate connection IDs, connections to missing
nodes or ports, self-loops and cycles, and requires at least one enabled
trigger, where trigger-ness comes from the registered node type, never from the
client's `Category`. Deploy maps validation errors to HTTP 400.

## Consequences

- Easier: every deployed flow is a DAG; runs always terminate structurally.
- Harder: iterative patterns need explicit nodes (e.g. a future loop node with a
  hard limit, which would need its own ADR).
- Revisit when: a real use case for bounded loops appears.
