# ADR-0019: One Lint path, schema markers and change alerts define `low`

**Status:** Accepted
**Date:** 2026-11-09
**Deciders:** maintainer
**Supersedes:** ADR-0012 (only the definition of the `low` agent deploy level)
**Design:** [docs/design/mcp.md §10](../design/mcp.md)

## Context

ADR-0012 defines `agent_deploy = low` as "risk at most `external`, no
recipient or destination changes and no new secret references versus the
last admin-approved version". To check that, the server must know which
settings are destinations, and agents need every problem deploy would find
before they propose a draft, not only the first one. Building the check
showed the definition was too narrow: an agent could keep every destination
and still turn an approved authenticated `POST` into a `DELETE`, raise a
daily request to ten a second, or write a global variable that another
flow uses as a URL.

## Options considered

### Option A: a separate validator for agents, and a hand-written list of destination fields per node

| Dimension | Assessment |
|---|---|
| Complexity | Medium |
| Security | Drifts from deploy; new nodes are unchecked until someone updates the list |
| Pi resources | None |
| Maintenance | High |

**Pros:** no change to node schemas. **Cons:** two validation paths
(against ADR-0007); a forgotten field is a bypass.

### Option B: deploy's checks as shared collectors; markers in the config schema; typed change alerts

| Dimension | Assessment |
|---|---|
| Complexity | Medium (markers on every builtin, a diff engine) |
| Security | One source of truth; a test fails when a node's field is unmarked |
| Pi resources | None at run time |
| Maintenance | Medium: a new node must be marked (enforced) |

**Pros:** "a flow without Lint errors deploys"; the `low` predicate is a
list of alert kinds. **Cons:** stricter `low` refuses some harmless edits.

## Decision

- **`flow.Lint`** returns every issue (stable codes, JSON-pointer paths,
  deterministic order) from the same collectors deploy runs (config typing,
  instantiation, structure, callable triggers, tool rules, required
  secrets). Deploy still stops at the first error of each stage with the
  same messages. This extends ADR-0007's single validation path. Lint
  messages never quote a setting agents may not read.
- **Schema markers** on `node.ConfigProperty`: `Destination` (where data
  goes or what is connected to), `RiskSwitch` (opt-ins such as
  `allowPrivate`, `allowDashArgs`), `Operation` (what selects the action at
  the destination: HTTP method and body, database query) and `Rate` (how
  often a trigger fires or how many messages pass). Every builtin is marked;
  `builtin/schema_policy_test.go` fails when a destination-, rate- or
  switch-like field is not.
- **`flow.Risk`** is the highest instance risk of the enabled nodes (a
  `RiskSwitch` turned on, or a dynamic URL or broker, makes an instance
  privileged).
- **`flow.Diff`** reports field and connection changes and typed alerts.
  `agent_deploy = low` allows an agent deploy only when the resulting risk
  is at most `external` and, against the last admin-approved version (or an
  empty flow), the diff raises none of: `risk_raised`,
  `privileged_node_added`, `destination_changed`, `dynamic_destination`,
  `new_host`, `secret_added`, `secret_moved`, `risk_switch_on`,
  `sensitive_literal`, `credentialed_request_changed`, `rate_changed`,
  `global_variable_write`, `global_variable_read`. A disabled node counts as
  absent. Alerts sent to agents carry values only for settings agents may
  read. `all` and `off` are unchanged from ADR-0012.
- **Strict secrets.** Nodes whose settings cannot work without their
  secrets (Telegram bot token, notification URL, database DSN) implement
  `node.SecretRequirer`. Deploy refuses (400) a flow naming an undefined
  one, and Lint reports `undefined_secret`. Previously only a setting that
  resolved to empty failed.

## Consequences

- Easier: new nodes inherit the policy by marking their schema; agents get
  all problems in one call.
- Harder: `low` refuses edits upstream of a credentialed request or on a
  tool path that reads global variables, even harmless ones; those need the
  admin's approval.
- Revisit when: `low` turns out too strict to be useful, or a node needs a
  marker the four do not cover.

## Action items

1. [x] Shared collectors, `flow.Lint`, `flow.Risk`, `flow.Diff`.
2. [x] Markers on every builtin and the schema policy test.
3. [x] The `low` predicate in `internal/service/deploy.go`, with a test per
   alert kind.
