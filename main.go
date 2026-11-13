// PiWorker is a visual flow-based automation system for Raspberry Pi.
// It provides a web interface for creating and managing automation workflows.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/Pegasus8/piworker/internal/agentauth"
	"github.com/Pegasus8/piworker/internal/api"
	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/events"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/mcpserver"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/tlsca"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/Pegasus8/piworker/internal/vars"
	"github.com/Pegasus8/piworker/internal/webhook"
	"github.com/Pegasus8/piworker/internal/webui"
	"github.com/Pegasus8/piworker/internal/zigbee"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	// Import builtin nodes to register them
	_ "github.com/Pegasus8/piworker/internal/node/builtin"
)

// Set by release builds with -ldflags "-X main.version=<tag>".
var version = "dev"

// Configuration flags
var (
	showVersion    = flag.Bool("version", false, "Print PiWorker version and exit")
	sessionSecure  = flag.Bool("session-secure", false, "Require HTTPS for session cookies (env: PIWORKER_SESSION_SECURE)")
	recoveryUser   = flag.String("recover-account", "", "Generate a one-use password recovery code for this username, then exit")
	setupCode      = flag.Bool("setup-code", false, "Generate a new first-installation setup code, then exit")
	configPath     = flag.String("config", "", "Path to TOML config file (default: piworker.toml if present)")
	listenAddr     = flag.String("addr", ":8443", "Listen address: HTTPS, or plain HTTP with -tls=off (env: PIWORKER_ADDR)")
	httpAddr       = flag.String("http-addr", ":8080", "Plain HTTP listener that redirects to HTTPS; empty disables it (env: PIWORKER_HTTP_ADDR)")
	tlsMode        = flag.String("tls", "auto", "TLS mode: auto (local CA), custom or off (env: PIWORKER_TLS_MODE)")
	tlsRegenerate  = flag.Bool("tls-regenerate-ca", false, "Replace the local CA (every device must trust the new one), then exit")
	dbPath         = flag.String("db", "piworker.db", "SQLite database path")
	debug          = flag.Bool("debug", false, "Enable debug logging")
	authEnabled    = flag.Bool("auth", true, "Enable session authentication (env: PIWORKER_AUTH)")
	allowedOrigins = flag.String("cors-origins", "", "Comma-separated origins allowed cross-origin, credentialed access, e.g. http://localhost:3000 for the Vite dev server; empty allows none (env: PIWORKER_CORS_ORIGINS)")
	allowedHosts   = flag.String("allowed-hosts", "", "Comma-separated extra Host names accepted besides localhost, the hostname, hostname.local and IPs (env: PIWORKER_ALLOWED_HOSTS)")
	adminUser      = flag.String("admin-user", "", "Default admin username (env: PIWORKER_ADMIN_USER)")
	adminPass      = flag.String("admin-pass", "", "Default admin password (env: PIWORKER_ADMIN_PASS)")
)

func main() {
	flag.Parse()
	if *showVersion {
		fmt.Printf("PiWorker %s\n", version)
		return
	}

	// Load configuration: defaults < TOML file < env vars < explicit flags.
	cfg, err := config.Load(config.ResolveConfigPath(*configPath))
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}
	applyFlagOverrides(&cfg)

	// Local maintenance commands need only the database and the data
	// directory, not the listeners: they run before the cross-field checks so
	// a listener or TLS conflict (which stops the server) never blocks
	// recovering access offline.
	if *recoveryUser != "" || *setupCode || *tlsRegenerate {
		setupLogging(cfg.Debug)
		runOfflineCommand(cfg)
		return
	}

	// Cross-field checks (e.g. MCP without auth) run before anything is opened.
	if err := cfg.Validate(); err != nil {
		log.Fatal().Err(err).Msg("Invalid configuration")
	}

	// Configure logging
	setupLogging(cfg.Debug)

	log.Info().
		Str("version", version).
		Str("addr", cfg.Addr).
		Str("tls", cfg.TLS.Mode).
		Str("db", cfg.DBPath).
		Msg("Starting PiWorker")

	// Initialize storage
	store, err := storage.NewSQLiteStore(cfg.DBPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize storage")
	}
	defer store.Close()
	log.Info().Msg("Storage initialized")

	// Load persisted secrets into the process-wide store and enable write-through,
	// so nodes can reference {{secret.NAME}} instead of embedding plaintext.
	if err := secrets.DefaultStore.Attach(store); err != nil {
		log.Fatal().Err(err).Msg("Failed to load secrets")
	}

	// Named MQTT broker connections the Zigbee nodes use (ADR-0026).
	if err := brokers.DefaultStore.Attach(store); err != nil {
		log.Fatal().Err(err).Msg("Failed to load broker connections")
	}
	// Watch each connection's Zigbee2MQTT network: device discovery for the
	// editor and the device states expressions read.
	zigbee.Default = zigbee.NewManager(brokers.DefaultStore)
	zigbee.Default.Start()
	defer zigbee.Default.Stop()

	// Persist flow variables (set-var/get-var) so they survive restarts.
	if err := vars.DefaultStore.Attach(store); err != nil {
		log.Fatal().Err(err).Msg("Failed to load variables")
	}

	vars.DefaultFlows.Factory = store.FlowVariables

	// Get the node registry (already populated by init functions in builtin package)
	registry := node.DefaultRegistry
	log.Info().
		Int("nodeTypes", registry.Count()).
		Msg("Node registry loaded")

	// List registered node types
	for _, info := range registry.List() {
		log.Debug().
			Str("type", info.Type).
			Str("name", info.Name).
			Str("category", string(info.Category)).
			Msg("Registered node type")
	}

	// Observability: a long-lived context drives the history recorder, which
	// bridges the runtime's per-node events to SQLite (run history) and the
	// events hub (live SSE). Created before the runtime manager so it observes
	// every deployed flow, including those restored on startup.
	appCtx, appCancel := context.WithCancel(context.Background())
	recorder := events.NewRecorder(store, events.WithHub(events.DefaultHub), events.WithLogger(log.Logger))
	recorderDone := make(chan struct{})
	go func() {
		recorder.Run(appCtx)
		close(recorderDone)
	}()

	// Create the runtime manager, wired to the recorder so every node execution
	// is observed.
	runtimeManager := flow.NewRuntimeManager(registry, log.Logger, flow.WithManagerObserver(recorder.Observe))

	var userStore *storage.SQLiteUserStore
	if cfg.Auth {
		// Share the flow store's database handle to avoid a second connection
		// pool on the same file.
		var err error
		userStore, err = storage.NewSQLiteUserStoreWithDB(store.DB())
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to initialize user store")
		}
		defer userStore.Close()

		// Optional unattended bootstrap; otherwise the installer claims the account with a code.
		if err := ensureDefaultAdmin(userStore, cfg); err != nil {
			log.Fatal().Err(err).Msg("Failed to setup admin user")
		}

		exists, err := userStore.UserExists()
		if err != nil {
			log.Fatal().Err(err).Msg("Cannot read accounts")
		}
		if !exists {
			code, err := userStore.NewAuthCode("setup", "", time.Now())
			if err != nil {
				log.Fatal().Err(err).Msg("Cannot generate setup code")
			}
			fmt.Printf("Setup code (one use, expires in 30 minutes): %s\nOpen PiWorker to create your administrator account.\n", code)
		}
	}

	// HTTPS certificates: the local CA (auto) or the configured files (custom).
	tlsSrc, err := newTLSSource(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Cannot set up HTTPS; to start without it use -tls=off or PIWORKER_TLS_MODE=off")
	}
	var tlsConf *tls.Config
	if tlsSrc != nil {
		tlsConf = tlsSrc.TLSConfig()
	}

	tlsHandler := newTLSHandler(cfg.TLS.Mode, tlsSrc)
	router, flowsHandler, services := buildApp(cfg, store, runtimeManager, registry, userStore, tlsRoutes(tlsHandler))
	// MCP audit retention (30 days, row cap): at start, then hourly.
	go mcpserver.RunAuditRetention(appCtx, store, mcpserver.RetentionOptions{Logger: log.Logger})

	// Bind before restoring flows, so a busy port fails before anything runs.
	servers, err := listen(cfg, router, tlsConf)
	if err != nil {
		log.Fatal().Err(err).Msg("Cannot listen")
	}
	// GET /api/tls reports the real ports (the trust page builds links from them).
	tlsHandler.SetPorts(servers.ports())

	// Restore previously running flows
	restoreRunningFlows(store, runtimeManager)

	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 2)
	servers.serve(errCh)
	announceListeners(servers, tlsSrc)
	watchTLS(appCtx, tlsSrc, tlsca.RefreshInterval)

	// Wait for shutdown signal or error
	select {
	case sig := <-sigCh:
		log.Info().
			Str("signal", sig.String()).
			Msg("Received shutdown signal")
	case err := <-errCh:
		log.Error().Err(err).Msg("Server error")
	}

	log.Info().Msg("Shutting down...")
	if err := gracefulShutdown(servers.all(), flowsHandler, runtimeManager, 10*time.Second, services.audit.Close); err != nil {
		log.Error().Err(err).Msg("Error during server shutdown")
	}

	// Flush and finalize the observability recorder before the store closes
	// (store.Close is deferred and runs after main returns).
	appCancel()
	select {
	case <-recorderDone:
	case <-time.After(2 * time.Second):
		log.Warn().Msg("Recorder did not drain within timeout on shutdown")
	}

	log.Info().Msg("PiWorker stopped")
}

// runOfflineCommand runs -tls-regenerate-ca, -setup-code or -recover-account
// and exits on failure. These are local-only commands: they never start flows
// or listeners.
func runOfflineCommand(cfg config.Config) {
	if *tlsRegenerate {
		fingerprint, err := regenerateCA(cfg)
		if err != nil {
			log.Fatal().Err(err).Msg("Cannot regenerate the local CA")
		}
		fmt.Printf("New local CA in %s\nCA fingerprint: %s\nRestart PiWorker, then trust the new CA on every device; the old one no longer works.\n", cfg.TLSDir(), fingerprint)
		return
	}

	store, err := storage.NewSQLiteStore(cfg.DBPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize storage")
	}
	defer store.Close()
	users, err := storage.NewSQLiteUserStoreWithDB(store.DB())
	if err != nil {
		log.Fatal().Err(err).Msg("Cannot open accounts")
	}
	purpose := "recovery"
	username := *recoveryUser
	if *setupCode {
		purpose = "setup"
		username = ""
	}
	code, err := users.NewAuthCode(purpose, username, time.Now())
	if err != nil {
		log.Fatal().Err(err).Msg("Cannot generate account code")
	}
	fmt.Printf("%s code (one use, expires in 30 minutes): %s\nEnter it on the PiWorker sign-in page.\n", purpose, code)
}

// maxHeaderBytes caps a request's line and headers on every listener: the
// editor's and agents' requests need a few KiB (cookies, a bearer token).
const maxHeaderBytes = 64 << 10

// newHTTPServer builds the HTTP server. Every request context derives from a
// base context that is cancelled as soon as Shutdown begins, so long-lived
// requests that honour r.Context() (the SSE event stream) end promptly instead
// of holding Shutdown until its timeout. Server errors go to zerolog, rate
// limited, with handshake noise at debug level (serverErrorWriter). The
// request line and headers are capped at maxHeaderBytes (Go's default is
// 1 MiB), larger ones refused with 431 before any handler runs.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	baseCtx, cancelBase := context.WithCancel(context.Background())
	srv := &http.Server{
		Addr:           addr,
		Handler:        handler,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   15 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: maxHeaderBytes,
		BaseContext:    func(net.Listener) context.Context { return baseCtx },
		ErrorLog:       newServerErrorWriter(log.Logger, time.Now).logger(),
	}
	srv.RegisterOnShutdown(cancelBase)
	return srv
}

// gracefulShutdown stops the process in dependency order: refuse new deploys,
// drain every HTTP server (which cancels streams via the base context), and
// only then stop the flows, so in-flight API calls never observe a
// half-stopped runtime and nothing can be deployed after StopAll.
// The flushers (the MCP audit writer) run last, once nothing can record
// anything any more, while the store is still open.
func gracefulShutdown(srvs []*http.Server, flows *api.FlowsHandler, manager *flow.RuntimeManager, timeout time.Duration, flushers ...func(context.Context) error) error {
	flows.BeginShutdown()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	errs := make([]error, len(srvs))
	var wg sync.WaitGroup
	for i, srv := range srvs {
		wg.Go(func() { errs[i] = srv.Shutdown(ctx) })
	}
	wg.Wait()
	manager.StopAll()
	flushCtx, cancelFlush := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelFlush()
	for _, flush := range flushers {
		errs = append(errs, flush(flushCtx))
	}
	return errors.Join(errs...)
}

// routerOption registers additional routes before the frontend catch-all.
type routerOption func(*mux.Router)

// appServices are the long-lived services buildApp creates besides the
// router: the tool service shared by REST and MCP, the read views of the
// MCP read tools, agent authoring and deploy (drafts, dry-runs, the
// agent_deploy policy) over the editor's lifecycle path, and the MCP audit
// writer that must be flushed on shutdown.
type appServices struct {
	tools     *service.Tools
	reads     *service.Reads
	authoring *service.Authoring
	audit     *service.AuditWriter
}

// buildRouter wires middleware and every HTTP route. users is nil when
// authentication is disabled.
func buildRouter(cfg config.Config, store *storage.SQLiteStore, runtimeManager *flow.RuntimeManager, registry *node.Registry, users *storage.SQLiteUserStore, opts ...routerOption) (*mux.Router, *api.FlowsHandler) {
	router, flows, _ := buildApp(cfg, store, runtimeManager, registry, users, opts...)
	return router, flows
}

// buildApp is buildRouter that also returns the services main shuts down.
func buildApp(cfg config.Config, store *storage.SQLiteStore, runtimeManager *flow.RuntimeManager, registry *node.Registry, users *storage.SQLiteUserStore, opts ...routerOption) (*mux.Router, *api.FlowsHandler, appServices) {
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
		Secrets:        secrets.DefaultStore,
		Audit:          services.audit,
		Tokens:         tokenEvictor,
		MaxInflight:    cfg.MCP.MaxInflight,
		MaxResultBytes: cfg.MCP.MaxResultBytes,
		Logger:         log.Logger,
	})
	// What agents with the read scope may observe (redacted, bounded,
	// audited).
	services.reads = service.NewReads(service.ReadsOptions{
		Flows:    store,
		History:  store,
		Runtimes: runtimeManager,
		Registry: registry,
		Secrets:  secrets.DefaultStore,
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
	flowsHandler := api.NewFlowsHandler(store, runtimeManager, registry, log.Logger)
	flowsHandler.RegisterRoutes(router)
	// Agent authoring and deploy save and deploy through the editor's
	// lifecycle path (one lock, the restart intent, the deployment marker).
	services.authoring = service.NewAuthoring(service.AuthoringOptions{
		Store:         store,
		Lifecycle:     flowsHandler,
		Registry:      registry,
		Secrets:       secrets.DefaultStore,
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

	nodeTypesHandler := api.NewNodeTypesHandler(registry, log.Logger, store)
	nodeTypesHandler.RegisterRoutes(router)

	// Observability endpoints: live SSE stream + run/event history. Registered
	// under /api so AuthMiddleware applies; the frontend consumes the stream with
	// fetch + ReadableStream with the same HttpOnly session cookie as other
	// API calls (no token in JavaScript or URLs).
	eventsHandler := api.NewEventsHandler(events.DefaultHub, store, log.Logger)
	eventsHandler.RegisterRoutes(router)

	// Secrets management (names listable, values write-only).
	secretsHandler := api.NewSecretsHandler(secrets.DefaultStore, log.Logger).WithUsage(store, brokers.DefaultStore)
	secretsHandler.RegisterRoutes(router)
	api.NewVariablesHandler(vars.DefaultStore).RegisterRoutes(router)
	// Broker connections (passwords write-only).
	api.NewConnectionsHandler(brokers.DefaultStore, brokers.Tester{}, log.Logger).WithFlows(flowsHandler).RegisterRoutes(router)
	// Export and restore of variables, connections and secrets (ADR-0031).
	api.NewStoresBackupHandler(api.StoresBackupOptions{
		Flows: store, Globals: vars.DefaultStore, FlowVariables: vars.DefaultFlows,
		Secrets: secrets.DefaultStore, Connections: brokers.DefaultStore,
		Auth: authHandler, Restarter: flowsHandler, Logger: log.Logger,
	}).RegisterRoutes(router)
	networks := zigbee.Default
	if networks == nil { // not started (tests): no network to show
		networks = zigbee.NewManager(brokers.DefaultStore)
	}
	api.NewZigbeeHandler(networks).RegisterRoutes(router)

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
	router.PathPrefix(webhook.PathPrefix).Handler(webhook.DefaultHub.HandlerFunc())
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
	return router, flowsHandler, services
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

// setupLogging configures the global logger.
func setupLogging(debugMode bool) {
	// Use console writer for better readability
	output := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}

	// Set log level
	level := zerolog.InfoLevel
	if debugMode {
		level = zerolog.DebugLevel
	}

	// Configure global logger
	log.Logger = zerolog.New(output).
		With().
		Timestamp().
		Caller().
		Logger().
		Level(level)

	zerolog.DefaultContextLogger = &log.Logger
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

// applyFlagOverrides overlays explicitly-set command-line flags onto cfg, so the
// effective precedence is flags > env > TOML file > defaults.
func applyFlagOverrides(cfg *config.Config) {
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "addr":
			cfg.Addr = *listenAddr
		case "http-addr":
			cfg.HTTPAddr = *httpAddr
		case "tls":
			// An invalid value is kept as given and reported by Validate.
			cfg.TLS.Mode = *tlsMode
			if mode, err := config.ParseTLSMode(*tlsMode); err == nil {
				cfg.TLS.Mode = mode
			}
		case "db":
			cfg.DBPath = *dbPath
		case "debug":
			cfg.Debug = *debug
		case "auth":
			cfg.Auth = *authEnabled
		case "session-secure":
			cfg.SessionSecure = *sessionSecure
		case "cors-origins":
			cfg.CORSOrigins = config.SplitOrigins(*allowedOrigins)
		case "allowed-hosts":
			cfg.AllowedHosts = config.SplitList(*allowedHosts)
		case "admin-user":
			cfg.AdminUser = *adminUser
		case "admin-pass":
			cfg.AdminPass = *adminPass
		}
	})
}

// restoreRunningFlows attempts to restart flows that were running before the
// server stopped. Settings that no longer pass the deploy-time schema check
// (values saved before it existed) are logged, not fatal: the flow keeps
// running as it did, and the next explicit deploy asks for a fix.
func restoreRunningFlows(store *storage.SQLiteStore, manager *flow.RuntimeManager) {
	runningFlows, err := store.GetRunningFlows()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get previously running flows")
		return
	}

	if len(runningFlows) == 0 {
		log.Debug().Msg("No flows to restore")
		return
	}

	log.Info().Int("count", len(runningFlows)).Msg("Restoring previously running flows")

	ctx := context.Background()
	restored := 0
	failed := 0

	for _, f := range runningFlows {
		issues, err := manager.Restore(ctx, f)
		for _, issue := range issues {
			log.Warn().
				Str("flowID", f.ID).
				Str("flowName", f.Name).
				Str("path", issue.Path).
				Str("problem", issue.Message).
				Msg("Restored flow has a setting that the next deploy will refuse; the node clamps or defaults it as before")
		}
		if err != nil {
			log.Error().
				Err(err).
				Str("flowID", f.ID).
				Str("flowName", f.Name).
				Msg("Failed to restore flow")
			failed++

			// Update flow state to failed in storage
			if updateErr := store.UpdateFlowState(f.ID, flow.FlowStateFailed); updateErr != nil {
				log.Error().Err(updateErr).Str("flowID", f.ID).Msg("Failed to update flow state to failed")
			}
		} else {
			log.Info().
				Str("flowID", f.ID).
				Str("flowName", f.Name).
				Msg("Flow restored successfully")
			restored++
			// What an editor deploy would answer as warnings (an undefined
			// secret an optional credential references, ...): logged, as
			// nobody sees a response at startup.
			var warnings []flow.Issue
			if rt, ok := manager.GetRuntime(f.ID); ok {
				warnings = rt.Warnings()
			}
			for _, is := range warnings {
				log.Warn().Str("flowID", f.ID).Str("flowName", f.Name).Str("code", is.Code).Str("path", is.Path).
					Str("problem", is.Message).Msg("Restored flow has a warning")
			}
			if err := markRestored(store, f); err != nil {
				log.Error().Err(err).Str("flowID", f.ID).Msg("Failed to record who deployed the restored version")
			}
		}
	}

	log.Info().
		Int("restored", restored).
		Int("failed", failed).
		Msg("Flow restoration complete")
}

// markRestored keeps the deployment marker of a restored flow true (H6): a
// restore is not a new deployment, so a marker that names the restored
// version is kept; one that names another version (a redeploy that failed
// after a save left the running intent on the newer version, such as an
// agent's draft) is replaced by the version's own author, who saved it to
// run. A flow without stored versions keeps its marker.
func markRestored(store *storage.SQLiteStore, f *flow.Flow) error {
	d, err := store.GetDeployment(f.ID)
	if err != nil || (d != nil && d.Version == f.Version) {
		return err
	}
	v, err := store.GetFlowVersion(f.ID, f.Version)
	if errors.Is(err, storage.ErrFlowVersionNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return store.RecordDeployment(f.ID, storage.Deployment{Version: f.Version, ActorKind: v.ActorKind, TokenID: v.TokenID, TokenName: v.TokenName})
}

// ensureDefaultAdmin optionally bootstraps the first user from explicit credentials.
// Without credentials the browser setup flow requires a local one-use code.
func ensureDefaultAdmin(store *storage.SQLiteUserStore, cfg config.Config) error {
	exists, err := store.UserExists()
	if err != nil {
		return fmt.Errorf("failed to check user existence: %w", err)
	}

	if exists {
		log.Info().Msg("User store initialized (existing users found)")
		return nil
	}

	// No users exist - require admin credentials
	username := cfg.AdminUser
	password := cfg.AdminPass

	if username == "" && password == "" {
		return nil
	}
	if username == "" || password == "" {
		return fmt.Errorf("no users exist and no admin credentials provided. " +
			"Set PIWORKER_ADMIN_USER and PIWORKER_ADMIN_PASS environment variables")
	}

	username, ok := api.NormalizeNewCredentials(username, password)
	if !ok {
		return fmt.Errorf("invalid admin credentials: username must be 1-%d bytes (surrounding whitespace is ignored) and password 8-72 bytes", api.MaxUsernameLength)
	}

	// Hash password and create user
	hash, err := api.HashPassword(password)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	if err := store.CreateUser(&types.User{
		Username:     username,
		PasswordHash: hash,
	}); err != nil {
		return fmt.Errorf("failed to create admin user: %w", err)
	}

	log.Info().Str("username", username).Msg("Default admin user created")
	return nil
}
