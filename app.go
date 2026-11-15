package main

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/Pegasus8/piworker/internal/agentauth"
	"github.com/Pegasus8/piworker/internal/api"
	"github.com/Pegasus8/piworker/internal/backup"
	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/events"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/mcpserver"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/vars"
	"github.com/Pegasus8/piworker/internal/webhook"
	"github.com/Pegasus8/piworker/internal/webui"
	"github.com/Pegasus8/piworker/internal/zigbee"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

// routerOption registers additional routes before the frontend catch-all.
type routerOption func(*mux.Router)

// appServices are the long-lived services buildApp creates besides the
// router: the tool service shared by REST and MCP, the read views of the
// MCP read tools, agent authoring and deploy (drafts, dry-runs, the
// agent_deploy policy) over the editor's lifecycle path, and the MCP audit
// writer that must be flushed on shutdown.
type appServices struct {
	backups   *backup.Service
	tools     *service.Tools
	reads     *service.Reads
	authoring *service.Authoring
	audit     *service.AuditWriter
}

// appDeps are the stores, hubs and runtimes the process shares, built once
// (main, or processDeps) and handed to everything that needs them: nothing
// in buildApp or internal/api reaches for a process-wide singleton.
type appDeps struct {
	store *storage.SQLiteStore
	// users is nil when authentication is disabled.
	users    *storage.SQLiteUserStore
	registry *node.Registry
	runtimes *flow.RuntimeManager
	secrets  *secrets.Store
	// globals are the global variables; flowVars each flow's own.
	globals  *vars.Store
	flowVars *vars.FlowStores
	brokers  *brokers.Store
	zigbee   *zigbee.Manager
	events   *events.Hub
	webhooks *webhook.Hub
	// llm holds the LLM connections and their daily usage; memory the
	// conversations and knowledge the knowledge base of the LLM nodes
	// (ADR-0038).
	llm       *llm.Store
	memory    *llm.Memory
	knowledge *llm.Knowledge
}

// processDeps are the dependencies over the process-wide stores and hubs
// the builtin nodes fall back to (secrets.DefaultStore and the like): the
// one place that names them. The Zigbee manager is the started one, or an
// idle one when none was started (tests).
func processDeps(store *storage.SQLiteStore, runtimes *flow.RuntimeManager, registry *node.Registry, users *storage.SQLiteUserStore) appDeps {
	networks := zigbee.Default
	if networks == nil {
		networks = zigbee.NewManager(brokers.DefaultStore)
	}
	return appDeps{
		store: store, users: users, registry: registry, runtimes: runtimes,
		secrets:   secrets.DefaultStore,
		globals:   vars.DefaultStore,
		flowVars:  vars.DefaultFlows,
		brokers:   brokers.DefaultStore,
		zigbee:    networks,
		events:    events.DefaultHub,
		webhooks:  webhook.DefaultHub,
		llm:       llm.DefaultStore,
		memory:    llm.DefaultMemory,
		knowledge: llm.DefaultKnowledge,
	}
}

// attachLLM loads the LLM connections (with today's usage), the
// conversations and the knowledge base from deps.store and writes their
// changes through to it.
func attachLLM(deps appDeps) error {
	if err := deps.llm.Attach(deps.store); err != nil {
		return err
	}
	if err := deps.memory.Attach(deps.store); err != nil {
		return err
	}
	return deps.knowledge.Attach(deps.store)
}

// openDeps opens the installation's state and builds what runs on it: the
// database, the persistent secrets, connections and variables, the Zigbee
// watcher, the node registry, the history recorder (run until ctx is done;
// the returned channel closes when it has flushed), the runtime manager and,
// with authentication, the accounts. Any failure is fatal. The returned
// function closes what it opened, in reverse order.
func openDeps(cfg config.Config, ctx context.Context) (appDeps, <-chan struct{}, func()) {
	store, err := storage.NewSQLiteStore(cfg.DBPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize storage")
	}
	log.Info().Msg("Storage initialized")
	deps := processDeps(store, nil, node.DefaultRegistry, nil)

	// Persisted secrets, with write-through, so nodes can reference
	// {{secret.NAME}} instead of embedding plaintext.
	if err := deps.secrets.Attach(store); err != nil {
		log.Fatal().Err(err).Msg("Failed to load secrets")
	}
	// Named MQTT broker connections (ADR-0026).
	if err := deps.brokers.Attach(store); err != nil {
		log.Fatal().Err(err).Msg("Failed to load broker connections")
	}
	// LLM connections, conversations and the knowledge base (ADR-0038).
	if err := attachLLM(deps); err != nil {
		log.Fatal().Err(err).Msg("Failed to load the LLM connections, conversations and knowledge base")
	}
	// Watch each connection's Zigbee2MQTT network: device discovery for the
	// editor and the device states expressions read (zigbee.Default).
	deps.zigbee = zigbee.NewManager(deps.brokers)
	zigbee.Default = deps.zigbee
	deps.zigbee.Start()
	// Variables (set-var/get-var) survive restarts: the global ones and
	// each flow's own.
	if err := deps.globals.Attach(store); err != nil {
		log.Fatal().Err(err).Msg("Failed to load variables")
	}
	deps.flowVars.Factory = store.FlowVariables

	// The registry was populated by the builtin package's init functions.
	log.Info().Int("nodeTypes", deps.registry.Count()).Msg("Node registry loaded")
	for _, info := range deps.registry.List() {
		log.Debug().Str("type", info.Type).Str("name", info.Name).Str("category", string(info.Category)).Msg("Registered node type")
	}

	// Observability: the recorder bridges the runtime's per-node events to
	// SQLite (run history) and the events hub (live SSE). Created before the
	// runtime manager so it observes every deployed flow, including those
	// restored on startup.
	recorder := events.NewRecorder(store, events.WithHub(deps.events), events.WithLogger(log.Logger))
	recorderDone := make(chan struct{})
	go func() {
		recorder.Run(ctx)
		close(recorderDone)
	}()
	deps.runtimes = flow.NewRuntimeManager(deps.registry, log.Logger,
		flow.WithManagerObserver(recorder.Observe),
		flow.WithRuntimeOptions(flow.WithFlowVariables(deps.flowVars)))

	if cfg.Auth {
		deps.users = openUsers(cfg, store)
	}
	return deps, recorderDone, func() {
		if deps.users != nil {
			deps.users.Close()
		}
		deps.zigbee.Stop()
		store.Close()
	}
}

// openUsers opens the accounts on the flow store's database handle (one
// connection pool per file), bootstraps the administrator from the
// configuration when asked to, and prints a setup code while no account
// exists. Any failure is fatal.
func openUsers(cfg config.Config, store *storage.SQLiteStore) *storage.SQLiteUserStore {
	users, err := storage.NewSQLiteUserStoreWithDB(store.DB())
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize user store")
	}
	// Optional unattended bootstrap; otherwise the installer claims the account with a code.
	if err := ensureDefaultAdmin(users, cfg); err != nil {
		log.Fatal().Err(err).Msg("Failed to setup admin user")
	}
	exists, err := users.UserExists()
	if err != nil {
		log.Fatal().Err(err).Msg("Cannot read accounts")
	}
	if !exists {
		code, err := users.NewAuthCode("setup", "", time.Now())
		if err != nil {
			log.Fatal().Err(err).Msg("Cannot generate setup code")
		}
		fmt.Printf("Setup code (one use, expires in 30 minutes): %s\nOpen PiWorker to create your administrator account.\n", code)
	}
	return users
}

// buildRouter wires middleware and every HTTP route over the process-wide
// stores. users is nil when authentication is disabled.
func buildRouter(cfg config.Config, store *storage.SQLiteStore, runtimeManager *flow.RuntimeManager, registry *node.Registry, users *storage.SQLiteUserStore, opts ...routerOption) (*mux.Router, *service.Lifecycle) {
	router, flows, _ := buildApp(cfg, processDeps(store, runtimeManager, registry, users), opts...)
	return router, flows
}

// buildApp wires middleware and every HTTP route over deps, and returns the
// lifecycle path and the services main shuts down.
func buildApp(cfg config.Config, deps appDeps, opts ...routerOption) (*mux.Router, *service.Lifecycle, appServices) {
	store, users, registry, runtimeManager := deps.store, deps.users, deps.registry, deps.runtimes
	router := mux.NewRouter()

	// CORS allowed origins (from config).
	corsOrigins := cfg.CORSOrigins
	log.Info().
		Strs("origins", corsOrigins).
		Msg("CORS allowed origins configured")

	// Setup middleware. Recovery is outermost so it catches panics from every
	// downstream handler; then logging, metrics, and CORS.
	router.Use(api.RecoveryMiddleware(log.Logger))
	router.Use(api.LoggingMiddleware(log.Logger))
	router.Use(api.MetricsMiddleware)
	// Host allowlist (DNS rebinding) before CORS, so preflights from unknown
	// names are refused too. /webhooks/ and the CA certificate are exempt.
	hostGuard := api.NewHostGuard(api.HostGuardOptions{AllowedHosts: cfg.AllowedHosts})
	log.Info().Strs("hosts", hostGuard.Names()).Msg("Accepted Host names (plus any IP address)")
	router.Use(hostGuard.Middleware)
	router.Use(makeCorsMiddleware(corsOrigins))
	// CSRF + JSON Content-Type enforcement for unsafe /api/ requests. Installed
	// unconditionally: with auth disabled it is the only thing stopping a page in
	// the admin's browser from creating and deploying flows.
	router.Use(api.RequestGuardMiddleware(cfg.SessionSecure, cfg.CORSOrigins))

	// The tool service is shared by the editor's REST twin and the MCP
	// endpoint; agent tokens only exist with authentication.
	var agentTokens *agentauth.TokenVerifier
	var tokenEvictor service.TokenEvictor
	if users != nil {
		agentTokens = agentauth.NewTokenVerifier(store, agentauth.WithLogger(log.Logger))
		tokenEvictor = agentTokens
	}
	services := appServices{audit: service.NewAuditWriter(store, service.AuditOptions{Logger: log.Logger})}
	services.tools = service.NewTools(service.Options{
		Runtimes:       runtimeManager,
		Store:          store,
		Secrets:        deps.secrets,
		Audit:          services.audit,
		Tokens:         tokenEvictor,
		MaxInflight:    cfg.MCP.MaxInflight,
		MaxResultBytes: cfg.MCP.MaxResultBytes,
		Logger:         log.Logger,
	})
	// Agent nodes (process-llm-agent) call flow tools through the same
	// service (ADR-0039); set before any flow is deployed or restored.
	runtimeManager.SetToolCaller(services.tools)
	// What agents with the read scope may observe (redacted, bounded,
	// audited).
	services.reads = service.NewReads(service.ReadsOptions{
		Flows:    store,
		History:  store,
		Runtimes: runtimeManager,
		Registry: registry,
		Secrets:  deps.secrets,
		Settings: store,
		Version:  version,
		DataDir:  filepath.Dir(cfg.DBPath),
		MCP:      cfg.MCP,
		Audit:    services.audit,
	})

	// Kept for the step-up of sensitive editor actions (exporting secret
	// values); nil while authentication is disabled.
	var authHandler *api.AuthHandler
	if users != nil {
		// With native HTTPS the session cookie is always Secure.
		authHandler = api.NewAuthHandler(users, cfg.SessionSecure, cfg.CORSOrigins, api.WithSecureCookies(cfg.TLSEnabled()))
		authHandler.RegisterRoutes(router)
		router.Use(api.AuthMiddleware(authHandler))
		log.Info().Msg("Session authentication enabled")

		// Agent tokens, approvals, audit and settings (editor side of MCP).
		// Recovery and password changes revoke tokens through the verifier;
		// approved tool calls run through the tool service.
		authHandler.SetAgentTokenRevoker(agentTokens)
		api.NewAgentHandler(store, authHandler, agentTokens, api.AgentHandlerOptions{
			AuthorEnabled: cfg.MCP.AuthorEnabled,
			MCPEnabled:    cfg.MCP.Enabled,
			LANAccess:     cfg.MCP.LANAccess,
			Executor:      services.tools,
			Logger:        log.Logger,
		}).RegisterRoutes(router)
		api.NewAgentToolsHandler(runtimeManager).RegisterRoutes(router)
	} else {
		router.HandleFunc("/api/auth/status", api.AuthStatusHandler(false)).Methods(http.MethodGet)
		log.Warn().Msg("Authentication disabled - API is unprotected")
	}

	// Register handlers
	// The one lifecycle path (ADR-0034): the editor's requests, agent
	// authoring and deploy, connection changes and the restore at startup
	// share its lock, the restart intent and the deployment marker.
	lifecycle := service.NewLifecycle(store, runtimeManager, log.Logger)
	api.NewFlowsHandler(store, lifecycle, registry, log.Logger, deps.flowVars).RegisterRoutes(router)
	services.authoring = service.NewAuthoring(service.AuthoringOptions{
		Store:         store,
		Lifecycle:     lifecycle,
		Registry:      registry,
		Secrets:       deps.secrets,
		Audit:         services.audit,
		AuthorEnabled: cfg.MCP.AuthorEnabled,
		Logger:        log.Logger,
	})
	drafts := api.NewDraftsHandler(services.authoring, log.Logger)
	if users != nil {
		// Drafts exist only with agent tokens, hence with authentication.
		drafts.RegisterRoutes(router)
	} else {
		drafts.RegisterVersionRoutes(router)
	}
	// The editor's "Test tool" (REST twin of an MCP tool call).
	api.NewToolCallHandler(services.tools, log.Logger).RegisterRoutes(router)

	api.NewNodeTypesHandler(registry, log.Logger, store).WithVariables(deps.globals, deps.flowVars).RegisterRoutes(router)

	// Observability endpoints: live SSE stream + run/event history. Registered
	// under /api so AuthMiddleware applies; the frontend consumes the stream with
	// fetch + ReadableStream with the same HttpOnly session cookie as other
	// API calls (no token in JavaScript or URLs).
	eventsHandler := api.NewEventsHandler(deps.events, store, log.Logger)
	eventsHandler.RegisterRoutes(router)

	// Secrets management (names listable, values write-only).
	secretsHandler := api.NewSecretsHandler(deps.secrets, log.Logger).WithUsage(store, deps.brokers).WithLLMConnections(deps.llm)
	secretsHandler.RegisterRoutes(router)
	api.NewVariablesHandler(deps.globals).RegisterRoutes(router)
	// Broker connections (passwords write-only).
	api.NewConnectionsHandler(deps.brokers, brokers.Tester{}, log.Logger).WithFlows(lifecycle).RegisterRoutes(router)
	// LLM connections (API keys write-only), what the LLM nodes keep
	// (conversations, knowledge base) and the flow assistant (ADR-0038,
	// ADR-0041).
	api.NewLLMConnectionsHandler(deps.llm, deps.secrets, log.Logger).WithFlows(lifecycle).RegisterRoutes(router)
	api.NewLLMMemoryHandler(deps.memory, deps.knowledge, log.Logger).RegisterRoutes(router)
	api.NewAssistantHandler(service.NewAssistant(service.AssistantOptions{
		Registry: registry, LLM: deps.llm, Brokers: deps.brokers, Secrets: deps.secrets, Logger: log.Logger,
	}), log.Logger).RegisterRoutes(router)
	// Automatic backups and whole-state restore (ADR-0032).
	services.backups = newBackupService(cfg, store)
	api.NewBackupsHandler(api.BackupsOptions{
		Service: services.backups, Auth: authHandler, Restart: requestRestart, Logger: log.Logger,
	}).RegisterRoutes(router)
	// Export and restore of variables, connections and secrets (ADR-0031).
	api.NewStoresBackupHandler(api.StoresBackupOptions{
		Flows: store, Globals: deps.globals, FlowVariables: deps.flowVars,
		Secrets: deps.secrets, Connections: deps.brokers, LLMConnections: deps.llm,
		Auth: authHandler, Restarter: lifecycle, Logger: log.Logger,
	}).RegisterRoutes(router)
	api.NewZigbeeHandler(deps.zigbee).RegisterRoutes(router)

	healthHandler := api.NewHealthHandler(store)
	router.HandleFunc(healthPath, healthHandler.Health).Methods(http.MethodGet, http.MethodHead)

	// Prometheus metrics live under /api so they share the API's auth (and the
	// session cookie, which is scoped to /api).
	router.Handle("/api/metrics", promhttp.Handler()).Methods(http.MethodGet)
	log.Info().Msg("Prometheus metrics endpoint available at /api/metrics")

	for _, opt := range opts {
		opt(router)
	}

	// The author and deploy tools serve the same authoring service as the
	// editor's drafts pages, over the editor's lifecycle path.
	mountMCP(router, cfg, services, agentTokens, hostGuard, mcpBuiltins{authoring: services.authoring, registry: registry, settings: store})

	// Webhook endpoints: trigger-webhook nodes register their paths on the shared
	// hub, and incoming requests are dispatched to them. Public (the node's
	// optional token provides authentication); mounted before the frontend
	// catch-all so it takes precedence.
	router.PathPrefix(webhook.PathPrefix).Handler(deps.webhooks.HandlerFunc())
	log.Info().Str("prefix", webhook.PathPrefix).Msg("Webhook endpoint mounted")

	// Serve embedded frontend for non-API routes
	if webui.IsAvailable() {
		log.Info().Msg("Serving embedded frontend")
		router.PathPrefix("/").Handler(webui.Handler())
	} else {
		log.Warn().Msg("Embedded frontend not available - run frontend dev server separately")
		router.PathPrefix("/").Handler(webui.Handler()) // Serves placeholder page
	}

	// Error handlers for API routes (these are overridden by the PathPrefix above for non-API)
	router.NotFoundHandler = http.HandlerFunc(api.NotFoundHandler)
	router.MethodNotAllowedHandler = http.HandlerFunc(api.MethodNotAllowedHandler)
	return router, lifecycle, services
}

// mountMCP mounts the MCP endpoint (docs/design/mcp.md §8) at /mcp for every
// method, before the web UI catch-all, when mcp.enabled (which requires
// authentication, so agent tokens exist); otherwise /mcp answers a JSON 404.
// OAuth discovery under /.well-known/ always answers a JSON 404, so clients
// fall back to their configured Bearer token instead of an OAuth flow.
func mountMCP(router *mux.Router, cfg config.Config, services appServices, tokens *agentauth.TokenVerifier, hosts *api.HostGuard, builtins mcpBuiltins) {
	notFound := http.HandlerFunc(api.NotFoundHandler)
	router.PathPrefix("/.well-known/").Handler(notFound)
	if !cfg.MCP.Enabled || tokens == nil {
		router.Handle(mcpserver.Path, notFound)
		return
	}
	opts := mcpserver.Options{
		Tools:          services.tools,
		Tokens:         tokens,
		Rejections:     services.audit,
		StaticTools:    mcpserver.ReadTools(services.reads),
		Prompts:        mcpserver.ReadPrompts(services.reads),
		AuthorEnabled:  cfg.MCP.AuthorEnabled,
		TLS:            cfg.TLSEnabled(),
		LANAccess:      cfg.MCP.LANAccess,
		HostAllowed:    hosts.Allowed,
		AllowedOrigins: cfg.CORSOrigins,
		Version:        version,
		Logger:         log.Logger,
	}
	if a := builtins.authoring; a != nil {
		// The author tools (mcp.author_enabled, scope author), the deploy
		// tools (agent_deploy above off, scope deploy) and author_flow; the
		// endpoint audits what runs no service (node types, the prompt).
		opts.StaticTools = append(append(opts.StaticTools, mcpserver.AuthorTools(a, builtins.registry, services.audit)...),
			mcpserver.DeployTools(a)...)
		opts.Prompts = append(opts.Prompts, mcpserver.AuthorPrompts(services.audit)...)
		opts.DeployLevel = builtins.settings
	}
	handler, err := mcpserver.New(opts)
	if err != nil {
		log.Fatal().Err(err).Msg("Cannot set up the MCP endpoint")
	}
	router.Handle(mcpserver.Path, handler)
	log.Info().Str("path", mcpserver.Path).Bool("tls", cfg.TLSEnabled()).Bool("lanAccess", cfg.MCP.LANAccess).Msg("MCP endpoint mounted")
}

// mcpBuiltins are what /mcp serves besides the flow and read tools: with
// the authoring service, the author and deploy tools and author_flow, the
// node types they list and the agent deploy level (settings) that gates the
// deploy tools.
type mcpBuiltins struct {
	authoring *service.Authoring
	registry  *node.Registry
	settings  mcpserver.DeployLevelReader
}

// makeCorsMiddleware creates a CORS middleware that grants cross-origin,
// credentialed access to the allowed origins only, echoing the request's
// origin. It fails closed (ADR-0004): no origins means no cross-origin
// access, never "*" (an empty cors_origins once let any web page read the
// API of a Pi with authentication off).
func makeCorsMiddleware(allowedOrigins []string) mux.MiddlewareFunc {
	// Create a set for fast lookup
	originSet := make(map[string]bool)
	for _, origin := range allowedOrigins {
		originSet[origin] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if origin != "" && originSet[origin] {
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-PiWorker-Request, "+api.BackgroundRequestHeader)
			w.Header().Set("Access-Control-Max-Age", "86400") // 24 hours

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
