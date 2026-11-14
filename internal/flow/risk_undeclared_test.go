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

type undeclaredRiskNode struct{}

func (undeclaredRiskNode) Process(_ context.Context, msg *types.Message) ([]*types.Message, error) {
	return []*types.Message{msg}, nil
}

func (undeclaredRiskNode) Ports() ([]types.Port, []types.Port) {
	return []types.Port{{ID: "input", Name: "Input"}}, []types.Port{{ID: "output", Name: "Output"}}
}

func (undeclaredRiskNode) Validate() error { return nil }

// registerRisk registers a pass-through node type whose info declares risk
// (possibly nothing, or a value no builtin uses) for the length of the test.
func registerRisk(t *testing.T, typ string, risk node.Risk) {
	t.Helper()
	require.NoError(t, node.DefaultRegistry.Register(node.NodeTypeInfo{
		Type: typ, Name: typ, Category: types.NodeCategoryProcessing, Risk: risk,
		Inputs:  []types.Port{{ID: "input", Name: "Input"}},
		Outputs: []types.Port{{ID: "output", Name: "Output"}},
	}, func(map[string]any) (node.Node, error) { return undeclaredRiskNode{}, nil }))
	t.Cleanup(func() { node.DefaultRegistry.Unregister(typ) })
}

// A node type registered without a valid risk counts as privileged, like an
// unknown type: a tool reaching it is privileged (so all_tools never covers
// it and its arguments must be strict), whatever else it reaches.
func TestToolRiskOfAnUndeclaredNodeRiskIsPrivileged(t *testing.T) {
	registerRisk(t, "test-risk-undeclared", "")
	registerRisk(t, "test-risk-bogus", "harmless")

	for _, typ := range []string{"test-risk-undeclared", "test-risk-bogus"} {
		info, _ := onlyTool(t, chainFlow(strictToolConfig(), riskStep{id: "n", typ: typ}, reply()))
		assert.Equal(t, node.RiskPrivileged, info.Risk, typ)

		// Reached before a privileged node, it does not hide it either.
		info, _ = onlyTool(t, fanFlow(strictToolConfig(),
			riskStep{id: "n", typ: typ},
			riskStep{id: "cmd", typ: "action-command", config: map[string]any{"command": "echo hi"}},
		))
		assert.Equal(t, node.RiskPrivileged, info.Risk, typ)
	}

	// The strict-arguments rule applies.
	cfg := strictToolConfig()
	cfg["inputSchema"] = map[string]any{
		"type":       "object",
		"properties": map[string]any{"note": map[string]any{"type": "string", "maxLength": float64(20)}},
	}
	_, issues := analyze(t, chainFlow(cfg, riskStep{id: "n", typ: "test-risk-undeclared"}))
	require.Len(t, issues, 1, "%v", issues)
	assert.Equal(t, flow.IssueToolStrictArguments, issues[0].Code)
}
