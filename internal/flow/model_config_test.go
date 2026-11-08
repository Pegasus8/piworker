package flow

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// configCheckRegistry registers a trigger and an action that both declare a
// typed config schema. created counts factory calls, to prove a config error
// is reported before any node is instantiated.
func configCheckRegistry(t *testing.T, created *atomic.Int32) *node.Registry {
	t.Helper()
	min := float64(100)
	triggerSchema := node.ConfigSchema{Properties: map[string]node.ConfigProperty{
		"interval": {Type: "number", Unit: "milliseconds", Minimum: &min},
	}}
	actionSchema := node.ConfigSchema{Properties: map[string]node.ConfigProperty{
		"level":    {Type: "string", Enum: []string{"info", "warn"}},
		"password": {Type: "string", Sensitive: true},
	}}
	reg := node.NewRegistry()
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "typed-trigger", Category: NodeCategoryInput, Config: &triggerSchema},
		func(cfg map[string]interface{}) (node.Node, error) {
			created.Add(1)
			return newMockTriggerNode(cfg)
		}))
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "typed-action", Category: NodeCategoryOutput, Config: &actionSchema},
		func(cfg map[string]interface{}) (node.Node, error) {
			created.Add(1)
			return newMockActionNode(cfg)
		}))
	return reg
}

// typedFlow returns trigger -> action with the given configs.
func typedFlow(triggerCfg, actionCfg map[string]interface{}) *Flow {
	f := NewFlow("typed")
	trig := NewNode("typed-trigger", NodeCategoryInput)
	trig.Config = triggerCfg
	act := NewNode("typed-action", NodeCategoryOutput)
	act.Config = actionCfg
	f.AddNode(*trig)
	f.AddNode(*act)
	f.AddConnection(*NewConnection(trig.ID, "output", act.ID, "input"))
	return f
}

func TestConfigIssuesCarryNodePaths(t *testing.T) {
	var created atomic.Int32
	reg := configCheckRegistry(t, &created)
	f := typedFlow(
		map[string]interface{}{"interval": "5s"},
		map[string]interface{}{"level": "verbose", "password": float64(1234)},
	)

	issues := f.ConfigIssues(reg)
	require.Len(t, issues, 3)
	assert.Equal(t, "nodes[0].config.interval", issues[0].Path)
	assert.Equal(t, `expected number (milliseconds), got string "5s"`, issues[0].Message)
	assert.Equal(t, "nodes[1].config.level", issues[1].Path)
	assert.Equal(t, "nodes[1].config.password", issues[2].Path)
	assert.NotContains(t, issues[2].Message, "1234", "sensitive values are never echoed")
}

func TestConfigIssuesSkipDisabledAndUnknownNodes(t *testing.T) {
	var created atomic.Int32
	reg := configCheckRegistry(t, &created)
	f := typedFlow(map[string]interface{}{"interval": 500}, map[string]interface{}{"level": 7})
	f.Nodes[1].Enabled = false // never instantiated, so not checked
	unknown := NewNode("no-such-type", NodeCategoryProcessing)
	unknown.Config = map[string]interface{}{"interval": "x"}
	f.AddNode(*unknown)

	assert.Empty(t, f.ConfigIssues(reg))
	assert.Empty(t, f.ConfigIssues(nil))
}

func TestNewFlowRuntimeRejectsMistypedConfig(t *testing.T) {
	var created atomic.Int32
	reg := configCheckRegistry(t, &created)
	f := typedFlow(map[string]interface{}{"interval": "5s"}, map[string]interface{}{"level": "warn"})

	_, err := NewFlowRuntime(f, reg)
	require.Error(t, err)
	var verr *ValidationError
	require.True(t, errors.As(err, &verr), "config errors are flow validation errors (HTTP 400): %T", err)
	assert.Contains(t, err.Error(), `nodes[0].config.interval: expected number (milliseconds), got string "5s"`)
	assert.Zero(t, created.Load(), "no node may be instantiated from an invalid config")
}

func TestDeployReportsEveryConfigIssue(t *testing.T) {
	var created atomic.Int32
	reg := configCheckRegistry(t, &created)
	m := NewRuntimeManager(reg, zerolog.Nop())
	t.Cleanup(m.StopAll)
	f := typedFlow(map[string]interface{}{"interval": 50}, map[string]interface{}{"level": "loud"})

	err := m.Deploy(context.Background(), f)
	require.Error(t, err)
	var verr *ValidationError
	require.True(t, errors.As(err, &verr))
	assert.Contains(t, err.Error(), "nodes[0].config.interval: expected number >= 100 (milliseconds), got 50")
	assert.Contains(t, err.Error(), `nodes[1].config.level: expected one of "info", "warn", got "loud"`)
	_, running := m.GetRuntime(f.ID)
	assert.False(t, running)
}

func TestDeployAcceptsWellTypedConfig(t *testing.T) {
	var created atomic.Int32
	reg := configCheckRegistry(t, &created)
	m := NewRuntimeManager(reg, zerolog.Nop())
	t.Cleanup(m.StopAll)
	f := typedFlow(map[string]interface{}{"interval": float64(1000)}, map[string]interface{}{"level": "info", "password": "{{secret.pw}}"})

	require.NoError(t, m.Deploy(context.Background(), f))
	assert.Equal(t, int32(2), created.Load())
}
