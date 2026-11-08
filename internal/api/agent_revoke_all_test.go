package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/agentauth"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type revokedTokens struct {
	Revoked int `json:"revoked"`
}

// "Revoke all" narrows access only, so it needs the session and the CSRF
// header but no password (design §7). It revokes every token in one
// transaction, voids their pending approvals, bumps the tokens epoch and
// stops cached verifications at once.
func TestRevokeAllTokens(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	a := env.createToken(t, map[string]any{"name": "laptop", "scopes": []string{"call"}})
	b := env.createToken(t, map[string]any{"name": "desktop", "scopes": []string{"read"}})
	already := env.createToken(t, map[string]any{"name": "old", "scopes": []string{"read"}})
	require.Equal(t, http.StatusOK, env.do("DELETE", "/api/agent/tokens/"+already.Info.ID, nil).Code)
	// Expired but never revoked: revoked too, but it was not active.
	stale, _, err := env.store.CreateAPIToken(storage.NewAPIToken{Name: "stale", Scopes: []string{storage.ScopeCall}, ExpiresAt: time.Now().Add(-time.Hour)}, time.Now().Add(-2*time.Hour))
	require.NoError(t, err)
	for _, tok := range []createdToken{a, b} {
		_, err := env.verifier.Verify(tok.Token)
		require.NoError(t, err)
	}
	queued := addApprovalFor(t, env.store, a.Info.ID, time.Now(), 15*time.Minute)
	epoch, err := env.store.TokensEpoch()
	require.NoError(t, err)

	w := env.do("POST", "/api/agent/tokens/revoke-all", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, 2, data[revokedTokens](t, w).Revoked, "only the tokens active now: not the one revoked before, nor the expired one")
	storedStale, err := env.store.GetAPIToken(stale.ID)
	require.NoError(t, err)
	assert.NotNil(t, storedStale.RevokedAt, "an expired token is revoked as well")

	for _, tok := range []createdToken{a, b} {
		_, err := env.verifier.Verify(tok.Token)
		require.ErrorIs(t, err, agentauth.ErrInvalidToken, tok.Info.Name)
		stored, err := env.store.GetAPIToken(tok.Info.ID)
		require.NoError(t, err)
		assert.NotNil(t, stored.RevokedAt, tok.Info.Name)
	}
	approval, err := env.store.GetAgentApproval(queued.ID)
	require.NoError(t, err)
	assert.Equal(t, storage.ApprovalRejected, approval.Status, "nothing a revoked token queued can still run")
	assert.Equal(t, storage.ApprovalDecidedByTokenRevoked, approval.DecidedBy)
	after, err := env.store.TokensEpoch()
	require.NoError(t, err)
	assert.Greater(t, after, epoch, "other processes learn about it through the epoch")

	// Nothing left to revoke.
	w = env.do("POST", "/api/agent/tokens/revoke-all", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 0, data[revokedTokens](t, w).Revoked)
}

func TestRevokeAllTokensNeedsSessionAndCSRF(t *testing.T) {
	env := newAgentTestEnv(t, AgentHandlerOptions{})
	created := env.createToken(t, map[string]any{"name": "laptop", "scopes": []string{"call"}})

	require.Equal(t, http.StatusUnauthorized, authRequest(env.router, "POST", "/api/agent/tokens/revoke-all", nil, nil).Code)
	r := httptest.NewRequest("POST", "http://piworker.local/api/agent/tokens/revoke-all", nil)
	r.AddCookie(env.cookie)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, r)
	require.Equal(t, http.StatusForbidden, w.Code, "a cross-site form cannot revoke every token")

	_, err := env.verifier.Verify(created.Token)
	require.NoError(t, err)
}
