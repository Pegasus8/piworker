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
hook, and no CI.

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
./build/piworker -auth=false                           # run (auth off for local dev)
```

- `make build` compiles `./...` but does **not** embed the frontend — use
  `make build-all` / `make build-release` (both run `frontend-embed` first) when
  the UI must be current.
- Local dev with hot-reload: `go run main.go -debug` (backend :8080) +
  `cd web && bun run dev` (UI :3000, proxies `/api` to the backend). The
  frontend uses **Bun**, not npm/node.
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
  HTTP server mounts the hub at `/api/webhooks/` and dispatches matching requests.
  This is the bridge that lets pull-only trigger nodes react to inbound HTTP.
- `vars.DefaultStore` — in-memory key/value store backing the `set-var`/`get-var`
  nodes (process lifetime, not persisted).
- Shared builtin helpers live alongside the nodes: `evalExpr` (sandboxed
  `expr-lang`, used by transform/filter/switch/set-var), `renderTemplate`, and
  `inferDataType`. Reuse these rather than re-implementing.

### Storage, auth, config
- `internal/storage`: SQLite tuned in `buildDSN` (WAL, `busy_timeout`,
  `_txlock=immediate`) with `SetMaxOpenConns(1)` (single writer). Flows are
  stored as a JSON blob in the `data` column; a `settings` table persists the
  auto-generated JWT secret. The user store **shares the same `*sql.DB` handle**
  (`NewSQLiteUserStoreWithDB(store.DB())`) — don't open a second pool on the file.
- Auth: JWT. The admin user is bootstrapped from `PIWORKER_ADMIN_USER`/`_PASS`
  on first run (server refuses to start with auth on and no users); user
  registration is bootstrap-only. The JWT secret is generated and persisted so
  tokens survive restarts. `-auth=false` disables it for local dev.
- Config precedence is **flags > env (`PIWORKER_*`) > `piworker.toml` >
  defaults**. `config.Load` layers defaults+TOML+env; `applyFlagOverrides` in
  `main.go` overlays only explicitly-set flags (via `flag.Visit`).

### Frontend (`web/`)
Vue 3 + Vue Flow canvas + Pinia + Tailwind/shadcn-vue. API calls go through
`composables/useApi.ts` (axios, baseURL `/api`, Bearer token from localStorage,
401 → redirect to login). Responses use a `{ success, data }` envelope, hence the
`data.data` unwrapping in the composable. Flow editing state is the
`stores/flows.ts` Pinia store; the canvas is `components/flow/FlowCanvas.vue`.

## Adding a new node (most common extension)
1. Create `internal/node/builtin/<name>.go`. Implement `node.Node` (+ `TriggerNode`
   if it initiates flows, + `ConfigurableNode` for settings); embed `*node.BaseNode`.
2. Write a `…Info()` returning `NodeTypeInfo` (type id, category, ports, markdown
   `Documentation`, icon) and a `New…` factory; register both in `init()`.
3. If it has settings, implement `GetConfigSchema()` — the UI form is generated
   from it; no frontend changes needed for the config panel.
4. Multi-output nodes: declare each output port and set `SourcePort` on every
   emitted message. Add a `_test.go` next to it.

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
