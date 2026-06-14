package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func secretsRouter(t *testing.T) (*mux.Router, *secrets.Store) {
	t.Helper()
	store := secrets.NewStore()
	h := NewSecretsHandler(store, zerolog.Nop())
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	return router, store
}

func TestSecretsListReturnsNamesOnly(t *testing.T) {
	router, store := secretsRouter(t)
	require.NoError(t, store.Set("alpha", "v1"))
	require.NoError(t, store.Set("beta", "v2"))

	req := httptest.NewRequest(http.MethodGet, "/api/secrets", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "alpha")
	assert.Contains(t, body, "beta")
	// Values must never be returned by the listing.
	assert.NotContains(t, body, "v1")
	assert.NotContains(t, body, "v2")
}

func TestSecretsSetAndDelete(t *testing.T) {
	router, store := secretsRouter(t)

	body, _ := json.Marshal(map[string]string{"value": "s3cr3t"})
	req := httptest.NewRequest(http.MethodPut, "/api/secrets/api_key", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	v, ok := store.Get("api_key")
	assert.True(t, ok)
	assert.Equal(t, "s3cr3t", v)

	del := httptest.NewRequest(http.MethodDelete, "/api/secrets/api_key", nil)
	rrDel := httptest.NewRecorder()
	router.ServeHTTP(rrDel, del)
	require.Equal(t, http.StatusOK, rrDel.Code)
	_, ok = store.Get("api_key")
	assert.False(t, ok)
}

func TestSecretsSetRejectsInvalidName(t *testing.T) {
	router, _ := secretsRouter(t)
	body, _ := json.Marshal(map[string]string{"value": "x"})
	req := httptest.NewRequest(http.MethodPut, "/api/secrets/bad%20name", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}
