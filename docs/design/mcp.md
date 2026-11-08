# MCP integration and native HTTPS — design

Status: accepted (2026-11-08). This document is the implementation contract for
the MCP work. Sections marked **Contract** define package APIs that several
work streams depend on; change them only together with every consumer.

## 1. Goals and non-goals

PiWorker runs on a Raspberry Pi administered by one person on a local network.
MCP lets AI agents (Claude Code, Claude Desktop, other MCP clients) use it:

1. **Flows as tools (call).** The admin publishes deployed flows as typed MCP
   tools (`trigger-tool → … → action-reply`). Agents supply *data* only, validated
   against a closed JSON Schema subset; never graph or config.
2. **Observe (read).** Agents inspect flows, runs and system status, with
   default-deny redaction.
3. **Author (author).** Agents draft flows; the admin approves in the editor.
4. **Agent deploy (deploy) — opt-in risk.** Off by default. The admin may accept
   the risk of letting agents deploy/stop flows, at one of two levels.
5. **Native HTTPS by default** with a local CA generated on first start, so MCP
   (and the editor) are encrypted and the server identity can be verified.

Non-goals: OAuth / public exposure, Bearer tokens on `/api`, one tool per node
type, raw GPIO/shell/I2C tools, reading secrets or variables over MCP, the
legacy HTTP+SSE transport, `subscriptions/listen`, an LLM/agent node in flows,
an MCP client node (`action-mcp-call`, deferred to its own design).

## 2. Native HTTPS (local CA)

### Behaviour
- `tls.mode`: `auto` (default), `custom`, `off`.
  - `auto`: on first start generate a **local CA** (ECDSA P-256, 10 years,
    `CN=PiWorker Local CA <hostname>`, name constraints are not used) and a
    **server certificate** signed by it (ECDSA P-256, 397 days, capped at the
    CA's expiry; issuing is refused once the CA has expired, with the
    `-tls-regenerate-ca` hint; `Status.Warning` and `GET /api/tls` `warning`
    report it from 90 days before). Stored under
    the data directory: `tls/ca.crt`, `tls/ca.key` (0600), `tls/server.crt`,
    `tls/server.key` (0600). The data directory is the directory of `db`.
  - SANs: `localhost`, `127.0.0.1`, `::1`, `os.Hostname()`, `<hostname>.local`,
    every non-loopback interface IP, and `tls.hosts` (extra names/IPs).
  - The server certificate is re-issued automatically (hot, via
    `tls.Config.GetCertificate`) when it is within 30 days of expiry or when the
    current SAN set (re-computed every 10 minutes and at start) is not covered.
    Clients that trust the CA never notice. The CA is never rotated
    automatically; `piworker -tls-regenerate-ca` does it explicitly.
  - `custom`: `tls.cert_file` + `tls.key_file`, loaded at start (reload on
    SIGHUP).
  - `off`: plain HTTP (e.g. behind a reverse proxy). `/mcp` is then
    loopback-only unless `mcp.lan_access = true`.
- Listeners:
  - `addr` (default `:8443`) serves HTTPS when TLS is on, HTTP when off.
  - `http_addr` (default `:8080`, empty disables) serves plain HTTP that
    **308-redirects** everything to HTTPS, except `/webhooks/` (served directly
    when `webhooks_http = true`, the default, because many devices cannot trust
    a custom CA), `/piworker-ca.crt` and GET/HEAD `/api/health`.
- `GET /piworker-ca.crt` (public, both listeners): the CA certificate (PEM body
  served as `application/x-x509-ca-cert`, so Firefox, iOS and Android offer
  their CA-import flow; 404 without a local CA). `GET /api/tls` (authenticated): mode, CA SHA-256
  fingerprint, server certificate SANs and expiry.
- Session cookies are `Secure` automatically when TLS is on (the existing
  `session_secure` remains for `off` behind a proxy).
- Startup log and `piworkerctl install` print the HTTPS URL and the CA
  fingerprint. The editor's HTTP warning links to a "Trust this device" page
  (download CA, fingerprint, per-OS instructions, how to remove the trust).
  Whether a browser trusts the CA is not detectable from the page, so the page
  is always reachable from Settings as well.
- `addr` and `http_addr` resolving to the same port is a startup error. The
  installer, `piworker.example.toml` and compose files set `addr = ":8443"`,
  `http_addr = ":8080"`. `/api/health` is also served on the plain listener
  without redirect, so health checks need no CA.
- Recovery: `-tls=off` flag / `PIWORKER_TLS_MODE=off`; `piworkerctl tls off`.
  Local development (`go run`, Vite proxy) uses `-tls=off`.
- Docker: the data volume holds `tls/`; compose publishes 8443 and 8080.

### Contract — `internal/tlsca`
```go
type Options struct { Dir string; ExtraHosts []string; Now func() time.Time; Interfaces func() ([]net.IP, error); Hostname func() (string, error) }
type Manager struct { /* … */ }
func NewManager(opts Options) (*Manager, error)          // loads or creates CA + server cert
func (m *Manager) TLSConfig() *tls.Config                // GetCertificate with hot re-issue; MinVersion TLS1.2
func (m *Manager) CACertPEM() []byte
func (m *Manager) CAFingerprint() string                  // "SHA256:AB:CD:…"
func (m *Manager) Status() Status                         // SANs, NotAfter, fingerprint
func (m *Manager) Refresh() error                          // re-check SANs/expiry (called by a ticker)
func RegenerateCA(dir string) error
```

## 3. Phase 0 — prerequisites (no MCP)

- `Node.Enabled` defaults to `true` when absent in JSON.
- Config type errors are deploy errors with a path, e.g.
  `nodes[3].config.interval: expected number (milliseconds), got string "5s"`.
  `ConfigSchema` descriptions state units. Enums match exactly unless
  `CaseInsensitive`. `RuntimeManager.Restore` (startup) logs these issues and
  deploys anyway, so values saved before the check never stop a running flow.
  This leniency covers config typing only: Restore still enforces the tool risk
  analysis of §5, so a flow whose tools fail it is not restored until the admin
  fixes and redeploys it.
- **Host allowlist** for `/api`, `/mcp` and the webui: top-level `allowed_hosts`
  (TOML, `PIWORKER_ALLOWED_HOSTS`, `-allowed-hosts`), added to: `localhost`,
  hostname, `hostname.local`, current interface IPs (re-read lazily), and any
  IP-literal Host (rebinding needs a hostname). Port stripped, case-insensitive.
  403 on mismatch. Compose files set `PIWORKER_ALLOWED_HOSTS`.
- `action-http` resolves `{{secret.NAME}}` in headers and in the URL userinfo/
  query at request time (never in payload-derived text); `trigger-webhook`
  `token` is `Sensitive` and supports `{{secret.NAME}}`. When the URL or a
  header uses a secret, the URL must start with a literal scheme and host
  (deploy error otherwise; §5 extends this to static credentials), and
  cross-host redirects drop secret-bearing headers and the Referer.
- `action-command` rejects placeholder values starting with `-` unless
  `allowDashArgs: true`.
- `ConfigProperty` gains `Sensitive bool` and `AgentVisible bool` (default
  false = redacted for agents); `NodeTypeInfo` gains `Risk` (`safe`,
  `external`, `privileged`). Every registered type declares `Risk` (test).
  Classification: privileged = action-command, action-file, action-database,
  action-gpio, action-i2c; external = action-http, action-mqtt, action-email,
  action-telegram, action-notify; everything else safe.

## 4. Runtime: synchronous calls

### Contract — `internal/flow`
```go
type CallStatus string // "completed", "completed_no_reply", "error", "running", "expired", "approval_required", "rejected"
type CallResult struct {
    Status     CallStatus      `json:"status"`
    RunID      string          `json:"runId"`
    Result     json.RawMessage `json:"result,omitempty"`  // deep-copied, serialized
    Error      *CallError      `json:"error,omitempty"`
    DurationMs int64           `json:"durationMs"`
}
type CallError struct { NodeID, NodeType, Message string }
type CallRequest struct {
    TriggerNodeID string
    Args          map[string]any // already validated
    Actor         string         // "token:<name>", "editor", …
    Timeout       time.Duration
}
// Call starts a run on a trigger-tool node and waits for its reply, the end of
// the run, the timeout, or ctx cancellation (whichever first). On timeout it
// returns Status "running"; the run continues and Wait can be used later.
// The timeout counts from the start of Call, including the hand-off to the
// trigger. An error without a run ID means no run was started.
func (r *FlowRuntime) Call(ctx context.Context, req CallRequest) (CallResult, error)
// "Busy, retry later" refusals of Call (no run started); both wrap ErrBusy.
var ErrBusy, ErrTooManyCalls, ErrTriggerBusy error
// Wait returns the final result of a run started by Call (or "expired").
func (r *FlowRuntime) Wait(ctx context.Context, runID string) (CallResult, error)
// RunDone returns a channel closed when the run identified by runID finishes.
func (r *FlowRuntime) RunDone(runID string) <-chan struct{}
```
- The waiter registry lives on `r.ctx` (like `flowvars.WithFlowStore`), keyed by
  correlation ID. `ReplyFromContext(ctx) Replier` lets `action-reply` deliver:
  `Reply(correlationID string, payload any, isError bool) bool` (first wins,
  serialized deep copy inside the call).
- `runID` is generated server-side (UUID) and set as `correlationID`.
- Node errors in a called run are collected (`CallError`, redacted by the
  caller); `runDone` closes the waiter. A message of the run that is dropped
  before it executes (full node queue, unknown target, routing timeout) is
  recorded the same way (`message dropped at node X: …`): the call ends as
  `error`, not `completed_no_reply`, unless a reply already won.
- Waiter hard lifetime: `timeout + 10 min` from the call's start, then
  `expired`. `Stop` cancels waiters (`error`: "flow stopped") and frees them.
- **Results are handed over once.** A final result returned by `Call` is
  forgotten when the run ends; only results `Call` returned as `running` stay
  retrievable with `Wait`. Callers that need a result later (approvals,
  `pw_get_result`) keep the `CallResult` they received.
- Bounds: at most `DefaultMaxCallWaiters` (64) tracked calls per runtime (the
  uncollected result closest to expiry is evicted first; `ErrTooManyCalls`
  only when all belong to running runs); a reply larger than
  `DefaultMaxCallResultBytes` (256 KiB) is refused (the service truncates to
  `max_result_bytes` itself). `RunDone` also closes on `Stop` and on waiter
  expiry, so a stuck run cannot hold service slots forever.
- The request timeout counts from the start of `Call` and covers the hand-off
  to the trigger as well as the wait: a trigger that does not accept the
  message in time (its output stays full) fails the call with
  `ErrTriggerBusy`; no run is started and there is no run ID.
- `ErrTooManyCalls` and `ErrTriggerBusy` both wrap `flow.ErrBusy`; callers map
  `errors.Is(err, flow.ErrBusy)` to `busy` (retryable), never to an internal
  error.
- A trigger-tool with no active router edge (`targetCount == 0`) fails deploy.
- `flow_runs` gains an `actor` column, filled from the run's meta `actor`.

## 5. Tool nodes, schemas, risk

### `trigger-tool` (input)
Config: `toolName` (`^[A-Za-z0-9_.-]{1,128}$`, not starting with `pw_`, unique
across running flows → 409: `Start` claims the name in a process-wide registry,
like the webhook hub, and returns an error wrapping `node.ErrResourceConflict`
when it is taken; `Stop` releases it), `title`, `description` (≥ 20 chars),
`inputSchema` (JSON), `outputSchema` (optional JSON), `timeoutMs` (default
10000, max 30000), `confirm` (`none` | `client` | `admin`), `idempotent`
(bool), `maxConcurrent` (default 1; readOnly tools default 4), `cooldownMs`.
Implements `TriggerNode`; `Emit(ctx, msg) error` is a blocking, ctx-bounded send
(payload = validated args; meta `correlationID`, `toolName`, `actor`).

The two schemas are stored as config properties of the type `"json"` (a
node-config contract addition the editor renders as a JSON editor): a parsed
object, or text holding exactly one JSON object; absent, `null` or `""` means
unset. The parsed config is `node.ToolSpec`, read from the instance through
`node.ToolProvider`. Its `MaxConcurrent` is 0 when unset, so the read-only
default can be applied once the flow's risk is known
(`ToolSpec.EffectiveMaxConcurrent(readOnly)`). `Emit` refuses arguments outside
the schema with `*node.ToolArgumentsError` (the `[]toolschema.Issue`; matches
`node.ErrInvalidToolArguments`), and returns an error matching
`flow.ErrFlowNotRunning` when the trigger stopped after `Call`'s running check.

### `action-reply` (output)
`mode`: `result` | `error`. No-op when the run was not started by `Call`.

### Contract — `internal/toolschema`
Closed subset of JSON Schema 2020-12:
- root `type: object`, `additionalProperties: false` (forced), `properties`,
  `required`;
- property: single primitive `type` (`string`, `integer`, `number`,
  `boolean`), or `array` of primitives with `maxItems`, or `object` with the
  same rules recursively (max depth 3);
- string keywords: `enum`, `pattern` (must be anchored `^…$` and use only the
  syntax Go RE2 and ECMA-262 with the `u` flag share, since clients read it as
  ECMA-262: no inline flags, named groups, `\A`, `\z`, `\Q…\E`, POSIX classes,
  `\pL` without braces or other Go-only escapes), `minLength`,
  `maxLength` (forced ≤ 256 when absent), `format` (ignored);
- number keywords: `minimum`, `maximum`, `enum`;
- `title`, `description`, `default`, `examples` allowed;
- `x-piworker-allow` (array of probe strings, see below) allowed and stripped
  from the schema advertised by `Schema.JSON()`;
- everything else rejected (`$ref`, `$defs`, `anyOf`, `oneOf`, `allOf`, `not`,
  `if/then/else`, `patternProperties`, `dependent*`, `unevaluated*`, boolean
  schemas, untyped schemas, type arrays, `x-mcp-header`, …).
```go
type Schema struct { /* resolved */ }
func Compile(raw json.RawMessage) (*Schema, error)            // subset check + jsonschema-go Resolve
func (s *Schema) Validate(args map[string]any) []Issue          // Issue{Pointer, Message}
func (s *Schema) RequireStrictFor(paths []string) []Issue       // enum/anchored pattern for strings, min+max for numbers, probe corpus
func (s *Schema) JSON() json.RawMessage                         // normalized schema advertised to clients
```
Probe corpus for strict strings: `-x`, `--a=b`, `..`, `/`, ` `, `\n`, `$(`, `;`,
`|`, `` ` ``. A pattern accepting any probe is rejected unless the property sets
`"x-piworker-allow": ["…"]` listing the accepted probes (admin acknowledgement).
A probe stands for every value of its class, not just itself (`^[a-z].*$`
rejects `/` but accepts `a/b`): `-x` and `--a=b` are values starting with `-`
or `--`; `..` a `..` path segment; ` ` any non-line-break whitespace and `\n`
any line break (`\n`, `\r`, U+0085, U+2028, U+2029) anywhere; `/`, `$(` (`$`),
`;`, `|` and backtick that character anywhere. The check is a reachability
search over the pattern's compiled program, so it covers every accepted value.

### Risk analysis (`internal/flow/risk.go`)
- Per type from `NodeTypeInfo.Risk`; per instance escalation to `privileged`
  when a destination is dynamic (placeholder in scheme/host/broker/URL of
  http, mqtt, notify; `allowPrivate: true`); a dynamic destination combined
  with static credentials (headers/userinfo/password) is a deploy error.
- Taint through shared state for flows reachable from a `trigger-tool`:
  `set-var` must use `scope: flow`; reaching `action-mqtt` or `action-file`
  makes the tool `privileged`. Flow variables are read by every branch of the
  flow, so a tool that reaches a `set-var` also reaches every enabled node that
  reads flow variables (`get-var` with `scope: flow`, or config text naming
  `flowVars` or `$env`) and everything downstream of them: the risk, credential
  and `set-var` rules apply to those nodes too (the stateful-node rules below
  only to the tool's own branch).
- Stateful nodes reachable from `trigger-tool`: `process-aggregate` → error;
  `process-join` → error unless its key is `meta.correlationID`;
  `process-delay` with `delayMs > timeoutMs` → warning.
- Strings/numbers that reach a `privileged` node must satisfy
  `RequireStrictFor` (conservative: all args when the tool is privileged).
- Annotations (always explicit): `destructiveHint` = privileged;
  `openWorldHint` = privileged or external; `readOnlyHint` = only
  `action-reply`/`action-log`/`process-*` reachable and no `set-var`;
  `idempotentHint` = admin flag. Destructive or `confirm != none` tools carry
  `_meta["anthropic/requiresUserInteraction"] = true`.
```go
type ToolInfo struct {
    FlowID, TriggerNodeID, ToolName, Title, Description string
    Schema       *toolschema.Schema
    OutputSchema json.RawMessage
    Risk         node.Risk
    Annotations  Annotations
    Fingerprint  string // sha256(risk, annotations, inputSchema, confirm)
    Confirm      string
    Timeout      time.Duration
    MaxConcurrent int
    Cooldown     time.Duration
}
func (m *RuntimeManager) Tools() (version uint64, tools []ToolInfo) // snapshot, bumps on any deploy/undeploy
```

## 6. Service layer — `internal/service`
Shared by REST and MCP (thin adapters):
- `Tools.Call(ctx, actor Actor, toolRef, args) (flow.CallResult, error)`:
  permission + fingerprint check, schema validation (`toolschema`), slots
  (`maxConcurrent`, global `max_inflight`, per-token 2) held until
  `RunDone`, cooldown from run end (`busy` / `cooldown` tool errors; the
  runtime's `flow.ErrBusy` refusals, `ErrTooManyCalls` and `ErrTriggerBusy`,
  are `busy` too), confirm policy (`admin` → creates an approval, returns
  `approval_required`), result size cap (`max_result_bytes`, `truncated`), redaction of known secret
  values (literal, URL-encoded, base64) and URLs (`urlWithoutSecrets`).
- `Tools.Result(actor, runID)`: only the actor that started the run.
- Read views (phase 2), drafts/dry-run (phase 3), deploy policy (agent deploy).
- REST twin: `POST /api/flows/{id}/call/{nodeId}` (editor "Test tool").

## 7. Tokens

Table `api_tokens(id, name, prefix, secret_sha256, scopes, privileged,
grants_json, all_tools, expires_at, last_used_at, revoked_at, created_at)`.
- Format `pwk_<id>_<32 random bytes base32>`; lookup by id, constant-time hash
  compare. Shown once. Default expiry 90 days.
- Scopes: `call`, `read`, `author`, `deploy`. `author` can only be granted, and
  author tools are only exposed, when `mcp.author_enabled = true`; `deploy`
  can only be granted when agent deploy is enabled (§10). The `privileged`
  flag (default false) is needed to call privileged tools.
- Grants bind to `(flowID, triggerNodeID, fingerprint)`. A redeploy that
  changes the fingerprint (risk, annotations, input schema or confirm mode)
  suspends the grant until the admin re-confirms: any change, not only a higher
  risk or a wider schema, since whether a schema is wider cannot be decided
  reliably. `all_tools` never includes privileged tools. An approved
  `confirm: admin` call runs only if the tool still has the name and
  fingerprint recorded with the request (§9).
- Created only from the editor (cookie + CSRF) with **password step-up**;
  revoking needs no password (it only narrows access). Adding a grant or
  re-confirming a suspended or widened one needs step-up; suspend/remove do
  not. Grant changes are applied in one transaction. No token can manage
  tokens.
- `tokens_epoch` is bumped on every revoke and grant change; changing
  `agent_deploy` also bumps it so cached per-token MCP servers are rebuilt.
- In-memory cache of verified tokens; evicted on revoke; a `tokens_epoch`
  setting re-checked every 5 s covers revocations from other processes.
- Recovery-code redemption and password change revoke all tokens (password
  change: unless "keep agent tokens" is ticked), in the server process.
- API (cookie): `GET/POST /api/agent/tokens`, `DELETE /api/agent/tokens/{id}`,
  `POST /api/agent/tokens/{id}/grants` (re-confirm), `GET /api/agent/audit`,
  `GET/POST /api/agent/approvals[/{id}/approve|reject]`,
  `GET/PUT /api/agent/settings` (agent deploy level; PUT needs step-up),
  `GET /api/agent/tools` (the published tools, for grant pickers):
  ```json
  {"version": 7, "tools": [{"flowId": "…", "flowName": "…", "triggerNodeId": "…",
    "toolName": "garage_open", "title": "…", "description": "…",
    "risk": "privileged", "annotations": {"readOnlyHint": false,
    "destructiveHint": true, "idempotentHint": false, "openWorldHint": true},
    "fingerprint": "sha256:…", "confirm": "admin", "inputSchema": {…}}]}
  ```

## 8. `/mcp` endpoint — `internal/mcpserver`

- SDK: `github.com/modelcontextprotocol/go-sdk` pinned exactly.
  `mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, DisableLocalhostProtection: true, MaxRequestBodyBytes: …, PropagateRequestCancellation: true})`.
- Capabilities: tools and prompts with `listChanged: false`;
  `subscriptions/listen` rejected; `cacheScope: private`, short `ttlMs`.
- `getServer(r)` builds (and caches by token id + tools snapshot version +
  token epoch) a per-token `*mcp.Server` with only permitted tools, inside
  `recover()` so one bad tool is skipped.
- Middleware (in order): loopback check when TLS is off and `lan_access` is
  false → 403; Host allowlist; Origin must be allowlisted if present; write
  deadline extended to 35 s; own Bearer verifier (401 +
  `WWW-Authenticate: Bearer`, own failure limiter); per-token token bucket
  (30/min, burst 10); scope check.
- Mounted at `/mcp` without method restriction, before the webui catch-all;
  `/.well-known/*` returns JSON 404. Disabled → JSON 404.
- `mcp.enabled = true` with auth disabled is a startup error.
- Tools: dynamic tools + `pw_get_result` (call); `pw_list_flows`,
  `pw_get_flow`, `pw_list_runs`, `pw_get_run`, `pw_system_status`, prompt
  `diagnose_flow` (read); `pw_search_node_types`, `pw_get_node_type`,
  `pw_list_secret_names`, `pw_validate_flow`, `pw_test_node`, `pw_dry_run`,
  `pw_save_draft`, `pw_get_draft_status`, prompt `build_flow` (author);
  `pw_deploy_draft`, `pw_stop_flow`, `pw_start_flow` (deploy, policy-gated).
- Tool errors are `isError: true` results; protocol errors only for protocol
  problems. Validation errors list `{pointer, message}`.

## 9. Approvals (`confirm: admin`) and audit

- Table `agent_approvals(id, kind, token_id, flow_id, ref, tool_name,
  tool_fingerprint, args_json, summary, status, created_at, decided_at,
  decided_by, expires_at, result_json)`; kinds: `tool_call`, `draft_deploy`.
  Pending approvals expire (tool calls: 15 min).
- A `confirm: admin` call returns `{status:"approval_required", runId}`; the
  editor shows a queue (badge + page) with tool, args (text only), risk and
  token; approve runs the call and stores its `CallResult` on the approval
  (retrievable with `pw_get_result`; a call still running past its timeout
  stores `running`, then its final result); reject → `rejected`. The call runs
  only if the tool still has the name and fingerprint recorded with the
  request; otherwise nothing runs and the stored result is an error ("the tool
  changed since the request; ask again"). Arguments over 64 KiB cannot be
  queued (`invalid_arguments`).
- Revoking or expiring a token voids its pending approvals (closed as
  rejected, `decided_by` "token revoked"/"token expired"); approving one is
  refused. Pending approvals are capped per token and kind
  (`ErrTooManyPendingApprovals` → `busy`); tool-call arguments are deleted on
  expiry; decided approvals are kept 30 days.
- Table `mcp_audit(ts, token_id, token_name, tool, flow_id, run_id,
  args_sha256, args_preview, outcome, dur_ms)`; async batched writes; retention
  30 days by time; rejections (`busy`, cooldown, invalid args) stored as
  per-token per-minute counters (`mcp_rejections`); privileged calls never
  evicted early. The audit API pages by `id` (most recently recorded first).
- Agent-authored text is rendered as plain text only (never `marked`/`v-html`);
  the webui sends `Content-Security-Policy: script-src 'self'`.

## 10. Authoring and agent deploy

- Flows gain `version` (incremented on every save). Table `flow_drafts(id,
  flow_id, base_version, data, note, author_token_id, status, risk,
  issues_json, created_at, decided_at)`.
- `pw_save_draft{flowId?, baseVersion, flow | ops, note}`: `ops` = add/update/
  remove node, connect/disconnect; auto-layout for new nodes; stale
  `baseVersion` → conflict. The redaction sentinel `"<redacted>"` means "keep
  the base value"; any other literal in a `Sensitive` field is rejected (only
  `{{secret.NAME}}` allowed). Author scope cannot create/modify `trigger-tool`,
  `action-reply`, webhook token/path, `allowPrivate` or `allowDashArgs`.
- `flow.Lint(f, registry)` returns all issues `{path, code, severity,
  message}` (validation + config typing + risk); `pw_validate_flow` exposes it.
- `pw_dry_run`: instantiates the flow with `Registry.Derive` stubs for output
  nodes and triggers, isolated empty variable **and secret** stores
  (`RuntimeOption`s), no recorder; one at a time; 50 nodes / 10 s cap; returns
  per-node trace and `wouldExecute[]`.
- `pw_test_node`: processing nodes only, empty stores.
- Approval page: per-node type and diff, agent text marked as such, risk,
  alerts for recipient/destination changes (`chatId`, `to`, topic, broker,
  URL), dynamic destinations, secrets moved between fields, new hosts, and any
  opt-in risk switch turned on (`allowPrivate`, `allowDashArgs`).
- **Agent deploy (opt-in risk).** Setting `agent_deploy`: `off` (default),
  `low` (agent may deploy/start/stop flows and approve its own drafts when the
  resulting flow's risk is ≤ external, no recipient/destination changes vs the
  last admin-approved version, no new secrets references), `all` (any flow,
  including privileged). Changing it needs password step-up and shows the risk
  text; tokens need the `deploy` scope (only grantable when not `off`). Every
  agent deploy is audited, marked "deployed by agent" in the editor, and the
  admin can roll back to the last admin-approved version in one click.

## 11. Configuration

```toml
addr = ":8443"
http_addr = ":8080"
allowed_hosts = []
webhooks_http = true

[tls]
mode = "auto"          # auto | custom | off
hosts = []
cert_file = ""
key_file = ""

[mcp]
enabled = false
lan_access = false     # only relevant when tls.mode = "off"
max_inflight = 4
max_result_bytes = 16384
author_enabled = false
```
Environment: `PIWORKER_TLS_MODE`, `PIWORKER_HTTP_ADDR`, `PIWORKER_ALLOWED_HOSTS`,
`PIWORKER_MCP_ENABLED`, … (strict booleans). The agent deploy level lives in the
`settings` table (editor, step-up), not in the TOML.

## 12. Clients

Claude Code (trust the CA once via `NODE_EXTRA_CA_CERTS`):
```bash
export NODE_EXTRA_CA_CERTS=~/piworker-ca.crt
claude mcp add --transport http --scope user piworker https://raspberrypi.local:8443/mcp \
  --header "Authorization: Bearer pwk_…"
```
Claude Desktop: stdio bridge `piworker-mcp` (`cmd/piworker-mcp`, usage in its
README; pure Go, `CGO_ENABLED=0`, enforced by a test; `make
build-mcp-bridge-all` builds linux amd64/arm64/armv7, darwin amd64/arm64 and
windows amd64, attached to releases) with `--url`, `--token-file`, `--ca-file`
/ `--ca-fingerprint` pinning, `--timeout`.
- It is a protocol-version-agnostic JSON-RPC relay, not an SDK proxy: each
  stdin line is POSTed unchanged with the Bearer token, `Accept: application/json,
  text/event-stream` and `MCP-Protocol-Version` (from the request's `_meta`, else
  from the `initialize` answer; `Mcp-Method`/`Mcp-Name` from 2026-07-28), and
  JSON or SSE answers are written to stdout one per line. So tools, prompts and
  anything else relay unchanged. No session id, no GET stream, no batches.
- `--url` is https, or plain http to a loopback IP literal only; no
  credentials, query or fragment. The token comes only from `--token-file`
  (starts with `pwk_`, trimmed, BOM skipped; refused when group/others have
  any access on Unix, or when its DACL lets anyone but the user, SYSTEM and
  Administrators read or change it on Windows); `~` is expanded. Neither is
  ever logged or echoed, and a flag value that looks like a token is refused
  without being echoed.
- One trust anchor per connection, verified for the `--url` host: the pinned CA
  (file, filtered by the fingerprint when both are given) or the system roots.
  `internal/tlsca` presents [server certificate, CA] in auto mode, so a CA
  pinned by fingerprint alone is found in the handshake before the first
  request, and a presented root that does not match stops the bridge before
  any request; only a server presenting no root (e.g. custom mode with a
  leaf-only file) gets an unauthenticated GET of `/piworker-ca.crt`. Only a
  certificate with that SHA-256 is used. No HTTP proxy, no redirects. Errors
  never quote an unverified server (Go checks the host name before the
  signature): a certificate's names are listed only when its chain verifies
  against the trust anchor.
- Up to 8 HTTP requests at once, 64 queued, answered in completion order;
  `notifications/cancelled` aborts the matching POST (stateless servers cannot
  match a forwarded one) and drops its answer; nothing is retried. From
  2026-07-28 the aborted POST cancels the call on PiWorker
  (`PropagateRequestCancellation`); with earlier protocol versions go-sdk
  v1.8.0 lets the call run to its end, so a cancelled call may still take
  effect. HTTP
  401/403/404/429/5xx become JSON-RPC errors `-32000` with clear text and
  `data.httpStatus` (429: `data.retryAfter`); other JSON-RPC error bodies pass
  through. Limits: 4 MiB per stdin message, 16 MiB per answer, 2 min per
  request; at stdin EOF, in-flight requests get 10 s, then exit 0.

## 13. Testing

TDD throughout; `-race` for runtime and service. E2E in `main_mcp_e2e_test.go`
with a real `mcp.NewClient` over `StreamableClientTransport` against
`httptest.NewTLSServer`-style setup using the generated CA: filtered
`ListTools`, annotations, `CallTool` structured content, invalid args → isError,
401/403 cases, hidden tool not callable, foreign `runId` rejected, approval
flow, read redaction canaries (one canary per string/map config field of every
registered node type), drafts round-trip, agent-deploy policy levels. Golden
raw JSON-RPC tests for protocol versions 2025-06-18, 2025-11-25 and 2026-07-28.
Binary size measured for `build-arm64` and `build-arm` (`-s -w`).
