package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
)

// Bounds of GetFlow.
const (
	// maxViewNodes and maxViewConnections cap the nodes and connections
	// shown (FlowView.Truncated reports the rest).
	maxViewNodes       = 200
	maxViewConnections = 400
	// maxViewTools caps the tools shown.
	maxViewTools = 100
	// maxViewIDBytes bounds IDs, types and port names.
	maxViewIDBytes = 256
)

// FlowView is a flow as GetFlow shows it.
type FlowView struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	State       string           `json:"state"` // running, stopped or failed
	Nodes       []NodeView       `json:"nodes"`
	Connections []ConnectionView `json:"connections"`
	// Tools are the tools the flow publishes while running, sorted by name.
	Tools []ToolView `json:"tools"`
	// TotalNodes and TotalConnections count the flow's nodes and
	// connections, shown or not.
	TotalNodes       int `json:"totalNodes"`
	TotalConnections int `json:"totalConnections"`
	// Truncated reports that nodes, connections, tools or node configs
	// were left out to keep the view bounded.
	Truncated bool `json:"truncated"`
}

// NodeView is a node of a FlowView.
type NodeView struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	// Name is the display name of the node's type (the editor's label).
	Name     string        `json:"name"`
	Enabled  bool          `json:"enabled"`
	Position flow.Position `json:"position"`
	// Risk is the node type's risk (empty for an unknown type).
	Risk node.Risk `json:"risk,omitempty"`
	// Config shows the AgentVisible, non-Sensitive properties; every other
	// present value is "<redacted>". Nil when ConfigOmitted.
	Config        map[string]any `json:"config"`
	ConfigOmitted bool           `json:"configOmitted,omitempty"`
}

// ConnectionView is a connection of a FlowView.
type ConnectionView struct {
	ID         string `json:"id"`
	SourceNode string `json:"sourceNode"`
	SourcePort string `json:"sourcePort"`
	TargetNode string `json:"targetNode"`
	TargetPort string `json:"targetPort"`
}

// ToolView is a tool a running flow publishes, as computed at its deploy.
type ToolView struct {
	Name          string           `json:"name"`
	TriggerNodeID string           `json:"triggerNodeId"`
	Title         string           `json:"title,omitempty"`
	Risk          node.Risk        `json:"risk"`
	Annotations   flow.Annotations `json:"annotations"`
	// Fingerprint identifies the tool's contract; grants bind to it.
	Fingerprint string `json:"fingerprint"`
	Confirm     string `json:"confirm"`
	TimeoutMs   int64  `json:"timeoutMs"`
}

// GetFlow shows a stored flow: its nodes with their default-deny redacted
// configs and type risks, its connections, and the tools it publishes while
// running. Known secret values never appear. The view is bounded
// (maxViewNodes, maxViewConnections, maxViewTools) and its JSON within
// maxReadViewBytes: the structure first (nodes without their configs, then
// connections, then tools, each in order while they fit), then the node
// configs in node order while they fit (the others are omitted).
func (r *Reads) GetFlow(ctx context.Context, actor Actor, flowID string) (_ FlowView, err error) {
	defer r.audited(actor, ReadToolGetFlow, readArgs{FlowID: flowID}, r.now())(&err)
	if err := r.authorize(ctx, actor); err != nil {
		return FlowView{}, err
	}
	f, err := r.loadFlow(flowID)
	if err != nil {
		return FlowView{}, err
	}
	red := r.redactor(r.sensitiveValues(f)...)
	id := func(s string) string { return capText(red.text(s), maxViewIDBytes) }

	view := FlowView{
		ID:               id(f.ID),
		Name:             r.flowName(red, f.Name),
		State:            flowState(f, r.runningSet()[f.ID]),
		Nodes:            make([]NodeView, 0, min(len(f.Nodes), maxViewNodes)),
		Connections:      make([]ConnectionView, 0, min(len(f.Connections), maxViewConnections)),
		Tools:            []ToolView{},
		TotalNodes:       len(f.Nodes),
		TotalConnections: len(f.Connections),
	}
	budget := newViewBudget(view)
	schemas := make([]*node.ConfigSchema, 0, cap(view.Nodes))
	for i := range f.Nodes {
		n := &f.Nodes[i]
		nv := NodeView{ID: id(n.ID), Type: id(n.Type), Enabled: n.Enabled, Position: n.Position, ConfigOmitted: true}
		var schema *node.ConfigSchema
		if info, ok := r.registry.Get(n.Type); ok {
			nv.Name, nv.Risk, schema = red.text(info.Name), info.Risk, info.Config
		}
		if i == maxViewNodes || !budget.takeElement(nv) {
			view.Truncated = true
			break
		}
		view.Nodes = append(view.Nodes, nv)
		schemas = append(schemas, schema)
	}
	for i, c := range f.Connections {
		cv := ConnectionView{
			ID: id(c.ID), SourceNode: id(c.SourceNode), SourcePort: id(c.SourcePort),
			TargetNode: id(c.TargetNode), TargetPort: id(c.TargetPort),
		}
		if i == maxViewConnections || !budget.takeElement(cv) {
			view.Truncated = true
			break
		}
		view.Connections = append(view.Connections, cv)
	}
	for i, ti := range r.toolsByFlow()[f.ID] {
		tv := ToolView{
			Name:          capText(red.text(ti.ToolName), maxViewIDBytes),
			TriggerNodeID: id(ti.TriggerNodeID),
			Title:         capText(red.text(ti.Title), maxFlowNameBytes),
			Risk:          ti.Risk,
			Annotations:   ti.Annotations,
			Fingerprint:   red.text(ti.Fingerprint),
			Confirm:       ti.Confirm,
			TimeoutMs:     ti.Timeout.Milliseconds(),
		}
		if i == maxViewTools || !budget.takeElement(tv) {
			view.Truncated = true
			break
		}
		view.Tools = append(view.Tools, tv)
	}
	// A shown config replaces "config":null,"configOmitted":true.
	const omitted = len(`null,"configOmitted":true`)
	for i := range view.Nodes {
		cfg := configView(red, schemas[i], f.Nodes[i].Config)
		if !budget.take(jsonLen(cfg) - omitted) {
			view.Truncated = true
			continue
		}
		view.Nodes[i].Config, view.Nodes[i].ConfigOmitted = cfg, false
	}
	return view, nil
}

// loadFlow reads a stored flow; a missing one is a flow_not_found tool error.
func (r *Reads) loadFlow(flowID string) (*flow.Flow, error) {
	f, err := r.flows.GetFlow(flowID)
	if errors.Is(err, storage.ErrFlowNotFound) || errors.Is(err, storage.ErrInvalidFlowData) {
		return nil, toolError(ErrFlowNotFound, fmt.Sprintf("unknown flow %q", capText(flowID, maxViewIDBytes)))
	}
	if err != nil {
		return nil, fmt.Errorf("get flow: %w", err)
	}
	return f, nil
}
