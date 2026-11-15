# ADR-0037: Triggers wait for room in the flow

**Status:** Proposed
**Date:** 2026-11-15
**Deciders:** maintainer
**Supersedes:** the backpressure consequence of ADR-0005

## Context

ADR-0005 gave every node a bounded queue and a dispatcher that never
blocks: a message that does not fit a node's queue is dropped and counted.
That keeps one slow node from stalling the others, but it sheds load in the
middle of a run: a burst of webhooks was accepted (HTTP 200), ran its first
nodes, and lost messages at the slow node, after side effects had already
happened. ADR-0005 left it as "revisit when a node needs backpressure".

Blocking the dispatcher on a full queue brings the stall back, and
per-connection blocking can deadlock on fan-out and fan-in.

## Options considered

### Option A: Larger queues

**Pros:** trivial. **Cons:** more memory on a Pi; the loss only moves.

### Option B: Lossless queues with blocking sends

**Pros:** nothing is dropped inside the flow. **Cons:** head-of-line
blocking between branches again, or one goroutine per connection; deadlock
analysis for every graph shape.

### Option C: Admission at the trigger

| Dimension | Assessment |
|---|---|
| Complexity | Low: one counter and one wait in the trigger seeder |
| Correctness | A trigger alone never overflows a queue; the dispatcher still never blocks |
| Pi resources | Bounded as before |
| Maintenance | The triggers already refuse when their channel is full |

## Decision

The runtime counts the messages a flow holds, queued or executing, over all
its runs (`FlowRuntime.Inflight`). A trigger's message enters the flow only
while that count is below a bound: the node queue limit by default
(`WithMaxInflight` sets it; 0 removes it). Until there is room the trigger's
seeder waits, its channel fills, and the trigger refuses at its own door, as
it already does for a full channel: a webhook answers that it is busy, a
timer skips a tick, a tool call waits within its timeout. After ten seconds
of waiting the log says which trigger waits and how many messages are held.

The bound is checked when a message enters, not held: a message that
multiplies inside the flow can take it above the bound, and what then does
not fit a node's queue is still dropped and counted (ADR-0005). The
dispatcher, the per-node queues and FIFO delivery are unchanged.

## Consequences

- Easier: overload is refused before a run starts, where the sender can be
  told, instead of half-running it.
- Harder: a node that never returns now makes its flow's triggers wait
  instead of dropping at that node; the log line above and the run that
  never finishes are how it shows.
- Revisit when: a flow needs its own bound (a per-flow setting), or
  messages must survive a restart (a persistent queue).

## Action items

1. [x] Counter, `admit`, `WithMaxInflight`, tests with the race detector.
