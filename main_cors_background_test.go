package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/api"
	"github.com/stretchr/testify/assert"
)

// An editor on an allowlisted origin may mark its own refreshes as
// background requests (api.BackgroundRequestHeader), so the preflight must
// allow that header next to the CSRF one.
func TestCorsAllowsTheBackgroundRequestHeader(t *testing.T) {
	h := makeCorsMiddleware([]string{"https://editor.example"})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := httptest.NewRequest(http.MethodOptions, "/api/agent/approvals", nil)
	r.Header.Set("Origin", "https://editor.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	allowed := strings.Split(w.Header().Get("Access-Control-Allow-Headers"), ",")
	for i := range allowed {
		allowed[i] = strings.TrimSpace(allowed[i])
	}
	assert.Contains(t, allowed, "X-PiWorker-Request")
	assert.Contains(t, allowed, api.BackgroundRequestHeader)
}
