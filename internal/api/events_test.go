package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/events"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newEventsRouter(t *testing.T) (*mux.Router, *storage.SQLiteStore, *events.Hub) {
	t.Helper()
	store, err := storage.NewSQLiteStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	hub := events.NewHub()
	h := NewEventsHandler(hub, store, zerolog.Nop())
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	return router, store, hub
}

func TestListRunsEndpoint(t *testing.T) {
	router, store, _ := newEventsRouter(t)
	require.NoError(t, store.UpsertFlowRun("r1", "f1", time.Now()))
	require.NoError(t, store.UpsertFlowRun("other", "f2", time.Now()))

	req := httptest.NewRequest(http.MethodGet, "/api/flows/f1/runs", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Runs []storage.FlowRun `json:"runs"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.True(t, resp.Success)
	require.Len(t, resp.Data.Runs, 1)
	assert.Equal(t, "r1", resp.Data.Runs[0].ID)
}

func TestListRunEventsEndpoint(t *testing.T) {
	router, store, _ := newEventsRouter(t)
	require.NoError(t, store.UpsertFlowRun("r1", "f1", time.Now()))
	require.NoError(t, store.InsertNodeEvents([]storage.NodeEventRecord{
		{RunID: "r1", FlowID: "f1", NodeID: "n1", NodeType: "x", Phase: "success", CreatedAt: time.Now()},
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/flows/f1/runs/r1/events", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp struct {
		Data struct {
			Events []storage.NodeEventRecord `json:"events"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.Data.Events, 1)
	assert.Equal(t, "n1", resp.Data.Events[0].NodeID)
}

func TestStreamEventsSSE(t *testing.T) {
	router, _, hub := newEventsRouter(t)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/flows/f1/events")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	// Publish repeatedly to win the subscribe race (Publish drops with no subs).
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				hub.Publish(events.Event{FlowID: "f1", NodeID: "n1", Phase: events.PhaseRunning})
			}
		}
	}()

	scanner := bufio.NewScanner(resp.Body)
	var dataLine string
	deadline := time.Now().Add(3 * time.Second)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			dataLine = line
			break
		}
		if time.Now().After(deadline) {
			break
		}
	}
	require.Contains(t, dataLine, `"nodeId":"n1"`)
}

func TestStreamEventsOnlyDeliversMatchingFlow(t *testing.T) {
	router, _, hub := newEventsRouter(t)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/flows/wanted/events")
	require.NoError(t, err)
	defer resp.Body.Close()

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				hub.Publish(events.Event{FlowID: "other", NodeID: "x", Phase: events.PhaseRunning})
				hub.Publish(events.Event{FlowID: "wanted", NodeID: "yes", Phase: events.PhaseRunning})
			}
		}
	}()

	scanner := bufio.NewScanner(resp.Body)
	deadline := time.Now().Add(3 * time.Second)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			assert.Contains(t, line, `"nodeId":"yes"`)
			assert.NotContains(t, line, `"nodeId":"x"`)
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("no event received")
		}
	}
}

// TestStreamEventsRequiresNoTokenInURL documents that the SSE stream carries no
// token in its URL: auth is enforced by AuthMiddleware via the Authorization
// header (the frontend consumes the stream with fetch + ReadableStream, which can
// set headers), so the JWT never leaks into a URL/query string.
func TestStreamEventsRequiresNoTokenInURL(t *testing.T) {
	router, _, _ := newEventsRouter(t)
	// Without AuthMiddleware in front (as in these unit tests), the stream opens
	// normally — proving the handler itself imposes no query-token contract.
	srv := httptest.NewServer(router)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/flows/f1/events")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
