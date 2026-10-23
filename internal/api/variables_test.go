package api

import (
	"github.com/Pegasus8/piworker/internal/storage"
	flowvars "github.com/Pegasus8/piworker/internal/vars"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestVariablesAPIPersistsAndReportsFailures(t *testing.T) {
	db, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "vars.db"))
	require.NoError(t, err)
	defer db.Close()
	store := flowvars.NewStore()
	require.NoError(t, store.Attach(db))
	router := mux.NewRouter()
	NewVariablesHandler(store).RegisterRoutes(router)
	request := func(method, body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest(method, "/api/variables/limit", strings.NewReader(body)))
		return rr
	}
	for _, bad := range []string{`{}`, `{"value":}`, `{"value":1} {"value":2}`} {
		require.Equal(t, 400, request("PUT", bad).Code)
	}
	require.Equal(t, 200, request("PUT", `{"value":{"nested":[false,null,12]}}`).Code)
	restored := flowvars.NewStore()
	require.NoError(t, restored.Attach(db))
	v, ok := restored.Get("limit")
	require.True(t, ok)
	require.Equal(t, map[string]interface{}{"nested": []interface{}{false, nil, float64(12)}}, v)
	require.NoError(t, db.Close())
	require.Equal(t, 500, request("PUT", `{"value":99}`).Code)
	require.Equal(t, 500, request("DELETE", "").Code)
	after, _ := store.Get("limit")
	require.Equal(t, v, after)
}
