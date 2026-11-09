# ADR-0022: `low` keeps published tools and protected data the admin's

**Status:** Accepted (how a run's version is told superseded by ADR-0023)
**Date:** 2026-11-09
**Deciders:** maintainer
**Supersedes:** ADR-0019 (only the list of alert kinds `low` refuses);
ADR-0015 (only what the tool fingerprint covers)
**Design:** [docs/design/mcp.md §5, §6, §7, §10](../design/mcp.md)

## Context

The wave-5 security audit found that an agent with the author and deploy
scopes, at `agent_deploy = low`, could do what the level exists to prevent
without raising any of ADR-0019's alerts:

- **Rewire a tool.** Connecting a `confirm: none` tool to the action behind a
  `confirm: admin` tool, or repointing a granted tool to it, let the agent
  run that action at will: ADR-0012's server-side gate was gone, and the
  grant and fingerprint (ADR-0015: risk, annotations, schema, confirm) stayed
  valid because the risk did not change. An admin approving a pending call
  of a rewired tool also ran whatever it reached by then.
- **Read protected data through runs.** A side node reading global variables,
  the settings earlier nodes record in `meta` (an HTTP URL, a hidden
  expression) or a credentialed response could raise an error on purpose
  quoting an encoded copy, which `pw_get_run` returned: redaction by value
  cannot stop an encoding (§10 already says so for dry-runs). ADR-0012 never
  lets agents read secrets or variables.
- **Retype a node** under the same ID (an MQTT subscriber turned into a
  retained publisher with the same broker, topic and password), and **steer
  an approved dynamic destination** (`zigbee2mqtt/{{payload.device}}/set`)
  from upstream: both kept every destination value, so no alert fired.
- **Confirm guesses.** Draft alerts compared what the agent wrote with the
  admin's hidden values, against ADR-0021.

## Options considered

### Option A: patch each path (author restrictions on tool connections, error text hidden by value)

| Dimension | Assessment |
|---|---|
| Complexity | Low |
| Security | Encodings beat redaction by value; each new path needs its own patch |
| Pi resources | None |
| Maintenance | High |

**Pros:** small diffs. **Cons:** the next encoding or wiring trick is a new
finding; forbidding tool connections in drafts also blocks the admin from
reviewing such a proposal.

### Option B: alerts on what a change does, provenance for what agents see back

| Dimension | Assessment |
|---|---|
| Complexity | Medium (two more alert collectors, a diff option, a run check) |
| Security | `low` refuses whole classes of change; agents' answers never compare their text with hidden content |
| Pi resources | None at run time; one indexed read per `pw_get_run` |
| Maintenance | Medium: a new agent-facing view must keep the provenance rule |

**Pros:** drafts may still propose such changes for the admin to approve;
`low` stays a list of alert kinds. **Cons:** `low` refuses more harmless
edits (any edit on a tool's path, any new node reading variables).

## Decision

- **`tool_path_changed`** (`flow.Diff`, `internal/flow/diff_tools.go`): a
  running `trigger-tool`'s path — what its trigger reaches through
  connections and flow variables, plus everything that feeds that part —
  gained, lost or changed a node (added, enabled, retyped, reconfigured), or
  a connection into it was added or removed, compared with the base's path
  of the same trigger (a new or enabled trigger counts as changed). One
  alert per tool, naming node IDs and types only.
- **`data_read_changed`** (`internal/flow/diff_reads.go`): a running node
  added, enabled, retyped or reconfigured, or that gained an input, reads
  variables of any scope or message metadata (config text naming `vars`,
  `flowVars`, `meta` or `$env`; a `get-var`), or runs downstream of a node
  that uses a credential or reads variables. One alert per node.
- **`low` refuses both**, after ADR-0019's thirteen kinds
  (`service.LowDeployRefusedAlerts`).
- **Retyped nodes** count as removed and added in the alerts (a node is
  paired with its base node only when both have the same type; base secret
  uses are keyed by type), so a retype raises what adding the new node
  raises.
- **Dynamic destinations**: `dynamic_destination` also fires for a running
  node whose Destination value takes message data when anything upstream of
  it changed (the rule `credentialed_request_changed` applies to
  Operation properties), credentials or not.
- **Fingerprint** (amends ADR-0015): the tool fingerprint also covers the ID
  and type of every node above `safe` the tool reaches (through connections
  or the flow variables it sets), never their settings: agents see
  fingerprints, and a hash of a setting they may not read would confirm
  guesses. A tool repointed to another action, or wired to one more,
  suspends its grants, and a pending approval of it refuses to run.
  `all_tools` grants stay unbound.
- **Run errors by provenance**: agents see a run's recorded errors only
  when the run started in a later second than the flow's current deployment
  marker and the marker's version is admin-approved; otherwise a fixed text.
  Phases, nodes and timings stay. The editor sees everything.
- **Alerts by provenance** (ADR-0021 applied to alerts): `flow.DiffWith`
  takes what the agent wrote; a written setting agents may not read counts
  as changed whatever the base holds, and a written value's hosts and secret
  names count as known only from base settings agents may read. `SaveDraft`
  passes what the agent wrote, the `low` verdict on a draft every setting
  the draft did not keep.

## Consequences

- Easier: a published tool and its approval gate mean what the admin
  approved; what a changed node may read is decided before it runs.
- Harder: at `low`, any edit on a tool's path and any new or changed node
  reading variables, metadata or a credentialed response needs the admin;
  agents diagnosing their own deployments see which node failed but not
  why; a cosmetic repoint of a tool needs its grants re-confirmed.
- Residual: a node an agent deploys at `low` still runs on message data it
  may not see (a trigger's payload, the output an admin node computes from
  its hidden settings, a webhook's path in its payload). Which of its nodes
  run, and whether they fail, can carry a bit of that per node and run. The
  data is not a variable, a secret or a credential's response, but it may
  derive from a hidden setting.
- Revisit when: runs record the version they ran (then provenance needs no
  deployment marker), or `low` proves too strict to use.

## Action items

1. [x] `tool_path_changed`, `data_read_changed`, retype pairing, upstream
   dynamic destinations, refused at `low` with a test per finding
   (`internal/service/deploy_low_audit_test.go`).
2. [x] Fingerprint names the reached nodes above `safe`.
3. [x] `GetRun` shows agents errors only of admin-approved deployments.
4. [x] `flow.DiffWith` and provenance in `SaveDraft` and the `low` verdict.
5. [x] The drafts pages label the two new alert kinds.
