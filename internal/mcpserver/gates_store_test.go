package mcpserver

import (
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/agentauth"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// With the real store and verifier, changing agent_deploy bumps
// tokens_epoch in the same transaction, so the cached per-token server is
// rebuilt with (or without) the deploy tools on the verifier's next epoch
// read, without any other change.
func TestDeployLevelChangeRebuildsCachedServers(t *testing.T) {
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "mcp.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	_, raw, err := store.CreateAPIToken(storage.NewAPIToken{Name: "builder", Scopes: []string{storage.ScopeRead, storage.ScopeDeploy}}, time.Now())
	require.NoError(t, err)

	verifier := agentauth.NewTokenVerifier(store, agentauth.WithEpochInterval(time.Nanosecond))
	var handled atomic.Int32
	h, err := New(Options{
		Tools: &fakeTools{version: 1}, Tokens: verifier, DeployLevel: store, TLS: true, Logger: zerolog.Nop(),
		StaticTools:  []StaticTool{scopedTool("pw_deploy_thing", storage.ScopeDeploy, &handled)},
		RequestBurst: 100, RequestsPerMinute: 100,
	})
	require.NoError(t, err)
	e := &testEnv{t: t, h: h}

	assert.NotContains(t, names(listed(t, e, raw)), "pw_deploy_thing", "off by default")
	before := verifier.Epoch()

	require.NoError(t, store.SetAgentDeployLevel(storage.AgentDeployLow))
	assert.Contains(t, names(listed(t, e, raw)), "pw_deploy_thing")
	assert.Greater(t, verifier.Epoch(), before, "the level change bumped tokens_epoch")

	require.NoError(t, store.SetAgentDeployLevel(storage.AgentDeployAll))
	assert.Contains(t, names(listed(t, e, raw)), "pw_deploy_thing")
	require.NoError(t, store.SetAgentDeployLevel(storage.AgentDeployOff))
	assert.NotContains(t, names(listed(t, e, raw)), "pw_deploy_thing", "turned off again")
}

// With the real store and verifier, behind a local reverse proxy (TLS off,
// every request from 127.0.0.1): a client sending one made-up token every 6
// seconds keeps the address limited, and every revocation empties the
// verifier's cache, yet the agent is served on every request.
func TestProxiedAgentsAreNeverLockedOut(t *testing.T) {
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "mcp.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	clock := newClock()
	_, raw, err := store.CreateAPIToken(storage.NewAPIToken{Name: "agent", Scopes: []string{storage.ScopeCall}}, clock.Now())
	require.NoError(t, err)
	verifier := agentauth.NewTokenVerifier(store, agentauth.WithClock(clock.Now))
	h, err := New(Options{Tools: &fakeTools{version: 1}, Tokens: verifier, Logger: zerolog.Nop(), Now: clock.Now, RequestBurst: 1000})
	require.NoError(t, err)
	e := &testEnv{t: t, h: h}
	proxied := func(token string) *http.Request {
		r := withToken(rpc(listToolsBody), token)
		r.RemoteAddr = "127.0.0.1:40000"
		return r
	}

	for i := range 10 {
		requireJSONError(t, e.serve(proxied(madeUpToken(i))), http.StatusUnauthorized)
	}
	for i := range 20 {
		_, other, err := store.CreateAPIToken(storage.NewAPIToken{Name: "other", Scopes: []string{storage.ScopeCall}}, clock.Now())
		require.NoError(t, err)
		id, _, _ := storage.ParseAPIToken(other)
		require.NoError(t, store.RevokeAPIToken(id, clock.Now())) // bumps the epoch
		clock.Advance(6 * time.Second)
		requireJSONError(t, e.serve(proxied(madeUpToken(10+i))), http.StatusUnauthorized)
		requireJSONError(t, e.serve(proxied(madeUpToken(10+i))), http.StatusTooManyRequests)
		_, rpcErr := rpcResult(t, e.serve(proxied(raw)))
		require.Nil(t, rpcErr, "round %d", i)
	}
}
