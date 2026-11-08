package api

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/agentauth"
	"github.com/stretchr/testify/require"
)

func TestRecoveryRevokesAgentTokens(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	created := env.createToken(t, map[string]any{"name": "x", "scopes": []string{"call"}})
	_, err := env.verifier.Verify(created.Token)
	require.NoError(t, err)
	epoch, err := env.store.TokensEpoch()
	require.NoError(t, err)

	// A wrong code changes nothing.
	body := map[string]string{"username": "admin", "password": "replacement-password", "code": "wrong"}
	require.Equal(t, http.StatusForbidden, authRequest(env.router, "POST", "/api/auth/recover", body, nil).Code)
	_, err = env.verifier.Verify(created.Token)
	require.NoError(t, err)

	code, err := env.users.NewAuthCode("recovery", "admin", time.Now())
	require.NoError(t, err)
	body["code"] = code
	w := authRequest(env.router, "POST", "/api/auth/recover", body, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	_, err = env.verifier.Verify(created.Token)
	require.ErrorIs(t, err, agentauth.ErrInvalidToken, "recovery assumes the account was compromised")
	stored, err := env.store.GetAPIToken(created.Info.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.RevokedAt)
	after, err := env.store.TokensEpoch()
	require.NoError(t, err)
	require.Greater(t, after, epoch, "other processes learn about it through the epoch")
}

func TestPasswordChangeRevokesAgentTokensUnlessKept(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	kept := env.createToken(t, map[string]any{"name": "kept", "scopes": []string{"call"}})

	w := env.do("POST", "/api/auth/password", map[string]any{"currentPassword": "test-password", "password": "second-password", "keepAgentTokens": true})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	_, err := env.verifier.Verify(kept.Token)
	require.NoError(t, err, "the admin explicitly kept agent tokens")

	w = authRequest(env.router, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "second-password"}, nil)
	require.Equal(t, http.StatusOK, w.Code)
	env.cookie = w.Result().Cookies()[0]
	w = env.do("POST", "/api/auth/password", map[string]any{"currentPassword": "second-password", "password": "third-password"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	_, err = env.verifier.Verify(kept.Token)
	require.ErrorIs(t, err, agentauth.ErrInvalidToken, "by default a password change revokes agent tokens")
}

func TestFailedPasswordChangeKeepsAgentTokens(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	created := env.createToken(t, map[string]any{"name": "x", "scopes": []string{"call"}})
	w := env.do("POST", "/api/auth/password", map[string]any{"currentPassword": "wrong-password", "password": "second-password"})
	require.Equal(t, http.StatusForbidden, w.Code)
	_, err := env.verifier.Verify(created.Token)
	require.NoError(t, err)
}

type failingRevoker struct{}

func (failingRevoker) RevokeAll() (int, error) { return 0, errors.New("database is locked") }

func TestRevocationFailureIsReported(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	env.auth.SetAgentTokenRevoker(failingRevoker{})
	w := env.do("POST", "/api/auth/password", map[string]any{"currentPassword": "test-password", "password": "second-password"})
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, errorMessage(t, w), "agent tokens could not be revoked")

	code, err := env.users.NewAuthCode("recovery", "admin", time.Now())
	require.NoError(t, err)
	w = authRequest(env.router, "POST", "/api/auth/recover", map[string]string{"username": "admin", "password": "replacement-password", "code": code}, nil)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, errorMessage(t, w), "agent tokens could not be revoked")
}

func TestAuthWithoutRevokerStillWorks(t *testing.T) {
	router, _, store := sessionTestRouter(t, true)
	code, err := store.NewAuthCode("recovery", "admin", time.Now())
	require.NoError(t, err)
	w := authRequest(router, "POST", "/api/auth/recover", map[string]string{"username": "admin", "password": "replacement-password", "code": code}, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}
