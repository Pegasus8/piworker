package builtin

import (
	"context"
	"testing"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDebugProcess(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		node, err := NewDebugProcess(map[string]interface{}{})
		require.NoError(t, err)
		require.NotNil(t, node)

		debug := node.(*DebugProcess)
		assert.Equal(t, "Debug", debug.name)
		assert.True(t, debug.logPayload)
		assert.True(t, debug.logMeta)
		assert.True(t, debug.passThrough)
	})

	t.Run("creates with custom config", func(t *testing.T) {
		node, err := NewDebugProcess(map[string]interface{}{
			"name":        "MyDebug",
			"logPayload":  false,
			"logMeta":     false,
			"passThrough": false,
		})
		require.NoError(t, err)

		debug := node.(*DebugProcess)
		assert.Equal(t, "MyDebug", debug.name)
		assert.False(t, debug.logPayload)
		assert.False(t, debug.logMeta)
		assert.False(t, debug.passThrough)
	})
}

func TestDebugProcessPorts(t *testing.T) {
	node, err := NewDebugProcess(map[string]interface{}{})
	require.NoError(t, err)

	inputs, outputs := node.Ports()

	assert.Len(t, inputs, 1)
	assert.Equal(t, "input", inputs[0].ID)
	assert.Len(t, outputs, 1)
	assert.Equal(t, "output", outputs[0].ID)
}

func TestDebugProcessProcess(t *testing.T) {
	ctx := context.Background()

	t.Run("passes through message when enabled", func(t *testing.T) {
		node, err := NewDebugProcess(map[string]interface{}{
			"name":        "TestDebug",
			"passThrough": true,
		})
		require.NoError(t, err)

		debug := node.(*DebugProcess)
		msg := types.NewMessage(map[string]interface{}{"test": "data"}, types.DataTypeObject)
		msg.Topic = "test-topic"

		outputs, err := debug.Process(ctx, msg)
		require.NoError(t, err)
		require.Len(t, outputs, 1)

		output := outputs[0]
		assert.Equal(t, "output", output.SourcePort)

		debugged, ok := output.GetMeta("debugged")
		assert.True(t, ok)
		assert.Equal(t, true, debugged)

		debugName, ok := output.GetMeta("debugName")
		assert.True(t, ok)
		assert.Equal(t, "TestDebug", debugName)
	})

	t.Run("terminates message when passThrough disabled", func(t *testing.T) {
		node, err := NewDebugProcess(map[string]interface{}{
			"passThrough": false,
		})
		require.NoError(t, err)

		debug := node.(*DebugProcess)
		msg := types.NewMessage("test", types.DataTypeString)

		outputs, err := debug.Process(ctx, msg)
		require.NoError(t, err)
		assert.Nil(t, outputs)
	})

	t.Run("handles complex payload", func(t *testing.T) {
		node, err := NewDebugProcess(map[string]interface{}{
			"logPayload": true,
			"logMeta":    true,
		})
		require.NoError(t, err)

		debug := node.(*DebugProcess)
		msg := types.NewMessage(map[string]interface{}{
			"nested": map[string]interface{}{
				"key": "value",
			},
			"array": []interface{}{1, 2, 3},
		}, types.DataTypeObject)
		msg.SetMeta("customMeta", "metaValue")

		outputs, err := debug.Process(ctx, msg)
		require.NoError(t, err)
		require.Len(t, outputs, 1)
	})
}

func TestDebugProcessConfigSchema(t *testing.T) {
	node, err := NewDebugProcess(map[string]interface{}{})
	require.NoError(t, err)

	debug := node.(*DebugProcess)
	schema := debug.GetConfigSchema()

	assert.Contains(t, schema.Properties, "name")
	assert.Contains(t, schema.Properties, "logPayload")
	assert.Contains(t, schema.Properties, "logMeta")
	assert.Contains(t, schema.Properties, "passThrough")
}

func TestDebugProcessValidate(t *testing.T) {
	node, err := NewDebugProcess(map[string]interface{}{})
	require.NoError(t, err)

	debug := node.(*DebugProcess)
	assert.NoError(t, debug.Validate())
}
