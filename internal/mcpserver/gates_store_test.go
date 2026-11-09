package mcpserver

import (
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
