package api

import (
	"context"
	"encoding/json"
	"github.com/Pegasus8/piworker/internal/events"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	_ "github.com/Pegasus8/piworker/internal/node/builtin"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCoreStateIsServerOwned(t *testing.T) {
	server, store := createTestServer(t)
	f := createTestFlow("core")
	f.State = flow.FlowStateRunning
	rr := performRequest(server, "POST", "/api/flows", f)
	require.Equal(t, 201, rr.Code)
	saved, err := store.GetFlow(f.ID)
	require.NoError(t, err)
	require.Equal(t, flow.FlowStateInactive, saved.State, "creating a definition must not schedule it for startup")
	f.State = "made-up"
	rr = performRequest(server, "PUT", "/api/flows/"+f.ID, f)
	require.Equal(t, 200, rr.Code)
	saved, err = store.GetFlow(f.ID)
	require.NoError(t, err)
	require.Equal(t, flow.FlowStateInactive, saved.State)
}

func TestCoreToggleOffClearsRestoreIntent(t *testing.T) {
	server, store := createTestServer(t)
	f := createTestFlow("stale running")
	f.State = flow.FlowStateRunning
	require.NoError(t, store.CreateFlow(f))
	rr := performRequest(server, "PATCH", "/api/flows/"+f.ID+"/toggle", ToggleRequest{Enabled: false})
	require.Equal(t, 200, rr.Code)
	var result struct {
		Data FlowStatusResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &result))
	require.False(t, result.Data.Running)
	require.Equal(t, flow.FlowStateInactive, result.Data.Flow.State)
	running, err := store.GetRunningFlows()
	require.NoError(t, err)
	require.Empty(t, running, "a stopped flow must not be restored on restart")
}

// Exercise the same JSON contract as the editor against real built-in nodes,
// routing, persistence and restart selection (no mocked execution).
func TestCoreManualBranchesAndHistory(t *testing.T) {
	_, store := createTestServer(t)
	registry := node.DefaultRegistry
	hub := events.NewHub()
	recorder := events.NewRecorder(store, events.WithHub(hub), events.WithFlushInterval(5*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); recorder.Run(ctx) }()
	defer func() { cancel(); <-done }()
	server := NewServer(store, registry, zerolog.Nop())
	server.handler.manager = flow.NewRuntimeManager(registry, zerolog.Nop(), flow.WithManagerObserver(recorder.Observe))
	defer func() { server.handler.manager.StopAll() }()
	NewEventsHandler(hub, store, zerolog.Nop()).RegisterRoutes(server.Router())
	f := flow.NewFlow("branches")
	for _, spec := range []struct {
		id, kind string
		config   map[string]interface{}
	}{
		{"manual", "trigger-manual", map[string]interface{}{"payload": "true"}},
		{"switch", "process-switch", map[string]interface{}{"condition": "payload.data == true"}},
		{"yes", "process-debug", nil}, {"no", "process-debug", nil},
	} {
		info, ok := registry.Get(spec.kind)
		require.True(t, ok, spec.kind)
		n := flow.NewNode(spec.kind, info.Category)
		n.ID = spec.id
		n.Config = spec.config
		// Intentionally omit cached ports, as old editor definitions did.
		f.AddNode(*n)
	}
	f.AddConnection(*flow.NewConnection("manual", "output", "switch", "input"))
	f.AddConnection(*flow.NewConnection("switch", "true", "yes", "input"))
	f.AddConnection(*flow.NewConnection("switch", "false", "no", "input"))
	base := "/api/flows/" + f.ID
	require.Equal(t, 201, performRequest(server, "POST", "/api/flows", f).Code)
	rr := performRequest(server, "POST", base+"/deploy", nil)
	require.Equal(t, 200, rr.Code, rr.Body.String())
	require.Equal(t, 409, performRequest(server, "PUT", base, f).Code)
	require.Equal(t, 400, performRequest(server, "POST", base+"/inject/switch", nil).Code)
	for i, branch := range []string{"yes", "no"} {
		var body interface{}
		if i == 1 {
			body = map[string]interface{}{"payload": false}
		}
		require.Equal(t, 200, performRequest(server, "POST", base+"/inject/manual", body).Code)
		require.Eventually(t, func() bool {
			runs, err := store.ListFlowRuns(f.ID, 50, 0)
			if err != nil || len(runs) != i+1 || runs[0].Status != "success" {
				return false
			}
			evs, err := store.ListNodeEvents(runs[0].ID)
			if err != nil || len(evs) != 2 {
				return false
			}
			return evs[0].NodeID == "switch" && evs[1].NodeID == branch
		}, 3*time.Second, 10*time.Millisecond)
	}
	// A server restart restores deployed flows and retains their branches.
	server.handler.manager.StopAll()
	pending, err := store.GetRunningFlows()
	require.NoError(t, err)
	require.Len(t, pending, 1)
	server.handler.manager = flow.NewRuntimeManager(registry, zerolog.Nop(), flow.WithManagerObserver(recorder.Observe))
	require.NoError(t, server.handler.manager.Deploy(context.Background(), pending[0]))
	require.Equal(t, 200, performRequest(server, "POST", base+"/stop", nil).Code)
	require.Equal(t, 404, performRequest(server, "POST", base+"/inject/manual", nil).Code)
	pending, err = store.GetRunningFlows()
	require.NoError(t, err)
	require.Empty(t, pending)
	rr = performRequest(server, "GET", base+"/runs", nil)
	require.Equal(t, 200, rr.Code)
	var result struct {
		Data RunsResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &result))
	require.Len(t, result.Data.Runs, 2)
	rr = performRequest(server, "GET", base+"/runs/"+result.Data.Runs[0].ID+"/events", nil)
	require.Equal(t, 200, rr.Code)
	require.Contains(t, rr.Body.String(), `"nodeId":"no"`)
}

func TestCoreRejectsChunkedInvalidInjection(t *testing.T) {
	_, store := createTestServer(t)
	server := NewServer(store, node.DefaultRegistry, zerolog.Nop())
	defer server.handler.manager.StopAll()
	f := flow.NewFlow("manual")
	n := flow.NewNode("trigger-manual", flow.NodeCategoryInput)
	f.AddNode(*n)
	require.NoError(t, store.CreateFlow(f))
	require.Equal(t, 200, performRequest(server, "POST", "/api/flows/"+f.ID+"/deploy", nil).Code)
	request := httptest.NewRequest("POST", "/api/flows/"+f.ID+"/inject/"+n.ID, strings.NewReader("{invalid"))
	request.ContentLength = -1
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, request)
	require.Equal(t, 400, rr.Code, "unknown body length must not silently use the configured payload")
}

func TestCoreLifecyclePersistenceFailure(t *testing.T) {
	server, store := createTestServer(t)
	defer server.handler.manager.StopAll()
	f := createTestFlow("durable state")
	require.NoError(t, store.CreateFlow(f))
	base := "/api/flows/" + f.ID
	_, err := store.DB().Exec(`CREATE TRIGGER fail_state BEFORE UPDATE ON flows BEGIN SELECT RAISE(FAIL, 'disk failure'); END`)
	require.NoError(t, err)
	rr := performRequest(server, "POST", base+"/deploy", nil)
	require.Equal(t, 500, rr.Code, "do not report a deployment that cannot be restored")
	rt, ok := server.handler.manager.GetRuntime(f.ID)
	require.False(t, ok && rt.IsRunning(), "roll back an unpersisted deployment")
	_, err = store.DB().Exec(`DROP TRIGGER fail_state`)
	require.NoError(t, err)
	require.Equal(t, 200, performRequest(server, "POST", base+"/deploy", nil).Code)
	_, err = store.DB().Exec(`CREATE TRIGGER fail_state BEFORE UPDATE ON flows BEGIN SELECT RAISE(FAIL, 'disk failure'); END`)
	require.NoError(t, err)
	rr = performRequest(server, "POST", base+"/stop", nil)
	require.Equal(t, 500, rr.Code, "do not report a stop that will be undone at restart")
	rt, ok = server.handler.manager.GetRuntime(f.ID)
	require.True(t, ok && rt.IsRunning(), "keep running if stop intent cannot be persisted")
}
