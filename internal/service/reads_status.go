package service

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
)

// maxVersionBytes bounds the version SystemStatus shows.
const maxVersionBytes = 64

// SystemStatusView is the state of the PiWorker server (SystemStatus). It
// shows no network details (host name, addresses, listeners) and no paths.
type SystemStatusView struct {
	Version  string `json:"version"`
	Platform string `json:"platform"` // GOOS/GOARCH, e.g. linux/arm64
	// StartedAt is when the server started; UptimeSeconds the time since.
	StartedAt     time.Time  `json:"startedAt"`
	UptimeSeconds int64      `json:"uptimeSeconds"`
	Flows         FlowCounts `json:"flows"`
	// Tools counts the tools published by the running flows.
	Tools int       `json:"tools"`
	MCP   MCPLimits `json:"mcp"`
	// AgentDeploy is the agent deploy level: off, low or all.
	AgentDeploy string `json:"agentDeploy"`
	HostStatus
}

// FlowCounts counts the running and the stored flows.
type FlowCounts struct {
	Running int `json:"running"`
	Total   int `json:"total"`
}

// MCPLimits are the limits of agent tool calls (the [mcp] configuration).
type MCPLimits struct {
	// MaxInflight bounds the tool calls running at once, all tokens
	// together.
	MaxInflight int `json:"maxInflight"`
	// MaxResultBytes caps a tool result (larger ones are truncated).
	MaxResultBytes int `json:"maxResultBytes"`
}

// HostStatus is best-effort host health; a figure that is unknown (or not
// cheaply available on this platform) is nil and omitted.
type HostStatus struct {
	// Disk is the usage of the file system holding the data directory.
	Disk            *DiskStatus   `json:"disk,omitempty"`
	Memory          *MemoryStatus `json:"memory,omitempty"`
	CPUTemperatureC *float64      `json:"cpuTemperatureC,omitempty"`
}

// DiskStatus is a file system's usage.
type DiskStatus struct {
	// FreeBytes is the space available to PiWorker.
	FreeBytes  uint64 `json:"freeBytes"`
	TotalBytes uint64 `json:"totalBytes"`
}

// MemoryStatus is the host's memory.
type MemoryStatus struct {
	TotalBytes uint64 `json:"totalBytes"`
	// AvailableBytes is what can be allocated without swapping.
	AvailableBytes uint64 `json:"availableBytes"`
}

// SystemStatus shows the server's version, uptime, flows and tools, the MCP
// limits, the agent deploy level and, best effort, the data directory's free
// disk, memory and CPU temperature.
func (r *Reads) SystemStatus(ctx context.Context, actor Actor) (SystemStatusView, error) {
	if err := r.authorize(ctx, actor); err != nil {
		return SystemStatusView{}, err
	}
	_, total, err := r.flows.ListFlowsPaginated(0, 1)
	if err != nil {
		return SystemStatusView{}, fmt.Errorf("count flows: %w", err)
	}
	_, tools := r.runtimes.Tools()
	now := r.now()
	st := SystemStatusView{
		Version:       capText(r.version, maxVersionBytes),
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
		StartedAt:     r.startedAt.UTC(),
		UptimeSeconds: max(int64(now.Sub(r.startedAt)/time.Second), 0),
		Flows:         FlowCounts{Running: len(r.runtimes.ListRunning()), Total: total},
		Tools:         len(tools),
		MCP:           MCPLimits{MaxInflight: r.mcp.MaxInflight, MaxResultBytes: r.mcp.MaxResultBytes},
		AgentDeploy:   string(r.agentDeployLevel()),
		HostStatus:    r.host(r.dataDir),
	}
	if st.Version == "" {
		st.Version = "unknown"
	}
	if t := st.CPUTemperatureC; t != nil && (math.IsNaN(*t) || math.IsInf(*t, 0)) {
		st.CPUTemperatureC = nil // not JSON-serializable
	}
	return st, nil
}

// agentDeployLevel is the stored level; off without settings or on a read
// error or corrupt value, as the deploy policy fails closed.
func (r *Reads) agentDeployLevel() storage.AgentDeployLevel {
	if r.settings == nil {
		return storage.AgentDeployOff
	}
	level, err := r.settings.AgentDeployLevel()
	if err != nil {
		return storage.AgentDeployOff
	}
	return level
}
