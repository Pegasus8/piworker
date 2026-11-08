package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/stretchr/testify/require"
)

func manualFlow(t *testing.T, app testApp) *flow.Flow {
	t.Helper()
	f := flow.NewFlow("shutdown")
	trigger := flow.NewNode("trigger-manual", flow.NodeCategoryInput)
	f.AddNode(*trigger)
	require.NoError(t, app.store.CreateFlow(f))
	return f
}

func TestGracefulShutdownDrainsHTTPBeforeStoppingFlows(t *testing.T) {
	app := newTestApp(t, false)
	f := manualFlow(t, app)
	require.NoError(t, app.manager.Deploy(context.Background(), f))

	runningSeenByStream := make(chan int, 1)
	streamStarted := make(chan struct{})
	mux := http.NewServeMux()
	// Behaves like the SSE handler: it only ends when r.Context() is done.
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(streamStarted)
		<-r.Context().Done()
		runningSeenByStream <- len(app.manager.ListRunning())
	})
	mux.Handle("/", app.handler)

	srv := newHTTPServer("127.0.0.1:0", mux)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(ln) }()
	base := "http://" + ln.Addr().String()

	resp, err := http.Get(base + "/stream")
	require.NoError(t, err)
	defer resp.Body.Close()
	<-streamStarted

	start := time.Now()
	require.NoError(t, gracefulShutdown(srv, app.flows, app.manager, 5*time.Second))
	require.Less(t, time.Since(start), 3*time.Second, "streams must end when shutdown begins instead of hitting the timeout")
	_, _ = io.Copy(io.Discard, resp.Body)

	require.Equal(t, 1, <-runningSeenByStream, "flows must keep running until in-flight HTTP requests have drained")
	require.Empty(t, app.manager.ListRunning(), "all flows stop after the HTTP server is down")
}

func TestDeployRejectedOnceShutdownBegins(t *testing.T) {
	app := newTestApp(t, false)
	f := manualFlow(t, app)
	app.flows.BeginShutdown()
	w := app.do("POST", "/api/flows/"+f.ID+"/deploy", "", "", editorHeaders)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.Empty(t, app.manager.ListRunning())
	saved, err := app.store.GetFlow(f.ID)
	require.NoError(t, err)
	require.NotEqual(t, flow.FlowStateRunning, saved.State)
}
