# Architecture Decision Records

An Architecture Decision Record (ADR) captures one significant decision, its
context, the options weighed and its consequences, so later changes can see
*why* PiWorker is built the way it is.

## When an ADR is required

Write an ADR **before or together with** the change (same PR) when a change:

- adds, removes or replaces a component, package boundary, process or listener;
- changes a public or cross-component contract: REST/MCP API shape, flow JSON
  model, node interfaces, storage schema semantics, configuration semantics;
- changes the security model: authentication, authorization, CSRF/origin/host
  checks, secrets handling, TLS, what agents may do;
- changes runtime semantics: delivery, ordering, concurrency, retries, deploy
  and lifecycle guarantees;
- adds a significant dependency (SDK, framework, cgo library) or drops platform
  support;
- reverses or materially changes an earlier ADR.

Bug fixes, refactors that keep contracts, new nodes that follow the existing
node recipe, UI changes and documentation do **not** need an ADR.

## Process

1. Copy [`template.md`](template.md) to `NNNN-short-title.md` with the next
   number (four digits, never reused) and add a row to the Index below
   (number, title as in the heading, status). Keep the Index status in sync.
2. Set **Status: Proposed**, fill in Context, Options, Decision, Consequences.
   Keep it short; link to longer design documents in `docs/design/` for detail.
3. Merge it with the implementing change and set **Status: Accepted**. If the
   decision is accepted before implementation is complete, keep the Action
   Items list current.
4. Never rewrite an accepted ADR's decision. To change it, write a new ADR
   (Accepted, with a `**Supersedes:** ADR-NNNN` line) and set the old one to
   **Superseded by ADR-NNNN**. A decision withdrawn without replacement becomes
   **Deprecated**. Corrections of facts, typos and links may be made in place.
5. Reference the ADR number in the commit message or PR description.

## Index

| ADR | Title | Status |
|---|---|---|
| [0001](0001-record-architecture-decisions.md) | Record architecture decisions | Accepted |
| [0002](0002-api-request-guard-without-auth-exceptions.md) | Guard every unsafe `/api` request, even without authentication | Accepted |
| [0003](0003-webhooks-outside-api.md) | Serve webhooks outside `/api` | Accepted |
| [0004](0004-configuration-fails-closed.md) | Configuration fails closed | Accepted |
| [0005](0005-per-node-queues-and-fifo-delivery.md) | Per-node bounded queues and FIFO delivery | Accepted |
| [0006](0006-all-or-nothing-deploy.md) | All-or-nothing deploy with network-tolerant triggers | Accepted |
| [0007](0007-registry-based-flow-validation.md) | Validate flows against the registry and reject cycles | Accepted |
| [0008](0008-node-closer-lifecycle-hook.md) | `node.Closer` lifecycle hook | Accepted |
| [0009](0009-mcp-flows-as-tools.md) | Expose PiWorker to agents over MCP as "flows as tools" | Accepted |
| [0010](0010-scoped-agent-tokens-on-mcp-only.md) | Scoped opaque agent tokens, only on `/mcp` | Accepted (grant suspension rule superseded by ADR-0015) |
| [0011](0011-native-https-with-local-ca.md) | Native HTTPS by default with a local CA | Accepted |
| [0012](0012-human-approval-and-opt-in-agent-deploy.md) | Human approval gates and opt-in agent deploy | Accepted |
| [0013](0013-closed-tool-schemas-and-server-computed-risk.md) | Closed tool input schemas and server-computed risk | Accepted |
| [0014](0014-host-allowlist-against-dns-rebinding.md) | Host allowlist against DNS rebinding | Accepted |
| [0015](0015-tool-identity-binds-grants-and-approvals.md) | Bind grants and approvals to the tool's identity | Accepted |
