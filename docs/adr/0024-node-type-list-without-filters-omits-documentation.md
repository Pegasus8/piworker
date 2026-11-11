# ADR-0024: The node-type list without filters omits documentation

**Status:** Proposed
**Date:** 2026-11-11
**Deciders:** maintainer
**Design:** [docs/design/mcp.md §10](../design/mcp.md), [docs/design/zigbee.md §4, phase 0a](../design/zigbee.md)

## Context

`pw_list_node_types` answers within 64 KiB (`maxNodeTypeListBytes`; a
result carries the list twice, as structured content and text). Without
filters it listed every type's structure and then documentation in order
while it fitted. With the 34 builtin types it used 65.2 KB of the 65.5 KB:
structures 33 KB, documentation 31 KB. `TestListNodeTypesOfTheBuiltins`
requires every builtin to be listed whole, so any new node with
documentation (the Zigbee nodes are next) fails it; dropping that
requirement instead would leave the *last* types — actions, the ones that
reach the outside world — without documentation, depending only on the
order of type ids.

## Options considered

### Option A: raise the bound

| Dimension | Assessment |
|---|---|
| Complexity | Low |
| Security | Unchanged |
| Pi resources | Larger results, kept twice in each response |
| Maintenance | Postpones the problem by a few nodes |

**Pros:** no change for agents. **Cons:** the bound protects the agent's
context and the read views share it; growth is open-ended.

### Option B: keep the order-based cut

**Pros:** no code change. **Cons:** which types lose their documentation
depends on their ids; the builtin test has to be weakened.

### Option C: without filters, structures only; documentation by category or type

| Dimension | Assessment |
|---|---|
| Complexity | Low: one branch in `listNodeTypes` |
| Security | Unchanged: registry metadata only |
| Pi resources | Smaller first answer (33 KB instead of 64 KB) |
| Maintenance | Room for about 30 more types; each category is listed whole with its documentation (20–31 KB today) |

**Pros:** predictable; an agent reads the markers and ports of every type at
once (what authoring needs to choose), then the documentation of the
category or type it uses. **Cons:** one more call for agents that want the
documentation.

## Decision

Option C. `pw_list_node_types` without `category` and `type` lists every
type's structure (ports, risk, settings and markers) with
`documentationOmitted: true` on each type that has documentation; that
omission does not set `truncated`. With `category` or `type` the list is as
before: structures first, then documentation in order while it fits, each cut
to 8 KiB. The tool's description and the `author_flow` prompt tell agents to
ask for a category or a type for documentation.
`TestListNodeTypesOfTheBuiltins` requires the unfiltered list and each
category to list every builtin whole, documentation included for the
categories.

## Consequences

- Easier: adding node types (Zigbee and others) without touching MCP bounds.
- Harder: an agent needs two calls to read the documentation of everything;
  clients that relied on the unfiltered list carrying documentation get
  `documentationOmitted` and the same hint as before.
- Revisit when: one category approaches 64 KiB with its documentation; then
  list documentation by type only.

## Action items

1. [x] `listNodeTypes`, tool description, `author_flow` prompt, goldens.
2. [x] `docs/design/mcp.md` §10 describes the unfiltered list.
