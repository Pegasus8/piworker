# ADR-0010: Scoped opaque agent tokens, only on `/mcp`

**Status:** Accepted; the grant suspension rule is superseded by [ADR-0015](0015-tool-identity-binds-grants-and-approvals.md)
**Date:** 2026-11-08
**Deciders:** maintainer
**Design:** [docs/design/mcp.md §7–8](../design/mcp.md)

## Context

MCP clients cannot use the editor's cookie session and CSRF header. PiWorker
is a LAN device: cloud clients (claude.ai, Claude Desktop connectors) cannot
reach it without a tunnel, and local clients (Claude Code) support static
headers.
Authorization is optional in the MCP specification; HTTP servers that
implement it SHOULD follow its OAuth 2.1 profile.
Agent credentials end up in client config files, so they will leak sooner or
later and must have bounded power.

## Options considered

### Option A: OAuth 2.1 authorization server in PiWorker
**Pros:** spec-preferred. **Cons:** large surface (metadata, registration,
consent UI) for a single-admin LAN device; few local clients need it.

### Option B: Reuse sessions (cookie) for agents
**Pros:** no new credential. **Cons:** full admin power; agents would need the
editor's CSRF header/Origin or an exception in the `/api` request guard
(ADR-0002).

### Option C: Opaque, scoped, expiring tokens accepted only on `/mcp`
| Dimension | Assessment |
|---|---|
| Complexity | Medium |
| Security | Bounded scopes and grants, hashed at rest, revocable, audited |
| Pi resources | In-memory cache; no SQLite read per request |
| Maintenance | Own verifier and limiter |

## Decision

Tokens `pwk_<id>_<secret>` stored as SHA-256 (constant-time compare), shown
once, 90-day default expiry. Scopes: `call`, `read`, `author`, `deploy`
(`author` tools and scope only when `mcp.author_enabled = true`, default false;
`deploy` only when agent deploy is enabled, ADR-0012). A per-token
`privileged` flag (default off) is required to call privileged tools. Grants
bind to `(flowID, triggerNodeID, fingerprint)`; a redeploy that raises risk or
widens the schema suspends the grant until re-confirmed; `all_tools` never
includes privileged tools. Tokens are created and revoked only from the editor
with password step-up; no token manages tokens. A cache with a `tokens_epoch`
re-check every 5 s applies revocations, including those from other processes.
Recovery-code redemption revokes all tokens; password change does too unless
explicitly kept. `/api` keeps rejecting Bearer tokens. Failed bearer attempts
use their own limiter (not the login limiter).

## Consequences

- Easier: giving an agent narrowly scoped, auditable access.
- Harder: clients must store a secret; cloud clients need a bridge or tunnel.
- Revisit when: MCP clients broadly require OAuth, or remote (non-LAN) access
  becomes a goal.

## Action items

1. [x] `api_tokens` storage, verifier with cache and `tokens_epoch`.
2. [ ] Own per-token rate limiter on `/mcp`.
3. [x] Editor REST with password step-up; grants with fingerprints.
4. [x] Editor UI for tokens and grants.
5. [x] Revocation on recovery-code redemption and password change.
