// PiWorker is a visual flow-based automation system for Raspberry Pi.
// It provides a web interface for creating and managing automation workflows.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/mcpserver"
	"github.com/Pegasus8/piworker/internal/tlsca"
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
	restoreFile    = flag.String("restore", "", "Validate a backup file and stage it; the next start restores it (accounts, flows, secrets, CA), then exit")
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

// exitRestart is the exit status after a restore was staged from the
// editor: systemd (Restart=on-failure) and Docker (restart: unless-stopped)
// start PiWorker again, which applies the restore before opening the
// database.
const exitRestart = 75

// restartRequests receives a request to stop and start again (a staged
// restore).
var restartRequests = make(chan struct{}, 1)

// requestRestart asks main to shut down gracefully and exit with
// exitRestart.
func requestRestart() {
	select {
	case restartRequests <- struct{}{}:
	default:
	}
}

// resolvedConfigPath is the configuration file in use (it may not exist);
// backups include it.
var resolvedConfigPath string

func main() {
	flag.Parse()
	if *showVersion {
		fmt.Printf("PiWorker %s\n", version)
		return
	}
	// Registered first, so it runs after every other deferred cleanup.
	exitCode := 0
	defer func() {
		if exitCode != 0 {
			os.Exit(exitCode)
		}
	}()

	// Load configuration: defaults < TOML file < env vars < explicit flags.
	resolvedConfigPath = config.ResolveConfigPath(*configPath)
	cfg, err := config.Load(resolvedConfigPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}
	applyFlagOverrides(&cfg)

	// Local maintenance commands need only the database and the data
	// directory, not the listeners: they run before the cross-field checks so
	// a listener or TLS conflict (which stops the server) never blocks
	// recovering access offline.
	if *recoveryUser != "" || *setupCode || *tlsRegenerate || *restoreFile != "" {
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

	// A restore staged before the last stop replaces the database and tls/
	// before either is opened (ADR-0032).
	if err := applyPendingRestore(cfg); err != nil {
		log.Fatal().Err(err).Msg("Cannot apply the staged restore; nothing was started")
	}

	// A long-lived context drives everything that runs beside the servers:
	// the history recorder, audit retention, scheduled backups, the TLS
	// watcher.
	appCtx, appCancel := context.WithCancel(context.Background())
	deps, recorderDone, closeDeps := openDeps(cfg, appCtx)
	defer closeDeps()
	store, runtimeManager := deps.store, deps.runtimes

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
	router, lifecycle, services := buildApp(cfg, deps, tlsRoutes(tlsHandler))
	// MCP audit retention (30 days, row cap): at start, then hourly.
	go mcpserver.RunAuditRetention(appCtx, store, mcpserver.RetentionOptions{Logger: log.Logger})
	// Scheduled backups (ADR-0032).
	go services.backups.Run(appCtx)

	// Bind before restoring flows, so a busy port fails before anything runs.
	servers, err := listen(cfg, router, tlsConf)
	if err != nil {
		log.Fatal().Err(err).Msg("Cannot listen")
	}
	// GET /api/tls reports the real ports (the trust page builds links from them).
	tlsHandler.SetPorts(servers.ports())

	// Restore previously running flows
	lifecycle.RestoreRunning(appCtx)

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
	case <-restartRequests:
		log.Warn().Msg("Restarting to apply a restore")
		exitCode = exitRestart
	}

	log.Info().Msg("Shutting down...")
	if err := gracefulShutdown(servers.all(), lifecycle, runtimeManager, 10*time.Second, services.audit.Close); err != nil {
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
	if exitCode == exitRestart {
		log.Warn().Msg("Start PiWorker again if your service manager does not: the restore is applied at the next start")
	}
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
