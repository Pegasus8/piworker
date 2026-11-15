# ADR-0040: Node outputs from a setting

**Status:** Accepted
**Date:** 2026-11-15
**Deciders:** maintainer
**Design:** [docs/design/llm.md](../design/llm.md) §5

## Context

The LLM Classify node (`process-llm-classify`) sends each message out of
the port of the category the model picked, and the admin types the
categories. Until now a type's output ports were fixed in its
`NodeTypeInfo.Outputs`; the runtime, deploy and Lint already ask a node
instance for its ports (`Ports()`), but the editor, the node-type API, the
flow assistant, agent ops (`add_node`, `update_node`) and the dry-run's
stubs of hidden nodes only know the type. They must all agree on a node's
ports, or edges are refused or silently dropped (multi-output routing is
fail-loud). Settings also had no list type in the editor.

## Options considered

### Option A: fixed ports, route downstream

The classifier has `output` and sets `meta.llm.category`; Switch nodes
route on it.

**Pros:** no contract change. **Cons:** one Switch per category; the
category names live in expressions far from the classifier.

### Option B: instances declare any ports from their config

**Pros:** fully general. **Cons:** nobody but a built instance knows the
ports: the editor, agents and the assistant would need a server round trip
per change, and a node whose config is refused would have no ports at all.

### Option C: the type names the setting its extra outputs come from

| Dimension | Assessment |
|---|---|
| Complexity | Low: one field, one pure function shared by every reader |
| Security | None new: the ports are names; routing stays fail-loud |
| Pi resources | None |
| Maintenance | One rule (`node.ConfigOutputs`) mirrored once in the editor |

## Decision

Option C.

- `NodeTypeInfo.OutputsFrom` (`json:"outputsFrom,omitempty"`) names an
  array-of-strings setting of the type's schema; the registry refuses a
  name that is not an `array` setting of `Config`. The node's extra output
  ports are the setting's items, after the static `Outputs`, each with ID
  and name equal to the item and any data type.
- `node.ConfigOutputs(info, cfg)` is the rule: the static outputs, then
  one port per string item, skipping empty and repeated items, items equal
  to a static output's ID and `error` (the implicit error port, ADR-0036,
  stays last). The node's `Validate` refuses such items; `ConfigOutputs`
  never fails. The node's `Ports()` returns the same ports; Lint uses
  `ConfigOutputs` for a node whose config is refused, agent ops store
  `ConfigOutputs` on `add_node` and on an `update_node` of such a type, as
  does an agent's whole-flow draft, the dry-run's stub of a node with
  hidden settings uses it, and the flow assistant gives proposed nodes
  these ports. Extra ports are `Multiple` like the static outputs.
- `GET /api/node-types` sends `outputsFrom`; the editor renders the ports
  from the node's config with the same rule (`web/src/lib/ports.ts`).
  Removing an item removes the edges leaving its port in the same undo
  step; renaming an item in place keeps them.
- Settings of `Type: "array"` are lists of strings in the editor (a list
  input), checked by `node.CheckConfig` as before. `ConfigProperty.Options`
  applies to each item of a list; the registry refuses an unknown
  suggestion source. New sources: `llm-connections`, `llm-models` (the
  models of the node's `connection`), `tools` (the published tool names)
  and `knowledge-collections`.

The classifier is the first user: `OutputsFrom = "categories"`, static
output `other`; `other` and `error` are reserved category names.

## Consequences

- Easier: a node whose routes the admin names (categories today, perhaps
  a multi-way switch later) without a Switch per route.
- Harder: every reader of a node's outputs must go through `ConfigOutputs`
  (or its editor twin); a new reader that uses `info.Outputs` alone drops
  the extra ports.
- Revisit when: a type needs ports from something other than one list
  setting (ports with their own names or types).

## Action items

1. [x] `OutputsFrom`, `ConfigOutputs`, registry checks, readers updated.
2. [x] Editor list input and ports from config.
