# ADR-0013: Closed tool input schemas and server-computed risk

**Status:** Accepted
**Date:** 2026-11-08
**Deciders:** maintainer
**Design:** [docs/design/mcp.md §5](../design/mcp.md)

## Context

Tool arguments flow into nodes that run commands, write files, call URLs or
toggle pins. JSON Schema is too expressive to lint reliably (`anyOf`, `$ref`,
untyped schemas, boolean schemas…) and its `pattern` is unanchored (`[0-9]+`
accepts `"5 --exec=x"`). Risk also travels through shared state: a tool that
only writes a global variable can steer another flow's `action-http` URL or
`action-command` argument. Annotations computed by the admin can be wrong.

## Options considered

### Option A: Full JSON Schema plus admin-declared risk
**Pros:** flexible. **Cons:** unlintable; trusts the admin to classify risk.

### Option B: Closed schema subset and server-computed risk with taint rules
| Dimension | Assessment |
|---|---|
| Complexity | Medium |
| Security | Argument injection and cross-flow escalation addressed at deploy |
| Maintenance | Risk rules must evolve with new nodes |

## Decision

- `internal/toolschema` accepts only a closed subset: object root with
  `additionalProperties: false`; single-type primitives, arrays of primitives
  with `maxItems`, nested objects (depth ≤ 3); anchored `pattern`s; strings
  capped at 256 when unbounded; composition keywords, refs, boolean/untyped
  schemas and `x-mcp-header` rejected. Validation is explicit in the service
  layer (the SDK's low-level `AddTool` does not validate).
- Strings reaching a privileged node need `enum` or an anchored `pattern`
  that rejects a probe corpus (`-x`, `--a=b`, `..`, `/`, whitespace, `$(`, `;`,
  `|`, backtick) unless acknowledged per property with `x-piworker-allow`
  (stripped from the schema advertised to clients); numbers need min and max.
- Risk per node type (`NodeTypeInfo.Risk`) escalated per instance (dynamic
  destinations, `allowPrivate`) and through shared state (`set-var` must be
  flow-scoped downstream of `trigger-tool`; reaching `action-mqtt`/`action-file`
  is privileged). Downstream of `trigger-tool`: `process-aggregate` is a deploy
  error; `process-join` is an error unless keyed by `meta.correlationID`;
  `process-delay` with `delayMs > timeoutMs` is a warning.
- These checks run within the single validation path of ADR-0007
  (`flow.Lint` collects all issues from the same checks) and map to HTTP 400.
- Annotations are computed by the server, always explicit, never lowered by
  the admin; `idempotentHint` is the only admin flag.
- `action-command` rejects placeholder values starting with `-` unless allowed.

## Consequences

- Easier: tools are safe to publish without security expertise.
- Harder: some schemas are rejected; new node types must declare `Risk` (a test
  enforces it) and dynamic-destination rules.
- Revisit when: adding nodes with new side-effect classes.

## Action items

1. [ ] `internal/toolschema` with the closed subset, probe corpus and
   `x-piworker-allow`.
2. [ ] `Risk` on every node type (test-enforced), `internal/flow/risk.go` with
   instance escalation and taint rules.
3. [ ] `allowDashArgs` in `action-command`.
