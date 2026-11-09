# ADR-0020: Sandboxed dry-run and node test; expressions honour their deadline

**Status:** Accepted
**Date:** 2026-11-09
**Deciders:** maintainer
**Design:** [docs/design/mcp.md §10](../design/mcp.md)

## Context

Agents that author flows need to see what a flow would do before the admin
reviews it: which nodes run, what each one emits, which would reach the
outside world. Running a draft for real could send messages, write
variables other flows read, use the admin's secrets or claim a webhook path.
On a Raspberry Pi, a runaway expression (`reduce` doubling a string) can
exhaust memory, and an expression that timed out kept running in the
background.

## Options considered

### Option A: no dry-run; agents learn from deploy errors and the admin's review

| Dimension | Assessment |
|---|---|
| Complexity | None |
| Security | Nothing runs before approval |
| Pi resources | None |
| Maintenance | None |

**Pros:** nothing to isolate. **Cons:** agents propose flows blind; admins
review more broken drafts.

### Option B: run the flow once in a private runtime where only `safe` nodes execute

| Dimension | Assessment |
|---|---|
| Complexity | Medium (a derived registry, isolated stores, a sandbox) |
| Security | No side effects, no real secrets or variables, bounded data |
| Pi resources | One run at a time; 10 s, 200 messages and 32 MiB per run |
| Maintenance | Medium: node code resolves secrets and variables through its context |

**Pros:** real outputs of the processing nodes; the agent sees what would
reach the outside, as stubs. **Cons:** every secret and variable lookup
must go through the context, and expressions must check their deadline.

## Decision

- `flow.DryRun` lints the flow, then runs it once in a private
  `FlowRuntime` that is never registered (no tools, webhook paths, tool
  names, recorder or metrics). `Registry.Derive` wraps every node: triggers
  never start (the payload is injected), and every node whose type or
  instance risk is not `safe` is a **stub** that records itself in
  `wouldExecute` and emits the caller's mock, if any. Nodes get empty
  global variables, flow variables seeded by the caller, an **empty secret
  store** and a `node.Sandbox`. Builtins resolve secrets and variables
  through their context when they run, never in a factory. A nil store
  means an empty one, never the real one.
- `flow.TestNode` runs one `safe` processing node on an input, with the
  same isolation.
- **Bounds:** one dry-run or node test at a time process-wide (`busy`
  otherwise); at most 50 nodes and 200 connections; 64 KiB per payload,
  mock or input; a 10 s wall clock (5 s for a node test); 200 routed
  messages; 1 MiB per value and 32 MiB of data per run; previews cut to
  2 KiB, results to 64 KiB. Nothing of a sandboxed run reaches the process
  log.
- For an agent's dry-run of a stored flow or a draft, settings the agent
  may not read are replaced before the run (ADR-0021). A node that this
  leaves invalid becomes a stub with a `hidden_settings` warning instead of
  stopping the run.
- **Runtime semantics change, every flow:** expression loops (`filter`,
  `map`, `reduce`, …) check the evaluation's context, so an evaluation
  stops at its deadline instead of running on. This costs about three times
  more on tight loops over large arrays; small arrays are unaffected. The
  size bounds on string building apply only in the sandbox. Secrets are
  resolved per execution, so a rotated secret applies without a redeploy.

## Consequences

- Easier: agents test their drafts; the admin reviews flows that already
  ran in the sandbox.
- Harder: a new node must resolve secrets and variables through its context
  and start goroutines with `node.Go`. Otherwise its lookups miss the
  isolated stores and its goroutines escape the sandbox's tracking. Review
  must check this.
- Revisit when: a node needs a side effect that can be simulated faithfully
  (e.g. a mock HTTP responder), or production flows need the data bounds
  too.

## Action items

1. [x] `Registry.Derive`, `node.Sandbox`, isolated stores, context-resolved
   secrets and variables.
2. [x] `flow.DryRun`, `flow.TestNode`, `pw_dry_run`, `pw_test_node`.
3. [x] The expression guard (`builtin/expr_guard.go`).
