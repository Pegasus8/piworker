# ADR-0005: Per-node bounded queues and FIFO delivery

**Status:** Accepted
**Date:** 2026-11-08
**Deciders:** maintainer

## Context

The runtime promised that one slow node cannot stall unrelated branches, but a
single dispatcher goroutine did blocking sends into 256-slot node channels: a
full channel (e.g. behind a long `process-delay`) stopped delivery to every
node. The router also handed each message to a 10-worker pool, so messages
between the same two nodes arrived out of order, breaking stateful nodes (join,
batch, dedupe, throttle) that rely on the per-node serial executor.

## Options considered

### Option A: Bigger channels / more workers
**Pros:** trivial. **Cons:** only delays the stall; ordering still broken.

### Option B: Per-target unbounded forwarding goroutines
**Pros:** no stalls. **Cons:** unbounded memory on a Pi; goroutine per edge.

### Option C: Per-node bounded queue, non-blocking dispatch, in-order routing
| Dimension | Assessment |
|---|---|
| Complexity | Medium |
| Correctness | FIFO per (source, target); no head-of-line blocking |
| Pi resources | Bounded (default 1000 messages per node) |
| Maintenance | Drops must be accounted for explicitly |

## Decision

Each node has its own bounded queue (`internal/flow/queue.go`, default 1000,
`WithNodeQueueLimit`). The dispatcher never blocks on a node: on overflow the
message is dropped with a warning, counted (`DroppedMessages()` and a metric)
and released from its run. The router sends in call order on a single channel
(each source routes from one goroutine), so delivery is FIFO per
(source, target); the worker pool was removed. Every overflow or timeout drop
path calls `runDone` (via `releaseDropped`), so overload never leaves a run
open; runs still in flight when a flow is stopped are finalized by the event
recorder's idle timeout.

## Consequences

- Easier: reasoning about ordering in stateful nodes; isolating slow branches.
- Harder: a node whose queue is full drops messages instead of applying
  backpressure. Upstream, backpressure is only bounded: the trigger seeder
  blocks on the router input, and router sends wait at most 5 s before dropping.
- Revisit when: a node needs backpressure semantics (e.g. lossless queues).
