package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// listSecrets returns the raw body of GET /api/secrets and its data member
// decoded generically, so the test sees every field the response has.
func listSecrets(t *testing.T, router *mux.Router) (string, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/secrets", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var env struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &env))
	require.True(t, env.Success)
	return rr.Body.String(), env.Data
}

func TestSecretsListReturnsNamesAndShortMarksOnly(t *testing.T) {
	router, store := secretsRouter(t)
	values := map[string]string{
		"pin":         "4096",
		"fifteen":     "abcdefghijklmno",  // 15 runes
		"sixteen":     "abcdefghijklmnop", // 16 runes
		"accents":     strings.Repeat("é", 15),
		"kanji":       strings.Repeat("日", 16),
		"emoji_short": strings.Repeat("🔑", 15),
	}
	for name, v := range values {
		require.NoError(t, store.Set(name, v))
	}

	body, data := listSecrets(t, router)

	assert.Equal(t, map[string]any{
		"shortBelow": float64(16),
		"undefined":  []any{},
		"secrets": []any{
			map[string]any{"name": "accents", "short": true, "uses": []any{}},
			map[string]any{"name": "emoji_short", "short": true, "uses": []any{}},
			map[string]any{"name": "fifteen", "short": true, "uses": []any{}},
			map[string]any{"name": "kanji", "short": false, "uses": []any{}},
			map[string]any{"name": "pin", "short": true, "uses": []any{}},
			map[string]any{"name": "sixteen", "short": false, "uses": []any{}},
		},
	}, data, "names, short marks and uses only: no value, no length")
	// Values never appear, in any form; nor do the byte or rune lengths of
	// the short ones (the only numbers in the body are the threshold).
	for _, v := range values {
		assert.NotContains(t, body, v)
	}
	for _, n := range []string{"4", "15", "30", "45", "60"} {
		assert.NotRegexp(t, `[^0-9]`+n+`[^0-9]`, body, "no length %s in the listing", n)
	}
}

func TestSecretsListIsAnEmptyListWithoutSecrets(t *testing.T) {
	router, _ := secretsRouter(t)
	_, data := listSecrets(t, router)
	assert.Equal(t, []any{}, data["secrets"])
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
