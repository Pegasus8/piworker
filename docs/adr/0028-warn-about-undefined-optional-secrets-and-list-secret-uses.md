# ADR-0028: Warn about undefined secrets in optional settings and list where secrets are used

**Status:** Accepted
**Date:** 2026-11-13
**Deciders:** maintainer

## Context

ADR-0019 made deploy refuse a flow whose *required* credential (Telegram bot
token, Notify URL, Database DSN: `node.SecretRequirer`) references a secret
that does not exist. Everywhere else an undefined `{{secret.NAME}}` was
silent: an MQTT or SMTP username or password resolved to an empty string,
so the node connected unauthenticated or failed to log in, and an HTTP
request with a secret in its URL or headers failed on every run. The admin
found out from a failed run, if at all.

The opposite question had no answer either: the Secrets dialog listed names
only, so deleting or renaming a secret could break flows (or a broker
connection) without warning.

## Options considered

### Option A: refuse the deploy for every undefined secret

**Pros:** one rule. **Cons:** turns existing deployable flows into refused
ones (an optional SMTP password left empty on purpose), changes ADR-0006's
all-or-nothing deploy for settings that work without the value.

### Option B: a Lint warning for optional settings, carried by the deploy response; a usage listing

| Dimension | Assessment |
|---|---|
| Complexity | Low: one node interface, one Lint collector, one response field, a reference scan |
| Security | Only names are reported, never values; agents get the same withholding rules as `undefined_secret` |
| Pi resources | A Lint pass per editor deploy (deploy is rare) and a scan of the stored flows per Secrets listing |
| Maintenance | Nodes that resolve secrets must declare where (as they already do for required ones) |

**Pros:** no flow that deploys today is refused; the admin sees the problem
at deploy time and where each secret is used. **Cons:** a warning can be
ignored.

## Decision

Option B.

- **Node interface.** `node.SecretReferencer` (`SecretReferences()
  map[string][]string`, by config property) is implemented by the nodes that
  resolve secrets when they run without requiring them: `action-email` and
  the MQTT nodes (`username`, `password`) and `action-http` (`url`,
  `headers`). Required settings stay `node.SecretRequirer`'s. Generic
  templates never resolve secrets and are not listed.
- **Lint.** A new warning code `unresolved_secret`: a `SecretReferencer`
  setting of an enabled node references a secret the store of the context
  does not define. The message names the setting and the secret, never a
  value. It is an environment issue (`flow.EnvironmentIssue`), like
  `undefined_secret`, and never blocks a deploy, a dry-run or a draft.
- **Deploy response.** `POST /api/flows/{id}/deploy` and
  `PATCH /api/flows/{id}/toggle` answer `data.warnings`: the warnings the
  deploy itself found with the instances it built (the tool risk warnings
  and `unresolved_secret`, rendered as Lint renders them;
  `FlowRuntime.Warnings`), always a list (empty on a stop). Every deploy
  path records them, with no second Lint. The editor shows them as a
  warning toast that stays until dismissed; startup logs them for the
  flows it restores.
- **Agents.** Where an agent wrote the setting, the authoring
  `unknown_secret` warning (which echoes what the agent wrote, ADR-0021) is
  kept and Lint's `unresolved_secret` of the same setting is left out; for
  a setting the agent cannot read, the message does not name the secret.
- **Usage.** `flow.SecretReferences(f)` lists every `{{secret.NAME}}` in a
  flow's node settings (node, setting, enabled), and
  `brokers.SecretReferences(c)` those of a connection's credentials (both
  parse references as `secrets.Expand` resolves them, `secrets.References`).
  `GET /api/secrets?usage=1` (only the Secrets dialog asks: it reads every
  stored flow; the plain listing, which the editor reads on every node it
  selects, stays names only) answers, for each secret, `uses` (flow ID and name,
  whether it is deployed, node ID and type, whether the node is disabled,
  setting; or connection and setting) and a new `undefined` list: the
  names referenced somewhere that no secret has, with their uses. Values
  and lengths are still never returned. The listing is admin-only, as
  before; MCP exposes none of it.

## Consequences

- Easier: an undefined optional credential is visible at deploy; the
  Secrets dialog says what deleting a secret affects and offers to define
  the referenced names that are missing.
- Harder: a node that starts resolving secrets in a new setting must also
  declare it in `SecretRequirer` or `SecretReferencer`, or Lint stays quiet
  about it.
- Revisit when: a warning should become an error (for example an HTTP
  request whose URL secret is undefined), or flows should be redeployed when
  a secret they use changes (today the next execution reads the new value).

## Action items

1. [x] `node.SecretReferencer`, the builtin implementations, the Lint
   collector and the deploy's warnings (`FlowRuntime.Warnings`).
2. [x] `data.warnings` on deploy and toggle; the editor's warning toast.
3. [x] `uses` and `undefined` in `GET /api/secrets`; the Secrets dialog.
