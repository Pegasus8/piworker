# ADR-0029: MQTT nodes take named connections; a changed connection restarts the flows that use it

**Status:** Accepted
**Date:** 2026-11-13
**Deciders:** maintainer
**Supersedes:** ADR-0026 (only its rules on changing and deleting a connection)

## Context

ADR-0026 introduced named broker connections for the Zigbee nodes and left
two things open: the MQTT nodes still repeated a broker URL and credentials
in every node, and a changed connection only applied to nodes that started
after the change ("running nodes keep their open connection until their
flow is redeployed"; "deleting a connection does not stop flows that use
it"). An admin who fixed a password or moved the broker had to find and
redeploy every flow by hand, and a deleted connection broke flows silently
at their next deploy or restart.

## Options considered

### Option A: keep per-node settings for MQTT, document the redeploy

**Pros:** no change. **Cons:** the problems above stay.

### Option B: MQTT nodes may name a connection; the API restarts the users of a changed connection and refuses to delete one in use

| Dimension | Assessment |
|---|---|
| Complexity | Medium: a setting on two nodes, a lifecycle helper, two response changes |
| Security | Credentials move out of flows into the connection store (write-only); agents choose among the admin's connections only (the setting is `AgentVisible` and `Destination`) |
| Pi resources | A restart briefly stops the flow; the pooled connections (ADR-0025) follow the new endpoint |
| Maintenance | One more place that redeploys flows; it goes through the editor's lifecycle lock |

**Pros:** settings typed once; a change takes effect everywhere at once;
deleting cannot strand a running flow. **Cons:** saving a connection
interrupts the flows that use it for a moment.

### Option C: hot-swap the connection inside running nodes

**Pros:** no restart. **Cons:** every node would need reconnect-in-place
logic and the pool would have to migrate subscriptions; far more code than
a restart for a rare admin action.

## Decision

Option B.

- **MQTT nodes.** `trigger-mqtt` and `action-mqtt` take either
  `connection` (a broker connection's name: a literal, `AgentVisible`,
  `Destination`, offered by the editor's connection picker) or `broker`
  (with the node's own optional `username`/`password`, as before), never
  both; neither is an error. With a connection the node's own credentials
  must be empty (the connection's are used). The connection is resolved
  through the node's context when it connects (`brokers.FromContext`), so a
  dry-run or node test sees an empty store (ADR-0020). The nodes implement
  `node.ConnectionRequirer`: deploy refuses an undefined connection
  (`undefined_connection`, ADR-0026). `broker` is no longer a required
  schema property; existing flows are unchanged.
- **Changing a connection.** `PUT /api/connections/{name}` stores it, then
  restarts every *running* flow whose nodes name it, as its running
  instances answer (`FlowRuntime.ConnectionsUsed`; Zigbee and MQTT alike):
  each is stopped and deployed again with the definition that was running,
  under the editor's lifecycle lock, keeping its deployment marker. A save
  that changes nothing restarts nothing; several connections changed at once
  (an import) restart each flow once. A flow that cannot start again is
  stored as failed. The response is `{restarted: [{id, name}], failed: [{id, name,
  error}]}`. Stopped flows pick the new settings up at their next deploy.
- **Deleting a connection.** `DELETE /api/connections/{name}` is refused
  with 409 while a running flow uses it; the error names the flows and
  `data.flows` lists them. Stopped flows that name it are refused at their
  next deploy, as before.
- Secrets referenced by a connection's credentials are still resolved when
  a node connects; rotating such a secret does not restart anything.

## Consequences

- Easier: one place for a broker's URL and credentials for MQTT and Zigbee
  nodes; fixing it applies to every running flow at once.
- Harder: saving a connection briefly stops and restarts its flows
  (messages in flight in them are handled as on any redeploy).
- Revisit when: a connection's secret rotation should restart flows too, or
  restarts become frequent enough to warrant hot-swapping.

## Action items

1. [x] `connection` on the MQTT nodes, `node.ConnectionRequirer`.
2. [x] `flow.ConnectionsUsed`, `FlowsHandler.RestartFlowsUsing`,
   `UnlessRunningFlowsUse`; PUT/DELETE responses.
3. [x] Editor: restart summary after saving; the server's refusal on delete.
