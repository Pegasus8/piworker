package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/Pegasus8/piworker/internal/api"
	"github.com/Pegasus8/piworker/internal/backup"
	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog/log"
)

// applyPendingRestore applies a restore staged by the editor or -restore.
func applyPendingRestore(cfg config.Config) error {
	applied, err := backup.ApplyPending(cfg.DBPath, cfg.TLSDir())
	if err != nil {
		return err
	}
	if applied {
		log.Warn().Str("previous", filepath.Join(filepath.Dir(cfg.DBPath), backup.PreviousDir)).
			Msg("Backup restored; the replaced database and certificates are kept in the previous directory until the next restore")
	}
	return nil
}

// newBackupService is the backup service of an installation's open store.
func newBackupService(cfg config.Config, store *storage.SQLiteStore) *backup.Service {
	return backup.NewService(backup.ServiceOptions{
		Sources:  backup.Sources{DB: store.DB(), TLSDir: cfg.TLSDir(), ConfigPath: resolvedConfigPath, Version: version},
		DBPath:   cfg.DBPath,
		Settings: store,
		Logger:   log.Logger,
	})
}

// runRestoreCommand stages -restore FILE: it validates the file, backs up
// the current state when the database can be read, and exits. The next
// start applies it.
func runRestoreCommand(cfg config.Config) {
	m, err := backup.Stage(*restoreFile, cfg.DBPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Cannot restore this file")
	}
	if store, err := storage.NewSQLiteStore(cfg.DBPath); err != nil {
		log.Warn().Err(err).Msg("Cannot read the current database to back it up first; it is still kept in the previous directory when the restore is applied")
	} else {
		if _, _, err := newBackupService(cfg, store).BackUp(context.Background(), backup.ReasonBeforeRestore); err != nil {
			log.Warn().Err(err).Msg("Cannot back up the current state first; it is still kept in the previous directory when the restore is applied")
		}
		store.Close()
	}
	fmt.Printf("Backup from %s (PiWorker %s) is ready to restore.\nStart PiWorker again (or restart its service) to apply it: accounts, flows, secrets and certificates return to that moment.\n",
		m.Created.Local().Format(time.RFC1123), m.PiWorker)
}

// runOfflineCommand runs -tls-regenerate-ca, -setup-code or -recover-account
// and exits on failure. These are local-only commands: they never start flows
// or listeners.
func runOfflineCommand(cfg config.Config) {
	if *restoreFile != "" {
		runRestoreCommand(cfg)
		return
	}
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
