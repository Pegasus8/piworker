package service

import (
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func toolNames(tools []flow.ToolInfo) []string {
	names := make([]string, 0, len(tools))
	for _, ti := range tools {
		names = append(names, ti.ToolName)
	}
	return names
}

// List shows an agent exactly the tools Call would let it call: the same
// rules decide both, so a listed tool is callable and a hidden one is not.
func TestListShowsExactlyTheCallableTools(t *testing.T) {
	e := newEnv(t)
	safe := e.deploy(fan(toolConfig(nil), echo()))
	priv := e.deploy(fan(strictConfig(), command()))
	require.Equal(t, node.RiskPrivileged, priv.Risk)
	stale := safe
	stale.Fingerprint = "sha256:" + strings.Repeat("0", 64)
	suspended := grant(safe)
	suspended[0].Suspended = true

	cases := []struct {
		name string
		tok  storage.NewAPIToken
		want []flow.ToolInfo
	}{
		{"granted", storage.NewAPIToken{Grants: grant(safe)}, []flow.ToolInfo{safe}},
		{"nothing granted", storage.NewAPIToken{}, nil},
		{"all tools never lists privileged tools", storage.NewAPIToken{AllTools: true, Privileged: true}, []flow.ToolInfo{safe}},
		{"privileged grant without the flag", storage.NewAPIToken{Grants: grant(priv)}, nil},
		{"privileged grant with the flag", storage.NewAPIToken{Privileged: true, Grants: grant(priv)}, []flow.ToolInfo{priv}},
		{"no call scope", storage.NewAPIToken{Scopes: []string{storage.ScopeRead}, Grants: grant(safe), AllTools: true}, nil},
		{"changed tool", storage.NewAPIToken{Grants: grant(stale)}, nil},
		{"suspended grant", storage.NewAPIToken{Grants: suspended}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent := e.agent(tc.tok)
			version, listed := e.tools.List(agent)
			assert.Positive(t, version)
			assert.ElementsMatch(t, toolNames(tc.want), toolNames(listed))

			// Same rules as Call: every listed tool passes the permission
			// check, every other one is refused.
			for _, ti := range []flow.ToolInfo{safe, priv} {
				err := e.tools.checkAccess(agent, ti)
				assert.Equal(t, err == nil, contains(listed, ti), "%s: %v", ti.ToolName, err)
			}
		})
	}
}

func contains(tools []flow.ToolInfo, ti flow.ToolInfo) bool {
	for _, t := range tools {
		if t.FlowID == ti.FlowID && t.TriggerNodeID == ti.TriggerNodeID {
			return true
		}
	}
	return false
}

// Listing has no side effects: a grant whose tool changed is hidden but not
// suspended (only a call suspends it).
func TestListDoesNotSuspendChangedGrants(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(nil), echo()))
	stale := info
	stale.Fingerprint = "sha256:" + strings.Repeat("0", 64)
	agent := e.agent(storage.NewAPIToken{Grants: grant(stale)})
	evicted := &recordingEvictor{}
	e.tools.tokens = evicted

	_, listed := e.tools.List(agent)
	assert.Empty(t, listed)
	tok, err := e.store.GetAPIToken(agent.TokenID)
	require.NoError(t, err)
	assert.False(t, tok.Grants[0].Suspended)
	assert.Empty(t, evicted.ids())
}

func TestEditorListsEveryTool(t *testing.T) {
	e := newEnv(t)
	safe := e.deploy(fan(toolConfig(nil), echo()))
	priv := e.deploy(fan(strictConfig(), command()))
	_, listed := e.tools.List(EditorActor())
	assert.ElementsMatch(t, []string{safe.ToolName, priv.ToolName}, toolNames(listed))
	_, listed = e.tools.List(Actor{Kind: "unknown"})
	assert.Empty(t, listed)
}

// The version follows the runtime manager's tools snapshot, so callers can
// cache what they built from a listing.
func TestListVersionFollowsDeploys(t *testing.T) {
	e := newEnv(t)
	agent := e.agent(storage.NewAPIToken{AllTools: true})
	v0, listed := e.tools.List(agent)
	assert.Empty(t, listed)
	info := e.deploy(fan(toolConfig(nil), echo()))
	v1, listed := e.tools.List(agent)
	assert.Greater(t, v1, v0)
	assert.Equal(t, []string{info.ToolName}, toolNames(listed))
	require.NoError(t, e.manager.Undeploy(info.FlowID))
	v2, listed := e.tools.List(agent)
	assert.Greater(t, v2, v1)
	assert.Empty(t, listed)
}
