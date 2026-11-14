package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/require"
)

// The local account and CA commands only need the database and the data
// directory: a listener or TLS conflict in the configuration (which keeps the
// server from starting) must not also block recovering access offline.
func TestOfflineCommandsIgnoreListenerConflicts(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "piworker")
	build := exec.Command("go", "build", "-o", binary, ".")
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))

	dir := t.TempDir()
	config := filepath.Join(dir, "piworker.toml")
	require.NoError(t, os.WriteFile(config, []byte("addr = \":8443\"\nhttp_addr = \":8443\"\n"), 0o600))
	db := filepath.Join(dir, "piworker.db")

	run := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, append([]string{"-config", config, "-db", db}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, ctx.Err(), "the command started a server instead of exiting")
		return string(out), err
	}
	code := regexp.MustCompile(`code \(one use, expires in 30 minutes\): [A-Za-z0-9_-]{43}`)

	out, err := run("-setup-code")
	require.NoError(t, err, out)
	require.Regexp(t, code, out)

	store, err := storage.NewSQLiteStore(db)
	require.NoError(t, err)
	users, err := storage.NewSQLiteUserStoreWithDB(store.DB())
	require.NoError(t, err)
	require.NoError(t, users.CreateUser(&types.User{Username: "admin", PasswordHash: "x"}))
	require.NoError(t, store.Close())

	out, err = run("-recover-account", "admin")
	require.NoError(t, err, out)
	require.Regexp(t, code, out)

	out, err = run("-tls-regenerate-ca")
	require.NoError(t, err, out)
	require.Contains(t, out, "CA fingerprint: SHA256:")

	// The server itself still refuses the conflicting configuration.
	out, err = run()
	require.Error(t, err, out)
	require.Contains(t, out, "same port")
}
