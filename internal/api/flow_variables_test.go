package api

import (
	"encoding/json"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	flowvars "github.com/Pegasus8/piworker/internal/vars"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestFlowVariablesAPIAndPreviewIsolation(t *testing.T) {
	db, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "flows.db"))
	require.NoError(t, err)
	defer db.Close()
	old := flowvars.DefaultFlows
	flowvars.DefaultFlows = &flowvars.FlowStores{Factory: db.FlowVariables}
	defer func() { flowvars.DefaultFlows = old }()
	f := flow.NewFlow("local")
	require.NoError(t, db.CreateFlow(f))
	r := mux.NewRouter()
	NewFlowsHandler(db, flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop()), node.DefaultRegistry, zerolog.Nop()).RegisterRoutes(r)
	NewNodeTypesHandler(node.DefaultRegistry, zerolog.Nop(), db).RegisterRoutes(r)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rr
	}
	path := "/api/flows/" + f.ID + "/variables"
	require.Equal(t, 200, request("PUT", path+"/count", `{"value":7}`).Code)
	rr := request("GET", path, "")
	require.Equal(t, 200, rr.Code)
	require.Contains(t, rr.Body.String(), `"count":7`)
	body := map[string]interface{}{"flowId": f.ID, "config": map[string]interface{}{"scope": "flow", "key": "count"}, "payload": 99}
	raw, _ := json.Marshal(body)
	rr = request("POST", "/api/nodes/set-var/test", string(raw))
	require.Equal(t, 200, rr.Code)
	require.NotContains(t, rr.Body.String(), `"error"`)
	rr = request("GET", path, "")
	require.Contains(t, rr.Body.String(), `"count":7`)
	raw, _ = json.Marshal(map[string]interface{}{"flowId": f.ID, "config": map[string]interface{}{"expression": "flowVars.count"}})
	rr = request("POST", "/api/nodes/process-transform/test", string(raw))
	require.Equal(t, 200, rr.Code)
	require.Contains(t, rr.Body.String(), `"payload":7`)
	require.Equal(t, 404, request("PUT", "/api/flows/missing/variables/count", `{"value":1}`).Code)
	require.Equal(t, 200, request("DELETE", path+"/count", "").Code)
	require.NotContains(t, request("GET", path, "").Body.String(), `"count"`)
}
