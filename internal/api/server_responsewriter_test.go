package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResponseWriterIsFlusher guards SSE: the status-capturing wrapper must
// propagate Flush() to the underlying writer, otherwise streaming handlers that
// type-assert http.Flusher break once wrapped by logging/metrics middleware.
func TestResponseWriterIsFlusher(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped := &ResponseWriter{ResponseWriter: rec, StatusCode: http.StatusOK}

	flusher, ok := interface{}(wrapped).(http.Flusher)
	require.True(t, ok, "wrapped ResponseWriter must implement http.Flusher")

	assert.NotPanics(t, func() {
		_, _ = wrapped.Write([]byte("data: x\n\n"))
		flusher.Flush()
	})
	assert.True(t, rec.Flushed, "Flush must reach the underlying writer")
}
