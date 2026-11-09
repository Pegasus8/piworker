# ADR-0002: Guard every unsafe `/api` request, even without authentication

**Status:** Accepted
**Date:** 2026-11-08
**Deciders:** maintainer

## Context

The CSRF check lived only in the authentication middleware. With
`-auth=false` (documented for local development and possible on a LAN Pi) any
web page open in the admin's browser could send a "simple" cross-origin POST
(`Content-Type: text/plain`, no preflight) to create and deploy a flow with an
`action-command` node: remote command execution. `json.Decoder` ignores the
content type, and flow IDs were client-chosen, so the attacker needed no
response. Sessions are cookie-only (HttpOnly, SameSite=Strict, `Path=/api`).

## Options considered

### Option A: Keep CSRF inside auth, document "never disable auth"
**Pros:** no change. **Cons:** the dangerous configuration stays one flag away.

### Option B: Request guard on all unsafe `/api` requests, independent of auth
| Dimension | Assessment |
|---|---|
| Complexity | Low |
| Security | Closes blind cross-site writes regardless of auth |
| Pi resources | Negligible |
| Maintenance | Clients must send a header (the editor already does) |

**Pros:** defence does not depend on configuration. **Cons:** non-browser API
clients must send the header and JSON.

### Option C: Bearer tokens for `/api`
**Pros:** standard for APIs. **Cons:** a second credential type on the editor
API, token storage in the browser; rejected (sessions stay cookie-only).

## Decision

`RequestGuardMiddleware` is always installed. Every unsafe method on `/api/`
requires `X-PiWorker-Request: 1`; if an `Origin` header is present it must be
this server (scheme://Host) or allowlisted, and without `Origin` the request is
refused when `Sec-Fetch-Site: cross-site`; a failed check is 403. A request
with a body must be JSON (415 otherwise). The request guard has **no
exceptions under `/api`** (webhooks moved out, see ADR-0003); session
authentication separately exempts only login, setup, recover, status and
health. Flow IDs are validated or server-generated. `/api` never accepts Bearer
tokens; agent access is a separate `/mcp` endpoint (ADR-0010).

## Consequences

- Easier: running with auth disabled during development is no longer an RCE.
- Harder: scripts using the REST API must send the header and JSON.
- Revisit when: a supported third-party REST client appears.
