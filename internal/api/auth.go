// Package api provides HTTP handlers for the PiWorker API.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/Pegasus8/piworker/internal/webhook"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
)

// Default auth configuration values.
const (
	DefaultTokenExpiry   = 24 * time.Hour
	DefaultRefreshExpiry = 7 * 24 * time.Hour
)

// dummyBcryptHash is compared against when a user does not exist, so the login
// path always performs a bcrypt comparison regardless of whether the username is
// valid. This keeps the response time constant and defeats username enumeration
// via timing. It is generated once at startup at the same cost as real hashes.
var dummyBcryptHash []byte

func init() {
	h, err := bcrypt.GenerateFromPassword([]byte("piworker-timing-equalizer"), bcrypt.DefaultCost)
	if err == nil {
		dummyBcryptHash = h
	}
}

// publicPaths are endpoints that bypass authentication. Hoisted to a
// package-level set so AuthMiddleware doesn't rebuild it on every request.
var publicPaths = map[string]struct{}{
	"/api/auth/login":    {},
	"/api/auth/register": {},
	"/api/auth/status":   {},
	"/api/health":        {},
	"/metrics":           {},
}

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

// Context keys for auth data.
const (
	ContextKeyUser contextKey = "user"
)

// Claims represents JWT claims.
type Claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// LoginRequest represents the login request body.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse represents the login response.
type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expiresAt"`
}

// UserStore defines the interface for user storage.
type UserStore interface {
	GetUser(username string) (*types.User, error)
	CreateUser(user *types.User) error
	UserExists() (bool, error)
}

// AuthHandler handles authentication-related requests.
type AuthHandler struct {
	jwtSecret   []byte
	tokenExpiry time.Duration
	userStore   UserStore
	logger      zerolog.Logger
}

// AuthOption is a functional option for configuring the AuthHandler.
type AuthOption func(*AuthHandler)

// WithTokenExpiry sets the token expiry duration.
func WithTokenExpiry(expiry time.Duration) AuthOption {
	return func(h *AuthHandler) {
		h.tokenExpiry = expiry
	}
}

// WithUserStore sets the user store.
func WithUserStore(store UserStore) AuthOption {
	return func(h *AuthHandler) {
		h.userStore = store
	}
}

// WithAuthLogger sets the logger for the auth handler.
func WithAuthLogger(logger zerolog.Logger) AuthOption {
	return func(h *AuthHandler) {
		h.logger = logger
	}
}

// NewAuthHandler creates a new AuthHandler.
// UserStore must be provided via WithUserStore option.
func NewAuthHandler(jwtSecret []byte, opts ...AuthOption) *AuthHandler {
	h := &AuthHandler{
		jwtSecret:   jwtSecret,
		tokenExpiry: DefaultTokenExpiry,
		userStore:   nil,
		logger:      zerolog.Nop(),
	}

	for _, opt := range opts {
		opt(h)
	}

	if h.userStore == nil {
		panic("userStore is required - use WithUserStore option")
	}

	return h
}

// AuthStatusResponse represents the response from the auth status endpoint.
type AuthStatusResponse struct {
	Enabled bool `json:"enabled"`
}

// AuthStatusHandler returns a handler that reports whether authentication is enabled.
// This is a standalone function (not a method on AuthHandler) because AuthHandler
// may not exist when auth is disabled.
func AuthStatusHandler(enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, APIResponse{
			Success: true,
			Data:    AuthStatusResponse{Enabled: enabled},
		})
	}
}

// RegisterRoutes registers auth routes on the router.
func (h *AuthHandler) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/api/auth/login", h.Login).Methods(http.MethodPost)
	router.HandleFunc("/api/auth/register", h.Register).Methods(http.MethodPost)
}

// Login authenticates a user and returns a JWT token.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "Username and password are required")
		return
	}

	// Get user from store
	user, err := h.userStore.GetUser(req.Username)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to get user")
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	if user == nil {
		// Still run a bcrypt comparison against a dummy hash so the response
		// time matches the password-wrong path (defeats username enumeration).
		_ = bcrypt.CompareHashAndPassword(dummyBcryptHash, []byte(req.Password))
		writeError(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	// Generate JWT token
	token, expiresAt, err := h.generateToken(user.Username)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to generate token")
		writeError(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	h.logger.Info().
		Str("username", user.Username).
		Msg("User logged in successfully")

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: LoginResponse{
			Token:     token,
			ExpiresAt: expiresAt.Unix(),
		},
	})
}

// Register creates a new user account.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "Username and password are required")
		return
	}

	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}

	// Registration is bootstrap-only. There is no role system, so any account
	// grants full API access; once any user exists, this public endpoint is
	// closed to prevent unauthenticated account creation. Additional users must
	// be provisioned out-of-band (admin credentials / env).
	if exists, err := h.userStore.UserExists(); err != nil {
		h.logger.Error().Err(err).Msg("Failed to check user existence")
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	} else if exists {
		writeError(w, http.StatusForbidden, "Registration is closed")
		return
	}

	// Check if user already exists
	existing, err := h.userStore.GetUser(req.Username)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to check user existence")
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	if existing != nil {
		writeError(w, http.StatusConflict, "Username already exists")
		return
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to hash password")
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	// Create user
	user := &types.User{
		Username:     req.Username,
		PasswordHash: string(hash),
	}

	if err := h.userStore.CreateUser(user); err != nil {
		h.logger.Error().Err(err).Msg("Failed to create user")
		writeError(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	h.logger.Info().
		Str("username", user.Username).
		Msg("User registered successfully")

	writeJSON(w, http.StatusCreated, APIResponse{
		Success: true,
		Message: "User registered successfully",
	})
}

// generateToken creates a new JWT token for the given username.
func (h *AuthHandler) generateToken(username string) (string, time.Time, error) {
	expiresAt := time.Now().Add(h.tokenExpiry)

	claims := &Claims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "piworker",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(h.jwtSecret)
	if err != nil {
		return "", time.Time{}, err
	}

	return tokenString, expiresAt, nil
}

// ValidateToken validates a JWT token and returns the claims.
func (h *AuthHandler) ValidateToken(tokenString string) (*Claims, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return h.jwtSecret, nil
	})

	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, jwt.ErrSignatureInvalid
	}

	return claims, nil
}

// AuthMiddleware creates a middleware that validates JWT tokens.
// It skips authentication for public endpoints like /api/auth/*.
func AuthMiddleware(authHandler *AuthHandler) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for public endpoints
			if isPublicEndpoint(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Get token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeError(w, http.StatusUnauthorized, "Authorization header required")
				return
			}

			// Expect "Bearer <token>"
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				writeError(w, http.StatusUnauthorized, "Invalid authorization header format")
				return
			}

			tokenString := parts[1]

			// Validate token
			claims, err := authHandler.ValidateToken(tokenString)
			if err != nil {
				authHandler.logger.Debug().
					Err(err).
					Str("path", r.URL.Path).
					Msg("Token validation failed")
				writeError(w, http.StatusUnauthorized, "Invalid or expired token")
				return
			}

			// Add claims to context
			ctx := context.WithValue(r.Context(), ContextKeyUser, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// isPublicEndpoint checks if the endpoint is public (no auth required).
func isPublicEndpoint(path string) bool {
	if _, ok := publicPaths[path]; ok {
		return true
	}

	// Webhook endpoints are public: external services call them, and the
	// trigger-webhook node's optional token provides authentication instead.
	if strings.HasPrefix(path, webhook.PathPrefix) {
		return true
	}

	// Also allow static assets and frontend routes
	if !strings.HasPrefix(path, "/api/") {
		return true
	}

	return false
}

// GetUserFromContext extracts the user claims from the request context.
func GetUserFromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(ContextKeyUser).(*Claims)
	return claims, ok
}

// HashPassword creates a bcrypt hash of the password.
// This is a utility function for creating users programmatically.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}
