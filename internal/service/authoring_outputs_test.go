package service

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/stretchr/testify/assert"
)

// A whole flow an agent sends gets, like the nodes ApplyOps adds, the
// output ports a node's settings give it (node.ConfigOutputs): a
// classifier's categories are ports of the stored node.
func TestNormalizeNodesGivesThePortsOfTheSettings(t *testing.T) {
	e := newAuthEnv(t)
	f := flow.NewFlow("triage")
	n := flow.NewNode("process-llm-classify", "")
	n.ID, n.Config = "classify", map[string]any{"connection": "claude", "categories": []any{"bug", "question"}}
	f.AddNode(*n)

	e.a.normalizeNodes(f)
	var ids []string
	for _, p := range f.Nodes[0].Outputs {
		ids = append(ids, p.ID)
	}
	assert.Equal(t, []string{"other", "bug", "question"}, ids)
	assert.Equal(t, flow.NodeCategoryProcessing, f.Nodes[0].Category)
}
