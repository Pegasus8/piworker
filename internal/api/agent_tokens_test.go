package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/agentauth"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type agentTestEnv struct {
	router   *mux.Router
	store    *storage.SQLiteStore
	users    *storage.SQLiteUserStore
	auth     *AuthHandler
	agent    *AgentHandler
	verifier *agentauth.TokenVerifier
	cookie   *http.Cookie
}

func newAgentTestEnv(t *testing.T, opts AgentHandlerOptions) agentTestEnv {
	t.Helper()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "agent.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	users, err := storage.NewSQLiteUserStoreWithDB(store.DB())
	require.NoError(t, err)
	hash, err := HashPassword("test-password")
	require.NoError(t, err)
	require.NoError(t, users.CreateUser(&types.User{Username: "admin", PasswordHash: hash}))

	auth := NewAuthHandler(users, false, nil)
	verifier := agentauth.NewTokenVerifier(store)
	auth.SetAgentTokenRevoker(verifier)
	agent := NewAgentHandler(store, auth, verifier, opts)
	router := mux.NewRouter()
	auth.RegisterRoutes(router)
	agent.RegisterRoutes(router)
	router.Use(AuthMiddleware(auth))
	router.HandleFunc("/api/private", func(w http.ResponseWriter, r *http.Request) { writeSuccess(w, http.StatusOK, nil, "") }).Methods("GET")
	env := agentTestEnv{router: router, store: store, users: users, auth: auth, agent: agent, verifier: verifier}
	env.cookie = loginCookie(t, router)
	return env
}

func (e agentTestEnv) do(method, path string, body any) *httptest.ResponseRecorder {
	return authRequest(e.router, method, path, body, e.cookie)
}

// data decodes the {success, data} envelope into T.
func data[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var env struct {
		Success bool `json:"success"`
		Data    T    `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), w.Body.String())
	require.True(t, env.Success, w.Body.String())
	return env.Data
}

func errorMessage(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var env APIResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), w.Body.String())
	require.False(t, env.Success)
	return env.Error
}

type createdToken struct {
	Token string           `json:"token"`
	Info  storage.APIToken `json:"info"`
}

func (e agentTestEnv) createToken(t *testing.T, body map[string]any) createdToken {
	t.Helper()
	if _, ok := body["password"]; !ok {
		body["password"] = "test-password"
	}
	w := e.do("POST", "/api/agent/tokens", body)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	return data[createdToken](t, w)
}

func countTokens(t *testing.T, s *storage.SQLiteStore) int {
	t.Helper()
	list, err := s.ListAPITokens()
	require.NoError(t, err)
	return len(list)
}

func TestAgentRoutesRequireSessionAndCSRF(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	for _, path := range []string{"/api/agent/tokens"} {
		w := authRequest(env.router, "GET", path, nil, nil)
		require.Equal(t, http.StatusUnauthorized, w.Code, path)
	}
	r := httptest.NewRequest("POST", "http://piworker.local/api/agent/tokens", strings.NewReader(`{"name":"x","scopes":["call"],"password":"test-password"}`))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(env.cookie)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, r)
	require.Equal(t, http.StatusForbidden, w.Code, "token creation needs the editor's CSRF header")
	require.Equal(t, 0, countTokens(t, env.store))
}

func TestCreateTokenRequiresPasswordStepUp(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	body := map[string]any{"name": "laptop", "scopes": []string{"call", "read"}}

	w := env.do("POST", "/api/agent/tokens", body)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Contains(t, errorMessage(t, w), "password")

	body["password"] = "wrong-password"
	w = env.do("POST", "/api/agent/tokens", body)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Equal(t, 0, countTokens(t, env.store))

	created := env.createToken(t, map[string]any{"name": "laptop", "scopes": []string{"call", "read"}})
	require.Regexp(t, regexp.MustCompile(`^pwk_[a-z2-7]{16}_[a-z2-7]{52}$`), created.Token)
	assert.Equal(t, "laptop", created.Info.Name)
	assert.Equal(t, []string{"call", "read"}, created.Info.Scopes)
	assert.False(t, created.Info.Privileged)
	assert.WithinDuration(t, time.Now().Add(storage.DefaultAPITokenLifetime), created.Info.ExpiresAt, time.Minute)

	info, err := env.verifier.Verify(created.Token)
	require.NoError(t, err, "the returned plaintext works once and is never shown again")
	assert.Equal(t, created.Info.ID, info.ID)
}

func TestStepUpFailuresAreRateLimited(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	body := map[string]any{"name": "x", "scopes": []string{"call"}, "password": "wrong-password"}
	for range 10 {
		require.Equal(t, http.StatusForbidden, env.do("POST", "/api/agent/tokens", body).Code)
	}
	body["password"] = "test-password"
	w := env.do("POST", "/api/agent/tokens", body)
	require.Equal(t, http.StatusTooManyRequests, w.Code, "even the right password waits once the budget is spent")
	require.NotEmpty(t, w.Header().Get("Retry-After"))
	require.Equal(t, 0, countTokens(t, env.store))

	// The login budget for this IP is untouched.
	w = authRequest(env.router, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "test-password"}, nil)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestCreateTokenScopePolicy(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	w := env.do("POST", "/api/agent/tokens", map[string]any{"name": "x", "scopes": []string{"call", "deploy"}, "password": "test-password"})
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Contains(t, errorMessage(t, w), "deploy")
	w = env.do("POST", "/api/agent/tokens", map[string]any{"name": "x", "scopes": []string{"author"}, "password": "test-password"})
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Contains(t, errorMessage(t, w), "author")
	require.Equal(t, 0, countTokens(t, env.store))

	require.NoError(t, env.store.SetAgentDeployLevel(storage.AgentDeployLow))
	created := env.createToken(t, map[string]any{"name": "deployer", "scopes": []string{"deploy"}})
	assert.Equal(t, []string{"deploy"}, created.Info.Scopes)

	authoring := newAgentTestEnv(t, AgentHandlerOptions{AuthorEnabled: true})
	created = authoring.createToken(t, map[string]any{"name": "author", "scopes": []string{"author", "read"}})
	assert.Equal(t, []string{"author", "read"}, created.Info.Scopes)
}

func TestCreateTokenFields(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	created := env.createToken(t, map[string]any{
		"name":          "kitchen",
		"scopes":        []string{"call"},
		"privileged":    true,
		"allTools":      true,
		"expiresInDays": 7,
		"grants":        []map[string]any{{"flowId": "f", "triggerNodeId": "n", "fingerprint": "fp", "suspended": true}},
	})
	assert.True(t, created.Info.Privileged)
	assert.True(t, created.Info.AllTools)
	assert.WithinDuration(t, time.Now().Add(7*24*time.Hour), created.Info.ExpiresAt, time.Minute)
	assert.Equal(t, []storage.TokenGrant{{FlowID: "f", TriggerNodeID: "n", Fingerprint: "fp"}}, created.Info.Grants, "new grants start active")
}

func TestCreateTokenValidation(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	cases := map[string]map[string]any{
		"empty name":       {"name": " ", "scopes": []string{"call"}},
		"no scopes":        {"name": "x", "scopes": []string{}},
		"unknown scope":    {"name": "x", "scopes": []string{"root"}},
		"negative expiry":  {"name": "x", "scopes": []string{"call"}, "expiresInDays": -1},
		"expiry too long":  {"name": "x", "scopes": []string{"call"}, "expiresInDays": 366},
		"bad grant":        {"name": "x", "scopes": []string{"call"}, "grants": []map[string]any{{"flowId": "f"}}},
		"unknown field":    {"name": "x", "scopes": []string{"call"}, "admin": true},
		"wrong field type": {"name": "x", "scopes": "call"},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			body["password"] = "test-password"
			w := env.do("POST", "/api/agent/tokens", body)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}
	require.Equal(t, 0, countTokens(t, env.store))
}

type tokenListResponse struct {
	Tokens []struct {
		storage.APIToken
		Status string `json:"status"`
	} `json:"tokens"`
}

func TestListTokensNeverExposesSecrets(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	active := env.createToken(t, map[string]any{"name": "active", "scopes": []string{"call"}})
	revoked := env.createToken(t, map[string]any{"name": "revoked", "scopes": []string{"call"}})
	require.Equal(t, http.StatusOK, env.do("DELETE", "/api/agent/tokens/"+revoked.Info.ID, nil).Code)
	_, _, err := env.store.CreateAPIToken(storage.NewAPIToken{Name: "old", Scopes: []string{"read"}, ExpiresAt: time.Now().Add(-time.Minute)}, time.Now().Add(-time.Hour))
	require.NoError(t, err)

	w := env.do("GET", "/api/agent/tokens", nil)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	for _, tok := range []createdToken{active, revoked} {
		assert.Empty(t, tok.Info.SecretSHA256, "the hash is not even sent on creation")
		_, secret, _ := storage.ParseAPIToken(tok.Token)
		assert.NotContains(t, body, secret)
		stored, err := env.store.GetAPIToken(tok.Info.ID)
		require.NoError(t, err)
		require.NotEmpty(t, stored.SecretSHA256)
		assert.NotContains(t, body, stored.SecretSHA256)
	}
	assert.NotContains(t, body, "secret")
	list := data[tokenListResponse](t, w)
	status := map[string]string{}
	for _, tok := range list.Tokens {
		status[tok.Name] = tok.Status
	}
	assert.Equal(t, map[string]string{"active": "active", "revoked": "revoked", "old": "expired"}, status)
}

func TestRevokeTokenTakesEffectImmediately(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	created := env.createToken(t, map[string]any{"name": "x", "scopes": []string{"call"}})
	_, err := env.verifier.Verify(created.Token)
	require.NoError(t, err)

	w := env.do("DELETE", "/api/agent/tokens/"+created.Info.ID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	_, err = env.verifier.Verify(created.Token)
	require.ErrorIs(t, err, agentauth.ErrInvalidToken)

	require.Equal(t, http.StatusNotFound, env.do("DELETE", "/api/agent/tokens/unknown", nil).Code)
}

func TestUpdateTokenGrants(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	created := env.createToken(t, map[string]any{
		"name": "x", "scopes": []string{"call"},
		"grants": []map[string]any{{"flowId": "f", "triggerNodeId": "n", "fingerprint": "v1"}},
	})
	id := created.Info.ID
	path := "/api/agent/tokens/" + id + "/grants"
	_, err := env.verifier.Verify(created.Token) // warm the cache
	require.NoError(t, err)
	grantsNow := func() []storage.TokenGrant {
		info, err := env.verifier.Verify(created.Token)
		require.NoError(t, err)
		return info.Grants
	}

	w := env.do("POST", path, map[string]any{"action": "suspend", "flowId": "f", "triggerNodeId": "n"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, []storage.TokenGrant{{FlowID: "f", TriggerNodeID: "n", Fingerprint: "v1", Suspended: true}}, grantsNow(), "the verifier sees grant changes at once")

	w = env.do("POST", path, map[string]any{"action": "confirm", "flowId": "f", "triggerNodeId": "n", "fingerprint": "v2"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, []storage.TokenGrant{{FlowID: "f", TriggerNodeID: "n", Fingerprint: "v2"}}, grantsNow())

	add := map[string]any{"action": "add", "flowId": "g", "triggerNodeId": "m", "fingerprint": "w1"}
	w = env.do("POST", path, add)
	require.Equal(t, http.StatusForbidden, w.Code, "adding a grant widens the token and needs the password")
	add["password"] = "test-password"
	w = env.do("POST", path, add)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, grantsNow(), 2)
	require.Equal(t, http.StatusConflict, env.do("POST", path, add).Code, "duplicate grant")

	w = env.do("POST", path, map[string]any{"action": "remove", "flowId": "f", "triggerNodeId": "n"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, []storage.TokenGrant{{FlowID: "g", TriggerNodeID: "m", Fingerprint: "w1"}}, grantsNow())

	for name, tc := range map[string]struct {
		body map[string]any
		code int
	}{
		"missing grant":       {map[string]any{"action": "confirm", "flowId": "zz", "triggerNodeId": "n", "fingerprint": "x"}, http.StatusNotFound},
		"confirm without fp":  {map[string]any{"action": "confirm", "flowId": "g", "triggerNodeId": "m"}, http.StatusBadRequest},
		"unknown action":      {map[string]any{"action": "grant-all", "flowId": "g", "triggerNodeId": "m"}, http.StatusBadRequest},
		"missing identifiers": {map[string]any{"action": "suspend"}, http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.code, env.do("POST", path, tc.body).Code)
		})
	}

	require.Equal(t, http.StatusNotFound, env.do("POST", "/api/agent/tokens/unknown/grants", map[string]any{"action": "suspend", "flowId": "g", "triggerNodeId": "m"}).Code)
	require.Equal(t, http.StatusOK, env.do("DELETE", "/api/agent/tokens/"+id, nil).Code)
	require.Equal(t, http.StatusNotFound, env.do("POST", path, map[string]any{"action": "confirm", "flowId": "g", "triggerNodeId": "m", "fingerprint": "w2"}).Code, "revoked tokens cannot be re-enabled through grants")
}
