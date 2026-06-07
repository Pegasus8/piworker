// PiWorker is a visual flow-based automation system for Raspberry Pi.
// It provides a web interface for creating and managing automation workflows.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Pegasus8/piworker/internal/api"
	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/Pegasus8/piworker/internal/webhook"
	"github.com/Pegasus8/piworker/internal/webui"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	// Import builtin nodes to register them
	_ "github.com/Pegasus8/piworker/internal/node/builtin"
)

// Configuration flags
var (
	configPath     = flag.String("config", "", "Path to TOML config file (default: piworker.toml if present)")
	listenAddr     = flag.String("addr", ":8080", "HTTP server listen address")
	dbPath         = flag.String("db", "piworker.db", "SQLite database path")
	debug          = flag.Bool("debug", false, "Enable debug logging")
	jwtSecret      = flag.String("jwt-secret", "", "JWT secret key (required in production)")
	authEnabled    = flag.Bool("auth", true, "Enable JWT authentication (env: PIWORKER_AUTH)")
	allowedOrigins = flag.String("cors-origins", "http://localhost:3000,http://localhost:8080", "Comma-separated list of allowed CORS origins")
	adminUser      = flag.String("admin-user", "", "Default admin username (env: PIWORKER_ADMIN_USER)")
	adminPass      = flag.String("admin-pass", "", "Default admin password (env: PIWORKER_ADMIN_PASS)")
)

func main() {
	flag.Parse()

	// Load configuration: defaults < TOML file < env vars < explicit flags.
	cfg, err := config.Load(config.ResolveConfigPath(*configPath))
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}
	applyFlagOverrides(&cfg)

	// Configure logging
	setupLogging(cfg.Debug)

	log.Info().
		Str("version", "1.0.0").
		Str("addr", cfg.Addr).
		Str("db", cfg.DBPath).
		Msg("Starting PiWorker")

	// Initialize storage
	store, err := storage.NewSQLiteStore(cfg.DBPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize storage")
	}
	defer store.Close()
	log.Info().Msg("Storage initialized")

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

	// Create the runtime manager
	runtimeManager := flow.NewRuntimeManager(registry, log.Logger)

	// Restore previously running flows
	restoreRunningFlows(store, runtimeManager)

	// Create the router
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
	router.Use(makeCorsMiddleware(corsOrigins))

	// Resolve auth setting from config.
	authIsEnabled := cfg.Auth

	// Register auth status endpoint unconditionally (public, no auth required)
	router.HandleFunc("/api/auth/status", api.AuthStatusHandler(authIsEnabled)).Methods(http.MethodGet)

	// Setup auth handler if enabled
	var authHandler *api.AuthHandler
	var userStore *storage.SQLiteUserStore
	if authIsEnabled {
		// Share the flow store's database handle to avoid a second connection
		// pool on the same file.
		var err error
		userStore, err = storage.NewSQLiteUserStoreWithDB(store.DB())
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to initialize user store")
		}
		defer userStore.Close()

		// Ensure default admin exists (fails if no users and no credentials provided)
		if err := ensureDefaultAdmin(userStore, cfg); err != nil {
			log.Fatal().Err(err).Msg("Failed to setup admin user")
		}

		secret, err := resolveJWTSecret(cfg, store)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to resolve JWT secret")
		}
		authHandler = api.NewAuthHandler(
			secret,
			api.WithUserStore(userStore),
			api.WithAuthLogger(log.Logger),
		)
		authHandler.RegisterRoutes(router)
		router.Use(api.AuthMiddleware(authHandler))
		log.Info().Msg("JWT authentication enabled")
	} else {
		log.Warn().Msg("JWT authentication disabled - API is unprotected")
	}

	// Register handlers
	flowsHandler := api.NewFlowsHandler(store, runtimeManager, registry, log.Logger)
	flowsHandler.RegisterRoutes(router)

	nodeTypesHandler := api.NewNodeTypesHandler(registry, log.Logger)
	nodeTypesHandler.RegisterRoutes(router)

	healthHandler := api.NewHealthHandler(store)
	router.HandleFunc("/api/health", healthHandler.Health).Methods(http.MethodGet)

	// Prometheus metrics endpoint
	router.Handle("/metrics", promhttp.Handler()).Methods(http.MethodGet)
	log.Info().Msg("Prometheus metrics endpoint available at /metrics")

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

	// Handle graceful shutdown
	shutdownCtx, cancel := context.WithCancel(context.Background())
	_ = shutdownCtx // Used for future context propagation

	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Create HTTP server
	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start the server in a goroutine
	errCh := make(chan error, 1)
	go func() {
		log.Info().
			Str("addr", cfg.Addr).
			Msg("HTTP server started")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Wait for shutdown signal or error
	select {
	case sig := <-sigCh:
		log.Info().
			Str("signal", sig.String()).
			Msg("Received shutdown signal")
	case err := <-errCh:
		log.Error().Err(err).Msg("Server error")
	}

	// Graceful shutdown
	log.Info().Msg("Shutting down...")

	// Stop all running flows
	runtimeManager.StopAll()

	// Cancel context
	cancel()

	// Shutdown HTTP server with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Error during server shutdown")
	}

	log.Info().Msg("PiWorker stopped")
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

// makeCorsMiddleware creates a CORS middleware with the given allowed origins.
// If no origins are provided, it defaults to allowing any origin (not recommended for production).
func makeCorsMiddleware(allowedOrigins []string) mux.MiddlewareFunc {
	// Create a set for fast lookup
	originSet := make(map[string]bool)
	for _, origin := range allowedOrigins {
		originSet[origin] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Decide the allowed origin. With no allowlist we fall back to "*"
			// (development mode); otherwise we echo a specific allowlisted origin.
			allowOrigin := ""
			if len(allowedOrigins) == 0 {
				allowOrigin = "*"
			} else if originSet[origin] {
				allowOrigin = origin
				w.Header().Set("Vary", "Origin")
			}

			if allowOrigin != "" {
				w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
				// Credentials may ONLY be combined with a specific origin, never
				// with the "*" wildcard (the classic CORS misconfiguration).
				if allowOrigin != "*" {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400") // 24 hours

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// resolveJWTSecret returns the JWT secret. An explicitly configured secret
// (flag/env/TOML) always wins. Otherwise a previously-persisted secret is
// reused, and if none exists one is generated and persisted — so issued tokens
// survive restarts instead of silently logging every user out on each restart.
func resolveJWTSecret(cfg config.Config, store *storage.SQLiteStore) ([]byte, error) {
	if cfg.JWTSecret != "" {
		return []byte(cfg.JWTSecret), nil
	}

	if v, ok, err := store.GetSetting("jwt_secret"); err != nil {
		return nil, fmt.Errorf("failed to read persisted JWT secret: %w", err)
	} else if ok && v != "" {
		return []byte(v), nil
	}

	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, fmt.Errorf("failed to generate JWT secret: %w", err)
	}
	secret := hex.EncodeToString(randomBytes)
	if err := store.SetSetting("jwt_secret", secret); err != nil {
		return nil, fmt.Errorf("failed to persist JWT secret: %w", err)
	}

	log.Info().Msg("Generated and persisted a new JWT secret (set jwt_secret in config to override)")
	return []byte(secret), nil
}

// applyFlagOverrides overlays explicitly-set command-line flags onto cfg, so the
// effective precedence is flags > env > TOML file > defaults.
func applyFlagOverrides(cfg *config.Config) {
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "addr":
			cfg.Addr = *listenAddr
		case "db":
			cfg.DBPath = *dbPath
		case "debug":
			cfg.Debug = *debug
		case "auth":
			cfg.Auth = *authEnabled
		case "jwt-secret":
			cfg.JWTSecret = *jwtSecret
		case "cors-origins":
			cfg.CORSOrigins = config.SplitOrigins(*allowedOrigins)
		case "admin-user":
			cfg.AdminUser = *adminUser
		case "admin-pass":
			cfg.AdminPass = *adminPass
		}
	})
}

// restoreRunningFlows attempts to restart flows that were running before the server stopped.
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
		if err := manager.Deploy(ctx, f); err != nil {
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
		}
	}

	log.Info().
		Int("restored", restored).
		Int("failed", failed).
		Msg("Flow restoration complete")
}

// ensureDefaultAdmin creates a default admin user if no users exist.
// This function fails if no users exist and no admin credentials are provided,
// preventing the server from starting without authentication.
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

	if username == "" || password == "" {
		return fmt.Errorf("no users exist and no admin credentials provided. " +
			"Set PIWORKER_ADMIN_USER and PIWORKER_ADMIN_PASS environment variables")
	}

	if len(password) < 8 {
		return fmt.Errorf("admin password must be at least 8 characters")
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
