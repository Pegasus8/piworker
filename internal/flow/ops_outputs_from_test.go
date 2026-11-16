package flow_test

import (
	"context"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sortNode is a test type whose "labels" setting adds output ports (like the
// LLM classifier's categories), after its static "rest".
type sortNode struct{ *node.BaseNode }

func sortInfo() node.NodeTypeInfo {
	return node.NodeTypeInfo{
		Type: "test-sort", Name: "Sort", Category: types.NodeCategoryProcessing, Risk: node.RiskSafe,
		Inputs:      []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}},
		Outputs:     []types.Port{{ID: "rest", Name: "Rest", DataType: types.DataTypeAny}},
		OutputsFrom: "labels",
		Config: &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
			"labels": {Type: "array", Title: "Labels", AgentVisible: true},
		}},
	}
}

func (n *sortNode) Process(_ context.Context, msg *types.Message) ([]*types.Message, error) {
	return []*types.Message{msg}, nil
}

func (n *sortNode) Ports() ([]types.Port, []types.Port) {
	info := sortInfo()
	return info.Inputs, node.ConfigOutputs(info, n.GetConfig())
}

func sortRegistry(t *testing.T) *node.Registry {
	t.Helper()
	reg := node.DefaultRegistry.Derive(nil)
	require.NoError(t, reg.Register(sortInfo(), func(cfg map[string]any) (node.Node, error) {
		return &sortNode{BaseNode: node.NewBaseNode(cfg)}, nil
	}))
	return reg
}

// An agent's operations see the ports a node's settings give it: a
// connection may leave an item's port, and the node's stored outputs follow
// its config.
func TestApplyOpsPortsFromASetting(t *testing.T) {
	reg := sortRegistry(t)
	next, issues := flow.ApplyOps(opsBase(), decodeOps(t, `[
		{"op":"add_node","node":{"id":"sort","type":"test-sort","config":{"labels":["bug","question"]}}},
		{"op":"connect","sourceNode":"t","sourcePort":"output","targetNode":"sort","targetPort":"input"},
		{"op":"connect","sourceNode":"sort","sourcePort":"bug","targetNode":"log","targetPort":"input"}
	]`), reg)
	require.Empty(t, issues)
	sort := nodeByID(next, "sort")
	require.NotNil(t, sort)
	assert.Equal(t, []string{"rest", "bug", "question"}, outputIDs(sort))

	_, issues = flow.ApplyOps(next, decodeOps(t, `[
		{"op":"connect","sourceNode":"sort","sourcePort":"praise","targetNode":"log","targetPort":"input"}
	]`), reg)
	require.Len(t, issues, 1)
	assert.Equal(t, flow.IssueConnectionUnknownPort, issues[0].Code)

	next, issues = flow.ApplyOps(next, decodeOps(t, `[
		{"op":"update_node","nodeId":"sort","config":{"labels":["bug","praise"]}},
		{"op":"connect","sourceNode":"sort","sourcePort":"praise","targetNode":"log","targetPort":"input"}
	]`), reg)
	require.Empty(t, issues)
	assert.Equal(t, []string{"rest", "bug", "praise"}, outputIDs(nodeByID(next, "sort")))
}

func outputIDs(n *flow.Node) []string {
	ids := make([]string, len(n.Outputs))
	for i, p := range n.Outputs {
		ids[i] = p.ID
	}
	return ids
}
