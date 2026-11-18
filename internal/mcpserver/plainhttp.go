package mcpserver

import (
	"fmt"
	"net/http"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/rs/zerolog"
)

// RefusePlainHTTP answers /mcp on the plain-HTTP listener while PiWorker
// serves HTTPS: 400 with the HTTPS URL to use, never a redirect. A client
// following a redirect (Go's http.Client keeps the Authorization header for
// the same host on another port) would keep working while sending its token
// in clear text on every request; refusing makes the misconfiguration fail
// on the first one. The token's id is logged (its secret never) so the
// admin knows which token to revoke.
func RefusePlainHTTP(w http.ResponseWriter, r *http.Request, httpsURL string, log zerolog.Logger) {
	ev := log.Warn().Str("client", r.RemoteAddr).Str("use", httpsURL)
	if raw, ok := bearerToken(r); ok {
		if id, _, parsed := storage.ParseAPIToken(raw); parsed {
			ev = ev.Str("tokenId", id)
		}
	}
	ev.Msg("Refused an MCP request over plain HTTP; its token crossed the network in clear text")
	writeJSONError(w, http.StatusBadRequest, fmt.Sprintf(
		"MCP requires HTTPS: use %s. A token sent here crossed the network in clear text; consider revoking it in PiWorker (Settings, Agent tokens).", httpsURL))
}
