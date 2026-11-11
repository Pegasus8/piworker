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
- A [user guide](docs/guide/README.md): getting started, concepts, recipes, AI
  agents and troubleshooting, plus a node reference generated from the nodes'
  own schemas.
- MQTT Subscribe and MQTT Publish nodes on one broker with the same
  credentials and no client id of their own share a connection (ADR-0025),
  instead of one connection per node; nodes with overlapping topic filters
  get separate connections, so no node receives a message twice.
- The System Metric trigger and the agents' host status read the CPU's
  temperature on x86_64 PCs too: the CPU thermal zone (`x86_pkg_temp`) or, on
  most AMD machines, the `coretemp`/`k10temp` sensor, instead of the first
  thermal zone (a board sensor on PCs). The automated validation script passes
  on x86_64.
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
- Port 8080 redirects to HTTPS (308) and serves `/piworker-ca.crt`, GET/HEAD
  `/api/health` and, by default (`webhooks_http`), `/webhooks/` for devices
  that cannot trust a local CA; `/mcp` there is refused (400), never
  redirected. Session cookies are always `Secure` with TLS on.
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
  bound to the tool's fingerprint (its risk, annotations, input schema,
  confirmation mode and the ID and type of every node above `safe` it
  reaches): any such change, repointing a tool to another action included,
  suspends them until re-confirmed, and its pending approvals run nothing. Account recovery revokes every token; a password change does
  too unless "keep agent tokens" is ticked.
- Tools with `confirm: admin` wait for approval in the editor (15 minutes);
  an approval runs only the tool version that was requested and keeps its
  result. Every agent call is audited for 30 days.
- Runs record who started them (editor, agent token or trigger) and the
  flow version they ran.
- Built-in tools by scope: `read` gets `pw_list_flows`, `pw_get_flow`,
  `pw_list_runs`, `pw_get_run`, `pw_system_status` and the `diagnose_flow`
  prompt (secrets, typed credentials, settings hidden from agents and
  variables stay hidden; run errors only of versions the admin approved);
  `call` gets `pw_get_result` for calls that waited for approval or
  outlasted their timeout.
- Authoring (`author` scope, only with `[mcp] author_enabled`):
  `pw_list_node_types` (without arguments the structure of every type,
  documentation by category or type; ADR-0024), `pw_validate_flow` (every
  problem a deploy would find, from the same checks), `pw_save_draft`, `pw_get_draft` and the
  `author_flow` prompt. Agents write drafts, never flows: a whole flow or
  operations against the version they read (a stale one is a conflict),
  without changing Agent Tool or Agent Reply nodes, webhook paths or tokens,
  risk switches or the legacy variables, and with only `{{secret.NAME}}`
  references in credential settings. At most 20 drafts per token wait;
  decided drafts are pruned after 30 days.
- `pw_dry_run` runs a flow once in a sandbox: triggers do not start, every
  node that is not `safe` is a stub returning the agent's mock, variables
  are empty and there are no secrets (at most 50 nodes, 10 s).
  `pw_test_node` runs one safe processing node. One dry-run or node test
  runs at a time.
- Agent deploy (`deploy` scope: `pw_deploy_flow`, `pw_stop_flow`) behind
  the opt-in **agent deploy** level, changed with the password: `off`
  (default), `low` or `all`. `low` deploys, stops and applies an agent's
  own drafts only for flows of at most `external` risk that, compared with
  the version the admin last approved, raise no risk, change no destination
  or host, no
  secret reference, risk switch, typed credential, credentialed request,
  rate, global variable, published tool or node reading variables or
  credentialed responses; refusals name the kinds of change, never values.
  `all` allows any flow. Every agent deploy is audited and marked.
- Flow versions: every definition save is a numbered version (the newest
  50 plus the last one the admin approved are kept); approving a draft, an
  agent's deploy of its draft and a rollback each save one.
- What agents read back from drafts, validation, dry-runs and node tests
  never depends on how their text compares with a hidden value. The read
  views and tool results still hide secret values by comparison, so an
  agent whose own text comes back to it can confirm guesses of short or
  predictable secrets: the secrets list marks values under 16 characters
  (ADR-0021).
- `/mcp` serves protocol versions 2025-06-18, 2025-11-25 and 2026-07-28
  (stateless, JSON responses), refuses JSON-RPC batches and bounds what
  decoding a message costs.
- `piworker-mcp`, a stdio bridge for Claude Desktop (and for Claude Code
  without trusting the CA), pins PiWorker's CA and reads the token from a
  private file. Releases attach it for Linux amd64/arm64/ARMv7, macOS
  amd64/arm64 and Windows amd64.

### Agent access pages

- New **Agents** pages: tokens and grants (created with your password, with
  ready-to-paste set-up for Claude Code and for Claude Desktop on macOS/Linux
  and Windows), revoke one or all tokens, the agent deploy level (password;
  it says what `low` still allows without review, such as changing an
  approved flow's MQTT command, and that lowering the level undoes nothing),
  the approval queue with a badge and an app-wide notice, and the audit log
  with rejection totals. The pages show whether MCP is on.
- Text from agents and flows is shown as plain text with hidden and
  direction-changing characters escaped, and the web UI sends a
  Content-Security-Policy that allows only its own scripts.
- The editor's **Test tool** calls an Agent Tool node as an agent would; the
  canvas shows who started each run.
- **Drafts** tab with a badge: waiting, decided or all drafts, and a review
  page with the agent's note, the risk before and after, the alerts, the
  problems found, the node and connection changes (credentials hidden,
  destinations highlighted), a rename, the comparison with the version you
  last approved and the proposed flow. Approve (optionally deploying) or
  reject with a reason the agent reads.
- The agent deploy setting explains each level's risk, what an agent can
  guess at every level and how many secrets are short; the **Secrets**
  dialog marks secrets shorter than 16 characters.
- Flow cards and the editor header show **Deployed by agent** and
  **Changed by agent**. **History** lists a flow's versions and rolls back
  to the version you last approved. Saving or deploying a version that
  changed meanwhile offers to reload instead of overwriting it.
- The [operations guide](docs/operations.md#agents-when-one-misbehaves) has
  a runbook for a misbehaving agent: revoke, turn agent deploy off, roll
  back, read the audit, rotate secrets.

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
- CORS fails closed: an empty `cors_origins` means no cross-origin access,
  never `*`, and the default no longer allows `http://localhost:3000` and
  `http://localhost:8080` (the Vite dev proxy is same-origin). Both
  listeners cap the request line and headers at 64 KiB (431).
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
  release binaries of PiWorker are not provided (only of the `piworker-mcp`
  bridge). Docker is the alternative on those desktop OSes.
- Migration from the `v0.1.0-alpha.1` release of 2020 is **not supported**. The
  revival is a new program with a different data model: it does not read the
  old tasks or database, and the installer does not touch an old installation.
  Install it separately and recreate your automations as flows.
- The hardware nodes (GPIO Edge, GPIO Write, I2C Read, DS18B20 Temperature) are
  **experimental**: covered by automated tests, not yet validated on real
  hardware. The editor and the node reference mark them.
- The first publication is a **prerelease**. Prereleases are installed by tag
  (`piworkerctl install vX.Y.Z-…`, or the image tag); they never become
  GitHub's latest release or the image's `latest`.
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
- Editor saves and deploys are optimistic: `PUT /api/flows/{id}`,
  `POST /api/flows/{id}/deploy` and `PATCH /api/flows/{id}/toggle` take the
  `version` the client loaded and answer 409 with `currentVersion` when
  another version was saved since. Requests without a version (scripts)
  are not checked.
- Saving a flow's definition keeps its stored state (the restart intent, or
  **Failed**); only deploy, stop and the list switch change it. Earlier
  versions marked every saved flow stopped.
- Deploy refuses (400, naming node, setting and secret) a flow whose
  Telegram bot token, Notify URL or Database DSN names a secret that does
  not exist, and startup restoration marks such a flow **Failed**; earlier
  versions used an empty string.
- Secret references resolve when the node uses them, so a rotated secret
  applies without a redeploy (except to a Database or MQTT connection that
  is already open); earlier versions resolved them at deploy.
- Expression loops (`filter`, `map`, `reduce`, …) check their deadline in
  every flow, so an expression stops at its 5-second timeout instead of
  running on in the background; this costs about three times as much on
  tight loops. `repeat`, `concat` and `flatten` are held to the
  evaluation's budget of 10⁶ items or bytes before they run, and
  expressions over 16 KiB or nesting brackets more than 256 levels deep
  are refused at deploy and at startup restoration.
- `GET /api/secrets` answers `{"secrets": [{"name", "short"}],
  "shortBelow": 16}` instead of `{"names": […]}`; `short` marks values
  under 16 characters, never a value or length.

See the [README](README.md) for setup, the [operations guide](docs/operations.md)
for recovery, the [MCP design](docs/design/mcp.md) for the agent contract, and
[TODO](TODO.md) for explicitly deferred features.
