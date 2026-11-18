# ADR-0016: Refuse `/mcp` on the plain listener and present the CA in the chain

**Status:** Accepted
**Date:** 2026-11-09
**Deciders:** maintainer
**Supersedes:** ADR-0011 (only the plain-listener rule for `/mcp` and the presented chain)
**Design:** [docs/design/mcp.md §2, §8, §12](../design/mcp.md)

## Context

ADR-0011 makes the plain-HTTP listener (`http_addr`, `:8080`) answer every
path with a 308 redirect to HTTPS, except `/webhooks/` (with `webhooks_http`),
`/piworker-ca.crt` and GET/HEAD `/api/health`. Two things showed up when the
MCP endpoint and the `piworker-mcp` bridge were built:

- An agent configured with `http://pi:8080/mcp` sends its Bearer token in
  clear text on the first request. A redirect makes that setup *work*: the
  client follows it, the call succeeds, and the token keeps crossing the LAN
  in clear text on every request without anyone noticing.
- The bridge pins PiWorker's CA by file or by SHA-256 fingerprint. Auto mode
  presented only the server certificate, so pinning by fingerprint alone had
  to download `/piworker-ca.crt` without authentication before the first
  request, an extra round trip that a client cannot verify by itself.

## Options considered

### Option A: keep redirecting `/mcp`; leaf-only chain

| Dimension | Assessment |
|---|---|
| Complexity | None |
| Security | A misconfigured agent leaks its token on every call, silently |
| Pi resources | None |
| Maintenance | None |

**Pros:** nothing changes. **Cons:** the leak is invisible; fingerprint pinning
needs an unauthenticated download.

### Option B: refuse `/mcp` on the plain listener; present [server certificate, CA]

| Dimension | Assessment |
|---|---|
| Complexity | Low (one branch in the redirect handler; one extra certificate in the chain) |
| Security | The leak happens at most once and is reported; pinning needs no download |
| Pi resources | About 1 KiB more per TLS handshake |
| Maintenance | Low |

**Pros:** a wrong URL fails loudly and tells the admin to revoke the token;
the bridge finds the pinned CA in the handshake. **Cons:** clients that send
`http://` must be fixed instead of being redirected.

## Decision

- With TLS on, the plain listener answers `/mcp` (and paths below it, after
  cleaning dot segments) with **400** and a JSON error naming the HTTPS URL
  and saying the token crossed the network in clear text and should be
  revoked. It is never redirected. The server logs a warning with the token's
  id (never the secret). The other ADR-0011 exceptions are unchanged.
- In `tls.mode = auto`, the server presents **[server certificate, CA]**.
  Clients that already trust the CA are unaffected. A client pinning the CA's
  fingerprint verifies the chain against that presented CA and never trusts
  anything else; a presented root whose fingerprint does not match stops the
  bridge before any request. Custom mode presents whatever its files hold.

## Consequences

- Easier: a misconfigured agent is noticed at the first request; the bridge
  needs only the CA fingerprint the installer and the editor show.
- Harder: nothing for correctly configured clients.
- Revisit when: PiWorker can be put behind an HTTPS reverse proxy as the
  norm (then the plain listener might not exist at all).

## Action items

1. [x] `redirectHandler` refuses `/mcp` (`main_tls.go`, `mcpserver.RefusePlainHTTP`).
2. [x] `internal/tlsca` presents [server certificate, CA] in auto mode.
3. [x] The bridge pins by fingerprint from the handshake, downloading
   `/piworker-ca.crt` only when the server presents no root (custom mode).
