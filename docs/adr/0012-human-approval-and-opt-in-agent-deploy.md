# ADR-0012: Human approval gates and opt-in agent deploy

**Status:** Accepted
**Date:** 2026-11-08
**Deciders:** maintainer
**Design:** [docs/design/mcp.md §9–10](../design/mcp.md)

## Context

Client-side confirmations (MCP tool annotations, elicitation, Claude Code's
permission prompt, `anthropic/requiresUserInteraction`) are UX, not security:
an agent with shell access can read its token and call `/mcp` directly. Some
tools actuate hardware or run commands. The maintainer also wants agents to be
able to deploy flows, as a risk each installation can choose to accept.

## Options considered

### Option A: Rely on client confirmations
**Pros:** no server work. **Cons:** bypassable by the agent itself.

### Option B: Server-side approval in the editor, agent deploy never allowed
**Pros:** strongest. **Cons:** rules out a workflow the maintainer wants.

### Option C: Server-side approvals plus an explicit, leveled risk setting
| Dimension | Assessment |
|---|---|
| Complexity | Medium |
| Security | Real human gate by default; deploy power only by explicit choice |
| Maintenance | Policy logic for "low risk" must stay correct |

## Decision

- Tools have `confirm`: `none`, `client` (UX only), `admin` (server-side
  gate). `_meta["anthropic/requiresUserInteraction"] = true` is set for
  `client` and `admin` tools and for every destructive (privileged) tool
  regardless of `confirm`. `admin` returns `approval_required`; the admin
  approves or rejects in the editor queue; the result is retrieved with
  `pw_get_result`. Pending approvals expire (15 min).
- Agent authoring produces drafts only; with `agent_deploy = off` (default)
  every deploy is approved by the admin in the editor (diff, risk,
  recipient/destination changes, agent text shown as text). The levels below
  relax this.
- `agent_deploy` setting: `off` (default); `low` — agents may deploy/start/stop
  flows whose resulting risk is at most `external`, with no recipient or
  destination changes and no new secret references versus the last
  admin-approved version; `all` — any flow. Changing it requires password
  step-up and shows the risk; tokens need the `deploy` scope, grantable only
  when not `off`. Agent deploys are audited, marked in the editor, and can be
  rolled back to the last admin-approved version in one click.
- Never available to agents at any level: managing tokens, users or settings,
  reading secrets or variables, approving tool calls.

## Consequences

- Easier: safe defaults with an explicit escape hatch for trusted setups.
- Harder: maintaining the "low risk" policy and the approval UI.
- Revisit when: incidents or usage show a level is too permissive or useless.

## Action items

1. [x] `agent_approvals` storage and editor REST (revoked tokens void their
   pending approvals; per-token cap).
2. [x] Approval queue UI and execution of approved calls.
3. [ ] Draft approval page with diff and alerts.
4. [ ] `agent_deploy` setting with step-up, policy checks, "deployed by agent"
   marker and one-click rollback.
