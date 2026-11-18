package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func guardedHandler() http.Handler {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	return RequestGuardMiddleware(false, []string{"http://localhost:3000"})(ok)
}

func guardRequest(method, path, body string, headers map[string]string) int {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, "http://piworker.local"+path, nil)
	} else {
		r = httptest.NewRequest(method, "http://piworker.local"+path, strings.NewReader(body))
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	guardedHandler().ServeHTTP(w, r)
	return w.Code
}

func TestRequestGuardBlocksBlindCrossSiteWrites(t *testing.T) {
	// The classic CSRF shape: a form/fetch with text/plain, no custom header.
	code := guardRequest("POST", "/api/flows", `{"name":"x"}`, map[string]string{
		"Content-Type": "text/plain",
		"Origin":       "http://evil.example",
	})
	require.Equal(t, http.StatusForbidden, code)

	code = guardRequest("POST", "/api/flows/abc/deploy", "", map[string]string{"Sec-Fetch-Site": "cross-site"})
	require.Equal(t, http.StatusForbidden, code)

	// The custom header alone is not enough from a foreign origin.
	code = guardRequest("POST", "/api/flows", `{}`, map[string]string{
		"Content-Type": "application/json", "X-PiWorker-Request": "1", "Origin": "http://evil.example",
	})
	require.Equal(t, http.StatusForbidden, code)
}

func TestRequestGuardRequiresJSONBodies(t *testing.T) {
	base := map[string]string{"X-PiWorker-Request": "1", "Origin": "http://piworker.local"}
	with := func(ct string) map[string]string {
		h := map[string]string{"Content-Type": ct}
		for k, v := range base {
			h[k] = v
		}
		return h
	}
	require.Equal(t, http.StatusUnsupportedMediaType, guardRequest("POST", "/api/flows", `{}`, with("text/plain")))
	require.Equal(t, http.StatusUnsupportedMediaType, guardRequest("PUT", "/api/flows/a", `{}`, with("application/x-www-form-urlencoded")))
	require.Equal(t, http.StatusUnsupportedMediaType, guardRequest("POST", "/api/flows", `{}`, base))
	require.Equal(t, http.StatusTeapot, guardRequest("POST", "/api/flows", `{}`, with("application/json; charset=utf-8")))
	// Body-less lifecycle calls (deploy/stop/logout/duplicate) need no Content-Type.
	require.Equal(t, http.StatusTeapot, guardRequest("POST", "/api/flows/a/deploy", "", base))
	// Allowlisted dev origin.
	dev := with("application/json")
	dev["Origin"] = "http://localhost:3000"
	require.Equal(t, http.StatusTeapot, guardRequest("DELETE", "/api/flows/a", "", dev))
}

func TestRequestGuardLeavesSafeAndExternalRequestsAlone(t *testing.T) {
	require.Equal(t, http.StatusTeapot, guardRequest("GET", "/api/flows", "", nil))
	// Webhooks are meant for external callers with arbitrary content types and
	// live outside /api.
	require.Equal(t, http.StatusTeapot, guardRequest("POST", "/webhooks/hook", "a=b", map[string]string{"Content-Type": "text/plain"}))
	// Nothing under /api is exempt any more.
	require.Equal(t, http.StatusForbidden, guardRequest("POST", "/api/webhooks/hook", "a=b", map[string]string{"Content-Type": "text/plain"}))
	require.Equal(t, http.StatusTeapot, guardRequest("POST", "/some/page", "", nil))
}
