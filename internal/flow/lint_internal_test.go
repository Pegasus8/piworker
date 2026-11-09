package flow

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
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
		"host":     {Type: "string"},
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

	// Without short values, the message keeps what is not sensitive.
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
