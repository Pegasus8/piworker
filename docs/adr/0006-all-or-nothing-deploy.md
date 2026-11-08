# ADR-0006: All-or-nothing deploy with network-tolerant triggers

**Status:** Accepted
**Date:** 2026-11-08
**Deciders:** maintainer

## Context

Triggers were started in goroutines and their start errors only logged, so
`Deploy` succeeded and the flow was stored as `running` even when, for example,
its webhook path was already taken. An inject right after deploy could fail
with "trigger not running" (a flaky test). Making start failures fatal must not
break a Raspberry Pi that boots before Wi-Fi is up and restores flows whose
MQTT broker or RSS feed is not yet reachable.

## Options considered

### Option A: Keep asynchronous starts, surface errors in the UI
**Pros:** never blocks. **Cons:** stored state lies; restarts restore broken flows.

### Option B: Synchronous starts; any error fails deploy
| Dimension | Assessment |
|---|---|
| Correctness | Stored state matches reality |
| Pi resources | Negligible |
| Risk | Network-dependent triggers would fail at boot |

### Option C: B, plus a rule for trigger authors
Start returns an error only for invalid configuration or exclusive resources
(e.g. duplicate webhook path, missing GPIO pin). Network-dependent triggers
connect and retry in the background and never fail Start for reachability.

## Decision

Option C. Triggers start synchronously in a fixed order; on the first error
the already started triggers are stopped and `Deploy` returns the error, so the
flow is not registered or stored as running. HTTP status: 400 for invalid flows,
409 when a previous instance is still stopping, 500 otherwise. MQTT and RSS
triggers follow the reachability rule (MQTT: connect-retry, subscribe in the
on-connect handler; RSS: seed the seen set on the first *successful* fetch).
New triggers must follow the same rule (documented in `CLAUDE.md`).

## Consequences

- Easier: trusting the running state and restore-on-boot.
- Harder: trigger authors must classify their start errors correctly.
- Revisit when: a trigger needs "start degraded" reporting in the UI.
