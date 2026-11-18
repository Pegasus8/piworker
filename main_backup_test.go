package main

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/backup"
	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The editor reaches the backups through /api/backups.
func TestBackupsRoutesAreMounted(t *testing.T) {
	app := newTestApp(t, false)
	w := app.do("GET", "/api/backups", "", "", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = app.do("POST", "/api/backups", "application/json", "", editorHeaders)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
}

// piworker -restore FILE validates and stages a backup (after backing up
// the current state) without starting a server; the next start applies it
// before opening the database.
func TestRestoreFlagStagesAndStartupApplies(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "piworker")
	build := exec.Command("go", "build", "-o", binary, ".")
	out, err := build.CombinedOutput()
	require.NoError(t, err, string(out))

	dir := t.TempDir()
	conf := filepath.Join(dir, "piworker.toml")
	require.NoError(t, os.WriteFile(conf, []byte("auth = true\n"), 0o600))
	cfg := config.Default()
	cfg.DBPath = filepath.Join(dir, "piworker.db")
	store, err := storage.NewSQLiteStore(cfg.DBPath)
	require.NoError(t, err)
	f := flow.NewFlow("Before")
	require.NoError(t, store.CreateFlow(f))
	info, err := backup.Create(context.Background(), backup.Sources{DB: store.DB()}, filepath.Join(dir, "elsewhere"), backup.ReasonManual, time.Now())
	require.NoError(t, err)
	f.Name = "After"
	require.NoError(t, store.UpdateFlow(f))
	require.NoError(t, store.Close())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-config", conf, "-db", cfg.DBPath, "-restore", filepath.Join(dir, "elsewhere", info.Name))
	cmd.Dir = dir
	out, err = cmd.CombinedOutput()
	require.NoError(t, ctx.Err(), "the command started a server instead of exiting")
	require.NoError(t, err, string(out))
	assert.Contains(t, string(out), "Start PiWorker again")
	assert.True(t, backup.Pending(cfg.DBPath))

	// The current state was backed up into the configured directory first.
	list, err := backup.List(filepath.Join(dir, "backups"))
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, backup.ReasonBeforeRestore, list[0].Manifest.Reason)

	require.NoError(t, applyPendingRestore(cfg))
	assert.False(t, backup.Pending(cfg.DBPath))
	store, err = storage.NewSQLiteStore(cfg.DBPath)
	require.NoError(t, err)
	defer store.Close()
	got, err := store.GetFlow(f.ID)
	require.NoError(t, err)
	assert.Equal(t, "Before", got.Name)

	// An invalid file fails without staging anything.
	bad := filepath.Join(dir, "bad.tar.gz")
	require.NoError(t, os.WriteFile(bad, []byte("nope"), 0o600))
	cmd = exec.CommandContext(ctx, binary, "-config", conf, "-db", cfg.DBPath, "-restore", bad)
	cmd.Dir = dir
	out, err = cmd.CombinedOutput()
	require.Error(t, err, string(out))
	assert.False(t, backup.Pending(cfg.DBPath))
}
