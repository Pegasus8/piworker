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

// agentLike has the settings of an agent node that bound what it may do
// (ADR-0039): the tools it calls (a Destination list) and allowPrivileged (a
// RiskSwitch).
type agentLike struct{ *node.BaseNode }

func (n *agentLike) Process(_ context.Context, msg *types.Message) ([]*types.Message, error) {
	return []*types.Message{msg}, nil
}

func (n *agentLike) Ports() ([]types.Port, []types.Port) {
	return []types.Port{{ID: "input", DataType: types.DataTypeAny}}, []types.Port{{ID: "output", DataType: types.DataTypeAny}}
}

func agentLikeRegistry(t *testing.T) *node.Registry {
	t.Helper()
	reg := node.DefaultRegistry.Derive(nil)
	require.NoError(t, reg.Register(node.NodeTypeInfo{
		Type: "test-agent", Name: "Agent", Category: types.NodeCategoryProcessing, Risk: node.RiskExternal,
		Inputs:  []types.Port{{ID: "input", DataType: types.DataTypeAny}},
		Outputs: []types.Port{{ID: "output", DataType: types.DataTypeAny}},
		Config: &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
			"tools":           {Type: "array", Title: "Tools", AgentVisible: true, Destination: true, Options: node.OptionsTools},
			"allowPrivileged": {Type: "boolean", Title: "Allow privileged tools", AgentVisible: true, RiskSwitch: true},
		}},
	}, func(cfg map[string]any) (node.Node, error) { return &agentLike{BaseNode: node.NewBaseNode(cfg)}, nil }))
	return reg
}

// Changing the tools an agent node may call is flagged like a changed
// destination, and turning allowPrivileged on like any risk switch.
func TestDiffFlagsAnAgentNodesTools(t *testing.T) {
	reg := agentLikeRegistry(t)
	base := diffFlow(trigger(), lintNode("agent", "test-agent", map[string]any{"tools": []any{"lights_on"}}))
	next := diffFlow(trigger(), lintNode("agent", "test-agent", map[string]any{"tools": []any{"lights_on", "open_garage"}, "allowPrivileged": true}))

	r := flow.Diff(base, next, reg)
	assert.Contains(t, alertKinds(r), flow.AlertDestinationChanged)
	assert.Contains(t, alertKinds(r), flow.AlertRiskSwitchOn)
	a := alertOf(t, r, flow.AlertDestinationChanged)
	assert.Equal(t, "agent", a.NodeID)
	assert.Equal(t, "config.tools", a.Path)

	same := flow.Diff(base, diffFlow(trigger(), lintNode("agent", "test-agent", map[string]any{"tools": []any{"lights_on"}})), reg)
	assert.NotContains(t, alertKinds(same), flow.AlertDestinationChanged)
}
