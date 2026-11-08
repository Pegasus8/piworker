# ADR-0011: Native HTTPS by default with a local CA

**Status:** Accepted
**Date:** 2026-11-08
**Deciders:** maintainer
**Design:** [docs/design/mcp.md §2](../design/mcp.md) · tracks issue #287

## Context

PiWorker served plain HTTP; session cookies and, with MCP, agent tokens would
travel in clear over Wi-Fi. Worse, a LAN attacker answering mDNS
(`raspberrypi.local`) or ARP could impersonate the Pi and feed injected tool
descriptions/results to an agent that has shell access on the admin's
workstation — a new vector compared with a sandboxed browser. Public CAs cannot
issue certificates for private LAN names/IPs. Issue #287 requires: no silent URL
change, explaining warnings, fingerprint verification, renewal, address changes
and recovery.

## Options considered

### Option A: Stay HTTP; loopback-only `/mcp` with SSH tunnels
**Pros:** no TLS code. **Cons:** editor stays unencrypted; awkward for agents.

### Option B: Self-signed leaf certificate
**Pros:** simple. **Cons:** every IP/hostname change or renewal requires
re-trusting on every device.

### Option C: Local CA generated on first start, signing a server certificate
| Dimension | Assessment |
|---|---|
| Complexity | Medium |
| Security | Encryption plus verifiable identity (CA fingerprint) |
| Pi resources | ECDSA P-256; negligible |
| Maintenance | Automatic leaf re-issue; CA trusted once per device |

### Option D: Only document a reverse proxy
**Pros:** no code. **Cons:** most single-admin installs would stay on HTTP.

## Decision

Option C, on by default (`tls.mode = auto`; `custom` and `off` available).
The CA (10 years) and server certificate (397 days, SANs: localhost, hostname,
`hostname.local`, interface IPs, `tls.hosts`) live under the data directory;
the leaf is re-issued hot when near expiry or when SANs change; the CA rotates
only on explicit request. HTTPS on `addr` (`:8443`); `http_addr` (`:8080`)
redirects to HTTPS except `/webhooks/` (devices often cannot trust a custom CA;
`webhooks_http`) and `/piworker-ca.crt`. Cookies are Secure when TLS is on.
The installer and logs print the CA fingerprint; the editor has a "Trust this
device" page. Recovery: `-tls=off`, `piworkerctl tls off`. With TLS off,
`/mcp` is loopback-only unless `mcp.lan_access = true`. Fresh installs start on
HTTPS (pre-release, so no existing URL changes silently).

## Consequences

- Easier: encrypted editor and MCP on the LAN; agents can verify the server.
- Harder: each client device must trust the CA once (browsers warn until
  then); Claude Code needs `NODE_EXTRA_CA_CERTS`.
- Revisit when: ACME for local names becomes practical, or a reverse-proxy
  deployment becomes the norm.
