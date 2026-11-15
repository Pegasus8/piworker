# ADR-0039: Agent nodes call flow tools through the tool service

**Status:** Accepted
**Date:** 2026-11-15
**Deciders:** maintainer
**Design:** [docs/design/llm.md](../design/llm.md) §3, §4

## Context

The AI Agent node (`process-llm-agent`) lets a model act: it asks the model,
runs the tool calls the answer asks for, sends the results back and repeats.
What it may act on is the security question. Its input is often untrusted
text (a chat message, an e-mail, a webhook body) that can carry a prompt
injection, and no prompt fully prevents one. PiWorker already has a vetted
way for a model to act: flows published as tools (ADR-0009), with closed
input schemas and server-computed risk (ADR-0013), confirmation policies
(ADR-0012), concurrency limits and cooldowns, and redaction of what an agent
sees (ADR-0021). Builtin nodes may not import `flow` or `service`.

## Options considered

### Option A: built-in actions in the agent node (HTTP, commands, files)

**Pros:** nothing to set up. **Cons:** a model would choose URLs, commands
and paths freely; none of the tool rules would apply.

### Option B: the node calls `FlowRuntime.Call` directly

**Pros:** short path. **Cons:** an import cycle (builtin → flow), and it
bypasses the service, the single path every tool call goes through (schema,
limits, redaction).

### Option C: the node calls PiWorker's own `/mcp` with an agent token

**Pros:** grants, audit and scopes as for any agent. **Cons:** a token per
node to create and store, a loopback HTTPS client that trusts the local CA,
an extra network hop on a Pi, and MCP features (approvals, client
confirmation) that a node cannot use anyway.

### Option D: a `node.ToolCaller` contract implemented by `service.Tools`

| Dimension | Assessment |
|---|---|
| Complexity | Low: one interface in `internal/node`, one actor kind, an option on the runtime manager |
| Security | Every call takes the service's path; the node may only call what its settings list, under rules stricter than a token's |
| Pi resources | In-process; no token, no HTTP |
| Maintenance | One path to keep correct for editor, MCP and agent nodes |

## Decision

Option D.

- **Contract.** `internal/node` defines `ToolCaller` (`AgentTools(caller)`,
  `CallAgentTool(ctx, caller, name, args)`), `AgentCaller{FlowID, NodeID,
  Tools, AllowPrivileged}`, `AgentTool` and `ToolCallOutcome{Status: ok |
  error | running, Result, Error}`. `ToolCallerFromContext` is never nil:
  without a caller, and always in a sandbox (dry-run, node test), it lists
  nothing and refuses every call. The flow runtime puts the node's flow and
  node IDs (`node.WithNodeInfo`) and the configured caller on every node's
  context. `RuntimeManager.SetToolCaller` sets the caller for flows deployed
  afterwards; the composition root sets it to the tool service before any
  flow is deployed or restored.
- **Rules** (`service.Tools`, actor kind `agent-node`, key
  `agent-node:<flowID>/<nodeID>`, no token):
  - only the tools the node's `tools` setting lists, by name; any other name
    is `not_found` without a lookup;
  - a tool published by the agent's own flow is refused (the run would wait
    for the node that waits for it);
  - privileged tools only with `allowPrivileged`, a `RiskSwitch`: turning it
    on makes the instance `privileged`, so the drafts' approval page flags
    it and the `low` agent-deploy policy refuses an agent turning it on
    (ADR-0019);
  - only tools with `confirm: none`: `client` needs a person the node does
    not have, `admin` an approval it cannot wait for;
  - arguments are validated against the tool's schema (the refusal lists
    the problems, so the model can fix its call); the tool's
    `maxConcurrent` and `cooldown` apply, and each agent node is limited
    like a token (`DefaultPerTokenInflight`, 2 calls at once);
  - results and errors are redacted as an MCP agent's: secrets removed,
    errors of nodes with settings agents may not read withheld whole;
  - a refusal is a tool result with `isError` for the model, and a step
    `refused` in `meta.llm.steps` (`{tool, status, durationMs}`, never
    arguments or results).
- **Records.** The run's actor meta is the node's key; there is no MCP
  audit row (no token): the call is a run in the tool flow's activity.
  Results are not kept for a later fetch (an agent node never asks).
- `tools` is a `Destination`: changing what an agent node may call is
  flagged on drafts like a changed destination.

## Consequences

- Easier: what an agent node can do is exactly the list of flows the admin
  published and listed, each with its own schema, risk and limits; the
  same tool can serve Claude over MCP and an agent node.
- Harder: an agent node needs its tools as separate running flows; tools
  that ask for confirmation are unavailable to it; its calls are not in the
  agents' audit page, only in the tool flows' runs.
- Revisit when: agent nodes need a human in the loop (an approval the flow
  can wait for), or a per-node audit view.

## Action items

1. [x] `node.ToolCaller`, `service.Tools` as its implementation, runtime
   option and node identity.
2. [x] Composition root sets the caller before restoring running flows.
