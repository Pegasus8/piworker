# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

PiWorker is a visual flow-based automation system (Node-RED-style): a Go backend
runs flows of connected nodes; a Vue 3 editor builds them. The README has the
full command/feature catalog — this file covers the non-obvious build steps and
the architecture you need to read multiple files to understand.

## Development workflow (TDD)

**TDD is the project convention.** Write the failing test first, then the minimum
code to make it pass, then refactor (red → green → refactor). No feature is
considered done without a test that failed first. Keep tests next to the code
(`_test.go`) — the "Adding a new node" recipe below already requires this. Use
`make test`, the per-package shortcuts
(`test-flow|test-node|test-storage|test-api|test-builtin`), and **`make test-race`**
for anything concurrency-sensitive (the flow runtime especially). Enforcement is by
convention and discipline — there is no coverage gate, no test-running pre-commit
hook, and CI runs tests, race detection, frontend and release builds.

## Architecture decisions (ADRs)

**Significant decisions are recorded as ADRs in `docs/adr/`** (process, index and
template in [`docs/adr/README.md`](docs/adr/README.md)). Rules:

- **Read the relevant ADRs before changing** auth/CSRF/host checks, TLS, webhooks,
  configuration parsing, runtime delivery/deploy semantics, flow validation, node
  lifecycle, or anything MCP/agent-related. Do not silently undo an accepted ADR.
- **Write an ADR in the same change** (status Proposed → Accepted on merge) when
  you add, remove or replace a component, process, listener or package boundary;
  change a REST/MCP,
  flow-model, node-interface, storage or configuration contract; change the
  security model or what agents may do; change runtime semantics; add a
  significant dependency or drop a platform; or reverse an earlier ADR. The list
  in `docs/adr/README.md` is canonical.
- To change a decision, write a **new ADR that supersedes** the old one and mark
  the old one "Superseded by ADR-NNNN"; never rewrite an accepted decision.
- Long designs go in `docs/design/` and are linked from the ADR. Reference the ADR
  number in the commit message or PR description. Bug fixes, contract-preserving
  refactors, new nodes following the recipe below, UI and docs need no ADR.

## Build & run

SQLite (`mattn/go-sqlite3`) is a **cgo** dependency, so builds need a C compiler
and `CGO_ENABLED=1` (the default locally; must be set explicitly when cross-
compiling for ARM/Raspberry Pi).

The frontend is **embedded** into the binary via `go:embed` from
`internal/webui/dist/`, which is populated by copying `web/dist/`. Editing files
under `web/` has **no effect on the binary** until you re-run the embed step.
Canonical full build:

```bash
make frontend-embed && go build -o build/piworker .   # embed UI, then build
./build/piworker -auth=false                           # HTTPS :8443 + redirect :8080
```

- `make build` compiles the root main package (`.`) but does **not** embed the frontend — use
  `make build-all` / `make build-release` (both run `frontend-embed` first) when
  the UI must be current.
- Local dev: `go run . -tls=off -addr 127.0.0.1:8080 -debug -auth=false` (plain
  HTTP backend :8080) + `cd web && bun run dev` (UI :3000, proxies `/api` to
  `http://localhost:8080`). Without `-tls=off` startup fails: HTTPS `addr` and
  the redirect `http_addr` (default :8080) cannot share a port. The frontend
  uses **Bun**. Restart Go after backend edits; Vite provides frontend HMR.
- Run one Go test: `go test -run TestName ./internal/flow/...`. Per-package
  shortcuts exist (`make test-flow|test-node|test-storage|test-api|test-builtin`),
  plus `make test-race` (the runtime has dedicated concurrency tests).
- `make hooks-install` wires a pre-commit hook (`scripts/hooks`) that auto-gofmts
  staged files; `make check` runs fmt-check + vet + lint + frontend typecheck.

## Architecture

Layering: **Vue editor → REST API (`internal/api`) → RuntimeManager
(`internal/flow`) → Node instances (`internal/node`)**, with `internal/storage`
(SQLite) for persistence. `internal/types` holds `Message`/`Port`/etc. and exists
solely to break the import cycle between `flow` and `node`.

### Node system (the core abstraction)
- Every node implements `node.Node` (`Process`, `Ports`, `Validate`). Triggers
  additionally implement `node.TriggerNode` (`Start(ctx, out)`/`Stop`); they
  *push* into a channel rather than being driven by inputs. Settings-bearing
  nodes implement `node.ConfigurableNode` and embed `*node.BaseNode` for the
  `GetConfigX` helpers.
- Nodes **self-register**: each builtin has a `New…` factory + a `…Info()`
  returning `node.NodeTypeInfo`, and an `init()` that calls `node.Register(info, factory)`.
  `main.go` imports the package for side-effects only:
  `_ "github.com/Pegasus8/piworker/internal/node/builtin"`. There is one global
  `node.DefaultRegistry`.
- The registry **derives the UI config form** from a throwaway probe instance's
  `GetConfigSchema()` when `Info.Config` is nil — so a node's schema is its single
  source of truth and the frontend renders the settings form automatically.

### Flow runtime (`internal/flow/runtime.go`, `router.go`)
Each deployed flow gets a `FlowRuntime`. Execution model:
- One **serial executor goroutine per node** (preserves message order for
  stateful nodes) draining a per-node channel, gated by a shared **semaphore**
  (`DefaultMaxConcurrency = 16`) so one slow/blocking node can't stall unrelated
  branches and fan-out can't spawn unbounded work.
- The `MessageRouter` builds an adjacency map from connections and **clones each
  message per target** (no shared mutable payloads). A `dispatch()` goroutine
  fans router output to the per-node executors.
- **Retries are opt-in and idempotency-preserving**: `IsRetryable` returns false
  unless the error is wrapped with `node.Transient(err)`. Default (permanent)
  errors are never retried, so side-effecting actions aren't blindly re-run; mark
  only genuinely transient failures transient. Backoff config in `RetryConfig`.
- **Multi-output routing is fail-loud**: in `executeNode`, an output with no
  `SourcePort` is auto-assigned only when the node has exactly one output port.
  With >1 output it is **dropped with a warning**. Switch/filter-style nodes must
  set `msg.SourcePort` on every emitted message.
- `RuntimeManager.Deploy/Undeploy` own the lifecycle; flows that were running are
  restored on startup (`restoreRunningFlows` in `main.go`).

### Shared process-wide singletons
- `webhook.DefaultHub` — webhook trigger nodes register a path on `Start`; the
  HTTP server mounts the hub at `/webhooks/` and dispatches matching requests.
  This is the bridge that lets pull-only trigger nodes react to inbound HTTP.
- `vars.DefaultStore` — in-memory key/value store backing the `set-var`/`get-var`
  nodes (persisted to SQLite through the attached store).
- Shared builtin helpers live alongside the nodes: `evalExpr` (sandboxed
  `expr-lang`, used by transform/filter/switch/set-var; compiled with
  `expr_guard.go`, so loops stop at the evaluation's deadline in every flow),
  `renderTemplate`, and `inferDataType`. Reuse these rather than re-implementing.
- Nodes resolve secrets and variables **through their context** when they run
  (`secrets.FromContext`, `vars.FromContext`/`vars.Scoped`), never in a factory, and start
  goroutines with `node.Go`: that is what lets a dry-run swap in empty stores
  and a `node.Sandbox` (ADR-0020).
- The tool-name registry (`builtin/tool_names.go`) — a `trigger-tool` claims its
  `toolName` on `Start` (duplicate → `node.ErrResourceConflict` → 409) and
  releases it on `Stop`.

### Agent tools (MCP; ADR-0009…0021, `docs/design/mcp.md`)
- A flow becomes a tool through a `trigger-tool` node (input schema, `confirm`,
  limits) answered by `action-reply`. `internal/toolschema` compiles the closed
  schema subset and validates arguments; `flow/risk.go` (`AnalyzeTools`) rates
  each tool at deploy (risk, annotations, fingerprint; errors → 400) and
  `RuntimeManager.Tools()` publishes the snapshot. `FlowRuntime.Call` runs a
  tool synchronously (busy refusals wrap `flow.ErrBusy`).
- `internal/service` is the **single path** for every tool call — the REST twin
  `POST /api/flows/{id}/call/{nodeId}` (editor "Test tool") and MCP alike:
  permission and grant fingerprint, schema, approval (`confirm: admin`), slots
  and cooldown, secret redaction and result caps, audit. Don't call
  `FlowRuntime.Call` from handlers directly.
- `internal/agentauth` verifies `pwk_` tokens (cache + `tokens_epoch`); tokens,
  grants, approvals and audit live in SQLite (`api_tokens`, `agent_approvals`,
  `mcp_audit`, `mcp_rejections`). Agent-supplied text is shown in the editor as
  plain text with hidden characters escaped (`components/agent/AgentText.vue`).
- `internal/mcpserver` serves `/mcp` (outside `/api`, so no CSRF/request guard):
  go-sdk Streamable HTTP, stateless, JSON responses, one cached `*mcp.Server`
  per token holding only what `service.Tools.List` allows plus the built-in
  `pw_*` tools and prompts (`StaticTool`/`StaticPrompt`; read tools wrap
  `service.Reads`). Middleware order: loopback (TLS off) → Host → Origin →
  write deadline → Bearer (own failure limiter) → per-token bucket → scope.
  Unknown and hidden tool names both go to the service (same `not_found`).
  `mountMCP` (`main.go`) registers it before the UI catch-all; with
  `mcp.enabled = false` it answers a JSON 404.
- `cmd/piworker-mcp` is the stdio bridge for Claude Desktop: pure Go
  (`CGO_ENABLED=0`, a test enforces it), a protocol-agnostic JSON-RPC relay
  that pins the CA; built for six OS/arch pairs by `make build-mcp-bridge-all`.
- Background polls from the editor send `X-PiWorker-Background: 1` (or use a
  passive path) so they never extend the session (ADR-0017).
- **Authoring** (ADR-0018…0021, design §10): agents never write flows, only
  drafts (`flow_drafts`) through `service.Authoring` (`pw_save_draft`, ops or a
  whole flow against a `baseVersion`; stale → `conflict`). The admin approves
  them on `/agents/drafts`. Every definition save is a numbered version in
  `flow_versions` (`UpdateFlowVersioned`; deploy/stop are `UpdateFlowState`,
  not versions); editor saves and deploys send the loaded `version` and get 409
  on a stale one. `flow.Lint` reports every issue from the **same collectors
  deploy runs** (keep it that way: a flow without Lint errors deploys);
  `flow.Risk`; `flow.Diff` gives field diffs plus the alerts that define
  `agent_deploy = low` (`internal/service/deploy.go`). Agent deploys go
  through the editor's lifecycle path (`FlowsHandler.WithLock`, one lifecycle)
  and record the deployment marker. `flow.DryRun`/`flow.TestNode` run in a
  private runtime from `Registry.Derive`: non-`safe` nodes are stubs, stores
  and secrets are empty, one run at a time.
- Agent-facing output **never depends on comparing the agent's text with a
  hidden value** (ADR-0021): echo what the agent wrote, withhold the admin's
  hidden content whole. Test new agent-facing paths by sending a secret's value
  and expecting it back unchanged.

### Storage, auth, config
- `internal/storage`: SQLite tuned in `buildDSN` (WAL, `busy_timeout`,
  `_txlock=immediate`) with `SetMaxOpenConns(1)` (single writer). Flows are
  stored as a JSON blob in the `data` column with a `version`, each definition
  also in `flow_versions` (newest 50 plus the last admin-approved); a
  `settings` table persists the legacy settings. The user store **shares the same `*sql.DB` handle**
  (`NewSQLiteUserStoreWithDB(store.DB())`) — don't open a second pool on the file.
- Auth: SQLite-backed opaque sessions with HttpOnly/SameSite=Strict cookies,
  Secure whenever PiWorker serves TLS itself (`session_secure` only matters with
  `tls.mode = "off"` behind an HTTPS proxy).
- HTTPS is native by default (ADR-0011): `[tls] mode = auto|custom|off`. In
  auto mode `internal/tlsca` creates a local CA plus a hot-renewed server
  certificate under `tls/` next to the DB (re-checked every 10 minutes and on
  SIGHUP; custom mode reloads its files on SIGHUP). `addr` (:8443) serves HTTPS;
  `http_addr` (:8080) 308-redirects except `/piworker-ca.crt`, GET/HEAD
  `/api/health` and, with `webhooks_http`, `/webhooks/`; `/mcp` there is refused
  with 400, never redirected (ADR-0016). Auto mode presents [server cert, CA]. Flags `-tls`,
  `-http-addr`, `-tls-regenerate-ca`; code in `main_tls.go`,
  `internal/config/tls.go`, `internal/api/tls.go`. A Host allowlist
  (`internal/api/hostguard.go`, `allowed_hosts`, ADR-0014) guards everything
  except webhooks and the CA download.
  Fresh installs print a one-use setup code; optional admin environment variables
  bootstrap unattended installs. Existing accounts remain unchanged. Local
  `-recover-account` issues a one-use recovery code; password changes revoke all
  sessions. No Bearer JWT compatibility. Auth state is queried from the server.
- Config precedence is **flags > env (`PIWORKER_*`) > `piworker.toml` >
  defaults**. `config.Load` layers defaults+TOML+env; `applyFlagOverrides` in
  `main.go` overlays only explicitly-set flags (via `flag.Visit`).

### Frontend (`web/`)
Vue 3 + Vue Flow canvas + Pinia + Tailwind/shadcn-vue. `App.vue` keeps a persistent
HTTP warning above the routed view; `HttpWarning.vue` checks the browser URL
protocol, independently of authentication, and links to the Trust this device
page (`views/TrustView.vue`, `/trust`, also reachable from Settings: CA
download, fingerprint, per-OS instructions).
API calls go through
`composables/useApi.ts` (axios, baseURL `/api`, HttpOnly session cookie and CSRF request header,
401 → redirect to login). Responses use a `{ success, data }` envelope, hence the
`data.data` unwrapping in the composable. Flow editing state is the
`stores/flows.ts` Pinia store; the canvas is `components/flow/FlowCanvas.vue`.
Agent access (`/agents`, `/agents/approvals`, `/agents/audit`) uses
`stores/agent.ts`; background polling goes through `lib/poller.ts` (pauses
while the tab is hidden).

## Adding a new node (most common extension)
1. Create `internal/node/builtin/<name>.go`. Implement `node.Node` (+ `TriggerNode`
   if it initiates flows, + `ConfigurableNode` for settings); embed `*node.BaseNode`.
2. Write a `…Info()` returning `NodeTypeInfo` (type id, category, ports, markdown
   `Documentation`, icon, **`Risk`**: `safe`/`external`/`privileged`) and a
   `New…` factory; register both in `init()`.
3. If it has settings, implement `GetConfigSchema()` — the UI form is generated
   from it and deploy type-checks configs against it (`node.CheckConfig`, 400 on
   mismatch). Mark credential fields `Sensitive`, enum/number/boolean and
   identifier-like fields `AgentVisible`, and give numbers a `Unit` repeated in
   the description. Mark where data goes `Destination` (URLs, hosts, topics,
   recipients, paths, pins), opt-ins that widen access `RiskSwitch`, what
   selects a request's action `Operation`, and rates `Rate` — the `low` agent
   deploy policy is built on these (ADR-0019). Settings that cannot work
   without their secrets implement `node.SecretRequirer`.
   `builtin/schema_policy_test.go` enforces all of this.
   Factories and `Validate` must reject bad config (deploy maps that to 400).
4. Multi-output nodes: declare each output port and set `SourcePort` on every
   emitted message. Add a `_test.go` next to it.
5. Triggers: `Start` returns an error only for invalid config or exclusive
   resources (wrap the latter with `node.ErrResourceConflict` → 409);
   network-dependent triggers connect/retry in the background and never
   fail `Start` for reachability (ADR-0006). Nodes holding resources implement
   `node.Closer` (ADR-0008).

Categories are `input` (triggers), `processing`, `output` (actions). Node-type id
convention: `trigger-*`, `process-*`, `action-*`, plus `set-var`/`get-var`.

## Notes
- `main.go` wires the router/middleware directly; the `api.Server` struct +
  `SetupMiddleware`/`corsMiddleware` in `internal/api/server.go` are legacy and
  not on the live path — the real CORS allowlist is `makeCorsMiddleware` in
  `main.go`.
- The stray `./piworker` binary and `./piworker.db` at the repo root are old
  artifacts; the build output is `build/piworker`.
- Main branch is `master`; active work is on `revival`.
