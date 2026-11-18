package mcpserver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

// With TLS on, /mcp on the plain-HTTP listener is refused, not redirected:
// a client following the redirect would keep sending its token in clear
// text on every request. The answer names the HTTPS URL and the token's id
// is logged (never its secret) so the admin can revoke it.
func TestRefusePlainHTTP(t *testing.T) {
	var logs bytes.Buffer
	r := httptest.NewRequest(http.MethodPost, "http://piworker.local:8080/mcp", strings.NewReader(listToolsBody))
	withToken(r, callToken)
	w := httptest.NewRecorder()
	RefusePlainHTTP(w, r, "https://piworker.local:8443/mcp", zerolog.New(&logs))

	msg := requireJSONError(t, w, http.StatusBadRequest)
	assert.Empty(t, w.Header().Get("Location"))
	assert.Contains(t, msg, "https://piworker.local:8443/mcp")
	assert.Contains(t, msg, "revok")
	assert.Contains(t, logs.String(), `"tokenId":"callcallcallcall"`)
	_, secret, _ := strings.Cut(strings.TrimPrefix(callToken, "pwk_"), "_")
	assert.NotContains(t, logs.String(), secret)
}
