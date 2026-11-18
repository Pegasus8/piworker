# ADR-0018: Versioned flow definitions; agents write drafts only

**Status:** Accepted
**Date:** 2026-11-09
**Deciders:** maintainer
**Design:** [docs/design/mcp.md §10](../design/mcp.md)

## Context

ADR-0012 lets agents author flows that the admin approves, and lets an
installation opt into agent deploys with a one-click rollback to the last
admin-approved version. Flows were stored as one mutable row: an approval
could overwrite an edit the admin made in another tab, nothing recorded who
wrote which definition, and "the last admin-approved version" did not exist.
Deploy and stop also rewrote the whole row, so a state change looked like an
edit.

## Options considered

### Option A: agents write the flow directly; audit rows record the change

| Dimension | Assessment |
|---|---|
| Complexity | Low |
| Security | The admin reviews after the fact; nothing to roll back to |
| Pi resources | None |
| Maintenance | Low |

**Pros:** no new tables. **Cons:** breaks ADR-0012 (drafts only); last write
wins between the editor and agents.

### Option B: numbered versions with optimistic saves; agent proposals as drafts

| Dimension | Assessment |
|---|---|
| Complexity | Medium (two tables, one migration, a conflict path) |
| Security | Every definition change is attributed; agents never touch the live row |
| Pi resources | Bounded: 50 versions per flow plus the last approved one; capped drafts |
| Maintenance | Medium |

**Pros:** review, rollback and the `low` comparison have a fixed baseline;
lost updates become a 409. **Cons:** the editor must send the version it
loaded and handle the conflict.

## Decision

- `flows.version` counts **definition** changes. Every definition save
  writes the next version to `flow_versions` (data, actor editor or agent,
  token, `admin_approved`, note, draft) in the same transaction. Deploy,
  stop, toggle and a failed restore change only the state
  (`UpdateFlowState`) and create no version; a definition save keeps the
  stored state and restart intent. Retention: the newest 50 versions plus
  always the newest admin-approved one. An agent version is never
  admin-approved.
- **Optimistic saves.** `PUT /api/flows/{id}` with the loaded `version`, and
  deploy or toggle with a `version`, answer 409 with `currentVersion` when
  another version was saved since. The editor offers Reload or Cancel, never
  a silent overwrite. Requests without a version are not checked (scripts).
- **Agents write drafts only** (`flow_drafts`): a whole flow or edit
  operations against a base version, stored with its risk, Lint issues and
  alerts. A draft is applied only by a save that checks, in its own
  transaction, that the draft is pending and still based on the version it
  replaces and that its token is unrevoked, then closes it with the version
  and who applied it (`applied_by`: `editor` for an admin approval, `agent`
  for a self-deploy at `low` or `all`). Every token-revoking transaction voids
  the token's pending drafts. Limits: 20 pending drafts per token; decided
  drafts kept 30 days, at most 50 per token and 200 in all.
- A stale base or a draft no longer pending is the service error kind
  **`conflict`** (`currentVersion` attached), on MCP and REST alike.
- The deployment marker (`flow_deployments`) records who deployed which
  version, for every successful deploy: editor, agent, or the author of the
  version restored at startup.

## Consequences

- Easier: "deployed by agent", "changed by agent", version history and
  rollback have one source; approvals cannot overwrite edits.
- Harder: every new writer of flow definitions must go through the
  versioned save; the editor must handle 409 on save, deploy and toggle.
- Revisit when: flows grow large enough that 50 full copies per flow matter
  on the SD card (store deltas instead).

## Action items

1. [x] `flow_versions`, `flow_deployments`, `flow_drafts` with idempotent
   migrations (`internal/storage`).
2. [x] Optimistic `PUT`/deploy/toggle and the editor's conflict dialog.
3. [x] Drafts REST, approval page, markers, version history and rollback.
