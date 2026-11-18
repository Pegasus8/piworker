package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GET /api/secrets?usage=1 says where each secret is used (flow, node, setting, or
// a broker connection's credentials) and lists the references to secrets
// that are not defined, so the admin sees what deleting or renaming one
// breaks. Values are never returned.
func TestListSecretsShowsWhereEachIsUsed(t *testing.T) {
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "s.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	sec := secrets.NewStore()
	require.NoError(t, sec.Set("smtp_pass", "a-long-value-of-the-password"))
	require.NoError(t, sec.Set("unused", "another-long-value-here"))
	conns := brokers.NewStore()
	require.NoError(t, conns.Set(brokers.Connection{Name: "home", Broker: "tcp://localhost:1883", Password: "{{secret.smtp_pass}}"}))

	f := flow.NewFlow("Mailer")
	mail := flow.NewNode("action-email", flow.NodeCategoryOutput)
	mail.ID = "mail"
	mail.Config = map[string]any{"password": "{{secret.smtp_pass}}", "username": "{{secret.ghost}}"}
	f.AddNode(*mail)
	require.NoError(t, store.CreateFlow(f))

	h := NewSecretsHandler(sec, zerolog.Nop()).WithUsage(store, conns)
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/secrets", nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.NotContains(t, rr.Body.String(), `"uses"`, "uses only on request: they read every flow")

	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/secrets?usage=1", nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.NotContains(t, rr.Body.String(), "a-long-value-of-the-password")

	var resp struct {
		Data SecretsListResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.Data.Secrets, 2)
	assert.Equal(t, "smtp_pass", resp.Data.Secrets[0].Name)
	assert.Equal(t, []SecretUse{
		{Connection: "home", Setting: "password"},
		{FlowID: f.ID, FlowName: "Mailer", NodeID: "mail", NodeType: "action-email", Setting: "password"},
	}, resp.Data.Secrets[0].Uses)
	assert.Equal(t, "unused", resp.Data.Secrets[1].Name)
	assert.Empty(t, resp.Data.Secrets[1].Uses)
	assert.Equal(t, []UndefinedSecret{{Name: "ghost", Uses: []SecretUse{
		{FlowID: f.ID, FlowName: "Mailer", NodeID: "mail", NodeType: "action-email", Setting: "username"},
	}}}, resp.Data.Undefined)

	// Without usage sources (older wiring) the listing still works.
	bare := NewSecretsHandler(sec, zerolog.Nop())
	r = mux.NewRouter()
	bare.RegisterRoutes(r)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/secrets", nil))
	require.Equal(t, http.StatusOK, rr.Code)
}

// The usage listing also names the LLM connections whose API key references
// a secret (llm.SecretReferences), after the broker connections and before
// the flows; a reference to an undefined secret is listed as undefined.
// The key itself is never returned.
func TestListSecretsShowsLLMConnectionUses(t *testing.T) {
	sec := secrets.NewStore()
	require.NoError(t, sec.Set("anthropic", "zzSECRETzz-anthropic-api-key"))
	conns := brokers.NewStore()
	require.NoError(t, conns.Set(brokers.Connection{Name: "home", Broker: "tcp://localhost:1883", Password: "{{secret.anthropic}}"}))
	models := llm.NewStore()
	require.NoError(t, models.Set(llm.Connection{Name: "claude", Provider: llm.ProviderAnthropic, APIKey: "{{secret.anthropic}}"}))
	require.NoError(t, models.Set(llm.Connection{Name: "router", Provider: llm.ProviderOpenAI, APIKey: "{{secret.openrouter}}"}))
	require.NoError(t, models.Set(llm.Connection{Name: "inline", Provider: llm.ProviderOpenAI, APIKey: "zzSECRETzz-plain-key"}))

	h := NewSecretsHandler(sec, zerolog.Nop()).WithUsage(nil, conns).WithLLMConnections(models)
	r := mux.NewRouter()
	h.RegisterRoutes(r)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/secrets?usage=1", nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.NotContains(t, rr.Body.String(), "zzSECRETzz")

	var resp struct {
		Data SecretsListResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.Data.Secrets, 1)
	assert.Equal(t, []SecretUse{
		{Connection: "home", Setting: "password"},
		{LLMConnection: "claude", Setting: "apiKey"},
	}, resp.Data.Secrets[0].Uses)
	assert.Equal(t, []UndefinedSecret{{Name: "openrouter", Uses: []SecretUse{{LLMConnection: "router", Setting: "apiKey"}}}}, resp.Data.Undefined)
	assert.Contains(t, rr.Body.String(), `"llmConnection":"claude"`)
}
