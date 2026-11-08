# ADR-0009: Expose PiWorker to agents over MCP as "flows as tools"

**Status:** Accepted
**Date:** 2026-11-08
**Deciders:** maintainer
**Design:** [docs/design/mcp.md](../design/mcp.md)

## Context

AI agents (Claude Code, Claude Desktop, other MCP clients) could make PiWorker
far more useful: run automations on the Pi, diagnose failures, draft flows. The
Pi controls physical outputs (GPIO, relays) and can run shell commands, so an
over-eager or prompt-injected agent is the main threat, alongside LAN intruders.
Three designs were produced independently and scored by three judges
(security, value, cost) out of 30.

## Options considered

### Option A: "Agent as operator" (management plane) — 18.5/30
Agent authors, deploys and debugs flows through ~16 tools.
**Pros:** maximal power. **Cons:** widest write surface; the agent becomes an
untrusted *author*, which invalidates defences designed against untrusted
*data*; risk classification by changed nodes is bypassable by editing upstream
nodes.

### Option B: "Flows as tools" — 21.5/30
The admin publishes deployed flows (`trigger-tool → … → action-reply`) as typed
MCP tools; agents supply validated data only.
| Dimension | Assessment |
|---|---|
| Complexity | Medium |
| Security | Smallest surface; existing defences (literal argv, SSRF guard, no secret expansion in payloads) stay valid |
| Pi resources | Stateless; no idle goroutines |
| Maintenance | One endpoint, official SDK |

### Option C: "Bidirectional hub" — 17/30
B plus an MCP client node and an LLM node inside flows.
**Pros:** flows can use external tools. **Cons:** lethal-trifecta risk (private
data + untrusted input + exfiltration) inside unattended flows; heavier on a Pi.

## Decision

Build Option B first, then add observation (read scope), draft authoring with
editor approval (author scope, only when `mcp.author_enabled = true`, default
false) and opt-in agent deploy (ADR-0012). Grafted from A: dry-run, lint and
draft approval; from C: nothing now (MCP client node deferred to its own ADR). Transport: one embedded endpoint `/mcp` (outside
`/api`), Streamable HTTP in stateless mode with JSON responses, using the
official `github.com/modelcontextprotocol/go-sdk` pinned to an exact version;
`listChanged: false`, no `subscriptions/listen`, private cache scope. The SDK's
localhost DNS-rebinding protection is disabled and replaced by PiWorker's Host
allowlist (ADR-0014) plus an `Origin` allowlist on `/mcp`. Off by default
(`mcp.enabled`), and a startup error when enabled with auth disabled.
Claude Desktop uses a pure-Go stdio bridge (`cmd/piworker-mcp`). Not built:
OAuth/public exposure, Bearer on `/api`, a tool per node type, raw
GPIO/shell/I2C tools, reading secrets or variables, legacy HTTP+SSE.

## Consequences

- Easier: agents use Pi automations like functions with typed results.
- Harder: runtime gains synchronous call/reply semantics (waiters keyed by
  correlation ID) and a service layer shared by REST and MCP.
- Revisit when: MCP spec revisions change transport or auth requirements; when
  an MCP client node is proposed.

## Action items

1. [x] Runtime `Call`/`Wait`/`RunDone` and waiters (bounded: waiter cap,
   reply size cap, results handed over once).
2. [x] Deploy rejects a duplicate tool name with 409 (ADR-0006).
3. [x] `trigger-tool`, `action-reply`, `internal/toolschema`, risk analysis.
4. [x] `internal/service` and the REST twin.
5. [ ] `internal/mcpserver`.
6. [ ] Read and author tools; stdio bridge; E2E with a real SDK client.
