package flow_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func boolPtr(b bool) *bool { return &b }

// opsBase is trigger-manual "t" → action-log "log", placed on the canvas.
func opsBase() *flow.Flow {
	f := flow.NewFlow("ops")
	t := lintNode("t", "trigger-manual", nil)
	t.Position = flow.Position{X: 100, Y: 100}
	l := lintNode("log", "action-log", map[string]any{
		"message": "hi", "level": "info",
		"extra": map[string]any{"x": float64(1), "y": float64(2)},
	})
	l.Position = flow.Position{X: 340, Y: 100}
	f.Nodes = []flow.Node{t, l}
	f.Connections = []flow.Connection{lintConn("c1", "t", "output", "log", "input")}
	return f
}

func decodeOps(t *testing.T, raw string) []flow.Op {
	t.Helper()
	var ops []flow.Op
	require.NoError(t, json.Unmarshal([]byte(raw), &ops))
	return ops
}

func snapshot(t *testing.T, f *flow.Flow) string {
	t.Helper()
	data, err := json.Marshal(f)
	require.NoError(t, err)
	return string(data)
}

func nodeByID(f *flow.Flow, id string) *flow.Node {
	for i := range f.Nodes {
		if f.Nodes[i].ID == id {
			return &f.Nodes[i]
		}
	}
	return nil
}

func TestApplyOpsAddNode(t *testing.T) {
	base := opsBase()
	before := snapshot(t, base)
	next, issues := flow.ApplyOps(base, decodeOps(t, `[
		{"op":"add_node","node":{"id":"dbg","type":"process-debug","config":{"name":"x"}}},
		{"op":"add_node","node":{"type":"action-log","enabled":false,"position":{"x":5,"y":6}}}
	]`), node.DefaultRegistry)
	require.Empty(t, issues)
	require.NotNil(t, next)
	assert.Equal(t, before, snapshot(t, base), "the base is never modified")
	require.Len(t, next.Nodes, 4)

	dbg := nodeByID(next, "dbg")
	require.NotNil(t, dbg)
	info, _ := node.DefaultRegistry.Get("process-debug")
	assert.Equal(t, flow.NodeCategoryProcessing, dbg.Category, "category from the registry")
	assert.Equal(t, info.Inputs, dbg.Inputs, "ports from the registry")
	assert.Equal(t, info.Outputs, dbg.Outputs)
	assert.Equal(t, map[string]any{"name": "x"}, dbg.Config)
	assert.True(t, dbg.Enabled, "enabled by default")
	assert.NotEqual(t, flow.Position{}, dbg.Position, "placed by auto-layout")

	generated := next.Nodes[3]
	assert.NotEmpty(t, generated.ID, "an ID is generated")
	assert.Equal(t, "action-log", generated.Type)
	assert.False(t, generated.Enabled)
	assert.Equal(t, flow.Position{X: 5, Y: 6}, generated.Position, "a given position is kept")
	assert.NotNil(t, generated.Config)
}

func TestApplyOpsUpdateNodeMergesConfig(t *testing.T) {
	base := opsBase()
	before := snapshot(t, base)
	next, issues := flow.ApplyOps(base, decodeOps(t, `[
		{"op":"update_node","nodeId":"log","config":{"level":null,"message":"bye","extra":{"y":null,"z":3}},"enabled":false,"position":{"x":1,"y":2}}
	]`), node.DefaultRegistry)
	require.Empty(t, issues)
	assert.Equal(t, before, snapshot(t, base))
	l := nodeByID(next, "log")
	assert.Equal(t, map[string]any{"message": "bye", "extra": map[string]any{"x": float64(1), "z": float64(3)}}, l.Config)
	assert.False(t, l.Enabled)
	assert.Equal(t, flow.Position{X: 1, Y: 2}, l.Position)
	assert.Equal(t, "info", nodeByID(base, "log").Config["level"], "the base keeps its nested values too")
	assert.Equal(t, float64(2), nodeByID(base, "log").Config["extra"].(map[string]any)["y"])
}

func TestApplyOpsRemoveNodeAndConnections(t *testing.T) {
	base := opsBase()
	next, issues := flow.ApplyOps(base, decodeOps(t, `[
		{"op":"remove_node","nodeId":"log"}
	]`), node.DefaultRegistry)
	require.Empty(t, issues)
	assert.Len(t, next.Nodes, 1)
	assert.Empty(t, next.Connections, "its connections go with it")
	assert.Len(t, base.Connections, 1)
}

func TestApplyOpsConnectAndDisconnect(t *testing.T) {
	base := opsBase()
	next, issues := flow.ApplyOps(base, decodeOps(t, `[
		{"op":"add_node","node":{"id":"dbg","type":"process-debug"}},
		{"op":"connect","sourceNode":"t","sourcePort":"output","targetNode":"dbg","targetPort":"input"},
		{"op":"disconnect","connectionId":"c1"}
	]`), node.DefaultRegistry)
	require.Empty(t, issues)
	require.Len(t, next.Connections, 1)
	c := next.Connections[0]
	assert.NotEmpty(t, c.ID)
	assert.Equal(t, [4]string{"t", "output", "dbg", "input"}, [4]string{c.SourceNode, c.SourcePort, c.TargetNode, c.TargetPort})

	next, issues = flow.ApplyOps(base, decodeOps(t, `[
		{"op":"disconnect","sourceNode":"t","sourcePort":"output","targetNode":"log","targetPort":"input"}
	]`), node.DefaultRegistry)
	require.Empty(t, issues)
	assert.Empty(t, next.Connections)
	assert.Empty(t, flow.Lint(mustApply(t, base, `[{"op":"add_node","node":{"id":"dbg","type":"process-debug"}},{"op":"connect","sourceNode":"log","sourcePort":"output","targetNode":"dbg","targetPort":"input"}]`), node.DefaultRegistry))
}

func mustApply(t *testing.T, base *flow.Flow, raw string) *flow.Flow {
	t.Helper()
	next, issues := flow.ApplyOps(base, decodeOps(t, raw), node.DefaultRegistry)
	require.Empty(t, issues)
	return next
}

func TestApplyOpsErrors(t *testing.T) {
	cases := []struct {
		name, ops, code string
	}{
		{"unknown op", `[{"op":"rename"}]`, flow.IssueUnknownOp},
		{"add without node", `[{"op":"add_node"}]`, flow.IssueInvalidOp},
		{"add without type", `[{"op":"add_node","node":{"id":"x"}}]`, flow.IssueInvalidOp},
		{"add unknown type", `[{"op":"add_node","node":{"type":"nope"}}]`, flow.IssueUnknownType},
		{"add existing id", `[{"op":"add_node","node":{"id":"log","type":"action-log"}}]`, flow.IssueNodeExists},
		{"add bad id", `[{"op":"add_node","node":{"id":"a/b","type":"action-log"}}]`, flow.IssueInvalidNodeID},
		{"update unknown node", `[{"op":"update_node","nodeId":"ghost","enabled":true}]`, flow.IssueNodeNotFound},
		{"update without id", `[{"op":"update_node","enabled":true}]`, flow.IssueInvalidOp},
		{"update nothing", `[{"op":"update_node","nodeId":"log"}]`, flow.IssueInvalidOp},
		{"remove unknown node", `[{"op":"remove_node","nodeId":"ghost"}]`, flow.IssueNodeNotFound},
		{"remove without id", `[{"op":"remove_node"}]`, flow.IssueInvalidOp},
		{"connect missing field", `[{"op":"connect","sourceNode":"t","sourcePort":"output","targetNode":"log"}]`, flow.IssueInvalidOp},
		{"connect unknown node", `[{"op":"connect","sourceNode":"t","sourcePort":"output","targetNode":"ghost","targetPort":"input"}]`, flow.IssueNodeNotFound},
		{"connect unknown port", `[{"op":"connect","sourceNode":"t","sourcePort":"nope","targetNode":"log","targetPort":"input"}]`, flow.IssueConnectionUnknownPort},
		{"connect to itself", `[{"op":"connect","sourceNode":"log","sourcePort":"output","targetNode":"log","targetPort":"input"}]`, flow.IssueSelfLoop},
		{"connect twice", `[{"op":"connect","sourceNode":"t","sourcePort":"output","targetNode":"log","targetPort":"input"}]`, flow.IssueConnectionExists},
		{"disconnect unknown id", `[{"op":"disconnect","connectionId":"ghost"}]`, flow.IssueConnectionNotFound},
		{"disconnect unknown endpoints", `[{"op":"disconnect","sourceNode":"log","sourcePort":"output","targetNode":"t","targetPort":"input"}]`, flow.IssueConnectionNotFound},
		{"disconnect both forms", `[{"op":"disconnect","connectionId":"c1","sourceNode":"t"}]`, flow.IssueInvalidOp},
		{"disconnect nothing", `[{"op":"disconnect"}]`, flow.IssueInvalidOp},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := opsBase()
			before := snapshot(t, base)
			ops := append(decodeOps(t, `[{"op":"update_node","nodeId":"t","position":{"x":7,"y":7}}]`), decodeOps(t, tc.ops)...)
			next, issues := flow.ApplyOps(base, ops, node.DefaultRegistry)
			assert.Nil(t, next, "any issue: no flow")
			require.Len(t, issues, 1, "%v", issues)
			assert.Equal(t, "/ops/1", issues[0].Path)
			assert.Equal(t, tc.code, issues[0].Code)
			assert.Equal(t, flow.IssueError, issues[0].Severity)
			assert.NotEmpty(t, issues[0].Message)
			assert.Equal(t, before, snapshot(t, base), "the base is never modified")
		})
	}
}

func TestApplyOpsReportsEveryFailingOp(t *testing.T) {
	_, issues := flow.ApplyOps(opsBase(), decodeOps(t, `[
		{"op":"add_node","node":{"id":"bad","type":"nope"}},
		{"op":"connect","sourceNode":"t","sourcePort":"output","targetNode":"bad","targetPort":"input"},
		{"op":"remove_node","nodeId":"log"},
		{"op":"update_node","nodeId":"log","enabled":true}
	]`), node.DefaultRegistry)
	var got []string
	for _, i := range issues {
		got = append(got, i.Path+" "+i.Code)
	}
	assert.Equal(t, []string{
		"/ops/0 " + flow.IssueUnknownType,
		"/ops/1 " + flow.IssueNodeNotFound,
		"/ops/3 " + flow.IssueNodeNotFound,
	}, got, "later ops see the effect of the earlier ones that succeeded")
}

func TestApplyOpsLimit(t *testing.T) {
	ops := make([]flow.Op, flow.MaxOps+1)
	for i := range ops {
		ops[i] = flow.Op{Op: flow.OpUpdateNode, NodeID: "t", Enabled: boolPtr(true)}
	}
	next, issues := flow.ApplyOps(opsBase(), ops, node.DefaultRegistry)
	assert.Nil(t, next)
	require.Len(t, issues, 1)
	assert.Equal(t, "/ops", issues[0].Path)
	assert.Equal(t, flow.IssueTooManyOps, issues[0].Code)

	next, issues = flow.ApplyOps(opsBase(), ops[:flow.MaxOps], node.DefaultRegistry)
	assert.Empty(t, issues)
	assert.NotNil(t, next)
}

func TestApplyOpsOnNoBase(t *testing.T) {
	next, issues := flow.ApplyOps(nil, decodeOps(t, `[{"op":"add_node","node":{"id":"t","type":"trigger-manual"}}]`), node.DefaultRegistry)
	require.Empty(t, issues)
	require.Len(t, next.Nodes, 1)
	assert.NotNil(t, next.Connections)
}

func TestOpJSONRoundTrip(t *testing.T) {
	raw := `{"op":"connect","sourceNode":"a","sourcePort":"output","targetNode":"b","targetPort":"input"}`
	var op flow.Op
	require.NoError(t, json.Unmarshal([]byte(raw), &op))
	data, err := json.Marshal(op)
	require.NoError(t, err)
	assert.JSONEq(t, raw, string(data))
}

// ---- AutoLayout ----

type box struct {
	id   string
	x, y float64
}

func boxes(f *flow.Flow) []box {
	out := []box{}
	for _, n := range f.Nodes {
		out = append(out, box{n.ID, n.Position.X, n.Position.Y})
	}
	return out
}

func assertNoOverlap(t *testing.T, f *flow.Flow) {
	t.Helper()
	b := boxes(f)
	for i := range b {
		for j := i + 1; j < len(b); j++ {
			dx, dy := b[i].x-b[j].x, b[i].y-b[j].y
			if dx < 0 {
				dx = -dx
			}
			if dy < 0 {
				dy = -dy
			}
			assert.Falsef(t, dx < flow.LayoutNodeWidth && dy < flow.LayoutNodeHeight, "%s and %s overlap: %v %v", b[i].id, b[j].id, b[i], b[j])
		}
	}
}

func TestAutoLayoutPlacesNewNodesRightOfTheirUpstream(t *testing.T) {
	f := opsBase() // t (100,100) → log (340,100)
	f.Nodes = append(f.Nodes, lintNode("a", "process-debug", nil), lintNode("b", "process-debug", nil), lintNode("c", "process-debug", nil))
	f.Connections = append(f.Connections,
		lintConn("c2", "log", "output", "a", "input"),
		lintConn("c3", "a", "output", "b", "input"),
		lintConn("c4", "log", "output", "c", "input"))

	flow.AutoLayout(f, []string{"b", "c", "a"}) // the order given does not matter

	assert.Equal(t, flow.Position{X: 100, Y: 100}, nodeByID(f, "t").Position, "unlisted nodes never move")
	assert.Equal(t, flow.Position{X: 340, Y: 100}, nodeByID(f, "log").Position)
	assert.Equal(t, flow.Position{X: 580, Y: 100}, nodeByID(f, "a").Position)
	assert.Equal(t, flow.Position{X: 820, Y: 100}, nodeByID(f, "b").Position, "a chain continues rightwards")
	assert.Equal(t, flow.Position{X: 580, Y: 220}, nodeByID(f, "c").Position, "the next free slot below")
	assertNoOverlap(t, f)
}

func TestAutoLayoutStartsANewColumnForUnconnectedNodes(t *testing.T) {
	f := opsBase()
	f.Nodes = append(f.Nodes, lintNode("x", "trigger-manual", nil), lintNode("y", "trigger-manual", nil))
	flow.AutoLayout(f, []string{"x", "y"})
	assert.Equal(t, flow.Position{X: 580, Y: 100}, nodeByID(f, "x").Position)
	assert.Equal(t, flow.Position{X: 580, Y: 220}, nodeByID(f, "y").Position)
	assertNoOverlap(t, f)

	empty := flow.NewFlow("empty")
	empty.Nodes = []flow.Node{lintNode("first", "trigger-manual", nil)}
	flow.AutoLayout(empty, []string{"first"})
	assert.Equal(t, flow.Position{X: flow.LayoutOrigin, Y: flow.LayoutOrigin}, empty.Nodes[0].Position)
}

func TestAutoLayoutKeepsGivenPositions(t *testing.T) {
	f := opsBase()
	n := lintNode("placed", "process-debug", nil)
	n.Position = flow.Position{X: 3, Y: 4}
	f.Nodes = append(f.Nodes, n)
	flow.AutoLayout(f, []string{"placed", "missing"})
	assert.Equal(t, flow.Position{X: 3, Y: 4}, nodeByID(f, "placed").Position)
}

func TestAutoLayoutIsDeterministicAndNeverOverlaps(t *testing.T) {
	build := func() *flow.Flow {
		f := opsBase()
		var ids []string
		for i := range 30 {
			id := fmt.Sprintf("n%02d", i)
			ids = append(ids, id)
			f.Nodes = append(f.Nodes, lintNode(id, "process-debug", nil))
			src := "log"
			if i%3 != 0 {
				src = fmt.Sprintf("n%02d", i-1)
			}
			f.Connections = append(f.Connections, lintConn("e"+id, src, "output", id, "input"))
		}
		// an obstacle in the way of the first column
		obstacle := lintNode("obstacle", "process-debug", nil)
		obstacle.Position = flow.Position{X: 600, Y: 150}
		f.Nodes = append(f.Nodes, obstacle)
		flow.AutoLayout(f, ids)
		return f
	}
	a, b := build(), build()
	assert.Equal(t, boxes(a), boxes(b))
	assertNoOverlap(t, a)
	for _, n := range a.Nodes {
		assert.NotEqualf(t, flow.Position{}, n.Position, "%s placed", n.ID)
	}
	assert.True(t, strings.HasPrefix(a.Nodes[2].ID, "n"))
}
