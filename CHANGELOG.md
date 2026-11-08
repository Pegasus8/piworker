# Release notes

## Unreleased — revival

This is the pending first release of the revived implementation, not a published
version. The version number and release date have not been selected.

### Included

- Visual flow editor, node configuration and playground, manual execution,
  deployment/restoration on restart, execution history and live debugging.
- Persistent global and flow variables, plus a separate named credential store.
- One-use administrator setup and recovery codes, server-side cookie sessions
  and account password changes.
- Embedded frontend in a single Go binary, Linux AMD64/ARM64/ARMv7 archives and
  a multi-platform Docker build/publication workflow.
- `piworkerctl install`, `update` and `uninstall` for Linux/systemd. Updates verify
  checksums and back up data; uninstall retains accounts, flows and configuration.
- `piworker -version` reports the build version without starting the server.
- Health checks return HTTP 503 when storage is closed. Resizing the flow editor
  keeps nodes reachable on smaller screens.

### Native HTTPS

- PiWorker serves HTTPS by default on port 8443. On first start it creates a
  local certificate authority (ECDSA P-256, 10 years) and a server certificate
  (397 days) in the `tls` directory next to the database. The certificate
  renews itself 30 days before expiry and when the device's addresses or host
  name change, without a restart. The CA is only replaced with
  `-tls-regenerate-ca`; a missing CA key stops startup instead of silently
  creating a new CA.
- Port 8080 redirects to HTTPS (308) and serves `/piworker-ca.crt` and, by
  default (`webhooks_http`), `/webhooks/` for devices that cannot trust a local
  CA. Session cookies are always `Secure` with TLS on.
- New **Trust this device** page (download the CA, check its fingerprint,
  install and remove the trust on macOS, Windows, Linux, iOS, Android and
  Firefox, plus curl and Claude Code). `GET /api/tls` reports the mode, CA
  fingerprint, certificate names and expiry. Startup and
  `piworkerctl install` print the HTTPS address and CA fingerprint.
- Configuration: `addr` now defaults to `:8443`; new `http_addr`,
  `webhooks_http` and `[tls]` (`mode` auto/custom/off, `hosts`, `cert_file`,
  `key_file`; custom certificates reload on SIGHUP), with `PIWORKER_TLS_*`,
  `PIWORKER_HTTP_ADDR` and `PIWORKER_WEBHOOKS_HTTP`, and the `-tls`,
  `-http-addr` and `-tls-regenerate-ca` flags. A configuration with `addr` and
  `http_addr` on the same port is rejected at startup.
- Recovery: `-tls=off`, `PIWORKER_TLS_MODE=off` or `sudo piworkerctl tls off`
  serve plain HTTP on `addr`; `piworkerctl tls on` restores HTTPS. Docker
  publishes 8443 and 8080 and keeps `tls/` in the data volume; set
  `PIWORKER_TLS_HOSTS` to the names browsers use for the Docker host.
- Upgrading a manual installation whose configuration sets `addr = ":8080"`:
  either move it to `:8443` (and keep `http_addr = ":8080"`), set
  `http_addr = ""`, or set `[tls] mode = "off"` to keep plain HTTP.

### Host allowlist

- The API, the web UI and the MCP endpoint answer only requests whose `Host`
  is a name of this server (403 otherwise), against DNS rebinding: `localhost`,
  the hostname, `<hostname>.local`, any IP address, plus `allowed_hosts`
  (`PIWORKER_ALLOWED_HOSTS`, `-allowed-hosts`). Webhooks and the CA download
  accept any name. The Compose files set `PIWORKER_ALLOWED_HOSTS`.

### AI agents (MCP)

- Flows can be published as tools for AI agents: an **Agent Tool** node
  (`trigger-tool`) starts the flow with arguments checked against a closed JSON
  Schema subset, and an **Agent Reply** node (`action-reply`) returns the
  result. Tool names are unique across running flows (409 on conflict).
- PiWorker rates each tool `safe`, `external` or `privileged` from the nodes
  it reaches (including through flow variables) and refuses to deploy a tool
  whose arguments reach a privileged node without strict constraints.
- The `/mcp` endpoint is off by default (`[mcp] enabled`, `PIWORKER_MCP_*`)
  and requires authentication; with TLS off it accepts only local clients
  unless `lan_access` is set.
- Agents use scoped tokens (`call`, `read`, `author`, `deploy`), shown once,
  90-day default expiry, accepted only on `/mcp`. Privileged tools need the
  token's privileged flag and are never covered by "all tools". Grants are
  bound to the tool's fingerprint: any change to a tool suspends them until
  re-confirmed. Account recovery revokes every token; a password change does
  too unless "keep agent tokens" is ticked.
- Tools with `confirm: admin` wait for approval in the editor (15 minutes);
  an approval runs only the tool version that was requested and keeps its
  result. Every agent call is audited for 30 days.
- Runs record who started them (editor, agent token or trigger).

### Agent access pages

- New **Agents** pages: tokens and grants (created with your password, with
  ready-to-paste set-up for Claude Code and for Claude Desktop on macOS/Linux
  and Windows), revoke one or all tokens, the agent deploy level (password),
  the approval queue with a badge and an app-wide notice, and the audit log
  with rejection totals. The pages show whether MCP is on.
- Text from agents and flows is shown as plain text with hidden and
  direction-changing characters escaped, and the web UI sends a
  Content-Security-Policy that allows only its own scripts.
- The editor's **Test tool** calls an Agent Tool node as an agent would; the
  canvas shows who started each run.

### Security correction

- Command-node payload/topic/metadata values are passed as literal arguments,
  including in quoted placeholders; data is never recursively expanded into code.
  Unsupported shell expansion contexts fail validation before execution.
- Command output reports the configured template instead of inlining message values.
  Positional shell arguments are reserved for bindings; templates using set/shift
  or explicit eval/interpreter re-execution must be reviewed.

### Hardening

- Write requests under `/api/` always require the editor's request header,
  a same-origin (or allowlisted) Origin and a JSON body, even with auth disabled.
- Webhooks are served at `/webhooks/<path>`, outside `/api`, so the session cookie
  is never sent to them. They answer 401/405/413/503 instead of always 200, and
  credentials (cookies, auth headers, the webhook token) never reach the payload.
- Configuration fails closed: empty or unrecognised boolean environment values,
  a missing explicit config file and unknown TOML keys stop startup.
- Metrics are served only at `/api/metrics`, behind the API's authentication.
- Deploys fail (400 invalid flow, 409 still stopping) instead of reporting a
  running flow when the graph is invalid (cycles, duplicate IDs) or a trigger
  cannot start. Network triggers (MQTT, RSS) start even if the network is not up
  yet and connect when it is.
- A slow node no longer stalls other branches, messages keep their order per
  connection, and nodes release their network and database connections when
  their flow stops.

### Compatibility and limits

- Native releases require systemd and glibc 2.35+ for the managed installer.
  Standalone binaries do not require systemd. ARMv6 and native macOS/Windows
  release binaries are not provided. Docker is the alternative on those desktop OSes.
- The old `v0.1.0-alpha.1` release from 2020 is not compatible with the installer.
  Its database migration has not been validated. Test with a copy and retain the
  original binary and data; no automatic migration guarantee is made.
- Existing accounts from the revival session-authentication migration retain
  their password hashes. Legacy JWT sessions must sign in again; Bearer JWT API
  authentication and the `jwt_secret` setting are no longer supported.
- HTTPS uses a local CA that each device must trust once (or your own
  certificate with `[tls] mode = "custom"`). With `mode = "off"` the server
  listens over HTTP, for example behind a reverse proxy; `session_secure` then
  only controls the cookie's Secure flag.
- Secrets have no application-level encryption at rest. Flow exports do not
  include the separate variable or secret stores. Credentials entered literally
  in a node can appear in its exported configuration.
- Updates interrupt flows. Restoring local data cannot undo commands or requests
  already sent to devices and external services.

See the [README](README.md) for setup, the [operations guide](docs/operations.md)
for recovery, and [TODO](TODO.md) for explicitly deferred features.
