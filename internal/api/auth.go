// Package api provides HTTP handlers for the PiWorker API.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/webhook"
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
}
type AuthStatusResponse struct {
	Enabled       bool `json:"enabled"`
	SetupRequired bool `json:"setupRequired"`
}
type attemptWindow struct {
	count int
	until time.Time
}
type AuthHandler struct {
	store    *storage.SQLiteUserStore
	secure   bool
	origins  map[string]bool
	mu       sync.Mutex
	attempts map[string]attemptWindow
	global   attemptWindow
}

func NewAuthHandler(store *storage.SQLiteUserStore, secure bool, origins []string) *AuthHandler {
	h := &AuthHandler{store: store, secure: secure, origins: make(map[string]bool), attempts: make(map[string]attemptWindow)}
	for _, origin := range origins {
		h.origins[origin] = true
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

// Rate limiting uses the socket peer, never an untrusted forwarded IP. A global
// budget and bounded peer map prevent bypass through unbounded source addresses.
func (h *AuthHandler) allowAttempt(r *http.Request) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if !now.Before(h.global.until) {
		h.global = attemptWindow{until: now.Add(time.Minute)}
		for ip, v := range h.attempts {
			if !now.Before(v.until) {
				delete(h.attempts, ip)
			}
		}
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	v, ok := h.attempts[ip]
	if !ok && len(h.attempts) >= 2048 {
		return false
	}
	if !now.Before(v.until) {
		v = attemptWindow{until: now.Add(time.Minute)}
	}
	if v.count >= 10 || h.global.count >= 100 {
		return false
	}
	v.count++
	h.global.count++
	h.attempts[ip] = v
	return true
}
func (h *AuthHandler) readCredentials(w http.ResponseWriter, r *http.Request) (LoginRequest, bool) {
	var req LoginRequest
	if !h.allowAttempt(r) {
		w.Header().Set("Retry-After", "60")
		writeError(w, 429, "Too many attempts. Try again in a minute.")
		return req, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid request body")
		return req, false
	}
	req.Username = strings.TrimSpace(req.Username)
	return req, true
}

var dummyBcryptHash, _ = bcrypt.GenerateFromPassword([]byte("piworker-dummy-password"), bcrypt.DefaultCost)

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	req, ok := h.readCredentials(w, r)
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
		writeError(w, 401, "Invalid credentials")
		return
	}
	token, err := h.store.CreateSession(user.Username, user.PasswordHash, time.Now())
	if err != nil {
		writeError(w, 500, "Cannot create session. Please try again.")
		return
	}
	h.setCookie(w, r, token, int(storage.SessionLifetime/time.Second))
	writeSuccess(w, 200, map[string]string{"username": user.Username}, "")
}
func (h *AuthHandler) setCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	cookie := &http.Cookie{Name: sessionCookie, Value: value, Path: "/api", HttpOnly: true, Secure: h.secure || r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: maxAge}
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
func validNewPassword(password string) bool                           { return len(password) >= 8 && len(password) <= 72 }
func (h *AuthHandler) Setup(w http.ResponseWriter, r *http.Request)   { h.redeem(w, r, "setup") }
func (h *AuthHandler) Recover(w http.ResponseWriter, r *http.Request) { h.redeem(w, r, "recovery") }
func (h *AuthHandler) redeem(w http.ResponseWriter, r *http.Request, purpose string) {
	req, ok := h.readCredentials(w, r)
	if !ok {
		return
	}
	if req.Username == "" || len(req.Username) > 128 || !validNewPassword(req.Password) {
		writeError(w, 400, "Enter a username and a password between 8 and 72 bytes.")
		return
	}
	if len(req.Code) != 43 {
		writeError(w, 403, "Invalid or expired code")
		return
	}
	hash, err := HashPassword(req.Password)
	if err != nil {
		writeError(w, 500, "Cannot set password")
		return
	}
	err = h.store.RedeemAuthCode(purpose, req.Code, req.Username, hash, time.Now())
	if errors.Is(err, storage.ErrAuthCode) {
		writeError(w, 403, "Invalid or expired code, or setup already completed")
		return
	}
	if err != nil {
		writeError(w, 500, "Cannot update account")
		return
	}
	h.setCookie(w, r, "", -1)
	status := 200
	if purpose == "setup" {
		status = 201
	}
	writeSuccess(w, status, nil, "")
}
func (h *AuthHandler) Password(w http.ResponseWriter, r *http.Request) {
	req, ok := h.readCredentials(w, r)
	if !ok {
		return
	}
	if !validNewPassword(req.Password) {
		writeError(w, 400, "Password must be between 8 and 72 bytes.")
		return
	}
	session, _ := GetUserFromContext(r.Context())
	user, err := h.store.GetUser(session.Username)
	if err != nil {
		writeError(w, 500, "Cannot read account")
		return
	}
	if user == nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)) != nil {
		writeError(w, 403, "Current password is incorrect")
		return
	}
	hash, err := HashPassword(req.Password)
	if err != nil {
		writeError(w, 500, "Cannot set password")
		return
	}
	if err = h.store.ChangePassword(user.Username, user.PasswordHash, hash); err != nil {
		writeError(w, 500, "Cannot change password. Please retry.")
		return
	}
	h.setCookie(w, r, "", -1)
	writeSuccess(w, 200, nil, "")
}
func (h *AuthHandler) csrfAllowed(r *http.Request) bool {
	if r.Header.Get("X-PiWorker-Request") != "1" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	scheme := "http"
	if r.TLS != nil || h.secure {
		scheme = "https"
	}
	return origin == scheme+"://"+r.Host || h.origins[origin]
}
func AuthMiddleware(h *AuthHandler) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, webhook.PathPrefix) {
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
