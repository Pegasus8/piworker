package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// leakyRegistry has a node type whose factory quotes its Sensitive settings
// in its errors, as a careless node might.
func leakyRegistry(t *testing.T) *node.Registry {
	t.Helper()
	reg := createTestRegistry()
	schema := &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
		"password": {Type: "string", Sensitive: true},
		"headers":  {Type: "object", Sensitive: true},
		"pin":      {Type: "string", Sensitive: true},
		"host":     {Type: "string", AgentVisible: true},
	}}
	require.NoError(t, reg.Register(node.NodeTypeInfo{
		Type: "leaky", Name: "Leaky", Category: NodeCategoryOutput, Risk: node.RiskExternal,
		Inputs: []Port{{ID: "input", DataType: DataTypeAny}}, Config: schema,
	}, func(cfg map[string]any) (node.Node, error) {
		headers, _ := cfg["headers"].(map[string]any)
		return nil, fmt.Errorf("cannot log in to %v as %v with headers %v (pin %v)", cfg["host"], cfg["password"], headers["Authorization"], cfg["pin"])
	}))
	return reg
}

// Lint builds node instances to validate them, as deploy does; it closes the
// ones holding resources (node.Closer, ADR-0008) before it returns, and so
// does a deploy that fails while building them.
func TestLintClosesTheNodesItBuilds(t *testing.T) {
	res := &closingNode{BaseNode: node.NewBaseNode(nil)}
	trig := &closingTrigger{lifecycleTrigger: lifecycleTrigger{BaseNode: node.NewBaseNode(nil)}}
	reg := node.NewRegistry()
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "trig", Name: "trig", Category: NodeCategoryInput, Risk: node.RiskSafe},
		func(map[string]any) (node.Node, error) { return trig, nil }))
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "res", Name: "res", Category: NodeCategoryOutput, Risk: node.RiskSafe},
		func(map[string]any) (node.Node, error) { return res, nil }))
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "broken", Name: "broken", Category: NodeCategoryOutput, Risk: node.RiskSafe},
		func(map[string]any) (node.Node, error) { return nil, errors.New("broken") }))

	f := NewFlow("closers")
	f.AddNode(*NewNode("trig", NodeCategoryInput))
	f.AddNode(*NewNode("res", NodeCategoryOutput))
	assert.Empty(t, Lint(f, reg))
	assert.Equal(t, int32(1), res.closed.Load())
	assert.Equal(t, int32(1), trig.closed.Load())

	f.AddNode(*NewNode("broken", NodeCategoryOutput))
	_, err := NewFlowRuntime(f, reg)
	require.ErrorIs(t, err, ErrInvalidNodeConfig)
	assert.Equal(t, int32(2), res.closed.Load(), "a failed build closes what it built")
	assert.Equal(t, int32(2), trig.closed.Load())

	// A deploy refused after the build (no trigger) closes the instances.
	lone := NewFlow("no-trigger")
	lone.AddNode(*NewNode("res", NodeCategoryOutput))
	err = NewRuntimeManager(reg, zerolog.Nop()).Deploy(context.Background(), lone)
	require.ErrorIs(t, err, ErrNoTriggerNodes)
	assert.Equal(t, int32(3), res.closed.Load(), "a refused deploy closes what it built")
}

func TestLintNeverEchoesSensitiveValues(t *testing.T) {
	reg := leakyRegistry(t)
	f := createSimpleFlow()
	n := NewNode("leaky", NodeCategoryOutput)
	n.ID = "leaky"
	n.Config = map[string]any{
		"host":     "db.example.com",
		"password": "hunter2-secret",
		"headers":  map[string]any{"Authorization": "Bearer tok-123456"},
		"pin":      "42",
	}
	f.AddNode(*n)

	issues := Lint(f, reg)
	require.Len(t, issues, 1)
	assert.Equal(t, IssueInvalidConfig, issues[0].Code)
	assert.Equal(t, "/nodes/leaky", issues[0].Path)
	for _, secret := range []string{"hunter2-secret", "tok-123456", "42"} {
		assert.NotContains(t, issues[0].Message, secret)
	}

	// Without short values, the message keeps what agents may read.
	n.Config["pin"] = "12345678"
	f.Nodes[len(f.Nodes)-1] = *n
	issues = Lint(f, reg)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0].Message, "db.example.com")
	assert.NotContains(t, issues[0].Message, "hunter2-secret")
	assert.NotContains(t, issues[0].Message, "12345678")
}

// The structural collector keeps validate's order, so deploy (which returns
// the first error) reports the same problem as before collectors existed.
func TestStructureFindingsKeepValidateOrder(t *testing.T) {
	f, _, a, b := structuralFlow()
	f.AddConnection(*NewConnection(a.ID, "output", a.ID, "input"))    // self loop
	f.AddConnection(*NewConnection(b.ID, "output", "ghost", "input")) // unknown node
	f.AddConnection(*NewConnection(b.ID, "output", a.ID, "input"))    // cycle
	dup := *passPorts(NewNode("process-x", NodeCategoryProcessing))
	dup.ID = b.ID
	f.AddNode(dup) // duplicate ID, found first

	err := f.Validate(modelTestRegistry())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate node ID "+b.ID)

	issues := Lint(f, modelTestRegistry())
	codes := map[string]bool{}
	for _, i := range issues {
		codes[i.Code] = true
	}
	for _, code := range []string{IssueDuplicateNodeID, IssueSelfLoop, IssueConnectionUnknownNode, IssueCycle} {
		assert.Truef(t, codes[code], "%s missing from %v", code, issues)
	}
	var verr *ValidationError
	assert.True(t, errors.As(err, &verr))
}

func TestJSONPointerEscaping(t *testing.T) {
	assert.Equal(t, "/nodes/a~1b~0c/config/x~1y", nodePointer("a/b~c", "x/y"))
	assert.Equal(t, "/nodes/n1", nodePointer("n1", ""))
}

// quotingRegistry has a node type whose factory quotes its settings the
// ways careless node errors do: %q, %+q, JSON, and CheckConfig-style
// truncation.
func quotingRegistry(t *testing.T) *node.Registry {
	t.Helper()
	reg := createTestRegistry()
	schema := &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
		"password": {Type: "string", Sensitive: true},
		"target":   {Type: "string"},
		"mode":     {Type: "string", AgentVisible: true},
	}}
	require.NoError(t, reg.Register(node.NodeTypeInfo{
		Type: "quoting", Name: "Quoting", Category: NodeCategoryOutput, Risk: node.RiskExternal,
		Inputs: []Port{{ID: "input", DataType: DataTypeAny}}, Config: schema,
	}, func(cfg map[string]any) (node.Node, error) {
		pw, _ := cfg["password"].(string)
		target, _ := cfg["target"].(string)
		js, _ := json.Marshal(pw)
		short := []rune(target)
		if len(short) > 32 {
			short = short[:32]
		}
		return nil, fmt.Errorf("bad password %q / %+q / %s; target %q / %q…; mode %q", pw, pw, js, target, string(short), cfg["mode"])
	}))
	return reg
}

func TestLintHidesQuotedForms(t *testing.T) {
	reg := quotingRegistry(t)
	f := createSimpleFlow()
	n := NewNode("quoting", NodeCategoryOutput)
	n.ID = "q"
	n.Config = map[string]any{
		"password": "s3cr3t\"tail-of-the-<secret>-ñandú",
		"target":   "https://hooks.example.com/services/T000/B000/XXXXXXXXXXXXXXXXXXXXXXXX",
		"mode":     "fast-mode",
	}
	f.AddNode(*n)

	issues := Lint(f, reg)
	require.Len(t, issues, 1)
	msg := issues[0].Message
	for _, leak := range []string{"s3cr3t", "tail-of-the", "secret", "XXXXXXXX", "services/T000", "hooks.example.com/serv"} {
		assert.NotContains(t, msg, leak)
	}
	assert.Contains(t, msg, `mode "fast-mode"`, "values agents may read stay")
	assert.Contains(t, msg, "<hidden>")
}
