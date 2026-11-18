package api

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A setup that fails in the database (a full disk, a read-only file) answers
// a generic 500, so the cause has to reach the log: otherwise the admin has
// nothing to go on. The code stays usable for a retry.
func TestSetupDatabaseFailureIsLoggedAndKeepsTheCode(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Logger
	log.Logger = zerolog.New(&buf)
	t.Cleanup(func() { log.Logger = prev })

	path := filepath.Join(t.TempDir(), "auth.db")
	store, err := storage.NewSQLiteUserStore(path)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	h := NewAuthHandler(store, false, nil)
	router := sessionRouterFor(h)
	code, err := store.NewAuthCode("setup", "", time.Now())
	require.NoError(t, err)

	raw, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	_, err = raw.Exec(`CREATE TRIGGER fail_insert BEFORE INSERT ON users BEGIN SELECT RAISE(ABORT, 'simulated disk full'); END`)
	require.NoError(t, err)

	body := map[string]string{"username": "admin", "password": "test-password", "code": code}
	w := authRequest(router, "POST", "/api/auth/setup", body, nil)
	require.Equal(t, 500, w.Code)
	assert.Contains(t, buf.String(), "simulated disk full")
	assert.Contains(t, buf.String(), "setup")
	assert.NotContains(t, buf.String(), code, "the code is a credential")
	assert.NotContains(t, buf.String(), "test-password")

	_, err = raw.Exec(`DROP TRIGGER fail_insert`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	assert.Equal(t, 201, authRequest(router, "POST", "/api/auth/setup", body, nil).Code, "the same code works once the cause is fixed")
}
