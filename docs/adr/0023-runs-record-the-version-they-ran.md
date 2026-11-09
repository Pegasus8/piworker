# ADR-0023: Runs record the version they ran

**Status:** Accepted
**Date:** 2026-11-09
**Deciders:** maintainer
**Supersedes:** ADR-0022 (only how a run's version is told for "run errors
by provenance")
**Design:** [docs/design/mcp.md §6](../design/mcp.md)

## Context

ADR-0022 shows agents a run's recorded errors only when the run is of a
version the admin approved. It told a run's version from the flow's current
deployment marker: the run had to start in a later second than the marker,
and the marker's version had to be admin-approved. Review found that this
fails open:

- The marker is written after the runtime starts. Until then it names the
  previous, approved version, so runs of an agent's new version recorded in
  that window showed their errors; a marker that failed to record (or a
  restore that failed to correct it) left them shown for good.
- A run's start time is the history recorder's clock when it handles the
  run's first event, from a queue of up to 4096 events. Runs of an agent's
  version still queued when the admin redeploys an approved one are stamped
  after the new marker and counted as the admin's.

## Options considered

### Option A: keep the marker, write it before the deploy

| Dimension | Assessment |
|---|---|
| Complexity | Low |
| Security | Closes the window, not the recorder's late stamps |
| Pi resources | None |
| Maintenance | Two writes that must stay in step; an undo on failure |

**Pros:** no schema change. **Cons:** still compares two clocks.

### Option B: the runtime reports its version with every node event; the history keeps it per run

| Dimension | Assessment |
|---|---|
| Complexity | Low (one field on the event, one column) |
| Security | Exact: what ran is recorded by what ran it |
| Pi resources | One integer per run |
| Maintenance | Low |

**Pros:** no clocks, no marker; a past run keeps its provenance whatever is
deployed later. **Cons:** runs recorded before the column exist read as
unknown (withheld) once.

## Decision

- `flow.NodeEvent` carries `FlowVersion`: the stored version of the
  definition the runtime runs, as it was when the runtime was built.
- The history records it with the run (`flow_runs.flow_version`, 0 when
  unknown) from the run's first event; later events never change it.
- Agents see a run's recorded errors only when that version is stored and
  admin-approved, whoever deployed it; otherwise the fixed text of
  ADR-0022. The deployment marker plays no part in it.

## Consequences

- Easier: provenance holds through failed markers, restores and recorder
  backlogs; earlier runs of an approved version stay readable after an
  agent deploys another.
- Harder: nothing notable; a pruned version reads as unknown, as before.
- Revisit when: runs are recorded by a process other than the runtime.

## Action items

1. [x] `NodeEvent.FlowVersion`, `storage.RunStart`/`StartFlowRun`, the
   `flow_version` migration, `Reads.adminRun` comparing it, with a test of
   a run recorded between deploy and marker and of a queued run.
