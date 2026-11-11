# ADR-0025: MQTT nodes share their broker connections

**Status:** Proposed
**Date:** 2026-11-11
**Deciders:** maintainer
**Design:** [docs/design/zigbee.md §4, phase 0b](../design/zigbee.md)

## Context

Every MQTT Subscribe and MQTT Publish node opened its own broker connection,
with its own client id. A Zigbee home through Zigbee2MQTT has one trigger
per sensor or device and one publisher per controlled device: 20 devices
mean about 40 connections, each with its keep-alive, reconnect loop and
memory on the Pi and on the broker. The Database node already shares one
pool per DSN between nodes, released through `node.Closer` (ADR-0008).

A shared connection must not change what each node receives. MQTT 3.1.1
lets a broker send one copy of a message per matching subscription of a
connection, and Mosquitto does by default (`allow_duplicate_messages`);
the client library (paho, 3.1.1 only, no subscription identifiers) then
hands each copy to every matching filter. Two nodes with overlapping
filters (`zigbee2mqtt/#`, `zigbee2mqtt/+/set`) on one connection would each
get every message twice.

## Options considered

### Option A: keep one connection per node

**Pros:** no change. **Cons:** the connection count grows with the house;
the problem the Zigbee nodes would make worse.

### Option B: a process-wide pool of connections, without overlapping filters on one

| Dimension | Assessment |
|---|---|
| Complexity | Medium: a pool with reference counts and a per-filter fan-out (`internal/mqttpool`) |
| Security | Connections are keyed by broker and resolved credentials; a node in a sandbox (ADR-0020) never shares one with nodes outside it |
| Pi resources | One connection per broker and credentials in the common case (non-overlapping per-device topics) |
| Maintenance | Same reconnect and resubscribe rules as before, in one place |

**Pros:** invisible to users; existing flows keep working. **Cons:** a
filter that overlaps another still opens a connection; one slow node handler
would delay the others on its connection (handlers only enqueue, without
blocking, as before).

### Option C: a named broker connection configured once in Settings

**Pros:** credentials typed once; the natural anchor for device discovery.
**Cons:** new storage, REST and flow-model contracts; it can come later on
top of Option B without changing it.

## Decision

Option B. MQTT nodes without a client id of their own (empty, or the
`piworker-sub`/`piworker-pub` constant older flows stored) use a pooled
connection keyed by broker URL, resolved username, a hash of the resolved
password and the node's sandbox (none outside dry-runs and node tests),
with a unique `piworker-<random>` client id. A node with its own client id
uses the connection of that id (the same key plus the id): brokers use it
for ACLs and persistent sessions, and two connections cannot share an id.

- Filters on one pooled connection never overlap, so the broker sends one
  copy and exactly one filter's handlers receive it. Nodes on the same filter
  and QoS share its subscription and each receive the message once; a
  filter that overlaps one already on a connection, or the same filter with
  another QoS, goes to another connection. On a connection with an own
  client id every filter shares it.
- The connection subscribes every filter again on each (re)connect (clean
  sessions), keeps retrying in the background when the broker is not
  reachable (ADR-0006), and orders its SUBSCRIBE/UNSUBSCRIBE so a node
  leaving a filter never unsubscribes one another node just joined.
- A trigger's `Stop` and an action's `Close` release their reference: the
  filter is unsubscribed when no node receives it, the connection closed
  when no node holds it.
- An action takes its connection on first use and waits for it at most 10 s
  (or the message's deadline); a connection not up yet is a transient error
  and keeps retrying for the next message.

## Consequences

- Easier: one trigger per Zigbee device without a connection each; the
  Zigbee nodes reuse the pool.
- Harder: what a node does on its connection (subscription QoS) affects the
  nodes sharing it only through the rules above; the editor shows no
  connection count.
- A secret rotated while a pooled connection is open applies to nodes that
  start after it (they key a new connection), as before for an open one.
- Revisit when: the named broker connection (Option C) is wanted, or MQTT 5
  subscription identifiers become available in the client.

## Action items

1. [x] `internal/mqttpool`; MQTT Subscribe and Publish use it; tests against
   the fake broker (`internal/mqtttest`), which routes like Mosquitto.
2. [x] Node documentation and the generated node reference describe the
   `clientId` setting.
