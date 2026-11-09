# ADR-0021: What agents see never confirms a guess

**Status:** Accepted
**Date:** 2026-11-09
**Deciders:** maintainer
**Design:** [docs/design/mcp.md §6, §10](../design/mcp.md)

## Context

Agents may not read secrets, credentials the admin typed into `Sensitive`
settings, or settings not marked `AgentVisible` (ADR-0010, ADR-0013). The
read views of wave 3 hide those by replacing every occurrence of a known
value with `"<redacted>"`. With authoring, agents also *write* content and
read back drafts, dry-run traces, Lint messages and alerts. If what comes
back depends on how the agent's text compares with a hidden value, the
agent can confirm guesses: it writes `"1234"` and sees `"<redacted>"`, or
leaves a setting "unchanged" and sees that accepted. A four-digit PIN falls
in a few hundred tries.

## Options considered

### Option A: redact by value everywhere (the wave-3 rule)

| Dimension | Assessment |
|---|---|
| Complexity | Low |
| Security | An oracle wherever agent text is echoed |
| Pi resources | None |
| Maintenance | Low |

**Pros:** one rule. **Cons:** leaks low-entropy secrets to any author token.

### Option B: decide by provenance: echo what the agent wrote, withhold the admin's content whole

| Dimension | Assessment |
|---|---|
| Complexity | Medium (track written vs kept settings per draft) |
| Security | Output depends only on the admin's content, never on the agent's guess |
| Pi resources | A small JSON list per draft |
| Maintenance | Medium: every new agent-facing view must follow the rule |

**Pros:** no oracle. **Cons:** agents lose some detail on nodes holding
hidden settings (fixed issue texts, no alert values).

## Decision

- What the agent wrote comes back exactly as written, in drafts, issues,
  alerts, dry-run and node-test output, even if it equals a secret's value.
- The admin's content an agent may not read is **withheld whole**, decided
  from that content alone: hidden settings, and settings whose base value
  holds a secret's value or a typed credential. A draft records the
  settings it kept and that are withheld (`KeptJSON`). Its dry-run hides
  them from the run, and `GetDraft` shows them as `"<redacted>"`.
- Whether a setting was written or kept is decided by what the agent sent
  (the sentinel, untouched properties, merge patches), never by comparing
  with the base value.
- Issue messages on nodes holding withheld settings use fixed texts. Alerts
  about withheld settings carry no values, hosts or secret names. The
  dry-run of a stored flow runs without what the agent may not read, so its
  output needs no redaction by value.
- Built-in tool refusals are counted once, by the `/mcp` endpoint.

## Consequences

- Easier: a single test shape ("send a secret's value, get it back
  unchanged") covers every agent-facing path.
- Harder: new agent-facing views must take provenance into account, not
  only redact.
- Open: the wave-3 read views (`pw_get_flow` and friends) still redact
  stored flows by value. Once an agent's text is saved in a version (an
  approved draft, or a self-deploy at `low` or `all`), they can confirm a
  guess. Closing this needs to know who wrote each setting across versions.
- Revisit when: the read views gain per-setting provenance (action item 2).

## Action items

1. [x] Written/kept tracking, withheld settings, echo tests per path
   (`internal/service/authoring_withheld.go`).
2. [ ] Per-setting provenance across versions so the read views stop
   redacting agent-written values by comparison.
