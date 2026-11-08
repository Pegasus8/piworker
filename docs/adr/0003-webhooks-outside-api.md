# ADR-0003: Serve webhooks outside `/api`

**Status:** Accepted
**Date:** 2026-11-08
**Deciders:** maintainer

## Context

Webhook triggers were mounted at `/api/webhooks/<path>`. The session cookie is
scoped to `Path=/api`, so an admin opening a webhook URL in the browser sent the
session cookie, and the webhook trigger copied all headers into the flow
payload, where debug/notify/http nodes could expose it. The `/api` request guard
also needed an exception for webhooks. The application was not yet released.

## Options considered

### Option A: Keep the path, strip sensitive headers
**Pros:** no URL change. **Cons:** cookie still sent to an endpoint meant for
third parties; guard keeps an exception.

### Option B: Move webhooks to `/webhooks/<path>`
| Dimension | Assessment |
|---|---|
| Complexity | Low |
| Security | Cookie never sent; no unauthenticated exceptions under `/api` |
| Maintenance | Breaking URL change (acceptable before release) |

## Decision

Webhooks are served at `/webhooks/<path>`. In addition, `Cookie`,
`Authorization`, `Proxy-Authorization`, `X-Webhook-Token`, `X-PiWorker-*` and the
`token` query parameter are stripped from payloads; the token is compared in
constant time; responses are 200/401/404/405/413/503 according to the outcome.

## Consequences

- Easier: `/api` has a single, exception-free security policy.
- Harder: nothing material (pre-release).
- Revisit when: never, unless webhooks need the editor session.
