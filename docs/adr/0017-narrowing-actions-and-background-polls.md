# ADR-0017: Narrowing actions need no step-up; background polls never extend a session

**Status:** Accepted
**Date:** 2026-11-09
**Deciders:** maintainer
**Supersedes:** ADR-0010 (only the step-up wording for revoking tokens)
**Design:** [docs/design/mcp.md §7](../design/mcp.md)

## Context

ADR-0010 says tokens "are created and revoked only from the editor with
password step-up". The editor needs a way to stop every agent at once
(a leaked token, a misbehaving agent at night), and the design already let a
single revoke skip the password. Asking for the password then slows down the
one action that should be instant, while it protects nothing: an attacker
holding the session can only take access away with it.

The editor also polls the pending-approvals count in every open tab, and the
Approvals page reloads its list when that count changes. Agents can change the
count at will by requesting approvals. With a sliding idle expiry
(`SessionIdleTimeout`), those requests kept a forgotten tab's session alive
forever, and agents could keep it alive on purpose.

## Options considered

### Option A: step-up for every token change; every request extends the session

| Dimension | Assessment |
|---|---|
| Complexity | None |
| Security | An unattended tab never expires; agents can keep it alive |
| Pi resources | One session write per poll per tab |
| Maintenance | None |

**Pros:** one rule. **Cons:** the emergency stop asks for a password; the
idle timeout is ineffective while a tab is open.

### Option B: step-up only for actions that widen access; background requests are not activity

| Dimension | Assessment |
|---|---|
| Complexity | Low (a passive path set and one request header) |
| Security | Idle timeout holds; widening still needs the password |
| Pi resources | Fewer SQLite writes |
| Maintenance | Low |

**Pros:** instant revoke-all; sessions expire as configured. **Cons:** the
frontend must mark its own background requests.

## Decision

- **Step-up covers widening only.** Creating a token, adding a grant,
  re-confirming a suspended grant and changing the agent deploy level
  (ADR-0012) need the password. Revoking a token, `POST /api/agent/tokens/revoke-all` (one
  transaction that also voids pending approvals and bumps `tokens_epoch`),
  suspending or removing a grant and rejecting an approval do not.
  Approving a call is the admin's decision on the call shown and needs the
  signed-in session only (ADR-0012).
- **Background requests are not activity.** `GET
  /api/agent/approvals/pending-count` is a passive path: it needs a valid
  session but never extends its idle expiry and runs no write transaction.
  Any guarded `/api` request carrying `X-PiWorker-Background: 1` is
  authenticated the same way. The header can only shorten the caller's own
  session, so it needs no allowlist; CORS allows it.

## Consequences

- Easier: stopping every agent is one click; an idle editor tab signs out on
  time even while agents are active.
- Harder: new polling code in the editor must send the background header (the
  agent store exports `BACKGROUND_HEADERS`; `lib/poller.ts` callers use it).
- Revisit when: the editor gains other polls or live streams that should not
  count as activity.

## Action items

1. [x] `revoke-all`, passive `pending-count`, `X-PiWorker-Background`
   (`internal/api/auth.go`, `internal/api/agent_tokens.go`, `internal/storage/sessions.go`).
2. [x] The editor's badge poll and approval reloads send the header.
