// Package api provides HTTP handlers for the PiWorker API.
package api

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
)

const sessionCookie = "piworker_session"

type contextKey string

const ContextKeyUser contextKey = "user"
const contextSessionCheck contextKey = "session-check"

type LoginRequest struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	Code            string `json:"code"`
	CurrentPassword string `json:"currentPassword"`
	// KeepAgentTokens keeps agent API tokens valid across a password change.
	KeepAgentTokens bool `json:"keepAgentTokens"`
}

// AgentTokenRevoker revokes every agent API token as part of a credential
// reset (agentauth.TokenVerifier).
type AgentTokenRevoker interface {
	// RevokeAllInTx revokes every token inside tx, the transaction that
	// changes the password, so both commit together or neither does.
	RevokeAllInTx(tx *sql.Tx) error
	// EvictAll drops cached verifications once that transaction committed.
	EvictAll()
}
type AuthStatusResponse struct {
	Enabled       bool `json:"enabled"`
	SetupRequired bool `json:"setupRequired"`
}
type AuthHandler struct {
	store   *storage.SQLiteUserStore
	secure  bool
	origins map[string]bool
	// login throttles unauthenticated credential/code attempts; password
	// throttles current-password checks by signed-in users separately, so they
	// never consume the login budget.
	login    *loginLimiter
	password *loginLimiter
	// agentTokens, when set, is revoked on recovery and password change.
	agentTokens AgentTokenRevoker
	// secureCookies marks session cookies Secure on every response, set when
	// PiWorker serves HTTPS itself.
	secureCookies bool
}

// SetAgentTokenRevoker makes account recovery, and password changes that do
// not ask to keep them, revoke every agent token in this process.
func (h *AuthHandler) SetAgentTokenRevoker(r AgentTokenRevoker) { h.agentTokens = r }

// tokenRevocation prepares revoking every agent token in a credential
// change's transaction. It returns the hooks to pass to the store (none when
// revoke is false or no revoker is set), a function reporting whether the
// revocation itself failed, and one to call after the change committed.
func (h *AuthHandler) tokenRevocation(revoke bool) (hooks []storage.TxHook, failed func() bool, committed func()) {
	if !revoke || h.agentTokens == nil {
		return nil, func() bool { return false }, func() {}
	}
	var revokeErr error
	hook := func(tx *sql.Tx) error {
		revokeErr = h.agentTokens.RevokeAllInTx(tx)
		return revokeErr
	}
	return []storage.TxHook{hook}, func() bool { return revokeErr != nil }, h.agentTokens.EvictAll
}

// AuthOption configures an AuthHandler.
type AuthOption func(*AuthHandler)

// WithSecureCookies marks session cookies Secure regardless of how a request
// arrived. PiWorker enables it whenever it serves HTTPS itself.
func WithSecureCookies(on bool) AuthOption {
	return func(h *AuthHandler) { h.secureCookies = on }
}

func NewAuthHandler(store *storage.SQLiteUserStore, secure bool, origins []string, opts ...AuthOption) *AuthHandler {
	h := &AuthHandler{store: store, secure: secure, origins: make(map[string]bool), login: newLoginLimiter(), password: newLoginLimiter()}
	for _, origin := range origins {
		h.origins[origin] = true
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}
func AuthStatusHandler(enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		writeSuccess(w, 200, AuthStatusResponse{Enabled: enabled}, "")
	}
}
func (h *AuthHandler) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/api/auth/status", h.Status).Methods("GET")
	router.HandleFunc("/api/auth/login", h.Login).Methods("POST")
	router.HandleFunc("/api/auth/setup", h.Setup).Methods("POST")
	router.HandleFunc("/api/auth/recover", h.Recover).Methods("POST")
	router.HandleFunc("/api/auth/session", h.CurrentSession).Methods("GET")
	router.HandleFunc("/api/auth/logout", h.Logout).Methods("POST")
	router.HandleFunc("/api/auth/password", h.Password).Methods("POST")
}
func (h *AuthHandler) Status(w http.ResponseWriter, r *http.Request) {
	exists, err := h.store.UserExists()
	if err != nil {
		writeError(w, 500, "Cannot read account status")
		return
	}
	writeSuccess(w, 200, AuthStatusResponse{Enabled: true, SetupRequired: !exists}, "")
}

// readBody decodes a bounded credentials body and trims the username.
func readBody(w http.ResponseWriter, r *http.Request) (LoginRequest, bool) {
	var req LoginRequest
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := json.UnmarshalRead(r.Body, &req); err != nil {
		writeError(w, 400, "Invalid request body")
		return req, false
	}
	req.Username = strings.TrimSpace(req.Username)
	return req, true
}

// reserve asks lim for an attempt by (peer IP, user), answering 429 with a
// Retry-After when refused.
func reserve(w http.ResponseWriter, lim *loginLimiter, ip, user string) bool {
	ok, wait := lim.allow(ip, user)
	if ok {
		return true
	}
	secs := int((wait + time.Second - 1) / time.Second)
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeError(w, 429, "Too many attempts. Please wait before trying again.")
	return false
}

// readCredentials decodes the body and reserves a login attempt for it.
func (h *AuthHandler) readCredentials(w http.ResponseWriter, r *http.Request) (LoginRequest, string, bool) {
	req, ok := readBody(w, r)
	if !ok {
		return req, "", false
	}
	ip := peerIP(r)
	return req, ip, reserve(w, h.login, ip, req.Username)
}

var dummyBcryptHash, _ = bcrypt.GenerateFromPassword([]byte("piworker-dummy-password"), bcrypt.DefaultCost)

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	req, ip, ok := h.readCredentials(w, r)
	if !ok {
		return
	}
	user, err := h.store.GetUser(req.Username)
	if err != nil {
		writeError(w, 500, "Cannot read account")
		return
	}
	hash := dummyBcryptHash
	if user != nil {
		hash = []byte(user.PasswordHash)
	}
	if err = bcrypt.CompareHashAndPassword(hash, []byte(req.Password)); err != nil || user == nil {
		h.login.failure(ip, req.Username)
		writeError(w, 401, "Invalid credentials")
		return
	}
	h.login.success(ip, req.Username)
	token, err := h.store.CreateSession(user.Username, user.PasswordHash, time.Now())
	if err != nil {
		writeError(w, 500, "Cannot create session. Please try again.")
		return
	}
	h.setCookie(w, r, token, int(storage.SessionLifetime/time.Second))
	writeSuccess(w, 200, map[string]string{"username": user.Username}, "")
}
func (h *AuthHandler) setCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	cookie := &http.Cookie{Name: sessionCookie, Value: value, Path: "/api", HttpOnly: true, Secure: h.secure || h.secureCookies || r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: maxAge}
	if maxAge < 0 {
		cookie.Expires = time.Unix(1, 0)
	} else {
		cookie.Expires = time.Now().Add(time.Duration(maxAge) * time.Second)
	}
	http.SetCookie(w, cookie)
}
func (h *AuthHandler) CurrentSession(w http.ResponseWriter, r *http.Request) {
	session, _ := GetUserFromContext(r.Context())
	writeSuccess(w, 200, session, "")
}
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		writeError(w, 401, "Session required")
		return
	}
	if err = h.store.DeleteSession(cookie.Value); err != nil {
		writeError(w, 500, "Could not sign out. Please retry.")
		return
	}
	h.setCookie(w, r, "", -1)
	writeSuccess(w, 200, nil, "")
}
func validNewPassword(password string) bool { return len(password) >= 8 && len(password) <= 72 }

// MaxUsernameLength bounds account names for setup, recovery and bootstrap.
const MaxUsernameLength = 128

// NormalizeNewCredentials applies the same rules as browser setup: the
// username is trimmed (login trims too) and must be 1..MaxUsernameLength bytes,
// and the password must be 8..72 bytes (bcrypt's limit).
func NormalizeNewCredentials(username, password string) (string, bool) {
	username = strings.TrimSpace(username)
	return username, username != "" && len(username) <= MaxUsernameLength && validNewPassword(password)
}

func (h *AuthHandler) Setup(w http.ResponseWriter, r *http.Request)   { h.redeem(w, r, "setup") }
func (h *AuthHandler) Recover(w http.ResponseWriter, r *http.Request) { h.redeem(w, r, "recovery") }
func (h *AuthHandler) redeem(w http.ResponseWriter, r *http.Request, purpose string) {
	req, ip, ok := h.readCredentials(w, r)
	if !ok {
		return
	}
	if _, ok := NormalizeNewCredentials(req.Username, req.Password); !ok {
		writeError(w, 400, "Enter a username and a password between 8 and 72 bytes.")
		return
	}
	if len(req.Code) != 43 {
		h.login.failure(ip, req.Username)
		writeError(w, 403, "Invalid or expired code")
		return
	}
	hash, err := HashPassword(req.Password)
	if err != nil {
		writeError(w, 500, "Cannot set password")
		return
	}
	// Recovery assumes the account was compromised: agent tokens are revoked
	// in the same transaction (a wrong code never reaches it).
	hooks, revokeFailed, revoked := h.tokenRevocation(purpose == "recovery")
	err = h.store.RedeemAuthCode(purpose, req.Code, req.Username, hash, time.Now(), hooks...)
	if errors.Is(err, storage.ErrAuthCode) {
		h.login.failure(ip, req.Username)
		writeError(w, 403, "Invalid or expired code, or setup already completed")
		return
	}
	if err != nil && revokeFailed() {
		writeError(w, 500, "The password was not reset because agent tokens could not be revoked. Please retry with the same code.")
		return
	}
	if err != nil {
		writeError(w, 500, "Cannot update account")
		return
	}
	revoked()
	h.login.success(ip, req.Username)
	h.setCookie(w, r, "", -1)
	status := 200
	if purpose == "setup" {
		status = 201
	}
	writeSuccess(w, status, nil, "")
}
func (h *AuthHandler) Password(w http.ResponseWriter, r *http.Request) {
	req, ok := readBody(w, r)
	if !ok {
		return
	}
	if !validNewPassword(req.Password) {
		writeError(w, 400, "Password must be between 8 and 72 bytes.")
		return
	}
	session, ok := GetUserFromContext(r.Context())
	if !ok || session == nil {
		writeError(w, 401, "Sign in to continue")
		return
	}
	// Signed-in users have their own budget, keyed by account, so a password
	// change never eats into the login budget for this IP.
	ip := peerIP(r)
	if !reserve(w, h.password, ip, session.Username) {
		return
	}
	user, err := h.store.GetUser(session.Username)
	if err != nil {
		writeError(w, 500, "Cannot read account")
		return
	}
	if user == nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)) != nil {
		h.password.failure(ip, session.Username)
		writeError(w, 403, "Current password is incorrect")
		return
	}
	h.password.success(ip, session.Username)
	hash, err := HashPassword(req.Password)
	if err != nil {
		writeError(w, 500, "Cannot set password")
		return
	}
	hooks, revokeFailed, revoked := h.tokenRevocation(!req.KeepAgentTokens)
	if err = h.store.ChangePassword(user.Username, user.PasswordHash, hash, hooks...); err != nil {
		if revokeFailed() {
			writeError(w, 500, "The password was not changed because agent tokens could not be revoked. Please retry.")
			return
		}
		writeError(w, 500, "Cannot change password. Please retry.")
		return
	}
	revoked()
	h.setCookie(w, r, "", -1)
	writeSuccess(w, 200, nil, "")
}
func (h *AuthHandler) csrfAllowed(r *http.Request) bool {
	return csrfAllowed(r, h.secure, h.origins)
}
func AuthMiddleware(h *AuthHandler) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isGuardedAPIPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" && !h.csrfAllowed(r) {
				writeError(w, 403, "Request origin could not be verified")
				return
			}
			switch r.URL.Path {
			case "/api/auth/login", "/api/auth/setup", "/api/auth/recover", "/api/auth/status", "/api/health":
				next.ServeHTTP(w, r)
				return
			}
			cookie, err := r.Cookie(sessionCookie)
			if err != nil {
				writeError(w, 401, "Sign in to continue")
				return
			}
			session, err := h.store.GetSession(cookie.Value, time.Now(), true)
			if errors.Is(err, storage.ErrSessionNotFound) {
				h.setCookie(w, r, "", -1)
				writeError(w, 401, "Session expired. Please sign in again.")
				return
			}
			if err != nil {
				writeError(w, 503, "Cannot validate session. Please retry.")
				return
			}
			ctx := context.WithValue(r.Context(), ContextKeyUser, session)
			// Streams recheck this without extending idle expiry, including after logout.
			check := func() bool { _, err := h.store.GetSession(cookie.Value, time.Now(), false); return err == nil }
			ctx = context.WithValue(ctx, contextSessionCheck, check)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
func GetUserFromContext(ctx context.Context) (*storage.Session, bool) {
	v, ok := ctx.Value(ContextKeyUser).(*storage.Session)
	return v, ok
}
func sessionStillValid(ctx context.Context) bool {
	check, ok := ctx.Value(contextSessionCheck).(func() bool)
	return !ok || check()
}
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}
