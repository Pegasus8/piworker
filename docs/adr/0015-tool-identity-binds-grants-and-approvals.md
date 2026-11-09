# ADR-0015: Bind grants and approvals to the tool's identity

**Status:** Accepted (what the fingerprint covers amended by ADR-0022)
**Date:** 2026-11-08
**Deciders:** maintainer
**Supersedes:** ADR-0010 (only the grant suspension rule)
**Design:** [docs/design/mcp.md §5, §7, §9](../design/mcp.md)

## Context

ADR-0010 suspends a grant when a redeploy "raises risk or widens the schema",
and ADR-0012 lets the admin approve a `confirm: admin` call that runs later.
Implementing both showed three gaps:

- Deciding that a new input schema is "wider" than the old one means comparing
  patterns, enums and bounds of a JSON Schema. It is easy to get wrong, and a
  wrong "not wider" silently keeps an agent's access to a tool that now
  accepts more.
- An approval stored only the flow and trigger node. If the flow was
  redeployed between the request and the decision, approving ran whatever
  that node published by then, possibly a renamed, repurposed or riskier tool
  than the one the admin read about. Approval outcomes also lived only in
  memory, so an agent asking later could get "unknown run".
- ADR-0013 rates a tool by the nodes reachable from its trigger. A tool that
  writes a flow variable lets another branch of the same flow (a cron or
  webhook trigger) feed that value into a command or a request, so the tool's
  real effect is not on its own branch.

## Options considered

### Option A: keep ADR-0010's rule and compare schemas

| Dimension | Assessment |
|---|---|
| Complexity | High (schema subsumption over the closed subset) |
| Security | Fails open on a comparison bug |
| Pi resources | Negligible |
| Maintenance | Every new schema keyword needs comparison rules |

**Pros:** fewer re-confirmations after harmless edits.
**Cons:** a subtle bug keeps access the admin never granted.

### Option B: any change to the tool's contract suspends; approvals pin it

| Dimension | Assessment |
|---|---|
| Complexity | Low (compare one fingerprint) |
| Security | Fails closed |
| Pi resources | Two short columns per approval, one result blob |
| Maintenance | Low |

**Pros:** simple to check and to explain; the admin approves exactly what
they read.
**Cons:** cosmetic changes (a title, a looser bound) also need a
re-confirmation; an approval made stale by a redeploy must be requested again.

## Decision

- **Fingerprint.** A tool's fingerprint is `sha256:` over the canonical JSON
  of its risk, annotations, input schema and `confirm` mode. **Any**
  fingerprint change suspends the grants bound to it until the admin
  re-confirms them (this replaces ADR-0010's "raises risk or widens the
  schema"). `all_tools` still never covers privileged tools.
- **Approvals pin the tool.** A `tool_call` approval records the tool's name,
  fingerprint and risk at request time (`agent_approvals.tool_name`,
  `tool_fingerprint`, `tool_risk`); the editor shows the recorded risk.
  Executing an approval re-resolves the tool and refuses to run, storing an
  error result, when the name or fingerprint differs or was not recorded
  (fail closed).
- **Approvals keep their outcome.** The call result of an executed approval is
  stored on the approval (`result_json`, at most 2 MiB): first the immediate
  result, then the final one when a still-running call ends. A storage
  failure stores an error result instead, so an approved call never stays
  "running" forever.
- **Risk through flow variables.** When a tool reaches a `set-var` (always
  flow-scoped, ADR-0013), every enabled node of the same flow that reads flow
  variables (`get-var` with flow scope, or config text mentioning `flowVars`
  or `$env`), and everything downstream of it, counts towards the tool's risk,
  credential checks and strict-argument checks. The check over-approximates:
  it ignores variable names.

## Consequences

- Easier: reasoning about what an agent may do: the grant or approval names
  one exact tool contract.
- Harder: every edit to a published tool's contract asks the admin to
  re-confirm its grants; flows mixing tools with flow variables and privileged
  branches are rated privileged or refused.
- Revisit when: re-confirmations after harmless edits become a real burden
  (then add a narrow, tested "harmless change" rule rather than schema
  comparison).

## Action items

1. [x] Fingerprint over risk, annotations, schema and confirm; suspension on
   any change (`internal/flow/risk.go`, `internal/service`).
2. [x] `tool_name`, `tool_fingerprint` and `result_json` on approvals, fail
   closed execution.
3. [x] `tool_risk` recorded on approvals and shown by the editor.
4. [x] Flow-variable taint in the risk analysis.
