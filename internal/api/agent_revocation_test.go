package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/agentauth"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecoveryRevokesAgentTokens(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	created := env.createToken(t, map[string]any{"name": "x", "scopes": []string{"call"}})
	_, err := env.verifier.Verify(created.Token)
	require.NoError(t, err)
	epoch, err := env.store.TokensEpoch()
	require.NoError(t, err)

	// A wrong code changes nothing, whether rejected before or by the store.
	code, err := env.users.NewAuthCode("recovery", "admin", time.Now())
	require.NoError(t, err)
	body := map[string]string{"username": "admin", "password": "replacement-password", "code": "wrong"}
	require.Equal(t, http.StatusForbidden, authRequest(env.router, "POST", "/api/auth/recover", body, nil).Code)
	body["code"] = strings.Repeat("A", len(code))
	require.Equal(t, http.StatusForbidden, authRequest(env.router, "POST", "/api/auth/recover", body, nil).Code)
	_, err = env.verifier.Verify(created.Token)
	require.NoError(t, err)
	unchanged, err := env.store.TokensEpoch()
	require.NoError(t, err)
	require.Equal(t, epoch, unchanged, "a bad code never revokes tokens")

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

// failingRevoker revokes inside the credential transaction, then fails, like
// a revocation hitting a database error half-way.
type failingRevoker struct{ evicted *int }

func (failingRevoker) RevokeAllInTx(tx *sql.Tx) error {
	if _, err := storage.RevokeAllAPITokensTx(tx, time.Now()); err != nil {
		return err
	}
	return errors.New("database is locked")
}
func (f failingRevoker) EvictAll() { *f.evicted++ }

// The credential change and the token revocation are one transaction: when
// the revocation fails, the password is not changed either (no window where
// a reset password leaves possibly stolen tokens working), and the admin can
// simply retry.
func TestRevocationFailureLeavesCredentialsUnchanged(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	created := env.createToken(t, map[string]any{"name": "x", "scopes": []string{"call"}})
	evicted := 0
	env.auth.SetAgentTokenRevoker(failingRevoker{&evicted})

	w := env.do("POST", "/api/auth/password", map[string]any{"currentPassword": "test-password", "password": "second-password"})
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, errorMessage(t, w), "agent tokens could not be revoked")
	require.Equal(t, http.StatusOK, env.do("GET", "/api/auth/session", nil).Code, "the session survives: nothing changed")

	code, err := env.users.NewAuthCode("recovery", "admin", time.Now())
	require.NoError(t, err)
	body := map[string]string{"username": "admin", "password": "replacement-password", "code": code}
	w = authRequest(env.router, "POST", "/api/auth/recover", body, nil)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, errorMessage(t, w), "agent tokens could not be revoked")
	assert.Equal(t, 0, evicted)

	login := authRequest(env.router, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "test-password"}, nil)
	require.Equal(t, http.StatusOK, login.Code, "the old password still works")
	stored, err := env.store.GetAPIToken(created.Info.ID)
	require.NoError(t, err)
	require.Nil(t, stored.RevokedAt, "the partial revocation was rolled back")

	// Retrying with a working revoker and the same code succeeds.
	env.auth.SetAgentTokenRevoker(env.verifier)
	w = authRequest(env.router, "POST", "/api/auth/recover", body, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	_, err = env.verifier.Verify(created.Token)
	require.ErrorIs(t, err, agentauth.ErrInvalidToken)
}

func TestAuthWithoutRevokerStillWorks(t *testing.T) {
	router, _, store := sessionTestRouter(t, true)
	code, err := store.NewAuthCode("recovery", "admin", time.Now())
	require.NoError(t, err)
	w := authRequest(router, "POST", "/api/auth/recover", map[string]string{"username": "admin", "password": "replacement-password", "code": code}, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}
