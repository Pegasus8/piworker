package builtin

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The documented example: a duration written as text instead of a number of
// milliseconds fails the deploy with the value's path and unit.
func TestDeployRejectsMistypedBuiltinConfig(t *testing.T) {
	f := flow.NewFlow("typed")
	trig := flow.NewNode("trigger-interval", flow.NodeCategoryInput)
	trig.Config = map[string]interface{}{"interval": "5s"}
	f.AddNode(*trig)

	_, err := flow.NewFlowRuntime(f, node.DefaultRegistry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `nodes[0].config.interval: expected number (milliseconds), got string "5s"`)
}

func TestDeployAcceptsEditorDefaults(t *testing.T) {
	// A node created by the editor carries the schema defaults.
	info, ok := node.DefaultRegistry.Get("trigger-interval")
	require.True(t, ok)
	cfg := map[string]interface{}{}
	for k, p := range info.Config.Properties {
		if p.Default != nil {
			cfg[k] = p.Default
		}
	}
	f := flow.NewFlow("defaults")
	trig := flow.NewNode("trigger-interval", flow.NodeCategoryInput)
	trig.Config = cfg
	f.AddNode(*trig)

	_, err := flow.NewFlowRuntime(f, node.DefaultRegistry)
	require.NoError(t, err)
}
