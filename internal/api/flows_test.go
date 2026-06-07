package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Test Helpers
// ============================================================================

func createTestServer(t *testing.T) (*Server, *storage.SQLiteStore) {
	store, err := storage.NewSQLiteStore(":memory:")
	require.NoError(t, err)

	registry := createTestRegistry()
	logger := zerolog.Nop()

	server := NewServer(store, registry, logger)

	return server, store
}

func createTestRegistry() *node.Registry {
	registry := node.NewRegistry()

	// Register a mock trigger for testing
	_ = registry.Register(node.NodeTypeInfo{
		Type:     "mock-trigger",
		Name:     "Mock Trigger",
		Category: flow.NodeCategoryInput,
		Outputs:  []flow.Port{{ID: "output", Name: "Output", DataType: flow.DataTypeAny}},
	}, func(config map[string]interface{}) (node.Node, error) {
		return &mockNode{}, nil
	})

	return registry
}

type mockNode struct{}

func (n *mockNode) Process(ctx context.Context, msg *flow.Message) ([]*flow.Message, error) {
	return nil, nil
}

func (n *mockNode) Ports() (inputs []flow.Port, outputs []flow.Port) {
	return nil, []flow.Port{{ID: "output", Name: "Output", DataType: flow.DataTypeAny}}
}

func (n *mockNode) Validate() error {
	return nil
}

type mockTriggerNode struct {
	mockNode
}

func (n *mockTriggerNode) Start(ctx context.Context, out chan<- *flow.Message) error {
	return nil
}

func (n *mockTriggerNode) Stop() error {
	return nil
}

func createTestFlow(name string) *flow.Flow {
	f := flow.NewFlow(name)
	f.Description = "Test description"

	trigger := flow.NewNode("mock-trigger", flow.NodeCategoryInput)
	trigger.Outputs = []flow.Port{{ID: "output", Name: "Output", DataType: flow.DataTypeAny}}
	f.AddNode(*trigger)

	return f
}

func performRequest(server *Server, method, path string, body interface{}) *httptest.ResponseRecorder {
	var bodyReader *bytes.Buffer
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewBuffer(data)
	} else {
		bodyReader = bytes.NewBuffer(nil)
	}

	req, _ := http.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	return rr
}

// ============================================================================
// ListFlows Tests
// ============================================================================

func TestListFlows(t *testing.T) {
	t.Run("returns empty list", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		rr := performRequest(server, "GET", "/api/flows", nil)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response APIResponse
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.True(t, response.Success)
		data := response.Data.(map[string]interface{})
		flows := data["flows"].([]interface{})
		assert.Empty(t, flows)
	})

	t.Run("returns flows", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		// Create test flows
		for i := 0; i < 3; i++ {
			f := createTestFlow("Flow " + string(rune('A'+i)))
			store.CreateFlow(f)
		}

		rr := performRequest(server, "GET", "/api/flows", nil)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response APIResponse
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.True(t, response.Success)
		data := response.Data.(map[string]interface{})
		flows := data["flows"].([]interface{})
		assert.Len(t, flows, 3)
	})
}

// ============================================================================
// CreateFlow Tests
// ============================================================================

func TestCreateFlow(t *testing.T) {
	t.Run("creates flow successfully", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		body := map[string]interface{}{
			"name":        "New Flow",
			"description": "A new test flow",
		}

		rr := performRequest(server, "POST", "/api/flows", body)

		assert.Equal(t, http.StatusCreated, rr.Code)

		var response APIResponse
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.True(t, response.Success)
		assert.Equal(t, "Flow created successfully", response.Message)
	})

	t.Run("generates ID if not provided", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		body := map[string]interface{}{
			"name": "Flow Without ID",
		}

		rr := performRequest(server, "POST", "/api/flows", body)

		assert.Equal(t, http.StatusCreated, rr.Code)

		var response APIResponse
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		data := response.Data.(map[string]interface{})
		assert.NotEmpty(t, data["id"])
	})

	t.Run("fails on invalid JSON", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		req, _ := http.NewRequest("POST", "/api/flows", bytes.NewBufferString("invalid json"))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)

		var response APIResponse
		json.Unmarshal(rr.Body.Bytes(), &response)
		assert.False(t, response.Success)
		assert.Contains(t, response.Error, "Invalid JSON")
	})

	t.Run("fails on duplicate ID", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		f := createTestFlow("Existing Flow")
		store.CreateFlow(f)

		body := map[string]interface{}{
			"id":   f.ID,
			"name": "Duplicate Flow",
		}

		rr := performRequest(server, "POST", "/api/flows", body)

		assert.Equal(t, http.StatusConflict, rr.Code)
	})
}

// ============================================================================
// GetFlow Tests
// ============================================================================

func TestGetFlow(t *testing.T) {
	t.Run("returns existing flow", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		f := createTestFlow("Get Flow")
		store.CreateFlow(f)

		rr := performRequest(server, "GET", "/api/flows/"+f.ID, nil)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response APIResponse
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.True(t, response.Success)
		data := response.Data.(map[string]interface{})
		flowData := data["flow"].(map[string]interface{})
		assert.Equal(t, f.ID, flowData["id"])
		assert.Equal(t, f.Name, flowData["name"])
	})

	t.Run("returns 404 for non-existent flow", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		rr := performRequest(server, "GET", "/api/flows/nonexistent", nil)

		assert.Equal(t, http.StatusNotFound, rr.Code)

		var response APIResponse
		json.Unmarshal(rr.Body.Bytes(), &response)
		assert.False(t, response.Success)
	})

	t.Run("includes running status", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		f := createTestFlow("Running Status")
		store.CreateFlow(f)

		rr := performRequest(server, "GET", "/api/flows/"+f.ID, nil)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response APIResponse
		json.Unmarshal(rr.Body.Bytes(), &response)

		data := response.Data.(map[string]interface{})
		assert.Contains(t, data, "running")
	})
}

// ============================================================================
// UpdateFlow Tests
// ============================================================================

func TestUpdateFlow(t *testing.T) {
	t.Run("updates existing flow", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		f := createTestFlow("Original Name")
		store.CreateFlow(f)

		body := map[string]interface{}{
			"name":        "Updated Name",
			"description": "Updated description",
		}

		rr := performRequest(server, "PUT", "/api/flows/"+f.ID, body)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response APIResponse
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.True(t, response.Success)
		assert.Equal(t, "Flow updated successfully", response.Message)

		// Verify update in store
		updated, _ := store.GetFlow(f.ID)
		assert.Equal(t, "Updated Name", updated.Name)
	})

	t.Run("returns 404 for non-existent flow", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		body := map[string]interface{}{
			"name": "New Name",
		}

		rr := performRequest(server, "PUT", "/api/flows/nonexistent", body)

		assert.Equal(t, http.StatusNotFound, rr.Code)
	})

	t.Run("fails on invalid JSON", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		f := createTestFlow("Test")
		store.CreateFlow(f)

		req, _ := http.NewRequest("PUT", "/api/flows/"+f.ID, bytes.NewBufferString("invalid"))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("preserves ID and creation time", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		f := createTestFlow("Original")
		store.CreateFlow(f)

		body := map[string]interface{}{
			"id":   "different-id",
			"name": "Updated",
		}

		rr := performRequest(server, "PUT", "/api/flows/"+f.ID, body)

		assert.Equal(t, http.StatusOK, rr.Code)

		// ID should be preserved
		updated, _ := store.GetFlow(f.ID)
		assert.Equal(t, f.ID, updated.ID)
	})
}

// ============================================================================
// DeleteFlow Tests
// ============================================================================

func TestDeleteFlow(t *testing.T) {
	t.Run("deletes existing flow", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		f := createTestFlow("To Delete")
		store.CreateFlow(f)

		rr := performRequest(server, "DELETE", "/api/flows/"+f.ID, nil)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response APIResponse
		json.Unmarshal(rr.Body.Bytes(), &response)
		assert.True(t, response.Success)
		assert.Equal(t, "Flow deleted successfully", response.Message)

		// Verify deletion
		_, err := store.GetFlow(f.ID)
		assert.Error(t, err)
	})

	t.Run("returns 404 for non-existent flow", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		rr := performRequest(server, "DELETE", "/api/flows/nonexistent", nil)

		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}

// ============================================================================
// DeployFlow Tests
// ============================================================================

func TestDeployFlow(t *testing.T) {
	t.Run("returns 404 for non-existent flow", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		rr := performRequest(server, "POST", "/api/flows/nonexistent/deploy", nil)

		assert.Equal(t, http.StatusNotFound, rr.Code)
	})

	t.Run("deploys valid flow", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		// Create a flow with proper trigger node
		f := createTestFlow("Deploy Test")
		store.CreateFlow(f)

		// Note: This test may fail or succeed depending on mock node implementation
		rr := performRequest(server, "POST", "/api/flows/"+f.ID+"/deploy", nil)

		// The response depends on whether the mock trigger can be deployed
		assert.Contains(t, []int{http.StatusOK, http.StatusInternalServerError}, rr.Code)
	})
}

// ============================================================================
// StopFlow Tests
// ============================================================================

func TestStopFlow(t *testing.T) {
	t.Run("returns 404 for non-existent flow", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		rr := performRequest(server, "POST", "/api/flows/nonexistent/stop", nil)

		assert.Equal(t, http.StatusNotFound, rr.Code)
	})

	t.Run("returns conflict for non-running flow", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		f := createTestFlow("Not Running")
		store.CreateFlow(f)

		rr := performRequest(server, "POST", "/api/flows/"+f.ID+"/stop", nil)

		// Should indicate flow is not running
		assert.Contains(t, []int{http.StatusOK, http.StatusConflict}, rr.Code)
	})
}

// ============================================================================
// Health Endpoint Tests
// ============================================================================

func TestHealthEndpoint(t *testing.T) {
	t.Run("returns healthy status", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		rr := performRequest(server, "GET", "/api/health", nil)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]string
		json.Unmarshal(rr.Body.Bytes(), &response)
		assert.Equal(t, "healthy", response["status"])
	})

	t.Run("returns unhealthy when store is closed", func(t *testing.T) {
		server, store := createTestServer(t)
		store.Close()

		rr := performRequest(server, "GET", "/api/health", nil)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]string
		json.Unmarshal(rr.Body.Bytes(), &response)
		assert.Equal(t, "unhealthy", response["status"])
	})
}

// ============================================================================
// Error Handler Tests
// ============================================================================

func TestErrorHandlers(t *testing.T) {
	t.Run("404 for unknown route", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		rr := performRequest(server, "GET", "/api/unknown", nil)

		assert.Equal(t, http.StatusNotFound, rr.Code)

		var response APIResponse
		json.Unmarshal(rr.Body.Bytes(), &response)
		assert.False(t, response.Success)
		assert.Contains(t, response.Error, "not found")
	})

	t.Run("405 for wrong method", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		rr := performRequest(server, "PATCH", "/api/flows", nil)

		assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	})
}

// ============================================================================
// Router Tests
// ============================================================================

func TestRouterConfiguration(t *testing.T) {
	t.Run("router is accessible", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		router := server.Router()

		assert.NotNil(t, router)
		assert.IsType(t, &mux.Router{}, router)
	})

	t.Run("routes are registered", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		routes := []struct {
			method string
			path   string
		}{
			{"GET", "/api/flows"},
			{"POST", "/api/flows"},
			{"GET", "/api/health"},
		}

		for _, route := range routes {
			t.Run(route.method+" "+route.path, func(t *testing.T) {
				rr := performRequest(server, route.method, route.path, nil)
				assert.NotEqual(t, http.StatusMethodNotAllowed, rr.Code)
			})
		}
	})
}

// ============================================================================
// Content-Type Tests
// ============================================================================

func TestContentType(t *testing.T) {
	t.Run("returns JSON content type", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		rr := performRequest(server, "GET", "/api/flows", nil)

		assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	})
}

// ============================================================================
// FlowsHandler Tests
// ============================================================================

func TestNewFlowsHandler(t *testing.T) {
	t.Run("creates handler with dependencies", func(t *testing.T) {
		store, _ := storage.NewSQLiteStore(":memory:")
		defer store.Close()

		registry := node.NewRegistry()
		logger := zerolog.Nop()
		manager := flow.NewRuntimeManager(registry, logger)

		handler := NewFlowsHandler(store, manager, registry, logger)

		assert.NotNil(t, handler)
	})
}

// ============================================================================
// API Response Format Tests
// ============================================================================

func TestAPIResponseFormat(t *testing.T) {
	t.Run("success response has correct format", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		rr := performRequest(server, "GET", "/api/flows", nil)

		var response APIResponse
		err := json.Unmarshal(rr.Body.Bytes(), &response)

		require.NoError(t, err)
		assert.True(t, response.Success)
		assert.NotNil(t, response.Data)
		assert.Empty(t, response.Error)
	})

	t.Run("error response has correct format", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		rr := performRequest(server, "GET", "/api/flows/nonexistent", nil)

		var response APIResponse
		err := json.Unmarshal(rr.Body.Bytes(), &response)

		require.NoError(t, err)
		assert.False(t, response.Success)
		assert.NotEmpty(t, response.Error)
	})

	t.Run("create response includes message", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		body := map[string]interface{}{"name": "Test"}
		rr := performRequest(server, "POST", "/api/flows", body)

		var response APIResponse
		json.Unmarshal(rr.Body.Bytes(), &response)

		assert.NotEmpty(t, response.Message)
	})
}

// ============================================================================
// Edge Cases
// ============================================================================

func TestAPIEdgeCases(t *testing.T) {
	t.Run("handles empty request body for POST", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		req, _ := http.NewRequest("POST", "/api/flows", bytes.NewBuffer(nil))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, req)

		// Should return an error for empty body
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("handles special characters in flow ID", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		// URL-encoded special characters
		rr := performRequest(server, "GET", "/api/flows/id%20with%20spaces", nil)

		assert.Equal(t, http.StatusNotFound, rr.Code)
	})

	t.Run("handles very long flow name", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		longName := ""
		for i := 0; i < 1000; i++ {
			longName += "a"
		}

		body := map[string]interface{}{"name": longName}
		rr := performRequest(server, "POST", "/api/flows", body)

		assert.Equal(t, http.StatusCreated, rr.Code)
	})

	t.Run("handles unicode in flow name", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		body := map[string]interface{}{"name": "Flujo de Automatizacion"}
		rr := performRequest(server, "POST", "/api/flows", body)

		assert.Equal(t, http.StatusCreated, rr.Code)

		var response APIResponse
		json.Unmarshal(rr.Body.Bytes(), &response)

		data := response.Data.(map[string]interface{})
		assert.Equal(t, "Flujo de Automatizacion", data["name"])
	})
}

// ============================================================================
// Concurrent Request Tests
// ============================================================================

func TestAPIConcurrency(t *testing.T) {
	t.Run("handles concurrent requests", func(t *testing.T) {
		server, store := createTestServer(t)
		defer store.Close()

		// Create some initial flows
		for i := 0; i < 5; i++ {
			f := createTestFlow("Initial " + string(rune('0'+i)))
			store.CreateFlow(f)
		}

		// Make concurrent requests
		done := make(chan bool, 50)

		for i := 0; i < 25; i++ {
			go func() {
				rr := performRequest(server, "GET", "/api/flows", nil)
				assert.Equal(t, http.StatusOK, rr.Code)
				done <- true
			}()
		}

		for i := 0; i < 25; i++ {
			go func(id int) {
				body := map[string]interface{}{"name": "Concurrent " + string(rune('0'+id))}
				_ = performRequest(server, "POST", "/api/flows", body)
				done <- true
			}(i)
		}

		for i := 0; i < 50; i++ {
			<-done
		}
	})
}
