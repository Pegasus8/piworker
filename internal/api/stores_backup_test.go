package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/storesbackup"
	"github.com/Pegasus8/piworker/internal/vars"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type backupEnv struct {
	agentTestEnv
	globals *vars.Store
	flows   *vars.FlowStores
	secrets *secrets.Store
	conns   *brokers.Store
}

// newBackupEnv is a signed-in admin with a flow "Heating" (one flow
// variable), a global variable, a secret and a connection with a password.
func newBackupEnv(t *testing.T, withAuth bool) backupEnv {
	t.Helper()
	env := backupEnv{agentTestEnv: newAgentTestEnv(t, AgentHandlerOptions{}), globals: vars.NewStore(),
		flows: &vars.FlowStores{}, secrets: secrets.NewStore(), conns: brokers.NewStore()}
	require.NoError(t, env.globals.Set("threshold", 21.5))
	f := flow.NewFlow("Heating")
	require.NoError(t, env.store.CreateFlow(f))
	fv, err := env.flows.Get(f.ID)
	require.NoError(t, err)
	require.NoError(t, fv.Set("count", 3.0))
	require.NoError(t, env.secrets.Set("api_key", "zzSECRETzz-api-key-value"))
	require.NoError(t, env.conns.Set(brokers.Connection{Name: "home", Broker: "tcp://10.0.0.2:1883", Password: "zzSECRETzz-broker"}))
	auth := env.auth
	if !withAuth {
		auth = nil
	}
	NewStoresBackupHandler(StoresBackupOptions{
		Flows: env.store, Globals: env.globals, FlowVariables: env.flows, Secrets: env.secrets, Connections: env.conns,
		Auth: auth, Logger: zerolog.Nop(),
	}).RegisterRoutes(env.router)
	return env
}

func (e backupEnv) export(t *testing.T, body map[string]any) (*httptest.ResponseRecorder, *storesbackup.Bundle) {
	t.Helper()
	w := e.do("POST", "/api/stores/export", body)
	if w.Code != http.StatusOK {
		return w, nil
	}
	var b storesbackup.Bundle
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &b), w.Body.String())
	return w, &b
}

// An export without a passphrase is the file itself (an attachment), with
// variables, connections without passwords and secret names only.
func TestExportStoresWithoutSecrets(t *testing.T) {
	env := newBackupEnv(t, true)
	w, b := env.export(t, map[string]any{})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Header().Get("Content-Disposition"), `attachment; filename="piworker-stores-`)
	assert.NotContains(t, w.Body.String(), "zzSECRETzz")
	assert.Equal(t, []string{"api_key"}, b.Secrets)
	require.Len(t, b.FlowVariables, 1)
	assert.Equal(t, "Heating", b.FlowVariables[0].FlowName)
}

// Exporting secret values needs the account password again (secrets are
// otherwise write-only over the API) and a long enough passphrase; with
// authentication disabled it is refused outright.
func TestExportingSecretValuesNeedsStepUpAndPassphrase(t *testing.T) {
	env := newBackupEnv(t, true)
	w, _ := env.export(t, map[string]any{"passphrase": "correct horse battery staple"})
	assert.Equal(t, http.StatusForbidden, w.Code)
	w, _ = env.export(t, map[string]any{"passphrase": "correct horse battery staple", "password": "wrong"})
	assert.Equal(t, http.StatusForbidden, w.Code)
	w, _ = env.export(t, map[string]any{"passphrase": "short", "password": "test-password"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, errorMessage(t, w), "at least 12")

	w, b := env.export(t, map[string]any{"passphrase": "correct horse battery staple", "password": "test-password"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), "zzSECRETzz")
	require.NotNil(t, b.Sealed)

	noAuth := newBackupEnv(t, false)
	w, _ = noAuth.export(t, map[string]any{"passphrase": "correct horse battery staple"})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, errorMessage(t, w), "authentication")
}

// Importing restores into this installation and reports what it did; a
// wrong passphrase or a file of another kind is a 400 that writes nothing.
func TestImportStores(t *testing.T) {
	src := newBackupEnv(t, true)
	_, b := src.export(t, map[string]any{"passphrase": "correct horse battery staple", "password": "test-password"})
	require.NotNil(t, b)

	dst := newBackupEnv(t, true)
	require.NoError(t, dst.secrets.Delete("api_key"))
	require.NoError(t, dst.conns.Delete("home"))

	w := dst.do("POST", "/api/stores/import", map[string]any{"bundle": b, "passphrase": "wrong passphrase here"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, errorMessage(t, w), "wrong passphrase")
	w = dst.do("POST", "/api/stores/import", map[string]any{"bundle": map[string]any{"format": "nope"}})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = dst.do("POST", "/api/stores/import", map[string]any{"bundle": b, "passphrase": "correct horse battery staple"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := data[StoresImportResponse](t, w)
	assert.Equal(t, storesbackup.Counts{Added: 1}, got.Report.Secrets)
	assert.Equal(t, storesbackup.Counts{Added: 1}, got.Report.Connections)
	assert.Equal(t, storesbackup.Counts{Skipped: 1}, got.Report.Variables, "existing values are kept by default")
	assert.NotNil(t, got.Restarted)
	v, _ := dst.secrets.Get("api_key")
	assert.Equal(t, "zzSECRETzz-api-key-value", v)
	assert.False(t, strings.Contains(w.Body.String(), "zzSECRETzz"), "the report never echoes values")
}
