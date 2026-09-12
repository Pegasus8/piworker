package api

import (
	"bytes"
	"encoding/json"
	"github.com/Pegasus8/piworker/internal/events"
	"github.com/rs/zerolog"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

func sessionTestRouter(t *testing.T, existing bool) (*mux.Router, *AuthHandler, *storage.SQLiteUserStore) {
	t.Helper()
	store, err := storage.NewSQLiteUserStore(filepath.Join(t.TempDir(), "auth.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	if existing {
		hash, err := HashPassword("test-password")
		require.NoError(t, err)
		require.NoError(t, store.CreateUser(&types.User{Username: "admin", PasswordHash: hash}))
	}
	h := NewAuthHandler(store, false, nil)
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	router.Use(AuthMiddleware(h))
	router.HandleFunc("/api/private", func(w http.ResponseWriter, r *http.Request) { writeSuccess(w, http.StatusOK, nil, "") }).Methods("GET", "POST")
	return router, h, store
}
func authRequest(router http.Handler, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	var b bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&b).Encode(body)
	}
	r := httptest.NewRequest(method, "http://piworker.local"+path, &b)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-PiWorker-Request", "1")
	r.Header.Set("Origin", "http://piworker.local")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}
func loginCookie(t *testing.T, router http.Handler) *http.Cookie {
	t.Helper()
	w := authRequest(router, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "test-password"}, nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotEmpty(t, w.Result().Cookies())
	return w.Result().Cookies()[0]
}
func TestCookieSessionLoginLogoutAndLegacyRejection(t *testing.T) {
	router, _, _ := sessionTestRouter(t, true)
	cookie := loginCookie(t, router)
	require.True(t, cookie.HttpOnly)
	require.False(t, cookie.Secure)
	require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	require.Equal(t, 200, authRequest(router, "GET", "/api/private", nil, cookie).Code)
	require.Equal(t, 200, authRequest(router, "POST", "/api/auth/logout", nil, cookie).Code)
	require.Equal(t, 401, authRequest(router, "GET", "/api/private", nil, cookie).Code)
	req := httptest.NewRequest("GET", "/api/private", nil)
	req.Header.Set("Authorization", "Bearer old-jwt")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, 401, w.Code)
}
func TestSessionCSRFAndSecureCookie(t *testing.T) {
	router, h, _ := sessionTestRouter(t, true)
	cookie := loginCookie(t, router)
	h.secure = true
	reqSecure := httptest.NewRequest("POST", "https://piworker.local/api/auth/login", strings.NewReader(`{"username":"admin","password":"test-password"}`))
	reqSecure.Header.Set("Origin", "https://piworker.local")
	reqSecure.Header.Set("X-PiWorker-Request", "1")
	secureResponse := httptest.NewRecorder()
	router.ServeHTTP(secureResponse, reqSecure)
	require.Equal(t, 200, secureResponse.Code)
	require.True(t, secureResponse.Result().Cookies()[0].Secure)
	for _, origin := range []string{"http://evil.example", "null"} {
		req := httptest.NewRequest("POST", "http://piworker.local/api/auth/logout", strings.NewReader("{}"))
		req.AddCookie(cookie)
		req.Header.Set("Origin", origin)
		req.Header.Set("X-PiWorker-Request", "1")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, 403, w.Code)
	}
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"test-password"}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, 403, w.Code)
}
func TestSetupCodeIsRequiredAndSingleUse(t *testing.T) {
	router, _, store := sessionTestRouter(t, false)
	code, err := store.NewAuthCode("setup", "", time.Now())
	require.NoError(t, err)
	body := map[string]string{"username": "admin", "password": "test-password", "code": "wrong"}
	require.Equal(t, 403, authRequest(router, "POST", "/api/auth/setup", body, nil).Code)
	body["code"] = code
	require.Equal(t, 201, authRequest(router, "POST", "/api/auth/setup", body, nil).Code)
	body["username"] = "second"
	require.Equal(t, 403, authRequest(router, "POST", "/api/auth/setup", body, nil).Code)
	require.NotNil(t, loginCookie(t, router))
}
func TestRecoveryAndPasswordChangeRevokeSessions(t *testing.T) {
	router, _, store := sessionTestRouter(t, true)
	first := loginCookie(t, router)
	second := loginCookie(t, router)
	code, err := store.NewAuthCode("recovery", "admin", time.Now())
	require.NoError(t, err)
	body := map[string]string{"username": "admin", "password": "replacement-password", "code": code}
	require.Equal(t, 200, authRequest(router, "POST", "/api/auth/recover", body, nil).Code)
	require.Equal(t, 403, authRequest(router, "POST", "/api/auth/recover", body, nil).Code)
	for _, c := range []*http.Cookie{first, second} {
		require.Equal(t, 401, authRequest(router, "GET", "/api/private", nil, c).Code)
	}
	w := authRequest(router, "POST", "/api/auth/login", body, nil)
	require.Equal(t, 200, w.Code)
	c := w.Result().Cookies()[0]
	require.Equal(t, 200, authRequest(router, "POST", "/api/auth/password", map[string]string{"currentPassword": "replacement-password", "password": "third-password"}, c).Code)
	require.Equal(t, 401, authRequest(router, "GET", "/api/private", nil, c).Code)
}
func TestLoginRateLimitedAndBodyBounded(t *testing.T) {
	router, _, _ := sessionTestRouter(t, true)
	for i := 0; i < 10; i++ {
		require.Equal(t, 401, authRequest(router, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "bad"}, nil).Code)
	}
	require.Equal(t, 429, authRequest(router, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "bad"}, nil).Code)
	router, _, _ = sessionTestRouter(t, true)
	require.Equal(t, 400, authRequest(router, "POST", "/api/auth/login", map[string]string{"username": strings.Repeat("x", 20000)}, nil).Code)
}

func TestRevokedSessionStopsLiveEvents(t *testing.T) {
	router, _, store := sessionTestRouter(t, true)
	hub := events.NewHub()
	NewEventsHandler(hub, nil, zerolog.Nop()).RegisterRoutes(router)
	cookie := loginCookie(t, router)
	server := httptest.NewServer(router)
	defer server.Close()
	req, err := http.NewRequest("GET", server.URL+"/api/flows/f/events", nil)
	require.NoError(t, err)
	req.AddCookie(cookie)
	client := &http.Client{Timeout: time.Second}
	response, err := client.Do(req)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Eventually(t, func() bool { return hub.HasSubscribers("f") }, time.Second, time.Millisecond)
	require.NoError(t, store.DeleteSession(cookie.Value))
	hub.Publish(events.Event{FlowID: "f", NodeID: "secret-output", Phase: events.PhaseSuccess})
	payload, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "secret-output")
}

func TestProxyHeadersDoNotOverrideLocalCookieMode(t *testing.T) {
	router, _, _ := sessionTestRouter(t, true)
	req := httptest.NewRequest("POST", "http://piworker.local/api/auth/login", strings.NewReader(`{"username":"admin","password":"test-password"}`))
	req.Header.Set("X-PiWorker-Request", "1")
	req.Header.Set("Origin", "http://piworker.local")
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code)
	require.False(t, w.Result().Cookies()[0].Secure)
}
