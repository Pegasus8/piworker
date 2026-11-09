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
- Open, accepted (action item 2): the read views (`pw_list_flows`,
  `pw_get_flow`, `pw_list_runs`, `pw_get_run`, the `diagnose_flow`
  prompt) still hide known values by comparison, so text an agent wrote
  comes back as `"<redacted>"` exactly when it holds one.
  - Compared with: secret values, and the credentials typed into the
    `Sensitive` settings of the flow shown (whole, and their words of 8
    bytes or more), in literal, URL-encoded, base64 and escaped forms;
    values under 4 bytes are never hidden. The errors `pw_get_run` shows
    are also compared with the parts of the flow's hidden settings (any
    setting agents may not see, `Sensitive` or not): literal values,
    words, URL paths, path segments, query values, fragments and user
    names of 8 bytes or more, and URL passwords from 4 bytes.
  - Who: a token with `author` and `read`, with `author_enabled`, whose
    text gets into a stored version: one it deploys from its own draft
    (`deploy`, `agent_deploy` at `low` or `all`: no approval per round,
    each deploy audited and marked "deployed by agent"), or a draft the
    admin approves (at any level, one approval per round).
  - What it needs: its text in a visible setting or the name of that
    flow, or in an error a run of it records, read back through the
    views. Agents see the errors only of runs of a version the admin
    approved (ADR-0022), so this last route needs an approved draft that
    is deployed (by the admin, or by an agent with `deploy` at `low` or
    `all`). Over such a version its later drafts read a guess
    back too, with `author` alone: the `before` values of alerts are
    redacted by comparison, and a kept setting (or name) whose stored
    value holds a known value is withheld, shown as `"<redacted>"` by
    `pw_get_draft`.
  - How fast: every occurrence is hidden, so one round tests many
    candidates. A view shows at most 64 KiB, so a four-digit PIN falls in
    a round or two, a six-digit PIN or a dictionary word in about a
    hundred; 16 random characters never. Length is not randomness: a long
    word or date falls like a short one. Hidden settings often hold
    structured, guessable text (paths, user names, a URL's short
    password), and nothing checks them.
  - Mitigation: the editor marks short secrets with an icon and words,
    and the Agents page counts them. Credentials belong in secrets: typed
    into settings they are hidden the same way but never checked. Admins
    should keep guessable passwords and identifiers out of the hidden
    settings of flows whose runs agents may read.
- Open, undecided (action item 3): the same comparison reaches agents
  outside that decision, at every `agent_deploy` level (`off` included)
  and without `author`:
  - `call`: a granted tool whose reply or error repeats its arguments;
    tool results and errors are compared with secret values
    (`Tools.finish`; not with typed credentials or hidden settings), so
    the agent sees whether a guess comes back as `"<redacted>"`, many
    guesses per call.
  - `call` and `read`: a granted tool whose flow records an error that
    repeats an argument; `pw_get_run` compares it with everything listed
    above, the flow's hidden settings included.
  - `read` and an inbound trigger the agent reaches another way: a
    webhook (one without a token answers anyone on the network,
    ADR-0003) or an MQTT topic of a flow whose node records an error that
    repeats the input.
  Tool results need the comparison as much as the read views: flows can
  return real secret values (resolved references, variables, `$env`, a
  credential a response echoes, a constant). Leaving out of the
  comparison the secrets an agent sent, tried in wave 5, was reverted: a
  tool that never repeats its arguments but returns a secret then showed
  it in clear to an agent that sent it among hundreds of candidates in
  one argument. The short-secret warnings cover these paths for secret
  values only.
- Revisit when: the read views gain per-setting provenance across versions,
  or runtime taint tracking becomes affordable.

## Action items

1. [x] Written/kept tracking, withheld settings, echo tests per path
   (`internal/service/authoring_withheld.go`).
2. [x] The read views keep redacting by comparison: stored flows and real
   runs hold real secret values only comparison catches (resolved
   references, variables, `$env`, node outputs), and tracking them across
   variables at run time is out of proportion. The residual is mitigated
   by warnings instead: `GET /api/secrets` marks each secret `short` (fewer
   than `secrets.ShortValueRunes`, 16 runes; never a value or a length);
   the secrets dialog marks short secrets with an icon and words; the
   Agents page explains the residual at every level, what `low` and `all`
   add, and counts the short secrets; README and design §6, §10 describe
   it, including that credentials typed into `Sensitive` settings are
   redaction targets too and should be secrets.
3. [ ] Decide the tool-result and inbound-trigger paths (the second "Open"
   consequence): keep the comparison, which tool results need because
   flows can return real secret values, with the warnings of item 2 as
   the mitigation, or mitigate them another way, never by comparing a
   result with the agent's arguments (see above). Until then they are
   open, not accepted.
