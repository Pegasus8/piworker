package flow_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newManager(t *testing.T) *flow.RuntimeManager {
	t.Helper()
	m := flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop())
	t.Cleanup(m.StopAll)
	return m
}

func globalSetVarFlow(toolCfg map[string]any) *flow.Flow {
	return chainFlow(toolCfg, riskStep{id: "set", typ: "set-var", config: map[string]any{"key": "door"}}, reply())
}

// A tool security issue is a validation error (HTTP 400): nothing starts and
// the tool name stays free.
func TestDeployRefusesToolSecurityIssues(t *testing.T) {
	m := newManager(t)
	cfg := strictToolConfig()
	bad := globalSetVarFlow(cfg)

	err := m.Deploy(context.Background(), bad)
	require.Error(t, err)
	var verr *flow.ValidationError
	require.True(t, errors.As(err, &verr), "%T %v", err, err)
	assert.Contains(t, err.Error(), "nodes[1].config.scope")
	_, running := m.GetRuntime(bad.ID)
	assert.False(t, running)

	good := chainFlow(cfg, reply()) // same tool name
	require.NoError(t, m.Deploy(context.Background(), good), "the name was never claimed")
}

func TestDeployAcceptsToolWarnings(t *testing.T) {
	m := newManager(t)
	f := chainFlow(strictToolConfig(), riskStep{id: "wait", typ: "process-delay", config: map[string]any{"delay": float64(20000)}}, reply())
	require.NoError(t, m.Deploy(context.Background(), f))
}

// Restore is lenient about config typing only: a flow failing the security
// checks never runs.
func TestRestoreRefusesToolSecurityIssues(t *testing.T) {
	m := newManager(t)
	f := globalSetVarFlow(strictToolConfig())
	_, err := m.Restore(context.Background(), f)
	require.Error(t, err)
	var verr *flow.ValidationError
	assert.True(t, errors.As(err, &verr))
	_, running := m.GetRuntime(f.ID)
	assert.False(t, running)
	assert.Empty(t, toolsOf(m))
}

func toolsOf(m *flow.RuntimeManager) []flow.ToolInfo {
	_, tools := m.Tools()
	return tools
}

func TestToolsSnapshot(t *testing.T) {
	m := newManager(t)
	v0, tools := m.Tools()
	assert.Empty(t, tools)

	cfgB := strictToolConfig()
	cfgB["toolName"] = "b_" + uniqueToolName()
	fb := chainFlow(cfgB, reply())
	require.NoError(t, m.Deploy(context.Background(), fb))
	v1, tools := m.Tools()
	assert.Greater(t, v1, v0, "deploy bumps the version")
	require.Len(t, tools, 1)
	assert.Equal(t, fb.ID, tools[0].FlowID)
	assert.Equal(t, "tool", tools[0].TriggerNodeID)
	assert.Equal(t, cfgB["toolName"], tools[0].ToolName)
	assert.Equal(t, node.RiskSafe, tools[0].Risk)
	assert.NotEmpty(t, tools[0].Fingerprint)

	cfgA := strictToolConfig()
	cfgA["toolName"] = "a_" + uniqueToolName()
	fa := chainFlow(cfgA, riskStep{id: "http", typ: "action-http", config: map[string]any{"url": "https://api.example.com/x"}})
	require.NoError(t, m.Deploy(context.Background(), fa))
	v2, tools := m.Tools()
	assert.Greater(t, v2, v1)
	require.Len(t, tools, 2)
	assert.Equal(t, cfgA["toolName"], tools[0].ToolName, "sorted by tool name")
	assert.Equal(t, node.RiskExternal, tools[0].Risk)

	// Same state, same version.
	again, _ := m.Tools()
	assert.Equal(t, v2, again)

	// A snapshot is a copy.
	tools[0].ToolName = "changed"
	_, fresh := m.Tools()
	assert.Equal(t, cfgA["toolName"], fresh[0].ToolName)

	require.NoError(t, m.Undeploy(fa.ID))
	v3, tools := m.Tools()
	assert.Greater(t, v3, v2, "undeploy bumps the version")
	require.Len(t, tools, 1)
	assert.Equal(t, fb.ID, tools[0].FlowID)

	m.StopAll()
	v4, tools := m.Tools()
	assert.Greater(t, v4, v3, "stop bumps the version")
	assert.Empty(t, tools)
}

// A failed deploy publishes nothing.
func TestToolsSnapshotIgnoresFailedDeploys(t *testing.T) {
	m := newManager(t)
	require.Error(t, m.Deploy(context.Background(), globalSetVarFlow(strictToolConfig())))
	assert.Empty(t, toolsOf(m))
}

func TestToolsSnapshotIsSafeForConcurrentUse(t *testing.T) {
	m := newManager(t)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					_, tools := m.Tools()
					for _, ti := range tools {
						_ = ti.Schema.JSON()
					}
				}
			}
		})
	}
	for range 20 {
		f := chainFlow(strictToolConfig(), reply())
		require.NoError(t, m.Deploy(context.Background(), f))
		require.NoError(t, m.Undeploy(f.ID))
	}
	close(stop)
	wg.Wait()
}
