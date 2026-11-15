package flow

import (
	"context"
	"sync"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// toolSniff is a safe processing node that tries the tools of its context
// and records what it got.
type toolSniff struct {
	*node.BaseNode
	mu    *sync.Mutex
	tools *[]int
	errs  *[]error
	infos *[]node.NodeInfo
}

func (n *toolSniff) Process(ctx context.Context, msg *Message) ([]*Message, error) {
	tc := node.ToolCallerFromContext(ctx)
	listed := len(tc.AgentTools(node.AgentCaller{FlowID: "f", NodeID: "n", Tools: []string{"lights_on"}}))
	_, err := tc.CallAgentTool(ctx, node.AgentCaller{FlowID: "f", NodeID: "n", Tools: []string{"lights_on"}}, "lights_on", nil)
	n.mu.Lock()
	*n.tools = append(*n.tools, listed)
	*n.errs = append(*n.errs, err)
	*n.infos = append(*n.infos, node.NodeInfoFromContext(ctx))
	n.mu.Unlock()
	out := msg.Clone()
	out.SourcePort = "output"
	return []*Message{out}, nil
}

func (n *toolSniff) Ports() (inputs, outputs []Port) {
	return []Port{{ID: "input", DataType: DataTypeAny}}, []Port{{ID: "output", DataType: DataTypeAny}}
}

type toolSniffs struct {
	mu    sync.Mutex
	tools []int
	errs  []error
	infos []node.NodeInfo
}

func (s *toolSniffs) register(t *testing.T, reg *node.Registry) {
	t.Helper()
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "toolsniff", Category: NodeCategoryProcessing, Risk: node.RiskSafe,
		Inputs: []Port{{ID: "input", DataType: DataTypeAny}}, Outputs: []Port{{ID: "output", DataType: DataTypeAny}}},
		func(cfg map[string]any) (node.Node, error) {
			return &toolSniff{BaseNode: node.NewBaseNode(cfg), mu: &s.mu, tools: &s.tools, errs: &s.errs, infos: &s.infos}, nil
		}))
}

// A dry-run never calls tools: its nodes get the tool caller that lists
// nothing and refuses every call, even when the caller's context carries
// one.
func TestDryRunHasNoToolCaller(t *testing.T) {
	p := newSandboxProbe()
	reg := probeRegistry(t, p)
	sniffs := &toolSniffs{}
	sniffs.register(t, reg)
	tc := &fakeToolCaller{}

	_, err := DryRun(node.WithToolCaller(context.Background(), tc), probeFlow(nil, "toolsniff"), reg, DryRunInput{TriggerNodeID: "trig", Payload: 1})
	require.NoError(t, err)
	sniffs.mu.Lock()
	defer sniffs.mu.Unlock()
	require.Len(t, sniffs.errs, 1, "the node ran")
	assert.Zero(t, sniffs.tools[0])
	assert.ErrorIs(t, sniffs.errs[0], node.ErrNoToolCaller)
	assert.Zero(t, tc.count())
}

// A node test never calls tools either, whatever the caller's context
// carries, and names no flow.
func TestTestNodeHasNoToolCaller(t *testing.T) {
	reg := node.NewRegistry()
	sniffs := &toolSniffs{}
	sniffs.register(t, reg)
	tc := &fakeToolCaller{}
	ctx := node.WithNodeInfo(node.WithToolCaller(context.Background(), tc), node.NodeInfo{FlowID: "leaked", NodeID: "leaked"})

	res, err := TestNode(ctx, reg, "toolsniff", nil, 1)
	require.NoError(t, err)
	require.Empty(t, res.Error)
	sniffs.mu.Lock()
	defer sniffs.mu.Unlock()
	require.Len(t, sniffs.errs, 1)
	assert.Zero(t, sniffs.tools[0])
	assert.ErrorIs(t, sniffs.errs[0], node.ErrNoToolCaller)
	assert.Equal(t, node.NodeInfo{}, sniffs.infos[0])
	assert.Zero(t, tc.count())
}

// A hidden-settings stub (never built from its settings) still has the
// output ports its visible settings give it (node.ConfigOutputs): the
// connections leaving them stay routable; a redacted list gives none.
func TestHiddenStubKeepsPortsFromItsSettings(t *testing.T) {
	info := node.NodeTypeInfo{
		Type: "test-sort", Category: NodeCategoryProcessing, Risk: node.RiskSafe,
		Inputs:      []Port{{ID: "input", DataType: DataTypeAny}},
		Outputs:     []Port{{ID: "rest", DataType: DataTypeAny}},
		OutputsFrom: "labels",
		Config: &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
			"labels": {Type: "array", AgentVisible: true},
		}},
	}
	factory := dryRunOverride(node.NewRegistry())(info, func(map[string]any) (node.Node, error) {
		t.Fatal("a hidden stub is never built from its settings")
		return nil, nil
	})
	stub, err := factory(map[string]any{hiddenStubKey: true, "labels": []any{"bug", "question"}})
	require.NoError(t, err)
	_, outs := stub.Ports()
	require.Len(t, outs, 3)
	assert.Equal(t, []string{"rest", "bug", "question"}, []string{outs[0].ID, outs[1].ID, outs[2].ID})

	stub, err = factory(map[string]any{hiddenStubKey: true, "labels": redactedConfigValue})
	require.NoError(t, err)
	_, outs = stub.Ports()
	require.Len(t, outs, 1)
	assert.Equal(t, "rest", outs[0].ID)
}
