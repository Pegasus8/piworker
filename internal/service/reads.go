package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/storage"
)

// Read views (docs/design/mcp.md §6, "Read views"): what an agent with the
// read scope may observe of flows, runs and the system. Every view is built
// for agents: node config is redacted by default (only AgentVisible,
// non-Sensitive properties are shown), known secret values are redacted from
// all text, and every result is bounded in size and deterministic in order.

// Bounds of the read views.
const (
	// maxListedFlows caps ListFlows (FlowList.Truncated reports the rest).
	maxListedFlows = 200
	// maxFlowNameBytes bounds a flow name shown to agents.
	maxFlowNameBytes = 256
	// maxSummaryTools caps the tool names of one flow in ListFlows
	// (FlowSummary.ToolCount counts them all).
	maxSummaryTools = 20
)

// Flow states shown by the read views.
const (
	// FlowStateRunning: the flow is deployed and running.
	FlowStateRunning = "running"
	// FlowStateStopped: the flow is not deployed, and is not meant to be.
	FlowStateStopped = "stopped"
	// FlowStateFailed: the flow is meant to run but is not running (its
	// restore at startup failed).
	FlowStateFailed = "failed"
)

// FlowSource reads stored flow definitions (*storage.SQLiteStore).
type FlowSource interface {
	GetFlow(id string) (*flow.Flow, error)
	// ListFlowsPaginated returns a page of flows (newest first) and the
	// number of flows.
	ListFlowsPaginated(offset, limit int) ([]*flow.Flow, int, error)
}

// RunHistory reads the recorded runs (*storage.SQLiteStore).
type RunHistory interface {
	ListFlowRunsBefore(flowID, beforeRunID string, limit int) ([]storage.FlowRun, error)
	GetFlowRun(flowID, runID string) (*storage.FlowRun, error)
	ListFlowRunEvents(flowID, runID string) ([]storage.NodeEventRecord, error)
}

// ReadRuntimes is what the read views need from the runtime manager
// (*flow.RuntimeManager).
type ReadRuntimes interface {
	Tools() (uint64, []flow.ToolInfo)
	ListRunning() []string
}

// NodeTypes looks up registered node types (*node.Registry).
type NodeTypes interface {
	Get(nodeType string) (node.NodeTypeInfo, bool)
}

// AgentSettings reads the agent deploy level (*storage.SQLiteStore).
type AgentSettings interface {
	AgentDeployLevel() (storage.AgentDeployLevel, error)
}

// ReadsOptions configures Reads.
type ReadsOptions struct {
	// Flows provides the stored flow definitions (required).
	Flows FlowSource
	// History provides the run history (nil: no runs are known).
	History RunHistory
	// Runtimes provides the running flows and their tools (required).
	Runtimes ReadRuntimes
	// Registry describes node types: config schemas (for redaction) and
	// risk (default node.DefaultRegistry).
	Registry NodeTypes
	// Secrets are redacted from every view (default secrets.DefaultStore).
	Secrets SecretSource
	// Settings provides the agent deploy level (nil: reported as off).
	Settings AgentSettings
	// Version is the PiWorker version (empty: "unknown").
	Version string
	// StartedAt is when the server started (zero: when NewReads ran).
	StartedAt time.Time
	// DataDir is the data directory (the database's), whose disk usage
	// SystemStatus reports; empty: disk usage is not reported.
	DataDir string
	// MCP holds the limits SystemStatus reports (zero fields: the
	// defaults of config.DefaultMCP).
	MCP config.MCPConfig
	// Host probes host health for SystemStatus (default: what Linux shows
	// cheaply; nothing elsewhere).
	Host func(dataDir string) HostStatus
	Now  func() time.Time
}

// Reads builds the read views for agents (MCP read tools) and the editor.
// It is safe for concurrent use.
type Reads struct {
	flows    FlowSource
	history  RunHistory
	runtimes ReadRuntimes
	registry NodeTypes
	secrets  SecretSource
	settings AgentSettings
	now      func() time.Time

	version   string
	startedAt time.Time
	dataDir   string
	mcp       config.MCPConfig
	host      func(dataDir string) HostStatus
}

// NewReads returns the read views.
func NewReads(opts ReadsOptions) *Reads {
	r := &Reads{
		flows:    opts.Flows,
		history:  opts.History,
		runtimes: opts.Runtimes,
		registry: opts.Registry,
		secrets:  opts.Secrets,
		settings: opts.Settings,
		now:      opts.Now,

		version:   opts.Version,
		startedAt: opts.StartedAt,
		dataDir:   opts.DataDir,
		mcp:       opts.MCP,
		host:      opts.Host,
	}
	if r.registry == nil {
		r.registry = node.DefaultRegistry
	}
	if r.secrets == nil {
		r.secrets = secrets.DefaultStore
	}
	if r.now == nil {
		r.now = time.Now
	}
	if r.startedAt.IsZero() {
		r.startedAt = r.now()
	}
	defaults := config.DefaultMCP()
	if r.mcp.MaxInflight <= 0 {
		r.mcp.MaxInflight = defaults.MaxInflight
	}
	if r.mcp.MaxResultBytes <= 0 {
		r.mcp.MaxResultBytes = defaults.MaxResultBytes
	}
	if r.host == nil {
		r.host = probeHost
	}
	return r
}

// authorize lets the editor and agents whose token has the read scope read.
func (r *Reads) authorize(ctx context.Context, actor Actor) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch {
	case actor.Kind == ActorEditor:
		return nil
	case actor.Kind == ActorAgent && actor.HasScope(storage.ScopeRead):
		return nil
	case actor.Kind == ActorAgent:
		return toolError(ErrPermissionDenied, "this token does not have the read scope")
	}
	return toolError(ErrPermissionDenied, "unknown caller")
}

// FlowList is the result of ListFlows.
type FlowList struct {
	// Flows are the newest flows first, at most maxListedFlows.
	Flows []FlowSummary `json:"flows"`
	// Total is the number of stored flows.
	Total int `json:"total"`
	// Truncated reports that Flows holds only the newest flows.
	Truncated bool `json:"truncated"`
}

// FlowSummary describes one flow in ListFlows.
type FlowSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"` // running, stopped or failed
	NodeCount int    `json:"nodeCount"`
	// Tools are the names of the tools the flow publishes while running,
	// sorted, at most maxSummaryTools; ToolCount counts them all.
	Tools     []string    `json:"tools"`
	ToolCount int         `json:"toolCount"`
	LastRun   *RunPointer `json:"lastRun,omitempty"`
}

// RunPointer is the latest run of a flow.
type RunPointer struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"` // running, success or error
	StartedAt time.Time `json:"startedAt"`
}

// ListFlows lists the stored flows (newest first, capped), with their state,
// size, published tools and latest run.
func (r *Reads) ListFlows(ctx context.Context, actor Actor) (FlowList, error) {
	if err := r.authorize(ctx, actor); err != nil {
		return FlowList{}, err
	}
	flows, total, err := r.flows.ListFlowsPaginated(0, maxListedFlows)
	if err != nil {
		return FlowList{}, fmt.Errorf("list flows: %w", err)
	}
	// Newest first, as stored; ties in the creation time in ID order.
	slices.SortStableFunc(flows, func(a, b *flow.Flow) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	red := r.redactor()
	running := r.runningSet()
	tools := r.toolsByFlow()
	out := FlowList{Flows: make([]FlowSummary, 0, len(flows)), Total: total, Truncated: total > len(flows)}
	for _, f := range flows {
		s := FlowSummary{
			ID:        capText(red.text(f.ID), maxViewIDBytes),
			Name:      r.flowName(red, f.Name),
			State:     flowState(f, running[f.ID]),
			NodeCount: len(f.Nodes),
			Tools:     []string{},
			ToolCount: len(tools[f.ID]),
		}
		for _, ti := range tools[f.ID][:min(len(tools[f.ID]), maxSummaryTools)] {
			s.Tools = append(s.Tools, capText(red.text(ti.ToolName), maxViewIDBytes))
		}
		s.LastRun = r.lastRun(f.ID)
		out.Flows = append(out.Flows, s)
	}
	return out, nil
}

// lastRun returns the latest recorded run of a flow; nil without history or
// on a read error (the field is best effort).
func (r *Reads) lastRun(flowID string) *RunPointer {
	if r.history == nil {
		return nil
	}
	runs, err := r.history.ListFlowRunsBefore(flowID, "", 1)
	if err != nil || len(runs) == 0 {
		return nil
	}
	return &RunPointer{ID: runs[0].ID, Status: runs[0].Status, StartedAt: runs[0].StartedAt.UTC()}
}

// runningSet returns the IDs of the running flows.
func (r *Reads) runningSet() map[string]bool {
	set := map[string]bool{}
	for _, id := range r.runtimes.ListRunning() {
		set[id] = true
	}
	return set
}

// toolsByFlow groups the published tools by flow, each group sorted by tool
// name (then trigger node).
func (r *Reads) toolsByFlow() map[string][]flow.ToolInfo {
	_, tools := r.runtimes.Tools()
	by := map[string][]flow.ToolInfo{}
	for _, ti := range tools {
		by[ti.FlowID] = append(by[ti.FlowID], ti)
	}
	for _, group := range by {
		slices.SortStableFunc(group, func(a, b flow.ToolInfo) int {
			if c := strings.Compare(a.ToolName, b.ToolName); c != 0 {
				return c
			}
			return strings.Compare(a.TriggerNodeID, b.TriggerNodeID)
		})
	}
	return by
}

// flowState maps a stored flow and whether it runs to the shown state.
func flowState(f *flow.Flow, running bool) string {
	switch {
	case running:
		return FlowStateRunning
	case f.State == flow.FlowStateRunning, f.State == flow.FlowStateFailed:
		return FlowStateFailed
	}
	return FlowStateStopped
}

// flowName is a flow name as agents see it: secrets and URL credentials
// redacted, bounded.
func (r *Reads) flowName(red secretRedactor, name string) string {
	return agentText(red, name, maxFlowNameBytes)
}

// agentText redacts known secrets and hides URLs past their host
// (hideURLsInText) in admin-written or recorded text, and bounds it to limit
// bytes.
func agentText(red secretRedactor, s string, limit int) string {
	return capText(hideURLsInText(red.text(s)), limit)
}
