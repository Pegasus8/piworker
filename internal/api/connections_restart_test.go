package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connTrigger is a trigger that names a broker connection
// (node.ConnectionRequirer) and records the connection's broker it started
// with, resolved through its context as the real nodes do.
type connTrigger struct {
	mockTriggerNode
	name    string
	started *[]string
}

func (n *connTrigger) RequiredConnections() map[string]string {
	return map[string]string{"connection": n.name}
}

func (n *connTrigger) Start(ctx context.Context, _ chan<- *types.Message) error {
	c, _ := brokers.FromContext(ctx).Get(n.name)
	*n.started = append(*n.started, c.Broker)
	return nil
}

// connectionsEnv is the flows handler with a "mock-conn" trigger type, a
// broker store holding "home", and the connections routes wired to the
// handler's lifecycle.
func connectionsEnv(t *testing.T) (*Server, *mux.Router, *brokers.Store, *[]string) {
	t.Helper()
	server, _ := createTestServer(t)
	t.Cleanup(server.handler.manager.StopAll)
	started := &[]string{}
	require.NoError(t, server.handler.registry.Register(node.NodeTypeInfo{
		Type: "mock-conn", Name: "Conn", Category: flow.NodeCategoryInput, Risk: node.RiskExternal,
		Outputs: []flow.Port{{ID: "output", Name: "Output", DataType: flow.DataTypeAny}},
	}, func(cfg map[string]any) (node.Node, error) {
		name, _ := cfg["connection"].(string)
		return &connTrigger{name: name, started: started}, nil
	}))
	conns := brokers.DefaultStore
	require.NoError(t, conns.Set(brokers.Connection{Name: "pw_test_home", Broker: "tcp://10.0.0.1:1883"}))
	require.NoError(t, conns.Set(brokers.Connection{Name: "pw_test_other", Broker: "tcp://10.0.0.9:1883"}))
	t.Cleanup(func() { _ = conns.Delete("pw_test_home"); _ = conns.Delete("pw_test_other") })
	r := mux.NewRouter()
	NewConnectionsHandler(conns, &fakeTester{}, zerolog.Nop()).WithFlows(server.handler).RegisterRoutes(r)
	return server, r, conns, started
}

func connFlow(t *testing.T, server *Server, name, connection string) *flow.Flow {
	t.Helper()
	f := flow.NewFlow(name)
	n := flow.NewNode("mock-conn", flow.NodeCategoryInput)
	n.Config = map[string]any{"connection": connection}
	n.Outputs = []flow.Port{{ID: "output", Name: "Output", DataType: flow.DataTypeAny}}
	f.AddNode(*n)
	require.NoError(t, server.handler.store.CreateFlow(f))
	return f
}

// Changing a connection restarts the running flows that use it, which then
// run with the new settings; flows that do not use it, or are stopped, are
// left alone. The response names the flows restarted.
func TestChangingAConnectionRestartsTheRunningFlowsThatUseIt(t *testing.T) {
	server, r, _, started := connectionsEnv(t)
	user := connFlow(t, server, "user", "pw_test_home")
	stopped := connFlow(t, server, "stopped user", "pw_test_home")
	other := connFlow(t, server, "other", "pw_test_other")
	require.NoError(t, server.handler.store.CreateFlow(flow.NewFlow("empty")))
	for _, f := range []*flow.Flow{user, other} {
		rr := performRequest(server, http.MethodPost, "/api/flows/"+f.ID+"/deploy", nil)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	}
	assert.Equal(t, []string{"tcp://10.0.0.1:1883", "tcp://10.0.0.9:1883"}, *started)

	rr, env := doJSON(t, r, http.MethodPut, "/api/connections/pw_test_home", `{"broker":"tcp://10.0.0.2:1883"}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	data := env["data"].(map[string]any)
	assert.Equal(t, []any{map[string]any{"id": user.ID, "name": "user"}}, data["restarted"])
	assert.Equal(t, []any{}, data["failed"])
	assert.Equal(t, []string{"tcp://10.0.0.1:1883", "tcp://10.0.0.9:1883", "tcp://10.0.0.2:1883"}, *started, "only the running user restarted, with the new broker")
	assert.True(t, server.handler.Running(user.ID))
	assert.False(t, server.handler.Running(stopped.ID))
	stored, err := server.handler.store.GetFlow(user.ID)
	require.NoError(t, err)
	assert.Equal(t, flow.FlowStateRunning, stored.State)
}

// A connection a running flow uses cannot be deleted (409, naming the
// flows); once they stop it can.
func TestDeletingAConnectionARunningFlowUsesIsRefused(t *testing.T) {
	server, r, conns, _ := connectionsEnv(t)
	user := connFlow(t, server, "user", "pw_test_home")
	require.Equal(t, http.StatusOK, performRequest(server, http.MethodPost, "/api/flows/"+user.ID+"/deploy", nil).Code)

	rr, env := doJSON(t, r, http.MethodDelete, "/api/connections/pw_test_home", "")
	require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
	assert.Contains(t, env["error"], `"user"`)
	assert.Equal(t, []any{map[string]any{"id": user.ID, "name": "user"}}, env["data"].(map[string]any)["flows"])
	_, ok := conns.Get("pw_test_home")
	assert.True(t, ok, "not deleted")

	require.Equal(t, http.StatusOK, performRequest(server, http.MethodPost, "/api/flows/"+user.ID+"/stop", nil).Code)
	rr, _ = doJSON(t, r, http.MethodDelete, "/api/connections/pw_test_home", "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
}
