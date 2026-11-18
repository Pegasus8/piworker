package main

import (
	"net/http"
	"testing"

	"github.com/Pegasus8/piworker/internal/webhook"
	"github.com/stretchr/testify/require"
)

// Webhooks live outside /api so the editor's session cookie (Path=/api) is never
// sent to them, and the /api tree has no unauthenticated exceptions.
func TestWebhooksAreServedOutsideAPI(t *testing.T) {
	require.Equal(t, "/webhooks/", webhook.PathPrefix)

	for _, auth := range []bool{false, true} {
		app := newTestApp(t, auth)
		got := make(chan webhook.Request, 1)
		require.NoError(t, webhook.DefaultHub.Register("outside-api", func(r webhook.Request) webhook.Result {
			got <- r
			return webhook.Accepted
		}))

		// External caller: no editor headers, no session, arbitrary content type.
		w := app.do("POST", "/webhooks/outside-api", "text/plain", "hello", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, "hello", (<-got).Body)

		// The old location is now just part of the guarded API.
		w = app.do("POST", "/api/webhooks/outside-api", "text/plain", "hello", nil)
		require.NotEqual(t, http.StatusOK, w.Code)
		webhook.DefaultHub.Unregister("outside-api")
	}
}
