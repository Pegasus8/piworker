package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTester struct {
	got brokers.Connection
	err error
}

func (f *fakeTester) TestConnection(_ context.Context, c brokers.Connection) error {
	f.got = c
	return f.err
}

func connectionsRouter(t *testing.T) (*mux.Router, *brokers.Store, *fakeTester) {
	t.Helper()
	store := brokers.NewStore()
	tester := &fakeTester{}
	router := mux.NewRouter()
	NewConnectionsHandler(store, tester, zerolog.Nop()).RegisterRoutes(router)
	return router, store, tester
}

func doJSON(t *testing.T, router *mux.Router, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	var env map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	return rr, env
}

// The password is write-only: set or replaced by PUT, kept when the request
// leaves it out, never returned (only hasPassword).
func TestConnectionsCRUDKeepsThePasswordWriteOnly(t *testing.T) {
	router, store, _ := connectionsRouter(t)

	rr, _ := doJSON(t, router, http.MethodPut, "/api/connections/home",
		`{"broker":"tcp://10.0.0.2:1883","username":"pw","password":"s3cret-value","baseTopic":""}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	got, ok := store.Get("home")
	require.True(t, ok)
	assert.Equal(t, "s3cret-value", got.Password)
	assert.Equal(t, brokers.DefaultBaseTopic, got.BaseTopic)

	rr, env := doJSON(t, router, http.MethodGet, "/api/connections", "")
	require.Equal(t, http.StatusOK, rr.Code)
	assert.NotContains(t, rr.Body.String(), "s3cret-value")
	assert.Equal(t, []any{map[string]any{"name": "home", "broker": "tcp://10.0.0.2:1883", "username": "pw",
		"hasPassword": true, "baseTopic": "zigbee2mqtt"}}, env["data"].(map[string]any)["connections"])

	// Without a password the stored one is kept; "" clears it.
	rr, _ = doJSON(t, router, http.MethodPut, "/api/connections/home", `{"broker":"tcp://10.0.0.3:1883","username":"pw"}`)
	require.Equal(t, http.StatusOK, rr.Code)
	got, _ = store.Get("home")
	assert.Equal(t, "s3cret-value", got.Password)
	assert.Equal(t, "tcp://10.0.0.3:1883", got.Broker)
	rr, _ = doJSON(t, router, http.MethodPut, "/api/connections/home", `{"broker":"tcp://10.0.0.3:1883","password":""}`)
	require.Equal(t, http.StatusOK, rr.Code)
	got, _ = store.Get("home")
	assert.Empty(t, got.Password)

	rr, _ = doJSON(t, router, http.MethodDelete, "/api/connections/home", "")
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, store.Names())
	rr, _ = doJSON(t, router, http.MethodDelete, "/api/connections/home", "")
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestConnectionsRejectInvalidSettings(t *testing.T) {
	router, store, _ := connectionsRouter(t)
	for _, c := range []struct{ path, body string }{
		{"/api/connections/my%20home", `{"broker":"tcp://x:1883"}`},
		{"/api/connections/home", `{"broker":"http://x"}`},
		{"/api/connections/home", `{"broker":"tcp://x:1883","baseTopic":"z/#"}`},
		{"/api/connections/home", `not json`},
	} {
		rr, env := doJSON(t, router, http.MethodPut, c.path, c.body)
		assert.Equal(t, http.StatusBadRequest, rr.Code, c.body)
		assert.Equal(t, false, env["success"])
	}
	assert.Empty(t, store.Names())
}

// Test connects with the settings sent (the stored password when none is
// sent) and reports the outcome without storing anything.
func TestConnectionsTest(t *testing.T) {
	router, store, tester := connectionsRouter(t)
	require.NoError(t, store.Set(brokers.Connection{Name: "home", Broker: "tcp://a:1883", Password: "stored-pass"}))

	rr, env := doJSON(t, router, http.MethodPost, "/api/connections/home/test", `{"broker":"tcp://b:1883","username":"u"}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, map[string]any{"ok": true}, env["data"])
	assert.Equal(t, "tcp://b:1883", tester.got.Broker)
	assert.Equal(t, "stored-pass", tester.got.Password, "the stored password when none is sent")
	got, _ := store.Get("home")
	assert.Equal(t, "tcp://a:1883", got.Broker, "nothing stored")

	tester.err = errors.New("connection refused")
	rr, env = doJSON(t, router, http.MethodPost, "/api/connections/new/test", `{"broker":"tcp://b:1883","password":"x"}`)
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, map[string]any{"ok": false, "error": "connection refused"}, env["data"])
	assert.Equal(t, "x", tester.got.Password)
}
