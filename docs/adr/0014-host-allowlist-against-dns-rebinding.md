# ADR-0014: Host allowlist against DNS rebinding

**Status:** Accepted
**Date:** 2026-11-08
**Deciders:** maintainer
**Design:** [docs/design/mcp.md §3, §8](../design/mcp.md)

## Context

A malicious web page can make the admin's browser resolve an attacker domain to
the Pi's LAN address (DNS rebinding). The browser then treats the Pi as
same-origin for that domain: the `Origin` equals `scheme://Host`, so the CSRF
check of ADR-0002 passes. With auth disabled that is enough to drive the API; with
MCP, only the bearer token stands in the way. The MCP Go SDK has a rebinding
check, but it only covers loopback listeners and would break a future HTTPS
reverse proxy that connects over `127.0.0.1`. Deployments differ: Docker
(container hostname and 172.x addresses), the Vite dev proxy (`Host:
backend:8080`), DHCP address changes, `.local` and router DNS names.

## Options considered

### Option A: Rely on Origin checks and tokens
**Pros:** nothing to configure. **Cons:** rebinding defeats same-origin Origin
checks by construction.

### Option B: Host allowlist with automatic defaults
| Dimension | Assessment |
|---|---|
| Complexity | Low |
| Security | Rebinding needs an attacker hostname; it is refused |
| Pi resources | Negligible (cached interface addresses) |
| Maintenance | Users with custom DNS names must add them |

### Option C: Only the SDK's loopback protection
**Pros:** built in. **Cons:** does not cover LAN listeners; breaks reverse
proxies.

## Decision

A Host allowlist middleware protects `/api`, `/mcp` and the web UI (not
`/webhooks/` or `/piworker-ca.crt`). Allowed: `localhost`, the hostname,
`hostname.local`, current interface IPs (re-read lazily), **any IP-literal
Host** (rebinding requires a hostname), and `allowed_hosts` (TOML,
`PIWORKER_ALLOWED_HOSTS`, `-allowed-hosts`). The port is stripped and names are
compared case-insensitively; a mismatch is 403. Compose files set
`PIWORKER_ALLOWED_HOSTS` (e.g. the dev proxy's `backend`). The SDK's localhost
protection is disabled on `/mcp` in favour of this allowlist plus an `Origin`
allowlist.

## Consequences

- Easier: one rebinding defence for every listener and deployment.
- Harder: access through a custom DNS name requires adding it to
  `allowed_hosts` (the 403 message says so).
- Revisit when: a reverse-proxy deployment needs forwarded-host handling.

## Action items

1. [ ] Middleware, config (`allowed_hosts`), compose settings and tests for
   Docker, dev proxy, IP literals and foreign hosts.
