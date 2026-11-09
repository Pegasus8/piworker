package service

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemStatusReportsFlowsToolsAndLimits(t *testing.T) {
	var probedDir string
	temp := 48.5
	e := newReadsEnv(t, func(o *ReadsOptions) {
		o.Version = "v1.4.2"
		o.StartedAt = time.Date(2026, 11, 8, 11, 58, 30, 0, time.UTC) // 90 s before the clock
		o.DataDir = "/var/lib/piworker"
		o.MCP = config.MCPConfig{MaxInflight: 6, MaxResultBytes: 4096}
		o.Host = func(dataDir string) HostStatus {
			probedDir = dataDir
			return HostStatus{
				Disk:            &DiskStatus{FreeBytes: 1 << 30, TotalBytes: 8 << 30},
				Memory:          &MemoryStatus{TotalBytes: 4 << 30, AvailableBytes: 3 << 30},
				CPUTemperatureC: &temp,
			}
		}
	})
	require.NoError(t, e.store.SetAgentDeployLevel(storage.AgentDeployLow))
	e.save(manualFlow("one"))
	e.save(manualFlow("two"))
	tools := e.save(readsToolFlow("tools", "reads_status_tool"))
	e.run(tools)

	st, err := e.reads.SystemStatus(context.Background(), readsAgent(storage.ScopeRead))
	require.NoError(t, err)
	assert.Equal(t, "v1.4.2", st.Version)
	assert.Equal(t, runtime.GOOS+"/"+runtime.GOARCH, st.Platform)
	assert.True(t, st.StartedAt.Equal(time.Date(2026, 11, 8, 11, 58, 30, 0, time.UTC)))
	assert.Equal(t, int64(90), st.UptimeSeconds)
	assert.Equal(t, FlowCounts{Running: 1, Total: 3}, st.Flows)
	assert.Equal(t, 1, st.Tools)
	assert.Equal(t, MCPLimits{MaxInflight: 6, MaxResultBytes: 4096}, st.MCP)
	assert.Equal(t, "low", st.AgentDeploy)
	assert.Equal(t, "/var/lib/piworker", probedDir, "disk usage is that of the data directory")
	require.NotNil(t, st.Disk)
	assert.Equal(t, uint64(1<<30), st.Disk.FreeBytes)
	require.NotNil(t, st.Memory)
	assert.Equal(t, uint64(3<<30), st.Memory.AvailableBytes)
	require.NotNil(t, st.CPUTemperatureC)
	assert.Equal(t, 48.5, *st.CPUTemperatureC)

	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(mustJSON(t, st)), &raw))
	for _, key := range []string{"version", "platform", "startedAt", "uptimeSeconds", "flows", "tools", "mcp", "agentDeploy", "disk", "memory", "cpuTemperatureC"} {
		assert.Containsf(t, raw, key, "system status field %s", key)
	}
	assert.Equal(t, map[string]any{"running": float64(1), "total": float64(3)}, raw["flows"])
	assert.Equal(t, map[string]any{"maxInflight": float64(6), "maxResultBytes": float64(4096)}, raw["mcp"])
	assert.Equal(t, map[string]any{"freeBytes": float64(1 << 30), "totalBytes": float64(8 << 30)}, raw["disk"])
	assert.Equal(t, map[string]any{"totalBytes": float64(4 << 30), "availableBytes": float64(3 << 30)}, raw["memory"])
	assert.NotContains(t, mustJSON(t, st), "/var/lib/piworker", "the data directory's path is not shown")
}

// Unknown host figures are omitted, never guessed; missing options fall back
// to the defaults the server runs with.
func TestSystemStatusDefaults(t *testing.T) {
	e := newReadsEnv(t, func(o *ReadsOptions) {
		o.Settings = nil
		o.Host = func(string) HostStatus { return HostStatus{} }
	})
	e.clock.advance(5 * time.Second)
	st, err := e.reads.SystemStatus(context.Background(), EditorActor())
	require.NoError(t, err)
	defaults := config.DefaultMCP()
	assert.Equal(t, MCPLimits{MaxInflight: defaults.MaxInflight, MaxResultBytes: defaults.MaxResultBytes}, st.MCP)
	assert.Equal(t, "off", st.AgentDeploy)
	assert.Equal(t, "unknown", st.Version)
	assert.Equal(t, int64(5), st.UptimeSeconds, "uptime counts from when the read views were built")
	assert.Equal(t, FlowCounts{}, st.Flows)
	out := mustJSON(t, st)
	for _, key := range []string{`"disk"`, `"memory"`, `"cpuTemperatureC"`} {
		assert.NotContains(t, out, key)
	}
}

// A corrupt agent deploy setting is reported as off: the deploy policy
// fails closed on it.
func TestSystemStatusCorruptDeployLevel(t *testing.T) {
	e := newReadsEnv(t, func(o *ReadsOptions) { o.Host = func(string) HostStatus { return HostStatus{} } })
	require.NoError(t, e.store.SetSetting("agent_deploy", "everything"))
	st, err := e.reads.SystemStatus(context.Background(), EditorActor())
	require.NoError(t, err)
	assert.Equal(t, "off", st.AgentDeploy)
}

// The real host probe shows no network details (host name, addresses) and
// stays within a small, bounded result.
func TestSystemStatusShowsNoNetworkDetails(t *testing.T) {
	e := newReadsEnv(t, func(o *ReadsOptions) {
		o.Version = strings.Repeat("v", 1000)
		o.DataDir = t.TempDir()
	})
	st, err := e.reads.SystemStatus(context.Background(), readsAgent(storage.ScopeRead))
	require.NoError(t, err)
	out := mustJSON(t, st)
	if host, err := os.Hostname(); err == nil && len(host) >= 4 {
		assert.NotContains(t, out, host)
	}
	assert.LessOrEqual(t, len(st.Version), maxVersionBytes)
	assert.Less(t, len(out), 1024)
}
