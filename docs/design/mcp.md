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
    a custom CA), `/piworker-ca.crt` and GET/HEAD `/api/health`. `/mcp` there
    is answered 400 with the HTTPS URL, never redirected: a client following
    the redirect would keep sending its Bearer token in clear text.
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
  values (literal, URL-encoded, base64) and, in error messages, of URLs past
  their host (`scheme://host/<redacted>`, the read views' rule: Telegram bot
  and webhook tokens sit in the path).
- `Tools.Result(actor, runID)`: only the actor that started the run.
- Read views (phase 2), `service.Reads`, for the editor and tokens with the
  `read` scope (others: `forbidden`); JSON with camelCase fields, bounded
  (each view's JSON at most 64 KiB besides the limits below), in a
  deterministic order:
  - `ListFlows`: the newest 200 flows that fit (`total`, `truncated`) with id, name,
    state (`running`, `stopped`, or `failed`: meant to run but not running),
    node count, tool names (at most 20, `toolCount`) and the latest run.
  - `GetFlow`: nodes (id, type, type name, enabled, position, type risk,
    config), connections and the running tools (risk, annotations,
    fingerprint, confirm, timeout); at most 200 nodes, 400 connections and
    100 tools, the structure first (nodes, connections, tools, each in
    order while they fit), then the node configs in node order while they
    fit (`configOmitted`; `truncated`). Config redaction is
    default-deny: a property is shown only when its schema marks it
    `AgentVisible` and not `Sensitive`; every other present value,
    undeclared properties, every property of an unknown type, and whole
    nested values under a hidden property become `"<redacted>"` (the
    sentinel drafts read as "keep the base value", §10). A shown value is
    cut to 8 KiB.
  - `ListRuns(flowId, limit ≤ 100, before)` newest first (`nextBefore` is the
    next page's cursor; a page that would exceed 64 KiB ends early) and
    `GetRun(flowId, runId)`: status, actor
    (`trigger`, `editor`, or `agent` with the token's name), timings,
    counts, and the node events in order (node, type, phase, duration,
    error ≤ 2 KiB, the run ≤ 64 KiB). `GetRun` reads a bounded part of a
    run: its events 64 at a time until the budget is spent, each error cut
    to 8 Ki characters in storage (a cut error also loses, after redaction,
    as many bytes as the longest redacted form, so a secret straddling the
    cut is never half shown), and the total from a count. The recorder stores no payloads, so
    there are no payload previews; events never carry node config. A run is
    read only through its own flow (`run_not_found`).
  - `SystemStatus`: version, platform, uptime, running/total flows, tools,
    `max_inflight`, `max_result_bytes`, the agent deploy level and, best
    effort on Linux, free disk of the data directory, memory and CPU
    temperature (omitted when unknown); no host names, addresses or paths.
  - `DiagnoseFlowPrompt` / `DiagnoseFlow`: the `diagnose_flow` prompt text,
    which points to the read tools and inlines only the flow's quoted ID and
    name.
  Known secret values (literal, URL-encoded, base64, JSON- or Go-escaped)
  are redacted from every view, as are credentials typed literally into a
  flow's `Sensitive` properties (in every view of that flow: its summary in
  `ListFlows`, `GetFlow`, `ListRuns`, `GetRun` and the prompt). URLs in
  recorded errors and flow names are cut after their host
  (`scheme://host/<redacted>`): a credential may sit in the userinfo, the
  query or the path (Telegram bot, Discord and Slack webhook URLs). Recorded
  errors also lose what `GetFlow` hides of the run's flow: the values of its
  hidden settings, split around `{{…}}` templates, whole, as words and as the
  path segments, query values and userinfo of their URLs (parts under 8
  bytes are kept, but a URL password of 4). An unknown flow is
  `flow_not_found`.
- Drafts/dry-run (phase 3), deploy policy (agent deploy).
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
  `POST /api/agent/tokens/revoke-all` (no password; one transaction that
  also voids pending approvals and bumps `tokens_epoch`; returns how many
  active, i.e. unexpired, tokens it revoked),
  `POST /api/agent/tokens/{id}/grants` (re-confirm), `GET /api/agent/audit`,
  `GET/POST /api/agent/approvals[/{id}/approve|reject]`,
  `GET /api/agent/approvals/pending-count` (the editor badge's poll: a
  read-only count of pending, unexpired approvals, authenticated without
  extending the session's idle expiry and without a write transaction),
  `GET/PUT /api/agent/settings` (agent deploy level; PUT needs step-up; also
  reports the read-only TOML settings `authorEnabled`, `mcpEnabled` and
  `lanAccess`, so the editor can say whether agents can connect),
  `GET /api/agent/audit/rejections` (rejection totals, §9),
  `GET /api/agent/tools` (the published tools, for grant pickers):
  ```json
  {"version": 7, "tools": [{"flowId": "…", "flowName": "…", "triggerNodeId": "…",
    "toolName": "garage_open", "title": "…", "description": "…",
    "risk": "privileged", "annotations": {"readOnlyHint": false,
    "destructiveHint": true, "idempotentHint": false, "openWorldHint": true},
    "fingerprint": "sha256:…", "confirm": "admin", "inputSchema": {…}}]}
  ```
- Requests the editor starts by itself, not on the admin's behalf (the
  Approvals page reloading its list when the badge count changes, which an
  agent can cause at will), carry `X-PiWorker-Background: 1`; any guarded
  path then checks the session without extending its idle expiry. The header
  can only shorten a session, so it needs no allowlist (CORS allows it).

## 8. `/mcp` endpoint — `internal/mcpserver`

- SDK: `github.com/modelcontextprotocol/go-sdk` pinned exactly (v1.8.0).
  `mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, DisableLocalhostProtection: true, MaxRequestBodyBytes: 1 MiB, PropagateRequestCancellation: true})`
  (1 MiB: arguments are at most 64 KiB in practice; the SDK default of 4 MiB
  is too much for a Pi under a flood of bodies).
- Protocol versions: 2026-07-28 (sessionless, `server/discover`),
  2025-11-25 and 2025-06-18 (`initialize`); others are refused. The legacy
  handshake asked for 2026-07-28 settles on 2025-11-25 (SDK behaviour). Every
  request but `initialize` must carry `Mcp-Protocol-Version` (the SDK would
  serve a request without it as 2025-03-26) and JSON-RPC batches are refused
  (none of these versions allows them; a batch would also carry many calls
  past the per-token rate limit): both 400, JSON-RPC `-32600`, before the
  SDK sees the request.
- Capabilities: tools and prompts with `listChanged: false` (always
  advertised, even when empty); `subscriptions/listen` is answered with
  JSON-RPC `-32601` before the SDK sees it; list and discover results carry
  `cacheScope: private`, `ttlMs: 5000`.
- `getServer(r)` builds a per-token `*mcp.Server` holding only what the token
  may use now: the flow tools `service.Tools.List(actor)` returns (exactly
  Call's permission rules, without suspending changed grants), plus the
  built-in tools and prompts of its scopes that the settings serve now: the
  `author` scope only with `mcp.author_enabled` (`Options.AuthorEnabled`),
  the `deploy` scope only while `agent_deploy` is `low` or `all`
  (`Options.DeployLevel`, the store's `AgentDeployLevel`; read only for
  tokens with the deploy scope, once per tokens epoch, which a level change
  bumps in its own transaction; a failed read counts as `off`, is logged and
  is read again on the next request). A deploy-scope tool or prompt
  registered without `DeployLevel` would never be listed, so `New` refuses
  it; `New` logs once what is registered (author and deploy tools and
  prompts, `authorEnabled`, whether the level is read) and warns when
  `mcp.author_enabled` or the level reader has nothing to serve. A
  built-in tool or prompt of a scope that is not served is, to the token, a
  name that does not exist (neither listed nor callable); the services check
  the same settings on every call. Each tool is built inside
  `recover()` so one bad tool is skipped and logged; a skipped tool is not
  listed and its calls are answered `not_found` without running it. Servers are cached by
  (token id, tools snapshot version, tokens epoch, the served gates, hash of
  the token's scopes, flags and grants), one per token, at most 32 (least
  recently used dropped first), so a redeploy, a grant change or an
  `agent_deploy` change shows on the next request after the verifier reads
  the new epoch (at most 5 s); a revoked token gets 401.
- Flow tools use the low-level `Server.AddTool` with the canonical input
  schema (`toolschema` `JSON()`), title, description, explicit annotations
  (all four hints), the output schema when set and
  `_meta["anthropic/requiresUserInteraction"]` when required. Arguments are
  validated by `service.Tools.Call`, not by the SDK; arguments that are not a
  JSON object (which the service cannot take) are refused by the endpoint as
  `invalid_arguments` and counted in `mcp_rejections` like the service's own
  refusals. Calls of names the server does not hold go to the service as
  well, so a tool the token has no grant for (including a privileged tool
  covered only by `all_tools`) gets exactly the `not_found` result (and
  audit) of an unknown name. A hidden tool the token does hold a grant for
  gets Call's answer instead: `reconfirm_required` while its fingerprint
  differs from the grant's or the grant is suspended, `forbidden` when it is
  privileged and the token lacks the `privileged` flag (the admin granted it
  to that token, so its existence is no secret and the answer says what to
  do). Names outside the tool-name syntax (1 to 128 of `[A-Za-z0-9_.-]`)
  and tools the server could not build are answered `not_found` by the
  endpoint itself (not audited), shortened.
- Built-in tools register as `StaticTool{Name (pw_…), Title, Description,
  InputSchema (closed toolschema subset) | RawInput, OutputSchema,
  Annotations, Scope, Handler(ctx, service.Actor, args)}`; prompts as
  `StaticPrompt{Prompt, Scope, Handler}`. Tool arguments are validated before
  the handler runs; a prompt handler checks its own. Flow-shaped arguments
  (nested objects, free-form node settings, any JSON payload) do not fit the
  closed subset: `RawInput{Schema, MaxBytes, Validate}` is the escape hatch,
  used by the four flow-shaped author tools and `pw_list_node_types` only
  (every other built-in tool stays on the closed subset). `Schema` (a JSON
  Schema of type object, at most 64 KiB, resolvable) is advertised as
  written; the endpoint bounds the arguments' JSON as sent (`MaxBytes`, at
  most the 1 MiB body limit), decodes them as every tool's (an object, no
  duplicate names, valid UTF-8) and runs the Go `Validate`, which must
  enforce at least what `Schema` says. The author tools build both from one
  description (`argSchema`: `JSON()` advertised, `check()` enforced): one
  type per schema or none (any value), closed objects unless free-form,
  string, integer and array bounds, every problem reported with its
  argument's pointer in toolschema's wording, at most 64, never quoting a
  value. Refusals of either contract are `invalid_arguments`, counted in
  `mcp_rejections`. The endpoint is the one owner of the counts of
  built-in tools: the services count none of their refusals, so a
  handler's `invalid_arguments` refusal (`*service.ToolError`: a sensitive
  literal, an unresolved `"<redacted>"`, ops that cannot be applied, H2
  blocking Lint errors, a deploy refused by validation) and its `busy` one
  (the sandbox slot of `pw_dry_run` and `pw_test_node`) are counted there
  too, once; other refusals (`conflict`, `forbidden`, `not_found`) are not
  rejections. (Flow tools' refusals are counted by the tool service, which
  answers them.)
  A successful result's structured content is checked against the tool's
  output schema before it is sent (a mismatch is logged and answered
  `failed`: clients refuse a result that does not match), and a destructive
  built-in tool carries `_meta["anthropic/requiresUserInteraction"]` like a
  flow tool. A prompt's refusal (`*service.ToolError`) is
  JSON-RPC `-32602` with its message, any other failure `-32603` without
  details (logged).
- Read tools (`mcpserver.ReadTools(*service.Reads)`, scope read; main wires
  them): `pw_list_flows{}`, `pw_get_flow{flowId}`,
  `pw_list_runs{flowId, limit?, before?}` (`limit` 1–100, default 20;
  `before` is the previous page's `nextBefore`), `pw_get_run{flowId,
  runId}` and `pw_system_status{}`. Ids are 1 to 256 characters without
  control characters (`^[^\x00-\x1f\x7f-\x9f]{1,256}$`): PiWorker generates
  UUIDs, but older versions let clients choose a flow's id (any text), the
  views list ids up to 256 bytes, and an id only reaches parameterised
  queries. Annotations: read-only,
  idempotent, not destructive, closed world. Each result is the §6 view as
  structured content plus its JSON as the text block; the output schemas
  describe the views (open objects, every always-present field required,
  no formats or type unions, kept in step with the Go types by a test).
  The views' refusals are `isError` results (`flow_not_found`,
  `run_not_found`; `forbidden` cannot occur, the tools are only served to
  the read scope). The views are bounded by their own limits (§6: each
  view's JSON is at most 64 KiB, so a result, which carries it twice, stays
  within what clients accept from a tool), not by `max_result_bytes`, which
  caps flow replies. Every read of an agent is audited like a tool call
  (`service.Reads`, `ReadsOptions.Audit`): an `mcp_audit` row with the
  token, the tool name (`prompt:diagnose_flow` for the prompt), the flow
  and run it named, its arguments (secrets redacted), the outcome
  (`completed`, `flow_not_found`, `run_not_found`, `forbidden`, `failed`)
  and its duration; arguments refused by a built-in tool's schema are
  counted in `mcp_rejections` (`invalid_arguments`). `pw_get_result` is not
  audited again: it returns the result of a call of the same token, which
  was.
  Prompt `diagnose_flow{flowId}` (scope read, `mcpserver.ReadPrompts`): one
  user message with the §6 text; a missing, malformed or extra argument and
  an unknown flow are `-32602`.
- Middleware (in order): loopback check when TLS is off and `lan_access` is
  false → 403; Host allowlist (also router-wide); Origin, if present, must be
  this server (scheme, host and port; both schemes without TLS) with an
  allowed host, or an allowed origin (`cors_origins`) → 403; write deadline
  extended to 35 s; own Bearer verifier (cookies and query tokens ignored;
  401 + `WWW-Authenticate: Bearer realm="piworker"` and a JSON body; 503 when
  the token store cannot be read; tokens never logged) with its own
  per-client failure limiter (10 failures a minute per IP, IPv6 per /64, then
  429 + `Retry-After`): every token lookup in the store reserves a unit of the
  client's budget first and gives it back unless the token was bad, so
  concurrent guesses cannot all pass; tokens the verifier already holds in its
  cache (`VerifyCached`, no lookup) are served even while their client is
  limited, so a client sharing the address cannot lock out known agents;
  per-token token bucket (30/min, burst 10 → 429 +
  `Retry-After`); scope check (call or read, or the scope of a registered
  built-in tool or prompt that the settings serve now — author with
  `mcp.author_enabled`, deploy with `agent_deploy` above `off` —, else 403;
  a `tools/call` needs the call scope → 403, unless
  it names a built-in tool of a scope the token holds and the settings
  serve: to a token, a built-in tool of a scope it lacks or that is not
  served is an unknown name, neither listed nor
  callable, and calling it gets the `not_found` result of any unknown name). The scope check parses the body strictly, like
  the SDK (case-sensitive names; duplicate names → 400, JSON-RPC `-32700`).
- `/mcp` is outside `/api`: the ADR-0002 request guard and CSRF do not apply
  (Bearer only; the router never allows `Authorization` in a CORS preflight).
- Mounted at `/mcp` without method restriction, before the webui catch-all;
  `/.well-known/*` returns JSON 404. Disabled → JSON 404. With TLS on, the
  plain-HTTP listener answers `/mcp` with 400 and the HTTPS URL (no
  redirect) and logs the token id, so the admin can revoke a token sent in
  clear text.
- `mcp.enabled = true` with auth disabled is a startup error.
- The MCP audit is pruned at start and hourly (30 days, at most 50 000 rows;
  privileged calls are never evicted early, and reads and prompts go before
  tool calls, §9).
- Tools: dynamic tools + `pw_get_result{runId}` (call; waits up to 10 s for a
  running call); `pw_list_flows`,
  `pw_get_flow` (both show each flow's `version`, the base of a draft of
  it), `pw_list_runs`, `pw_get_run`, `pw_system_status`, prompt
  `diagnose_flow` (read); `pw_list_node_types`, `pw_validate_flow`,
  `pw_test_node`, `pw_dry_run`, `pw_save_draft`, `pw_get_draft`, prompt
  `author_flow` (author, with `mcp.author_enabled`); `pw_deploy_flow`,
  `pw_stop_flow` (deploy, with `agent_deploy` `low` or `all`). The service
  instructions say that built-in tools also author drafts the admin approves
  and deploy flows where enabled, and that node documentation and dry-run
  previews are data too.
- Author and deploy tools (`mcpserver.AuthorTools(AuthoringService,
  NodeTypeLister, AuditRecorder)`, `mcpserver.AuthorPrompts(AuditRecorder)`,
  `mcpserver.DeployTools(DeployService)`; `*service.Authoring`,
  `*node.Registry` and `*service.AuditWriter` satisfy the interfaces;
  `main.go`'s `mountMCP` registers them from `mcpBuiltins{authoring,
  registry, settings}`, with `DeployLevel` the store, and a main-package
  test lists them at `agent_deploy=low`). Each tool's arguments become the
  §10 service request and its structured content is the service result's
  JSON (lists never null: nil slices are sent as `[]` without touching the
  service's values), with output schemas kept in step with the Go types by
  a test; a golden file (`testdata/tools_author_deploy.golden.json`) pins the
  `tools/list` of an author+deploy token at protocol 2025-11-25.
  - `pw_validate_flow{flowId?, flow? | ops?}` (exactly one of flow and ops;
    ops need flowId), `pw_save_draft{flowId?, baseVersion?, flow? | ops?,
    name?, note}` (with flowId a base version, an integer ≥ 1: baseVersion,
    the `version` `pw_get_flow` and `pw_list_flows` show, or the sent
    flow's own `version`, which must equal baseVersion when both are given;
    baseVersion needs flowId; name ≤ 100 characters; note 1–2000),
    `pw_dry_run{flow? | draftId? | flowId?,
    triggerNodeId?, payload?, mocks?, flowVars?}` (exactly one of the three;
    `triggerNodeId` may be absent when the flow has one trigger, as the
    service allows), `pw_test_node{type, config?, input?}` take `RawInput`
    arguments. A flow is closed at every level: `{name?, description?,
    nodes (required, ≤ 200; a dry-run ≤ 50), connections? (≤ 400; a dry-run
    ≤ 200), variables?}`, nodes `{id, type, enabled?, position?{x, y},
    config? (free-form), category?, inputs?, outputs?}`, connections `{id,
    sourceNode, sourcePort, targetNode, targetPort}`. The informational
    fields of the read views and of exported flows (`id`, `state`,
    `version`, `createdAt`, `updatedAt`, `tools`, `totalNodes`,
    `totalConnections`; a node's `name`, `risk`) are accepted, so a flow
    read can be sent back, and dropped before the service sees the flow
    (`version` is pw_save_draft's base, above); a flow's name may be as long
    as the read views show one (256 characters; a draft's 100-character rule
    is the service's). What a view left out, cut or partly hid is refused,
    because sent back it would replace the stored values: `truncated: true`,
    a node's `configOmitted: true`, a setting (or a string or key inside one)
    that is `"<truncated>"` or ends with `"…<truncated>"`, and a
    `"<redacted>"` that is not a whole setting (inside a value, a key, the
    flow's name or description); the settings add_node and update_node set
    are checked the same way. A whole setting `"<redacted>"` keeps the
    stored value with flowId (the service resolves it; without flowId it
    reports `unresolved_redaction`); in pw_dry_run's inline flow, which has
    no stored values, it is refused (dry-run by flowId or draftId instead),
    and the node settings' description says so for each tool. Ops (≤ 200) `{op, …}` take only their own
    fields (a field of another op is refused, not ignored): `add_node{node{id?
    (1–64 of [A-Za-z0-9_-]), type, config?, enabled?, position?}}`,
    `update_node{nodeId, config? | enabled? | position?}` (at least one),
    `remove_node{nodeId}`, `connect{sourceNode, sourcePort, targetNode,
    targetPort}`, `disconnect{connectionId}` or the four endpoints, not both;
    the add_node id syntax is `flow.OpNodeIDPattern`, advertised as it is.
    The ids a whole flow gives are those the editor, the server and add_node
    generate (`node_<time>_<n>`, `node_<uuid>`, `edge_<source>_<target>_<time>`,
    uuids) and dotted or colon-separated ones: `[A-Za-z0-9_.:-]`, nodes and
    types 1–128, connections 1–256, ports 1–64, so no id hides in an issue
    path (`/nodes/a/config/url`), breaks the editor's selectors (quotes) or
    reads as other text (spaces, control and format characters such as bidi
    overrides). Ids that name stored things (`flowId`, `draftId`, ops'
    `nodeId`, endpoints and `connectionId`) are 1 to 256 characters without
    control characters, so a stored flow with older ids is changed with ops.
    A refusal lists at most 64 problems (schema and the tools' own checks
    alike) and says more were left out. Bounds on the
    arguments' JSON: 576 KiB (validate, save draft: a 512 KiB draft and
    room), 768 KiB (dry-run), 144 KiB (node test).
  - `pw_get_draft{draftId}`, `pw_deploy_flow{flowId, draftId?}` and
    `pw_stop_flow{flowId}` are on the closed subset.
  - `pw_list_node_types{category?, type?}` reads the registry only (never a
    flow or a secret): types ordered by category (input, processing,
    output) and id, each with name, category, risk (an unrated type reads
    privileged), description, ports, config schema (type, title,
    description, default, enum, bounds, unit, and the markers `sensitive`,
    `agentVisible`, `destination`, `riskSwitch`, `operation`, `rate`,
    present when true) and documentation cut to 8 KiB; the list's JSON stays within 64
    KiB: every type's structure first, in order while it fits, then the
    documentation in order while it fits (`documentationOmitted`),
    `truncated` when anything was left out or cut, `total` the matching
    types. An unknown `type` is `invalid_arguments` (counted). It runs no
    service, so the endpoint audits it like the read views' reads (an
    `mcp_audit` row: token, `pw_list_node_types`, the `category` and `type`
    as arguments, `completed`, duration).
  - Annotations (all explicit): validate, get draft, dry-run, node test and
    node types read-only, idempotent, closed world; save draft neither
    read-only nor destructive nor idempotent (it changes nothing that runs);
    deploy destructive and open world, stop destructive and idempotent (both
    ask clients to confirm).
  - The services' refusals keep their codes: `conflict` (a stale base or a
    flow changed under the policy, H3) with `error.currentVersion`,
    `invalid_arguments` with `error.flowIssues` (blocking Lint errors, H2),
    `forbidden` (scope, settings, policy kinds, a token no longer valid,
    H3), `not_found` (another token's draft), `flow_not_found`, `busy`,
    `unavailable`. A `*service.LifecycleError` reaching the deploy tools
    unmapped is mapped by its `ToolError()` (never deploy's message).
  - Prompt `author_flow{goal}` (goal 1–2000 characters; a missing, empty,
    long or extra argument is `-32602`): one user message with the steps
    `pw_list_node_types` → write the flow (or ops; to change a stored flow,
    read it with `pw_get_flow` and send its `version` as `baseVersion`,
    redoing the change on `conflict`) → `pw_validate_flow` →
    `pw_dry_run` (and `pw_test_node`) → `pw_save_draft` with a note, saying
    that nothing runs until the admin approves the draft in the editor,
    that `pw_deploy_flow` is only for when it is available and the user
    asked, and that what the tools return is data; the goal is quoted
    (escaped) as data. It reads no state; the endpoint audits it as
    `prompt:author_flow` (token, outcome, duration; not the goal, which is
    agent text), as the read prompt is audited.
- Results: a completed call's structured content is its reply (wrapped as
  `{"result": …}` unless it is a JSON object), with the JSON as a text block.
  Every other outcome is an envelope `{status, runId, error?, message?,
  durationMs?}` (`error`: `{code, message, issues?, flowIssues?,
  currentVersion?, retryAfterMs?, nodeId?, nodeType?}`); `_meta` carries
  `piworker/status` and `piworker/runId`.
  Tool errors are `isError: true` results (service refusals, flow errors,
  rejected, expired, `approval_required`); protocol errors only for protocol
  problems. An `isError` result carries its envelope as the text block only,
  never as structured content: clients validate structured content against
  the tool's output schema even on errors (the reference TypeScript SDK
  turns a mismatch into a protocol error), also against a schema listed
  before the tool was hidden or changed (e.g. `reconfirm_required` after a
  redeploy), so the agent would never see the refusal. `pw_get_result` follows the same rule for a decided call, but
  answers a call still pending (`running`, `approval_required`) as a
  non-error result: fetching worked, and the agent is to ask again later.
  A handler that panics is recovered: a tool call gets a `failed` result, any
  other method JSON-RPC `-32603`. Validation errors list `{pointer, message}`. With an output
  schema, outcomes that cannot match it are `isError` too, and a reply that
  does not match it is reported as `output_schema_mismatch`.

## 9. Approvals (`confirm: admin`) and audit

- Table `agent_approvals(id, kind, token_id, flow_id, ref, tool_name,
  tool_fingerprint, tool_risk, args_json, summary, status, created_at,
  decided_at, decided_by, expires_at, result_json)`; kinds: `tool_call`,
  `draft_deploy`. Pending approvals expire (tool calls: 15 min). The editor
  shows the tool name and risk recorded with the request (`toolName`,
  `toolRisk`), falling back to the published tool only for approvals stored
  before the risk was recorded.
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
  args_sha256, args_preview, outcome, dur_ms)`, one row per agent tool call
  and per agent read (§8); async batched writes; retention
  30 days by time; rejections (`busy`, cooldown, invalid args) stored as
  per-token per-minute counters (`mcp_rejections`); privileged calls (and
  deploys of privileged flows, refused ones included) never evicted early.
  The row cap evicts as many rows as lie beyond the newest 50 000 (never
  privileged ones), reads and prompts first, oldest first whatever their age
  (`storage.IsMCPAuditRead`: `pw_list_flows`, `pw_get_flow`, `pw_list_runs`,
  `pw_get_run`, `pw_system_status`, `pw_get_result`, `pw_list_node_types`,
  `pw_validate_flow`, `pw_get_draft`, `pw_dry_run`, `pw_test_node` and
  `prompt:*`), then the oldest tool calls and actions (saving a draft,
  deploying, stopping), so a stream of reads cannot push calls out of the
  audit. The audit API pages by `id` (most recently recorded first);
  `GET /api/agent/audit/rejections?tokenId&since&until` sums the counters per
  token and reason in SQL (at most 500 groups, largest first, `truncated`
  when more matched).
- Agent-authored text is rendered as plain text only (never `marked`/`v-html`);
  every webui response sends `Content-Security-Policy` (`webui.ContentSecurityPolicy`:
  `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline';
  img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none';
  base-uri 'self'; form-action 'self'; frame-ancestors 'none'`). The built
  `index.html` has no inline script (tested).

## 10. Authoring and agent deploy

- **Flow versions** (`internal/storage/flow_versions.go`). `flows.version`
  (`flow.Flow.Version`, JSON `version`, server-controlled: client and agent
  values are ignored) counts definition changes. `CreateFlow` stores version
  1, and every definition save stores the next version in
  `flow_versions(flow_id, version, data, actor_kind, token_id, token_name,
  admin_approved, note, draft_id, created_at)` in the same transaction. The
  migration defaults the column to 1 and seeds every existing flow as
  version 1 (editor, admin-approved). State changes (deploy, stop, toggle, a
  failed restore: `UpdateFlowState`) are not versions, and a definition save
  is not a state change: `UpdateFlowVersioned` keeps the stored state
  (restart intent) and creation time and stamps the update time, whatever
  the flow it is given says; version rows are stored and read without a
  state, so a draft, a version or a rollback can never schedule a flow for
  startup or drop its restart intent. `UpdateFlow(f)` is an editor save
  (admin-approved); `UpdateFlowVersioned(f, expected, VersionMeta)` checks
  the expected version (0: no check; stale: `ErrVersionConflict`, as
  `*VersionConflictError{Current}`) and records an agent or draft version
  (an agent version is never admin-approved); `CreateFlowVersioned` records
  the meta of version 1 and always creates the flow inactive (`CreateFlow`
  keeps the state it is given; the API creates flows inactive). A
  `VersionMeta.DraftID` is applied in the save's own transaction: the draft
  must exist (for an agent, be its own), be pending, belong to that flow (or
  to a new flow) and be based on the version being replaced, and an agent's
  token must be unrevoked; it is then closed as approved with the new
  version (a new flow's draft also records the created flow's ID as its
  `flow_id`). Otherwise nothing is saved (`ErrFlowDraftNotFound`,
  `ErrDraftNotPending`, `ErrInvalidFlowDraft`, `*VersionConflictError`,
  `ErrDraftTokenRevoked`). Revocations void drafts in their own
  transactions, so a voided draft never becomes a version. Retention: the newest
  50 versions plus always the newest admin-approved one, pruned in the
  insert's transaction. Reads: `GetFlowVersion`, `ListFlowVersions` (newest
  first, metadata only), `LastAdminApprovedVersion` (nil when none).
  `flow_deployments` holds the last deployment marker (`RecordDeployment`,
  `GetDeployment`). Deleting a flow deletes its versions and marker and
  voids its pending drafts.
- **Editor saves are optimistic.** `GET /api/flows` items and `GET
  /api/flows/{id}` carry `version`. `PUT /api/flows/{id}` with the `version`
  the editor loaded answers 409 `{"success":false,"error":…,"data":
  {"currentVersion":N}}` when another version was saved since (an approved
  draft, another tab); without one there is no check (scripts). The editor
  sends the loaded version and on that 409 offers Reload (discarding its
  changes) or Cancel (keeping them on screen, unsaved): never a silent
  overwrite. Deploying is checked the same way: `POST
  /api/flows/{id}/deploy` takes an optional `{"version": N}` (and `PATCH
  …/toggle` an optional `version`); turning a flow on with a stale version
  answers the same 409 under the lifecycle lock, before anything runs. The
  editor's deploy sends the version it shows, so nobody deploys a definition
  saved since (an approved draft, an agent's version, a rollback) without
  seeing it.
- **Drafts** (`internal/storage/flow_drafts.go`): `flow_drafts(id, seq,
  flow_id ('' = new flow), base_version (0 = new), data (the resolved
  proposed flow), note, author_token_id, author_token_name, status
  pending|approved|rejected|void, risk, issues_json, alerts_json, reason,
  created_at, decided_at, applied_version, applied_by)`. `applied_by` is
  who applied an approved draft: `editor` (the admin approved it) or
  `agent` (its token deployed it at `agent_deploy` low or all, with no
  review), set by the save that closes it from `VersionMeta.ActorKind`;
  the migration adding it attributes an older approved draft to the actor
  of the version that names it (the admin when none does).
  `CreateFlowDraft` checks in its
  transaction that the author token is not revoked, that it has fewer than
  20 pending drafts (`ErrDraftLimit`) and that an existing flow is still at
  the base version; it drops `state`, `version`, `createdAt` and `updatedAt`
  from the data (server-owned) and prunes decided drafts older than 30
  days and, newest kept, those beyond `MaxDecidedDraftsPerToken` (50) of
  the author's and `MaxDecidedDrafts` (200) in all, so an agent looping on
  save and deploy (each a decided draft of up to about 2 MiB) cannot fill
  the disk within the 30 days.
  `ListFlowDrafts(filter{Status: pending, decided, one status or all;
  TokenID; FlowID}, limit, beforeID)` pages newest first without the data
  (the proposed `name` is read from it); `DecideFlowDraft` closes only
  pending drafts (`ErrDraftNotPending`; approving a draft already approved
  with the same version, as a save applying it does, is a no-op);
  `CountPendingFlowDrafts` is a
  read-only count for the badge. Every token-revoking transaction (revoke,
  revoke-all, recovery code, password change) voids the affected pending
  drafts together with the approvals.
- Agents write drafts through `service.Authoring.SaveDraft`
  (`pw_save_draft{flowId?, baseVersion?, flow | ops, name?, note}`): the
  authoring rules (stale base → conflict, the `"<redacted>"` sentinel keeps
  the base value, only `{{secret.NAME}}` in `Sensitive` fields, what the
  author scope may not change, limits) are in the service contract at the
  end of this section.
- **`flow.Lint(f, registry) []Issue`** (`internal/flow/lint.go`) returns
  every issue `{path, code, severity, message}` deploy would find, from the
  collectors deploy itself runs (ADR-0007): config typing
  (`node.CheckConfig`), node instantiation (unknown types, factory and
  `Validate` errors), structure, callable triggers without an active
  connection (a connection to any enabled node of a registered type, even
  one failing its own checks, is active), and the tool risk rules of
  `AnalyzeTools` (errors and warnings), the secrets strict settings require
  (`node.SecretRequirer`: deploy's last check, below), plus one definition
  check deploy does not need: node positions must be finite and within
  ±1,000,000 (`invalid_position`). Deploy still fails on the first error of
  each stage (config typing and the tool rules aggregate theirs) with the
  same errors, statuses and messages: `validateDeployable` (structure,
  callable triggers, tool rules) and `checkRequiredSecrets` are these
  collectors, so a flow without Lint errors deploys (H8), except for what
  only starting it can tell (an exclusive resource already taken: 409). The
  secrets are checked against the store deploy resolves them in:
  `flow.LintContext(ctx, f, registry)` uses `secrets.FromContext(ctx)` and
  `flow.Lint` the process-wide store; an undefined one is `undefined_secret`
  (error, at `/nodes/{id}/config/{prop}`, naming the secret, never a value).
  That error depends on the secret store, not on the definition (the admin
  may define the secret later): `flow.EnvironmentIssue` reports it, and
  `flow.BlockingIssues` — the error issues that are not environment issues
  — is what stops a dry-run and the storing or approval of a draft. Lint
  never modifies the flow and closes the node instances it builds
  (`node.Closer`, ADR-0008), as a failed build and a deploy refused after
  the build now do too. Its messages go to
  agents, so a node's messages never quote the value of a setting the read
  views hide (Sensitive, not AgentVisible, undeclared or schemaless), raw or
  as `%q`, `%+q`, JSON or `CheckConfig`'s cut-off quote (a short Sensitive
  value hides the whole message). The quoted forms of a node's hidden values
  are built once per node, each distinct form once, and only forms no
  longer than a message are looked for in it, so the cost grows with the
  issues plus the hidden leaves, not with their product (an agent's flow
  can hold hundreds of thousands of leaves). Issues are ordered deterministically:
  flow-level first, then nodes and connections in definition order; within
  one location by path, errors first, code, message. Paths: `/nodes`,
  `/nodes/{nodeId}`, `/nodes/{nodeId}/config/{prop}`, `/connections/{id}`
  (RFC 6901 escaping). Codes: `empty_node_id`, `duplicate_node_id`,
  `no_trigger`, `duplicate_connection_id`, `connection_unknown_node`,
  `connection_unknown_port`, `self_loop`, `cycle`, `unknown_type`,
  `config_type`, `invalid_config`, `trigger_not_connected`,
  `undefined_secret`, `invalid_position`, and the tool
  rules' `dynamic_destination_credentials`, `tool_global_variable`,
  `tool_stateful_aggregate`, `tool_stateful_join`,
  `tool_delay_exceeds_timeout` (warning), `tool_strict_arguments`,
  `tool_no_schema`. `pw_validate_flow` exposes it. `flow.Risk(f, registry)`
  is the highest instance risk over the enabled nodes: the type risk,
  escalated to privileged by a dynamic URL or broker host or by any
  `RiskSwitch` property turned on (`allowPrivate` of HTTP, notify and RSS
  nodes, `allowDashArgs`; the instance rules of §5, shared with the tool
  analysis); unregistered types are privileged.
- **Ops** (`flow.ApplyOps(base, ops, registry)`, at most 200, applied in
  order to a copy of the base): `add_node{node{id?, type, config?, enabled?,
  position?}}` (category and ports from the registry, a generated ID unless
  one of 1-64 `[A-Za-z0-9_-]` is given, enabled by default),
  `update_node{nodeId, config?, enabled?, position?}` (config is a JSON merge
  patch: null deletes), `remove_node{nodeId}` (with its connections),
  `connect{sourceNode, sourcePort, targetNode, targetPort}`,
  `disconnect{connectionId}` or the four endpoints. Every failing operation
  is an issue at `/ops/{index}` (`/ops` for the limit): `too_many_ops`,
  `unknown_op`, `invalid_op`, `invalid_node_id`, `invalid_position` (a
  position that is not finite or not within ±1,000,000), `node_exists`,
  `node_not_found`, `unknown_type`, `connection_unknown_port`, `self_loop`,
  `connection_exists`, `connection_not_found`; any issue returns no flow.
  `flow.AutoLayout(f, nodeIDs)` places the listed nodes without a position
  (the zero position counts as none) 240 right of their rightmost upstream
  node, or in a new column right of the canvas, at the first slot (120
  apart) whose 200×80 box overlaps no node; upstream first, deterministic.
  The slot search is bounded (a placed box blocks at most two slots, so it
  tries 2·n+1 and then takes the next), so it ends whatever the
  coordinates. ApplyOps lays out the nodes it added without a position.
- **Schema markers.** `ConfigProperty.Destination` marks where a node sends
  data or connects (URLs, hosts, brokers, topics, recipients, chat ids, file
  paths, webhook paths, pins and buses written, DSNs); `RiskSwitch` marks
  opt-ins that widen what it may do (`allowPrivate`, `allowDashArgs`);
  `Operation` marks what selects the action a request performs at its
  destination rather than message content (action-http `method` and
  `body`, action-database `query`); `Rate` marks what sets how often a
  trigger fires or how many messages a node lets through (trigger
  intervals, the cron expression, process-ratelimit `limit` and
  `windowMs`, aggregate and join counts, trigger-tool `cooldownMs`). Every
  builtin is marked; `builtin/schema_policy_test.go` enforces it (the
  operations and rates are review-decision lists, and a name that reads
  like a rate must be listed).
- **Diff** (`flow.Diff(base, next, registry)`, base nil = an empty flow):
  added, removed and changed nodes with field diffs (`type`, `enabled`,
  `config.<prop>`, each marked `sensitive`/`destination`; a position-only
  change is `moved` without fields), connections added and removed (by
  endpoints), `riskBefore`/`riskAfter` (`flow.Risk`), and alerts `{kind,
  nodeId, path, message, before?, after?}` about changes only (an unchanged
  approved flow raises none), in a deterministic order: `risk_raised`,
  `privileged_node_added` (an enabled node is privileged where the base had
  no enabled privileged node with its ID), `destination_changed` (also on
  added and removed nodes), `dynamic_destination` (a new or changed
  Destination value with a template other than secret references),
  `new_host` (the host of a URL or host Destination value that the base
  never uses; only the host is shown), `secret_added`, `secret_moved` (a
  secret the base uses, referenced at a new node and property),
  `risk_switch_on`, `sensitive_literal` (a new or changed Sensitive value
  that is not exactly `{{secret.NAME}}`), `credentialed_request_changed` (on
  a node that uses a credential in the base, a secret reference or a
  Sensitive value, an `Operation` property changed or a new input was
  connected: the approved credential would be used for something else;
  also when anything upstream of such a node with `Operation` properties
  changed — a running node feeding it, through connections or the
  flow-scope set-vars when a node feeding it reads flow variables, was
  added, enabled or reconfigured, or a connection into that part was added:
  a request's body is usually a template over the message — and, for any
  such node whose response reaches an `action-reply` (through connections
  or flow variables), when the node itself or a node between it and the
  reply changed, or a connection into that part from the node or that
  part was added: what the credential reveals goes back to the agent
  calling the tool; one alert per node),
  `rate_changed` (on a flow whose risk is above safe, a `Rate` property of
  a running node added, removed or changed, or a running node with one
  added, removed, enabled or disabled: a daily request with the admin's
  key turned into ten a second spends the credential as surely as a new
  destination; the direction is not computed, a cron expression has none),
  `global_variable_write` (`internal/flow/diff_vars.go`: a running `set-var`
  writes a global variable — its `scope` is not `flow`; absent means global
  — which other flows read for their URLs, commands and conditions, and
  what it writes may change: the set-var was added or enabled, its scope,
  key or value changed, or something upstream changed: a running node that
  feeds it, through connections or, when a node feeding it reads flow
  variables, through the flow-scope set-vars, was added, enabled or
  reconfigured, or a connection into that part was added; one alert per
  writer, `after` the variable's name), `global_variable_read` (a running
  `trigger-tool`'s path — the running part its trigger reaches through
  connections and flow variables, together with everything that feeds that
  part, through connections and the flow-scope set-vars: a join or an
  aggregate emits the call's message with what another branch sent, and
  the result goes back to the agent that calls it — reads global variables
  (a `get-var` whose scope is not `flow`, or config text naming `vars`,
  `flowVars` or `$env`: it errs on the side of yes) and changed: a node of
  it added, enabled or reconfigured, or a connection into one added; one
  alert per tool). The two global variable alerts are
  listed after the field alerts.
  The rate alert covers the settings whose purpose is the rate; connections,
  filter conditions and thresholds also change how many messages reach a
  node, as they change what the messages hold, and raise no rate alert.
  Alerts describe the flow that runs: a disabled node counts as absent
  (baselines come from the base's enabled nodes; enabling a node raises the
  alerts of adding it, disabling one those of removing it; changing or
  removing a node that is disabled raises none). Nodes are paired by ID and,
  for a repeated ID, by occurrence, so a duplicate shows as an added node
  with its alerts. Values of Sensitive properties (and every value of an
  unregistered type) read as `<hidden>` in the field diffs unless they are
  exactly a secret reference, whose name is shown. The node and field diffs
  are for the editor; alerts are stored with the draft and returned to its
  author, so they are agent-safe at the source: `before`/`after` hold a
  property's values only when agents may read it (AgentVisible and not
  Sensitive), and otherwise only risks, hosts, secret names and risk-switch
  booleans.
- Dry-run (`flow.DryRun`, behind `pw_dry_run`). The flow is first linted
  (`flow.LintContext` with the caller's secret store, on the flow as it
  would run, after `Redact`: the one validation path, ADR-0007); its issues
  are the result's `issues`. A blocking error (`flow.BlockingIssues`) means
  it does not run: the result holds only the issues, with an empty trace,
  and `DryRun` returns no error. Warnings and `undefined_secret` (the
  sandbox has no secrets anyway) are reported with the run. A valid flow
  then runs once in a private `FlowRuntime` that is never
  registered with the `RuntimeManager` (no tools published, no webhook path
  or tool name claimed, no recorder, not counted in the Prometheus metrics),
  built through `Registry.Derive`, which wraps every node: triggers never
  start — the payload is injected at the output of the chosen enabled input
  node (`triggerNodeId`) — and every node whose type risk or instance risk
  (`flow.InstanceRisk`: the escalation rules of the tool analysis) is not
  `safe` is a stub. A stub runs nothing: it records itself in `wouldExecute`
  (node, type, risk, input preview) and emits its mock (`mocks[nodeId]`, on
  its first output port) or nothing, in which case its branch ends there.
  Mocks are accepted only for stubbed nodes. Through `flow.WithNodeContext`
  the nodes get isolated variable stores (global empty, flow seeded from
  `flowVars`; the flow's persistent store is not even loaded), an empty
  secret store and a `node.Sandbox`. Every builtin secret lookup resolves
  through the node's context (`secrets.FromContext`) when the node runs,
  never in a factory, so no real secret value reaches a dry-run;
  `missingSecrets` lists the secret names the nodes the run reached
  reference. Asking for an isolated store with a nil one yields an empty
  store, never the real one. In a sandbox `action-log` and `process-debug`
  pass their input on without logging, so nothing of a dry-run reaches the
  process log (and `action-log` escapes control characters of its message
  everywhere, so a message cannot forge log lines).
  - Bounds. Refused up front (`ErrDryRunInput`): more than 50 nodes or 200
    connections, two connections between the same ports, a payload, a mock
    or the flow variables over 64 KiB of JSON each. The wall-clock cap is
    10 s from the call, building the nodes included (`timedOut`); the
    private runtime stops within a second of it. The private runtime runs
    one node at a time, queues at most 200 messages per node and routes at
    most 200 messages (each one execution); every routed copy is charged to
    the sandbox's data budget (32 MiB for everything a run generates: routed
    copies, strings expressions build, rendered templates); past either
    limit nothing more is routed (`capped`) and the run ends once the
    messages already routed ran. No single value may exceed 1 MiB: a real
    node's output over it, or a node that ran into a sandbox limit, is an
    error of its step. Retries are off. Each preview (the JSON of a payload)
    and error is cut to 2 KiB, the whole result to 64 KiB (`truncated`: a
    preview was cut or entries left out, keeping the issues first, then
    `wouldExecute`, then `missingSecrets`, then the trace; each issue
    message is cut to 2 KiB too).
  - Expressions (transform, filter, switch, set-var, join, database
    parameters) are compiled with a guard (`expr_guard.go`): every predicate
    body (the loops of `filter`, `map`, `count`, `reduce`, …) checks the
    evaluation's context, so an evaluation stops once its deadline passed
    instead of running on in the background — in every flow, not only in
    the sandbox; in a sandbox string `+`, `repeat`, `join`, `replace`,
    `toJSON`, `string` and `toBase64` size their result before it exists
    and charge the sandbox, and the result is capped at 1 MiB. Templates
    stop expanding at the sandbox's limits.
  - The result (`flow.DryRunResult`: `issues`, `trace`, `wouldExecute`,
    `missingSecrets`, `truncated`, `timedOut`, `capped`) lists the Lint
    issues and, in execution order, the trace (`nodeId`, `type`,
    `input`, `outputs[{port, preview}]`, `error`, `durationMs`, `stubbed`;
    the trigger first, stubbed, with the payload as its output) and
    `wouldExecute`. One dry-run or node test runs at a time process-wide;
    another fails at once (`flow.ErrBusy`). Every node instance built is
    closed. Work still running when the run ends — the private runtime
    draining, a goroutine a node started with `node.Go`, such as an
    expression evaluation finishing its last step — is waited for up to 1 s
    (the private runtime's stop timeout) before the call returns, so the
    next dry-run or node test right after (the same agent's, typically) does
    not find the slot taken; work still running then (a node ignoring
    cancellation) keeps the slot until it returns, so stuck or runaway
    sandboxed work cannot pile up. A node test waits the same way for the
    goroutines its node started.
  - Hidden settings. The flow package only bounds previews; the service
    redacts them for agents. Redacting derived output afterwards cannot work
    in general (a transform can encode a value), so for an agent dry-run of
    a stored flow or a draft (`flowId`, `draftId`) the service sets
    `DryRunInput.Redact`, which replaces the selected config values with
    `"<redacted>"` before the flow is validated and run: every property
    agents may not see (`Sensitive`, not `AgentVisible`, undeclared, or of
    an unknown type), and every one whose value holds a known value (a
    secret's value, or a credential typed into a `Sensitive` setting: what
    the read views redact), whose value the agent did not write itself —
    for a stored flow all of them, for a draft the settings it kept and
    recorded as withheld (see `SaveDraft`). The run then computes only on
    what the agent may see. A redacted value that leaves its node invalid (an
    expression, an object-typed setting such as HTTP headers: an error
    issue at a redacted property, or `invalid_config` at a node with
    redacted properties) does not stop the run: the node becomes a stub —
    never built from its settings, listed in `wouldExecute` with its
    type's risk, emitting its mock if one is given — and its issues are
    replaced by one `hidden_settings` warning at `/nodes/{nodeId}` (a fixed
    message). Every other issue stops the run as before. The stub marker is
    a config key of the sandbox's own copy that no caller can set. A flow
    the agent sends inline holds only what it wrote and is not redacted.
- Node test (`flow.TestNode`, behind `pw_test_node`): one node of a
  processing type whose type and instance risk are `safe` (anything else is
  forbidden), its config checked as deploy checks it (Lint's collectors:
  its problems are the result's `issues`, `config_type` at
  `/config/{prop}` and `invalid_config` at `/config`, and the node does not
  run then; the messages are not hidden, the config being the caller's
  own; result `flow.TestNodeResult`: `issues`, `outputs[{port, preview}]`,
  `error`, `durationMs`, `truncated`, `timedOut`), its input and config
  at most 64 KiB of JSON each, run once on the input with the same isolated
  stores and sandbox, a 5 s timeout (`timedOut`; it returns then even if the
  node ignores cancellation, and the node keeps the slot until it returns)
  and the dry-run's bounds; it shares the dry-run's slot.
- Secrets in strict settings. The Telegram bot token, the notification URL
  and the database DSN cannot work without the secrets they reference:
  deploy refuses a flow (400, naming node, setting and secret) while one of
  them is not defined (`node.SecretRequirer`), and Lint reports the same
  secrets as `undefined_secret`; the values themselves are resolved per
  execution. A dry-run does not stop at them: it stubs these nodes, keeps
  the issues and lists the names in `missingSecrets`.
- **Approval page** (editor: `/agents/drafts`, `/agents/drafts/:id`;
  `stores/drafts.ts`, presentation rules in `lib/drafts.ts`). The agent
  pages get a Drafts tab whose badge is the `drafts` of the passive pending
  count (polled with `X-PiWorker-Background`, like every background reload
  of the list or of an open draft). The list filters waiting, decided or all
  drafts and shows the flow (or "New flow"), the state in words (waiting,
  out of date, approved, deployed by agent, rejected, void: a draft its
  agent applied itself, `appliedBy: "agent"`, reads "Deployed by agent" —
  the agent deployed it as version N, you did not review it, the version
  history can roll it back — never as one you approved), risk, alert and issue counts,
  token and the note. A background reload replaces the list only while it
  shows its first page: with pages loaded (or loading) by Load more it is
  skipped and the list offers a Refresh. The review page shows, in this
  order: what the draft changes with an explanation when it is out of date
  or decided. A pending draft names the base and current versions. A decided
  one says which version it was written for, the version an approval saved
  and where the flow is now (or that it was deleted), since the server marks
  only pending drafts stale. Then the agent's note, labelled as written by
  the agent; for a decided draft, a note that its risk, alerts and changes
  are recomputed now against the current version (for an approved draft its
  changes are already part of it, so "no changes" there does not mean the
  draft changed nothing), also told by the changes heading ("recomputed
  now") and the alerts' empty text; the risk before → after; the alerts,
  before any diff, each kind with its own icon and label
  (`credentialed_request_changed`, `rate_changed`, `global_variable_write`
  and `global_variable_read` included; an unknown kind shows its name and
  message), its location, the server's message and `before`/`after` only
  when the alert carries them (H7). For a hidden setting the alert points to
  the field diff only when that comparison's node diff lists the setting;
  otherwise (a node disabled or enabled, its setting unchanged) it shows the
  setting's value from the proposed flow, or says it is not shown. Then the
  Lint issues; the name and description the draft changes (`nameChange`,
  `descriptionChange`, as agent text); the per-node diff (added, removed,
  changed; field before/after, `Sensitive` values as "hidden" with a lock,
  `Destination` fields highlighted with an icon and the word Destination, a
  node's alert kinds next to it, moves) and the connection diff; "Compared
  with the version you last approved" (`diffApproved`, with its own risk,
  alerts and diff); the proposed flow's JSON on demand. Approving asks for
  confirmation repeating the risk and the alerts; with `diffApproved` it
  states both comparisons (current version, and the version last approved)
  and that approving also approves the earlier agent changes, since the
  result becomes the baseline the `low` level compares against; it also
  names a rename and a changed description. "Deploy now" is checked and
  locked when the flow runs (it is always redeployed); rejecting takes an
  optional reason (≤ 500 characters) shown to the agent. After either
  decision (or a refusal) focus moves to the outcome at the top of the page,
  scrolled into view, since the decision section unmounts. Both are offered
  only while the draft is pending; approving is disabled, with the reason,
  when the draft is out of date or has blocking errors, or would deploy with
  `undefined_secret` errors (the server's H2 rules; it remains the judge: a
  400 lists its `flowIssues`, a 409 reloads the draft). Every agent-supplied
  string (names, notes, node IDs, values, messages, reasons) is rendered as
  text through `AgentText`, with hidden characters revealed; color is never
  the only signal (icons and words for states, risks and alerts).
- **Markers and version history** (editor, H6). Flow cards and the editor
  header show "Deployed by agent <token>" ("Last deployed by …" when
  stopped) when the deployment marker's actor is an agent, and "Changed by
  agent" when `agentChanged`; an editor deploy replaces the marker and an
  editor save clears `agentChanged` locally. After turning a flow on from
  the list, or a rollback, the list reads the flow back (`GET
  /api/flows/{id}`: live running, version, marks; a running intent without a
  running instance shows as failed) instead of guessing: the list may
  predate an agent's deploy, and a refused redeploy may have left the old
  version running. Flow names and descriptions (agents may write them
  through approved drafts) are shown as agent text in the cards, labels and
  dialogs. A version history dialog (each card, and the editor's History)
  lists every kept version with who saved it and whether it is current,
  deployed, not approved, last approved or from a draft, and offers "Roll
  back to version N" (the version last approved) after a confirmation naming
  it and saying whether the running flow is redeployed (the editor reloads
  the flow, warning that unsaved changes are discarded). The flows list
  sends the version it shows when turning a flow on (`PATCH …/toggle
  {enabled, version}`): a version saved since is a 409 that deploys nothing,
  and the list offers to open it for review.
- **Agent deploy (opt-in risk).** Setting `agent_deploy`: `off` (default),
  `low` (agent may deploy/start/stop flows and approve its own drafts when the
  resulting flow's risk is ≤ external, no recipient/destination changes vs the
  last admin-approved version, no new secrets references, no change to what
  an approved credential is used for: `credentialed_request_changed`, nor to
  how often a flow that reaches out fires: `rate_changed`, no change to what
  a global variable receives or to a tool path that reads global variables:
  `global_variable_write`, `global_variable_read`), `all`
  (any flow, including privileged). Changing it needs password step-up and
  shows the risk text; tokens need the `deploy` scope (only grantable when
  not `off`). Every agent deploy is audited, marked "deployed by agent" in
  the editor, and the admin can roll back to the last admin-approved version
  in one click.

### Service contract — authoring, drafts, agent deploy (`internal/service`)

Implemented in `authoring.go` (agent authoring), `authoring_proposal.go`
(resolving and checking a proposal), `authoring_audit.go` (who may call
what, the audit), `drafts.go` (the editor's side) and `deploy.go` (the
policy, rollback, versions, marks); the lifecycle path is
`internal/api/flows_lifecycle.go` and the editor REST
`internal/api/agent_drafts.go`. `main.go` builds the service over the
`FlowsHandler` (`appServices.authoring`).

**Construction.** `service.NewAuthoring(service.AuthoringOptions{Store,
Lifecycle, Registry, Secrets, Audit, AuthorEnabled, Now, Logger})
*service.Authoring`, safe for concurrent use.
- `Store service.AuthoringStore` (required; `*storage.SQLiteStore`):
  `GetFlow`, `CreateFlowVersioned`, `UpdateFlowVersioned`, `GetFlowVersion`,
  `ListFlowVersions`, `LastAdminApprovedVersion`, `GetDeployment`,
  `CreateFlowDraft`, `GetFlowDraft`, `ListFlowDrafts`, `DecideFlowDraft`,
  `GetAPIToken`, `AgentDeployLevel` (storage's signatures).
- `Lifecycle service.FlowLifecycle`: internal/api's `FlowsHandler` (below);
  without it `ApproveDraft`, `Rollback`, `Deploy` and `Stop` are
  `unavailable`.
- `Registry *node.Registry` (default `node.DefaultRegistry`); `Secrets
  *secrets.Store` (default `secrets.DefaultStore`: Lint's secret checks
  through `secrets.WithStore` + `flow.LintContext`, unknown-secret warnings,
  redaction of its values); `Audit *service.AuditWriter` (nil: not
  audited); `AuthorEnabled` (`mcp.author_enabled`); `Now`; `Logger`.

**Actors.** Agents need the token scope `author` and `AuthorEnabled` for
`Validate`, `SaveDraft`, `GetDraft`, `DryRun`, `TestNode`, and the scope
`deploy` with a level above `off` for `Deploy`, `Stop` (else `forbidden`).
Working on a stored flow — `Validate` or `DryRun` by `flowId`, `SaveDraft`
on an existing flow, `GetDraft` or `DryRun` of a draft of an existing flow
(`baseVersion` ≠ 0) — also needs the `read` scope (`forbidden` otherwise):
the author scope alone works on the agent's own flows (inline flows, new
flows' drafts), so an admin can issue an author token that cannot inspect
existing flows. The editor (`service.EditorActor()`) may call `Validate`, `DryRun`,
`TestNode` and the editor methods (`ListDrafts`, `GetDraftDetail`,
`ApproveDraft`, `RejectDraft`, `Rollback`, `Versions`, `Marks`), which
refuse agents (`forbidden`); `SaveDraft`, `GetDraft`, `Deploy` and `Stop`
refuse the editor (`forbidden`: it saves, reads and deploys through its own
paths). Agent operations are audited under the tool names
`service.AuthorTool*` / `service.DeployTool*` (`pw_validate_flow`,
`pw_save_draft`, `pw_get_draft`, `pw_dry_run`, `pw_test_node`,
`pw_deploy_flow`, `pw_stop_flow`), outcome only (never payloads).

**Errors.** `*service.ToolError` with `Code()`: `forbidden`, `not_found` (an
unknown draft, or another token's), `flow_not_found`, `invalid_arguments`
(`FlowIssues []flow.Issue` for a flow's problems), `conflict` (new kind
`service.ErrConflict`: a stale base version, with `CurrentVersion`; a draft
no longer pending; a flow already running or not running; an exclusive
resource taken; nothing to roll back), `busy` (the sandbox slot, which wraps
`flow.ErrBusy`), `unavailable`; any other error is internal. MCP results
carry `error.flowIssues` and `error.currentVersion`; the REST writer
(`api.ToolCallErrorData`) answers 403/404/400/409/429/503 with `{"success":
false, "error": "…", "data": {"code": "…", "issues"?: […], "flowIssues"?:
[{"path", "code", "severity", "message"}], "currentVersion"?: N,
"retryAfterMs"?: N}}`.

**Lifecycle.** `FlowLifecycle.WithLock(ctx, fn func(LockedLifecycle) error)
error` runs `fn` under `FlowsHandler.lifecycleMu` (not reentrant), so what
`fn` checked still holds when it deploys; `FlowLifecycle.Running(flowID)`
reads without the lock, for views (`GetDraftDetail`'s `running`: an advisory
value that `ApproveDraft` reads again under the lock, so reading a draft
never waits for a slow restart or a drain timeout). `LockedLifecycle`:
- `Running(flowID) bool`.
- `Deploy(ctx, flowID, by DeployActor, restart bool) (version int, err)`:
  deploys the stored current version as the editor's deploy does (a running
  flow is 409 unless `restart`, which stops it first; 503 after
  `BeginShutdown`; 404 unknown; 400 deploy validation; 409 still stopping or
  an exclusive resource taken), persists the restart intent and records the
  deployment marker (`RecordDeployment(flowID, by.Deployment(version))`). A
  redeploy failing after the old instance stopped leaves the flow stopped
  with its running intent (read views: `failed`). It lints nothing beyond
  deploy's checks: callers lint first.
- `Stop(ctx, flowID, by DeployActor) error`: stops and persists the stop
  intent (409 not running, 404 unknown; allowed during shutdown); the marker
  is kept.
Refusals are `*service.LifecycleError{Status, Message, Err}` (the editor
path's statuses; `Err` the cause for `errors.Is`); `ToolError()` maps 400 →
`invalid_arguments`, 404 → `flow_not_found`, 409 → `conflict`, 503 →
`unavailable`, anything else → nil (internal), always with a fixed message
(the lifecycle path's quote deploy's errors, which may quote config values
agents may not read, such as a webhook path): a 409 says its cause — an
exclusive resource another running flow holds (`node.ErrResourceConflict`:
"a webhook path or tool name"), already running, not running, still
stopping, or a generic conflict. `service.DeployActor{Kind, TokenID, TokenName}` comes from
`actor.DeployActor()` (the editor, or the agent's token). Every successful
deploy records the marker, the editor's (`changeRunning`, toggle) as actor
`editor` and the agent's as `agent` with the token (H6); restoring running
flows at startup keeps a marker that names the restored version, and
replaces one that names another version (a redeploy that failed after a
save — an agent's draft — left the running intent on the newer version)
with that version's author from its `VersionMeta` (`markRestored` in
`main.go`), so the marker and the versions dialog say what runs. The editor's own requests and
the interface share one implementation (`startLocked`/`stopLocked` in
`internal/api/flows_lifecycle.go`): `changeRunning` is a thin wrapper, so
there is no second lifecycle. A marker that cannot be recorded undoes the
deployment it would describe (500), so what runs is always attributed;
toggling a running flow on records nothing (it deploys nothing). A
`LockedLifecycle` used after its `WithLock` returned refuses to act.

**Methods** (requests and results marshal with these JSON names):

| Method | Request | Result |
|---|---|---|
| `Validate(ctx, actor, ValidateRequest)` | `{flowId?, flow?, ops?}` | `ValidateResult{valid, issues, risk}` |
| `SaveDraft(ctx, actor, DraftInput)` | `{flowId?, baseVersion?, flow? \| ops?, name?, note}` | `DraftResult{draftId, status, risk, issues, alerts, flowId, baseVersion}` |
| `GetDraft(ctx, actor, draftID)` | | `DraftView{draftId, status, reason?, risk, issues, alerts, flowId, baseVersion, appliedVersion?, appliedBy?, note, createdAt, decidedAt?, flow: DraftFlowView{id?, name, nodes: [NodeView], connections: [ConnectionView], totalNodes, totalConnections, truncated}}` |
| `DryRun(ctx, actor, DryRunRequest)` | `{flow? \| draftId? \| flowId?, triggerNodeId?, payload?, mocks?, flowVars?}` | `flow.DryRunResult` |
| `TestNode(ctx, actor, TestNodeRequest)` | `{type, config?, input?}` | `flow.TestNodeResult` |
| `ListDrafts(ctx, actor, DraftQuery)` | `{status, limit, beforeId}` | `DraftList{drafts: [DraftSummary], nextBefore}` |
| `GetDraftDetail(ctx, actor, draftID)` | | `DraftDetail` |
| `ApproveDraft(ctx, actor, draftID, deploy)` | | `ApplyResult{flowId, version, deployed, deployError?{status, message}}` |
| `RejectDraft(ctx, actor, draftID, reason)` | | (error only) |
| `Deploy(ctx, actor, DeployRequest)` | `{flowId, draftId?}` | `DeployResult{flowId, version, running, risk}` |
| `Stop(ctx, actor, flowID)` | | `DeployResult` (`running` false) |
| `Rollback(ctx, actor, flowID)` | | `ApplyResult` |
| `Versions(ctx, actor, flowID, limit)` | | `VersionList{current, lastAdminApproved, versions: [VersionEntry]}` |
| `Marks(ctx, actor, f)` | | `FlowMarks{deployment, agentChanged}` |

- `Validate` resolves the proposal exactly as `SaveDraft` (a whole flow, or
  with `flowId` ops or a flow whose `"<redacted>"` values keep the current
  version's), then reports `flow.LintContext` (with `Secrets`), the
  authoring issues and `flow.Risk`; `valid` means `flow.BlockingIssues` is
  empty (an `undefined_secret` error still blocks a deploy until the admin
  defines the secret). Request problems `SaveDraft` refuses outright are
  refused the same way. Nothing is stored. `ops` and `flowId` extend the
  contract's `{flow}`; `flowId` alone validates the current version as it
  is. The editor's proposals are linted only (no author restrictions, no
  authoring issues: the admin may type a credential).
- `SaveDraft` (`service.RedactedValue` = `"<redacted>"`): an existing flow
  needs `baseVersion` = its current version (else `conflict` with
  `currentVersion`; unknown: `flow_not_found`); a new flow needs `flow` (no
  `ops`) and a name (`name` or `flow.name`, ≤ `MaxDraftNameChars` = 100);
  an existing flow keeps its name unless `name` (or a whole flow's name) is
  set, and the bound applies only to a name the draft changes (a longer
  stored name the editor allowed is kept). `ops` apply to the base (their
  issues, at `/ops/{i}`:
  `invalid_arguments` with `flowIssues`); new nodes are laid out. Authoring
  issue codes (besides Lint's): `sensitive_literal` (error: a `Sensitive`
  property holds a literal, not `{{secret.NAME}}` or the sentinel; the
  message never quotes it), `unresolved_redaction` (error: a sentinel
  without a base value at the same node ID, type and property),
  `unknown_secret` (warning: a reference to a secret that is not defined;
  a strict setting's is Lint's `undefined_secret`). A proposal with
  blocking errors (`flow.BlockingIssues`: Lint's — duplicate or empty node
  IDs, unknown types, config typing… — and the two authoring errors) is
  refused with `invalid_arguments` and `flowIssues` (H2); `undefined_secret`
  and warnings are stored with it. `forbidden`, naming the path: adding,
  changing or removing `trigger-tool` or `action-reply` nodes, a webhook
  trigger's `path` or token, any `RiskSwitch` property turned on, the legacy
  `Variables`; and 20 pending drafts per token (the admin must decide
  pending drafts first). `invalid_arguments`: a note over
  `MaxDraftNoteChars` (2000), more than `MaxDraftNodes` (200) nodes or
  `MaxDraftConnections` (400) connections, over `MaxDraftFlowBytes` (512
  KiB) of JSON. The stored draft holds the resolved flow, its issues, risk
  and alerts (`flow.Diff` against the current version, an empty flow when
  new; agent-safe, H7).
  - Resolution, in order: the request shape and sizes; the base (the
    stored current flow); ops through `flow.ApplyOps`, or a copy of the
    whole flow whose nodes of registered types get their category and
    ports from the registry (a node without config an empty one, a
    connection without an ID a generated one) and whose nodes the base does
    not have and that have no position are laid out (`flow.AutoLayout`); a
    whole flow without a name, description or legacy `variables` keeps the
    base's (agents never see the variables); `id` is the `flowId`, and
    `state`, `version` and the timestamps are dropped. Then the node and
    connection limits, then `"<redacted>"` — only a top-level config value,
    which is what the read views show — resolved to the base value of the
    same node ID, type and property, then the author restrictions, then the
    authoring issues and Lint (`flow.LintContext` with `Secrets`), merged in
    Lint's order.
  - The author restrictions compare the base and the proposal node by node
    (same ID and type): a `trigger-tool` or `action-reply` node added,
    removed, retyped, or with another config or enabled flag (moving one is
    allowed); a webhook trigger added, turned on, or with a `path` or
    `Sensitive` setting (its `token`) the agent wrote — equal to the base's
    or not, since agents cannot read them; only a value kept (below) stays,
    and removing a set one is a change; a
    `RiskSwitch` property `true` on an enabled node where the base had it
    off or the node was absent or disabled (enabling a node counts as
    adding it; a disabled node counts as absent, so its switch, on or off,
    stops nothing until it is enabled); `variables`
    other than the base's.
  - No equality oracles: the service tells what the agent wrote from what
    it kept, never by comparing with the base (an exemption for a value
    equal to the base's would let an agent confirm a guess of a value it
    cannot read: a credential, a webhook path). Written: a whole flow's
    config values other than the sentinel, the configs of `add_node` and
    the patches of `update_node` (the patch, not the merged result). Kept:
    a value resolved from `"<redacted>"`, a property of a base node the ops
    left alone (not removed or added again), or an object patch merged into
    the base's object (the base's other members stay). `sensitive_literal`
    and `unknown_secret` concern what was written (a written `Sensitive`
    value must be references only even when it equals the base's; a literal
    the admin typed stays when kept), so the names of secrets referenced in
    settings agents cannot read are not revealed. An `unknown_secret` where
    Lint reports the same setting's `undefined_secret` is left out. The
    stored draft records its withheld settings (storage `KeptJSON`,
    `{"props": [[nodeId, prop], …], "name": bool}`): the kept ones that are
    hidden or whose base value holds a known value (a secret's value, or a
    credential typed into a `Sensitive` setting of the base), and whether
    the name is the base's and holds one. Its dry-run hides them from the
    run and `GetDraft` shows them as `"<redacted>"`.
  - No redaction oracle either: what agents see never depends on how what
    they sent compares with a value they may not read (hiding text equal to
    a secret's value or to the admin's credential would let them confirm a
    guess, a PIN in a few hundred tries). What the agent wrote comes back as
    it wrote it — in issue messages and paths, alert values, the draft
    view, dry-run and node-test output — a secret's value or not; the
    admin's content it may not read is withheld wholesale, decided by that
    content alone. Issue messages: an issue at a node holding withheld
    settings gets a fixed text (Lint quotes settings, hiding only what the
    read views hide, by comparison), except `config_type` and
    `undefined_secret` at a setting the agent wrote, whose messages are
    built again from that setting alone; codes whose messages never quote
    settings (IDs, types, ports, positions, the authoring issues) are kept.
    Alerts: one about a withheld setting carries no `before`/`after` and a
    message naming no value (the host of `new_host`, the secret of
    `secret_added`/`secret_moved`: enabling a disabled node whose hidden
    URL names a host and a secret raises the alerts without revealing
    either, H7); `before` values (the base's) are redacted as the read
    views redact them, other `after` values are what the agent wrote or may
    read. URLs are cut past their host and each message is at most 2 KiB.
    Open: the read views (§6) still redact known values from a stored
    flow, which includes what an agent wrote once a version holds it (an
    approved draft, or one it deployed itself at `low` or `all`); closing
    that needs per-setting provenance across versions.
    Storage errors map to `conflict` (the base moved meanwhile),
    `flow_not_found`, `forbidden` (the 21st pending draft, a revoked token)
    and `invalid_arguments` (too large to store). A new flow's draft has no
    ID: the approval assigns it, and the applied draft then records the new
    flow's ID as its `flowId` (its `baseVersion` stays 0).
- `GetDraft`: the agent's own drafts only (another token's: `not_found`,
  with the message of an unknown one); the proposed flow with GetFlow's
  shape and bounds: hidden and withheld settings (and a withheld name) as
  `"<redacted>"`, everything else as the agent wrote it (IDs, names and
  visible values are not redacted by comparison); the stored issues and
  alerts; the admin's reason with secret values and URLs past their host
  redacted (the admin's text, which the agent does not choose).
- `DryRun` of exactly one of `flow`, `draftId` (own draft) or `flowId` (the
  current version). For an agent by `flowId` or `draftId` it sets
  `DryRunInput.Redact` (H4): properties agents may not read (`Sensitive`,
  not `AgentVisible`, undeclared, unknown type) are hidden — all of them for
  a stored flow, for a draft of an existing flow (`baseVersion` ≠ 0) the
  kept ones the draft recorded (what the agent did not write, equal to the
  base's or not; a draft without a readable list hides every hidden
  setting, or without a readable list what the stored-flow rule hides); a
  stored flow also hides every visible setting whose value holds a known
  value; a node the hidden values leave invalid is a mockable stub with
  a `hidden_settings` warning (see the dry-run above), so a stored flow
  with hidden expressions or headers still runs around them; a new flow's
  draft, approved or not, and an inline `flow` are the agent's own and are
  not redacted, but no dry-run ever gets real secrets (the sandbox's store
  is empty). The editor sees previews and
  errors unredacted except secret values. `flow.ErrSandboxBusy` → `busy`,
  `flow.ErrDryRunInput` → `invalid_arguments`; an invalid flow is a result
  with `issues` (no run), not an error. For agents the issue messages, trace
  inputs, outputs and errors and the `wouldExecute` inputs lose URLs past
  their host and, for an inline flow, the literals the agent typed into its
  own `Sensitive` settings, and are cut to 2 KiB again. Nothing else is
  hidden from the result by comparison, secret values included: the run
  never holds a secret (its store is empty) nor what the agent may not
  read of a stored flow or draft (`Redact`), so text equal to one came
  from the agent (its payload, mocks or what it wrote), and hiding it
  would confirm the guess. The lint runs with the
  service's secret store (so `undefined_secret` is real); the nodes still
  get the sandbox's empty one.
- `TestNode`: `Sensitive` literals in `config` refused as in `SaveDraft`
  (agents only; `/config/{prop}`); `flow.ErrTestNodeForbidden` →
  `forbidden`; `flow.ErrInvalidNodeType` and `flow.ErrDryRunInput` →
  `invalid_arguments`; busy → `busy`; config problems are the result's
  `issues`; the config and input are the caller's own (and the sandbox's
  secret store is empty), so agents get the issues, outputs and error as
  sent, URLs cut past their host; the editor's lose secret values.
- `ListDrafts`: `status` `pending` (default), `decided` or `all` (else
  `invalid_arguments`), `limit` 1–100 (0: 50; more is 100), newest first,
  `nextBefore` the next page's `beforeId` ("" at the end). `flowName` is the
  stored flow's current name (the proposed one for a new flow); `stale` is
  a pending draft of a flow whose version moved (or that is gone).
- `GetDraftDetail`: recomputed on read against the current version and
  registry: `issues` (Lint now), `diff` (vs the current version; an empty
  flow when new; field diffs with full values, `Sensitive` ones
  `"<hidden>"` unless a secret reference, H7), `diffApproved` (vs
  `lastApprovedVersion` when that is not the current one, else null),
  `running`, `nameChange` and `descriptionChange` (`{before, after}` where
  the proposed name or description differs from the current version's, an
  empty flow's for a new one; null when unchanged: a rename shows on the
  approval page; agent text), and `flow` (the proposed flow, `Sensitive` literals
  `"<hidden>"`, and every value of an unregistered type; hidden on a copy,
  so the diffs compare the real values). The summary's
  `alertCount`/`issueCounts` come from what was stored at save time; the
  detail's `issues` add the `unknown_secret` warnings of now.
- `ApproveDraft(deploy)`, under the lifecycle lock: not pending → `conflict`;
  stale (base ≠ current) → `conflict` with `currentVersion`; unknown →
  `not_found`. Re-linted against the current registry (H2): blocking
  errors refuse it (`invalid_arguments` + `flowIssues`, REST 400), and so
  does any Lint error, `undefined_secret` included, when the flow will be
  redeployed (`deploy`, or the flow is running: a running flow is always
  redeployed) — the draft stays pending and nothing is saved. Saved by
  `UpdateFlowVersioned` (`CreateFlowVersioned` for a new flow, created
  inactive) with `VersionMeta{editor, AdminApproved, DraftID, note
  "approved draft <id> from <token name>"}`, which requires a real pending
  draft and closes it as approved in the same transaction (H5: never
  `DecideFlowDraft` afterwards). Then (re)deployed through
  `LockedLifecycle.Deploy(…, EditorActor().DeployActor(), restart =
  running)` when asked or running; a failed deploy is `deployError`
  (`{status, message}`, the editor path's; a failure that is not a
  `LifecycleError` is 500 "Failed to deploy flow"), the version stays saved.
  A new flow gets a generated ID. Without a lifecycle path: `unavailable`.
- `RejectDraft(reason ≤ 500 characters)`: not pending → `conflict`, unknown
  → `not_found`; the reason is shown to the author. It needs no lock:
  storage decides only pending drafts, and a save applying a draft checks it
  is still pending in its own transaction.
- `Deploy` / `Stop` (agent): level `off` → `forbidden`; `low` → allowed only
  when `flow.Risk` of the flow that would run (Stop: the current one) is at
  most `external` and `flow.Diff(last admin-approved version or an empty
  flow, candidate)` raises none of `service.LowDeployRefusedAlerts()`:
  `privileged_node_added`, `destination_changed`, `dynamic_destination`,
  `new_host`, `secret_added`, `secret_moved`, `risk_switch_on`,
  `sensitive_literal`, `credentialed_request_changed` (H1), `rate_changed`,
  `global_variable_write` (shared state other flows read) and
  `global_variable_read` (shared state a tool would return) — refused
  with `forbidden` listing the kinds, never values; `all` → any flow. With
  `draftId` the agent's own pending draft of `flowId` (another token's:
  `not_found`; stale: `conflict`) is evaluated, saved with
  `VersionMeta{agent, token, not admin-approved, DraftID}` (closing the
  draft, H5) and deployed, restarting a running flow; without it the current
  version is deployed (a running flow: `conflict`). The flow is linted first
  (any Lint error, `undefined_secret` included: `invalid_arguments` +
  `flowIssues`, rendered like the authoring issues — a node holding
  withheld settings gets a fixed text; a token without the `read` scope
  gets only the count, since the issues name the stored flow's nodes and
  settings). Under the lifecycle lock, right before deploying or
  stopping, the token is re-read — still active (not revoked or expired)
  with the `deploy` scope, else `forbidden` — the level re-read, and the
  flow's version must still be the one the policy evaluated, else
  `conflict`; nothing is deployed then (H3). Audited (the row is
  privileged when a privileged flow reached the lifecycle path); the
  marker records the agent's token.
  - As built: the policy is evaluated outside the lock (Lint, `flow.Risk`,
    the diff) and checked again inside it, where the token is always
    re-read (stronger than comparing `TokensEpoch`, which the service does
    not use: an expiry needs no epoch bump), the level re-read (`off`, or `low` when `all` was evaluated:
    `forbidden`) and the flow's version compared (`conflict` with
    `currentVersion`). A draft of another flow (or a new flow's draft) is
    `invalid_arguments`, a decided one `conflict`. At `low` a draft's
    deploy also applies the stop rule to the current version it replaces
    (risk at most `external`, none of the refused alerts against the last
    admin-approved version), running or not: what the level may not stop
    it may not replace (say, with the privileged node removed). The refusal
    of the low level names the risk ("this flow is privileged", "the
    current version, which the draft would replace, is privileged") and the
    alert kinds in `LowDeployRefusedAlerts` order. A draft saved before its deploy failed
    stays saved: the error says "the draft was saved as version N, but
    deploying it failed: …" with `currentVersion` N, whatever the lifecycle
    answered — a refusal `LifecycleError.ToolError` does not map (a 500: an
    unclassified deploy error, the marker not recorded) is `unavailable`
    with a fixed text ("the admin can see why in the editor"), never the
    lifecycle's message. The audit row (args:
    `flowId`, `draftId`, never the flow) is privileged only when a
    privileged flow's deploy or stop reached the lifecycle path (the lock
    and the re-check passed): a refusal before it is an ordinary row the
    row cap may evict, so a loop of refused deploys cannot fill the audit
    with rows that are never evicted. Stop answers the version it stopped;
    a stop needs no lint.
- `Rollback` (editor), under the lock: restores the last admin-approved
  version as a new version (`VersionMeta{editor, AdminApproved, note
  "rollback to vN"}`) and redeploys it when the flow is running (a failed
  redeploy is `deployError`); `conflict` when the current version is
  already admin-approved or none exists.
- `Versions` (editor): newest first, `limit` 1–50 (0: 50; more is 50),
  metadata only; `lastAdminApproved` 0 when none; `deployed` marks the
  version the deployment marker names.
- `Marks` (editor): `deployment` (the marker, or null) and `agentChanged`
  (the flow's current version is not admin-approved), through
  `service.FlowMarksOf(store, flow)`, which internal/api's flow list and
  view use too (two indexed reads per flow).
- Audit: every agent operation is one `mcp_audit` row under its tool name
  with only references as arguments (`flowId`, `draftId`, `baseVersion`,
  the number of `ops`, a node `type`, `triggerNodeId`) and the outcome code,
  except `busy` and `invalid_arguments`, which are not rows: the MCP
  endpoint counts them per token and minute (`mcp_rejections`), once, as
  it counts every built-in tool's (§8); the service counts none. The
  editor's calls are not audited (they are not agents').

**Editor REST** (section D of the wave contract; session cookie, CSRF on
writes, `{success, data}` envelope; errors as above; `api.DraftsHandler`
over `service.Authoring` as the editor; the drafts routes exist with
authentication, the versions and rollback routes always; an invalid
`limit` is 400; request bodies are optional and strict — unknown or
mistyped members are 400; an approval or rollback, once started, is not
cut short by the browser going away):
- `GET /api/agent/drafts?status=pending|decided|all&limit=&beforeId=` →
  `DraftList`: `{"drafts": [{"id", "flowId", "flowName", "isNew",
  "baseVersion", "currentVersion", "stale", "status", "risk", "alertCount",
  "issueCounts": {"error", "warning"}, "note" (≤ 200 characters), "tokenId",
  "tokenName", "createdAt", "decidedAt" (null while pending), "reason"?,
  "appliedVersion"?, "appliedBy"? ("editor" or "agent")}], "nextBefore":
  ""}`.
- `GET /api/agent/drafts/{id}` → `DraftDetail`: the summary's fields with
  the whole `note`, plus `"issues": [flow.Issue]`, `"running"`,
  `"nameChange"` and `"descriptionChange"` (`{"before", "after"}` or null),
  `"diff":
  {"nodes": [{"nodeId", "type", "typeName", "change", "fields": [{"path",
  "change", "before"?, "after"?, "sensitive", "destination"}], "moved"}],
  "connections": {"added": [Connection], "removed": [Connection]},
  "riskBefore", "riskAfter", "alerts": [{"kind", "nodeId"?, "path"?,
  "message", "before"?, "after"?}]}`, `"lastApprovedVersion"` (0: none),
  `"diffApproved"` (the same shape, or null), `"flow"` (the proposed flow).
- `POST /api/agent/drafts/{id}/approve` `{"deploy": bool}` → `ApplyResult`
  `{"flowId", "version", "deployed", "deployError"?: {"status",
  "message"}}`; 409 not pending or stale (`currentVersion`), 400 invalid
  (`flowIssues`), 404 unknown.
- `POST /api/agent/drafts/{id}/reject` `{"reason"?}` → `{}`.
- `GET /api/agent/approvals/pending-count` → `{"pending", "drafts"}`
  (`drafts`: `storage.CountPendingFlowDrafts`, read-only, on the same
  passive path).
- `GET /api/flows/{id}/versions?limit=` → `VersionList` `{"current",
  "lastAdminApproved", "versions": [{"version", "actorKind", "tokenId"?,
  "tokenName"?, "adminApproved", "note"?, "draftId"?, "createdAt",
  "deployed"}]}`.
- `POST /api/flows/{id}/rollback` → `ApplyResult`; 409 nothing to roll back.
- `GET /api/flows` → `{"flows": [FlowWithMarks], "total"}` and `GET
  /api/flows/{id}` → `{"flow": FlowWithMarks, "running"}`:
  `service.FlowWithMarks{*flow.Flow; FlowMarks}` is the flow's own JSON
  (with `version`) plus `"deployment": {"version", "actorKind",
  "tokenName"?, "deployedAt"} | null` and `"agentChanged": bool` (H6).

**MCP** (section E, `internal/mcpserver/author.go`, `deploy.go`,
`nodetypes.go`; §8 has the argument contracts): each built-in tool's
arguments are the request's JSON (`pw_validate_flow` → `Validate`,
`pw_save_draft` → `SaveDraft`, `pw_get_draft{draftId}` → `GetDraft`,
`pw_dry_run` → `DryRun`, `pw_test_node` → `TestNode`, `pw_deploy_flow` →
`Deploy`, `pw_stop_flow {flowId}` → `Stop`) and its structured content the
result's JSON (lists as `[]`, checked against the tool's output schema).
The endpoint only checks the arguments' shape (closed flows and ops, each
op's fields, the combinations, the bounds; refusals `invalid_arguments`,
counted in `mcp_rejections`) and drops the read views' informational fields
from a sent flow; everything else — scopes, `AuthorEnabled`, the deploy
level, limits, redaction, the H2 and H3 checks, audit — is the service's.
The author tools and `author_flow` are served only with `mcp.author_enabled`
and the deploy tools only while the level is above `off` (§8), so a token
never sees tools the service would refuse anyway. `pw_list_node_types` reads
the registry (no service state) and, like `author_flow`, is audited by the
endpoint. `mountMCP` registers the author and deploy tools, the prompt and
`DeployLevel` (the store) from `mcpBuiltins.authoring`: `buildApp` passes
the same authoring service the editor's drafts pages use, built over the
`FlowsHandler` lifecycle path; a deploy tool without `DeployLevel` is a
startup error. `main_mcp_test.go` drives the whole path end to end (the
real router over TLS with the SDK client, and the editor REST), and checks
the endpoint's raw `tools/list` for an author+deploy token against the
adapter's golden file.

**Hand-offs from the foundations (binding).** H1 the low policy also
refuses `credentialed_request_changed`. H2 `SaveDraft` and `ApproveDraft`
refuse a proposal with blocking Lint errors (re-linted at approval against
the current registry). H3 an agent deploy or stop re-checks the token and
the evaluated version under the lifecycle lock. H4 agent dry-runs by
`flowId`/`draftId` set `Redact`; inline flows are not redacted but never
get real secrets. H5 a version saved with a `DraftID` requires a real
pending draft and closes it in the same transaction (no `DecideFlowDraft`
afterwards); an agent applying another token's draft gets `not_found`. H6
`RecordDeployment` on every successful deploy (editor and agent; a restore
keeps the marker when it names the restored version, else attributes the
restored version to its author); flow list and view carry `version`, `deployment`,
`agentChanged`. H7 alerts carry `before`/`after` only for `AgentVisible`,
non-`Sensitive` properties; the editor's full values come from the field
diffs (`Sensitive` as `<hidden>`); disabled nodes count as absent for
alerts. H8 deploy's required-secret check is a Lint collector:
`undefined_secret`. H9 every expression loop checks its context (about 3×
on tight loops) and the sandbox bounds data; kept, to be recorded in an
ADR.

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

Claude Code (check the CA's fingerprint, then trust it through
`NODE_EXTRA_CA_CERTS` for the `claude` command only: Node.js trusts it for
every site, so it is never exported from the shell profile):
```bash
openssl x509 -in ~/piworker-ca.crt -noout -fingerprint -sha256   # must match
claude mcp add --transport http --scope user piworker https://raspberrypi.local:8443/mcp \
  --header "Authorization: Bearer pwk_…"
NODE_EXTRA_CA_CERTS=~/piworker-ca.crt claude
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
- The editor's new-token dialog gives the token-file command for macOS/Linux
  (`umask 077`, file recreated) and for Windows (PowerShell, file recreated,
  `icacls` restricted to the current user's SID, stopping before the token is
  written if it fails), both reading the token at a silent prompt so it stays
  out of the shell history, with the matching `claude_desktop_config.json`:
  `"command"` is the bridge's full path (Claude Desktop does not use the shell
  `PATH`), `--token-file ~/.piworker-token` (the bridge expands `~`), and with
  TLS off `--url http://127.0.0.1:<port>/mcp` (or `[::1]`), the only plain-HTTP
  destinations the bridge accepts, with a note recommending `mode = "auto"` for
  other computers.

## 13. Testing

TDD throughout; `-race` for runtime and service. E2E in `main_mcp_test.go`
with a real `mcp.NewClient` over `StreamableClientTransport` against
`httptest.NewTLSServer`-style setup using the generated CA: filtered
`ListTools`, annotations, `CallTool` structured content, invalid args → isError,
401/403 cases, hidden tool not callable, foreign `runId` rejected, approval
flow, read redaction canaries (one canary per string/map config field of every
registered node type), drafts round-trip, agent-deploy policy levels. Golden
raw JSON-RPC tests for protocol versions 2025-06-18, 2025-11-25 and 2026-07-28.
Binary size measured for `build-arm64` and `build-arm` (`-s -w`).
