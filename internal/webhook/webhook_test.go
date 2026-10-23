package webhook

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHubRegisterDuplicate(t *testing.T) {
	hub := NewHub()
	require.NoError(t, hub.Register("a", func(Request) {}))
	require.Error(t, hub.Register("a", func(Request) {}), "duplicate path must error")
	hub.Unregister("a")
	require.NoError(t, hub.Register("a", func(Request) {}), "path is reusable after unregister")
}

func TestHubHandlerFuncDelivers(t *testing.T) {
	hub := NewHub()
	received := make(chan Request, 1)
	require.NoError(t, hub.Register("myhook", func(r Request) { received <- r }))

	req := httptest.NewRequest("POST", PathPrefix+"myhook?a=b", strings.NewReader(`{"x":1}`))
	w := httptest.NewRecorder()
	hub.HandlerFunc()(w, req)

	assert.Equal(t, 200, w.Code)
	select {
	case r := <-received:
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "myhook", r.Path)
		body, ok := r.Body.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, float64(1), body["x"])
		assert.Equal(t, []string{"b"}, r.Query["a"])
	case <-time.After(time.Second):
		t.Fatal("expected delivery")
	}
}

func TestHubHandlerFuncUnregistered(t *testing.T) {
	hub := NewHub()
	w := httptest.NewRecorder()
	hub.HandlerFunc()(w, httptest.NewRequest("GET", PathPrefix+"nope", nil))
	assert.Equal(t, 404, w.Code)
}
