package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
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

// TestListRunEventsScopedToFlow: a run's events are only returned under the
// flow that owns the run, never through another flow's URL.
func TestListRunEventsScopedToFlow(t *testing.T) {
	router, store, _ := newEventsRouter(t)
	require.NoError(t, store.UpsertFlowRun("r1", "f1", time.Now()))
	require.NoError(t, store.InsertNodeEvents([]storage.NodeEventRecord{
		{RunID: "r1", FlowID: "f1", NodeID: "secret", NodeType: "x", Phase: "success", CreatedAt: time.Now()},
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/flows/f2/runs/r1/events", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp struct {
		Data struct {
			Events []storage.NodeEventRecord `json:"events"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Empty(t, resp.Data.Events, "another flow's run must not be readable through this flow")
}

// TestStreamEventsStopsWhenRequestContextEnds: the stream loop exits when the
// request context is cancelled — which is how server shutdown reaches it when
// http.Server.BaseContext is cancelled — even if no event or keepalive is due.
func TestStreamEventsStopsWhenRequestContextEnds(t *testing.T) {
	router, _, hub := newEventsRouter(t)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/flows/f1/events", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		router.ServeHTTP(rr, req)
		close(done)
	}()
	require.Eventually(t, func() bool { return hub.HasSubscribers("f1") }, time.Second, 5*time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not return after its context was cancelled")
	}
	assert.False(t, hub.HasSubscribers("f1"), "the hub subscription is released")
}

// TestStreamEventsStopsOnServerShutdown wires a real server whose BaseContext
// is cancelled on shutdown (as main.go does) and checks the stream ends.
func TestStreamEventsStopsOnServerShutdown(t *testing.T) {
	router, _, hub := newEventsRouter(t)
	baseCtx, cancelBase := context.WithCancel(context.Background())
	srv := httptest.NewUnstartedServer(router)
	srv.Config.BaseContext = func(net.Listener) context.Context { return baseCtx }
	srv.Start()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/flows/f1/events")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Eventually(t, func() bool { return hub.HasSubscribers("f1") }, time.Second, 5*time.Millisecond)

	cancelBase()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, srv.Config.Shutdown(shutdownCtx), "shutdown must not wait for the stream's client to leave")
	assert.False(t, hub.HasSubscribers("f1"))
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
// token in its URL: AuthMiddleware checks the HttpOnly session cookie. The
// frontend consumes the stream with fetch + ReadableStream.
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

// Exercise a real socket and both production wrappers: recorder-only tests do
// not enforce the HTTP server's write deadline.
func TestStreamEventsSurvivesServerWriteTimeout(t *testing.T) {
	router, _, hub := newEventsRouter(t)
	router.Use(LoggingMiddleware(zerolog.Nop()))
	router.Use(MetricsMiddleware)
	srv := httptest.NewUnstartedServer(router)
	srv.Config.WriteTimeout = 100 * time.Millisecond
	srv.Start()
	defer srv.Close()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(srv.URL + "/api/flows/f1/events")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Deliver an event well after the server's original deadline.
	time.Sleep(300 * time.Millisecond)
	hub.Publish(events.Event{FlowID: "f1", NodeID: "late", Phase: events.PhaseSuccess})
	scanner := bufio.NewScanner(resp.Body)
	require.True(t, scanner.Scan(), "stream closed before the late event: %v", scanner.Err())
	require.Contains(t, scanner.Text(), `"nodeId":"late"`)
}
