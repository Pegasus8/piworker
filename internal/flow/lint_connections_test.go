package flow_test

import (
	"context"
	"testing"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connNode names a broker connection in its "connection" setting.
type connNode struct{ name string }

func (n *connNode) Process(context.Context, *types.Message) ([]*types.Message, error) {
	return nil, nil
}
func (n *connNode) Ports() ([]types.Port, []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}}, nil
}
func (n *connNode) Validate() error { return nil }
func (n *connNode) RequiredConnections() map[string]string {
	return map[string]string{"connection": n.name}
}

// connTrigger is a trigger without settings.
type connTrigger struct{}

func (connTrigger) Process(context.Context, *types.Message) ([]*types.Message, error) {
	return nil, nil
}
func (connTrigger) Ports() ([]types.Port, []types.Port) {
	return nil, []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny}}
}
func (connTrigger) Validate() error                                    { return nil }
func (connTrigger) Start(context.Context, chan<- *types.Message) error { return nil }
func (connTrigger) Stop() error                                        { return nil }

func connRegistry(t *testing.T) *node.Registry {
	t.Helper()
	reg := node.NewRegistry()
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "trigger-x", Name: "X", Description: "x", Category: types.NodeCategoryInput,
		Outputs: []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny}}, Risk: node.RiskSafe},
		func(map[string]any) (node.Node, error) { return connTrigger{}, nil }))
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "action-conn", Name: "C", Description: "c", Category: types.NodeCategoryOutput,
		Inputs: []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}}, Risk: node.RiskExternal,
		Config: &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
			"connection": {Type: "string", Title: "Connection", AgentVisible: true, Destination: true}}}},
		func(cfg map[string]any) (node.Node, error) {
			s, _ := cfg["connection"].(string)
			return &connNode{name: s}, nil
		}))
	return reg
}

// Deploy and Lint refuse a node naming a broker connection that is not
// defined (node.ConnectionRequirer), in the store of the context; like an
// undefined secret it is an environment issue (the admin may define it).
func TestLintAndDeployCheckRequiredConnections(t *testing.T) {
	reg := connRegistry(t)
	f := flow.NewFlow("conns")
	trig := flow.NewNode("trigger-x", flow.NodeCategoryInput)
	trig.ID = "t"
	trig.Outputs = []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny}}
	act := flow.NewNode("action-conn", flow.NodeCategoryOutput)
	act.ID = "a"
	act.Inputs = []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}}
	act.Config = map[string]any{"connection": "pw_lint_home"}
	f.Nodes = []flow.Node{*trig, *act}
	f.Connections = []flow.Connection{lintConn("c1", "t", "output", "a", "input")}

	empty := brokers.WithStore(context.Background(), brokers.NewStore())
	issues := flow.LintContext(empty, f, reg)
	require.Equal(t, []string{"error:" + flow.IssueUndefinedConnection + "@/nodes/a/config/connection"}, issueKeys(issues))
	assert.Contains(t, issues[0].Message, `"pw_lint_home"`)
	assert.True(t, flow.EnvironmentIssue(issues[0]))
	assert.Empty(t, flow.BlockingIssues(issues))

	m := flow.NewRuntimeManager(reg, zerolog.Nop())
	err := m.Deploy(empty, f)
	require.Error(t, err)
	var verr *flow.ValidationError
	assert.ErrorAs(t, err, &verr, "400, not 500")
	assert.Contains(t, err.Error(), `"pw_lint_home"`)

	store := brokers.NewStore()
	require.NoError(t, store.Set(brokers.Connection{Name: "pw_lint_home", Broker: "tcp://x:1883"}))
	defined := brokers.WithStore(context.Background(), store)
	assert.Empty(t, flow.LintContext(defined, f, reg))
	require.NoError(t, m.Deploy(defined, f))
	require.NoError(t, m.Undeploy(f.ID))
}
