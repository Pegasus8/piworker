# ADR-0026: Named broker connections for the Zigbee nodes

**Status:** Proposed
**Date:** 2026-11-11
**Deciders:** maintainer
**Design:** [docs/design/zigbee.md](../design/zigbee.md)

## Context

The Zigbee nodes talk to Zigbee2MQTT through an MQTT broker. With the
existing MQTT nodes every node repeats the broker URL, username and password;
a house of Zigbee devices would repeat them in dozens of nodes, a broker
move would mean editing every one, and device discovery (which devices exist,
what each can do) has no single place to ask. Agents author flows (ADR-0018):
a per-node broker URL is a destination an agent can change, and the
credentials are settings it must never read.

## Options considered

### Option A: per-node broker settings, as the MQTT nodes

**Pros:** no new contract. **Cons:** the repetition above; discovery has no
broker to ask; an agent can point a node at another broker.

### Option B: named connections in the editor, referenced by name

| Dimension | Assessment |
|---|---|
| Complexity | Medium: a store, a table, four REST routes, a node interface and a Lint issue |
| Security | Credentials never in flows; the password is write-only over the API; agents see and choose names only; a sandboxed run sees an empty store |
| Pi resources | None beyond the pooled connections (ADR-0025) |
| Maintenance | One more store shaped like the secret store |

**Pros:** settings typed once; discovery and the editor's pickers know the
brokers; agents choose among the admin's connections. **Cons:** a node now
depends on state outside its flow (as with secrets).

## Decision

Option B.

- `internal/brokers`: a connection is a name (1–64 letters, digits, `_`,
  `-`), a broker URL (`tcp`, `mqtt`, `ssl`, `tls`, `mqtts`, `ws`, `wss`; no
  credentials, path or query in it, except a path for `ws`/`wss`), a username,
  a password and a Zigbee2MQTT base topic (default `zigbee2mqtt`, no
  wildcards). Username and password may hold `{{secret.NAME}}` references,
  resolved when a node connects (`secrets.Expand`). `brokers.DefaultStore`
  loads the `broker_connections` table at startup and writes through; nodes
  resolve connections through their context (`brokers.FromContext`), so a
  dry-run or node test gets an empty store (ADR-0020).
- REST (admin session, under `/api`): `GET /api/connections` (never the
  password: `hasPassword`), `PUT /api/connections/{name}` (a request without
  `password` keeps the stored one; `""` clears it), `DELETE` and
  `POST /api/connections/{name}/test` (connects with the settings sent, the
  stored password when none is sent, stores nothing, answers `{ok, error}`).
  MCP exposes none of it.
- `node.ConnectionRequirer` (`RequiredConnections() map[string]string`):
  deploy refuses a flow (400) whose node names a connection that is not
  defined, from the same collector Lint uses (`undefined_connection`, an
  environment issue like `undefined_secret`: it blocks a deploy, not a
  dry-run or a draft).
- The Zigbee nodes' `connection` setting is `AgentVisible` and
  `Destination`: changing it is a destination change for the approval page
  and the `low` policy (ADR-0019).
- A changed connection applies to nodes that start after the change; running
  nodes keep their open connection until their flow is redeployed. Deleting a
  connection does not stop flows that use it; their next deploy is refused.

## Consequences

- Easier: Zigbee nodes need a connection and a device name; discovery
  follows the defined connections.
- Harder: exporting a flow does not carry its connections (as with secrets).
- Revisit when: the MQTT nodes should take a connection too, or flows should
  restart when a connection they use changes.

## Action items

1. [x] Store, table, REST routes, `node.ConnectionRequirer`, Lint issue.
2. [x] Editor: connections dialog, pickers (with discovery).
