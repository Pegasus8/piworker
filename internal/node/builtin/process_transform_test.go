package builtin

import (
	"context"
	"testing"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTransformProcess(t *testing.T) {
	t.Run("creates with valid expression", func(t *testing.T) {
		node, err := NewTransformProcess(map[string]interface{}{
			"expression": "payload.value * 2",
		})
		require.NoError(t, err)
		require.NotNil(t, node)

		transform := node.(*TransformProcess)
		assert.Equal(t, "payload.value * 2", transform.expression)
		assert.NotNil(t, transform.program)
	})

	t.Run("uses default expression", func(t *testing.T) {
		node, err := NewTransformProcess(map[string]interface{}{})
		require.NoError(t, err)

		transform := node.(*TransformProcess)
		assert.Equal(t, "payload", transform.expression)
	})

	t.Run("fails with invalid expression", func(t *testing.T) {
		_, err := NewTransformProcess(map[string]interface{}{
			"expression": "payload.value +", // Invalid syntax
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid expression")
	})
}

func TestTransformProcessProcess(t *testing.T) {
	ctx := context.Background()

	t.Run("passes through payload unchanged", func(t *testing.T) {
		node, err := NewTransformProcess(map[string]interface{}{
			"expression": "payload",
		})
		require.NoError(t, err)

		transform := node.(*TransformProcess)
		msg := types.NewMessage(map[string]interface{}{
			"key": "value",
		}, types.DataTypeObject)

		outputs, err := transform.Process(ctx, msg)
		require.NoError(t, err)
		require.Len(t, outputs, 1)

		payload := outputs[0].Payload.(map[string]interface{})
		assert.Equal(t, "value", payload["key"])
	})

	t.Run("accesses nested fields", func(t *testing.T) {
		node, err := NewTransformProcess(map[string]interface{}{
			"expression": "payload.user.name",
		})
		require.NoError(t, err)

		transform := node.(*TransformProcess)
		msg := types.NewMessage(map[string]interface{}{
			"user": map[string]interface{}{
				"name": "John",
			},
		}, types.DataTypeObject)

		outputs, err := transform.Process(ctx, msg)
		require.NoError(t, err)
		require.Len(t, outputs, 1)

		assert.Equal(t, "John", outputs[0].Payload)
		assert.Equal(t, types.DataTypeString, outputs[0].PayloadType)
	})

	t.Run("performs arithmetic operations", func(t *testing.T) {
		node, err := NewTransformProcess(map[string]interface{}{
			"expression": "payload.price * 1.21",
		})
		require.NoError(t, err)

		transform := node.(*TransformProcess)
		msg := types.NewMessage(map[string]interface{}{
			"price": 100.0,
		}, types.DataTypeObject)

		outputs, err := transform.Process(ctx, msg)
		require.NoError(t, err)
		require.Len(t, outputs, 1)

		assert.Equal(t, 121.0, outputs[0].Payload)
		assert.Equal(t, types.DataTypeNumber, outputs[0].PayloadType)
	})

	t.Run("uses string concatenation", func(t *testing.T) {
		node, err := NewTransformProcess(map[string]interface{}{
			"expression": `payload.firstName + " " + payload.lastName`,
		})
		require.NoError(t, err)

		transform := node.(*TransformProcess)
		msg := types.NewMessage(map[string]interface{}{
			"firstName": "John",
			"lastName":  "Doe",
		}, types.DataTypeObject)

		outputs, err := transform.Process(ctx, msg)
		require.NoError(t, err)
		require.Len(t, outputs, 1)

		assert.Equal(t, "John Doe", outputs[0].Payload)
	})

	t.Run("uses conditional expressions", func(t *testing.T) {
		node, err := NewTransformProcess(map[string]interface{}{
			"expression": `payload.age >= 18 ? "adult" : "minor"`,
		})
		require.NoError(t, err)

		transform := node.(*TransformProcess)

		// Adult case
		msg := types.NewMessage(map[string]interface{}{
			"age": 25,
		}, types.DataTypeObject)
		outputs, err := transform.Process(ctx, msg)
		require.NoError(t, err)
		assert.Equal(t, "adult", outputs[0].Payload)

		// Minor case
		msg = types.NewMessage(map[string]interface{}{
			"age": 15,
		}, types.DataTypeObject)
		outputs, err = transform.Process(ctx, msg)
		require.NoError(t, err)
		assert.Equal(t, "minor", outputs[0].Payload)
	})

	t.Run("uses builtin string methods", func(t *testing.T) {
		node, err := NewTransformProcess(map[string]interface{}{
			"expression": "upper(payload.name)",
		})
		require.NoError(t, err)

		transform := node.(*TransformProcess)
		msg := types.NewMessage(map[string]interface{}{
			"name": "john",
		}, types.DataTypeObject)

		outputs, err := transform.Process(ctx, msg)
		require.NoError(t, err)
		assert.Equal(t, "JOHN", outputs[0].Payload)
	})

	t.Run("uses len on arrays", func(t *testing.T) {
		node, err := NewTransformProcess(map[string]interface{}{
			"expression": "len(payload.items)",
		})
		require.NoError(t, err)

		transform := node.(*TransformProcess)
		msg := types.NewMessage(map[string]interface{}{
			"items": []interface{}{"a", "b", "c"},
		}, types.DataTypeObject)

		outputs, err := transform.Process(ctx, msg)
		require.NoError(t, err)
		assert.Equal(t, 3, outputs[0].Payload)
	})

	t.Run("accesses message metadata", func(t *testing.T) {
		node, err := NewTransformProcess(map[string]interface{}{
			"expression": "topic",
		})
		require.NoError(t, err)

		transform := node.(*TransformProcess)
		msg := types.NewMessage("data", types.DataTypeString)
		msg.Topic = "my-topic"

		outputs, err := transform.Process(ctx, msg)
		require.NoError(t, err)
		assert.Equal(t, "my-topic", outputs[0].Payload)
	})

	t.Run("creates new object", func(t *testing.T) {
		node, err := NewTransformProcess(map[string]interface{}{
			"expression": `{"name": payload.user, "doubled": payload.value * 2}`,
		})
		require.NoError(t, err)

		transform := node.(*TransformProcess)
		msg := types.NewMessage(map[string]interface{}{
			"user":  "John",
			"value": 10,
		}, types.DataTypeObject)

		outputs, err := transform.Process(ctx, msg)
		require.NoError(t, err)
		require.Len(t, outputs, 1)

		payload := outputs[0].Payload.(map[string]interface{})
		assert.Equal(t, "John", payload["name"])
		assert.Equal(t, 20, payload["doubled"])
	})

	t.Run("handles boolean results", func(t *testing.T) {
		node, err := NewTransformProcess(map[string]interface{}{
			"expression": "payload.active == true",
		})
		require.NoError(t, err)

		transform := node.(*TransformProcess)
		msg := types.NewMessage(map[string]interface{}{
			"active": true,
		}, types.DataTypeObject)

		outputs, err := transform.Process(ctx, msg)
		require.NoError(t, err)
		assert.Equal(t, true, outputs[0].Payload)
		assert.Equal(t, types.DataTypeBoolean, outputs[0].PayloadType)
	})

	t.Run("sets metadata on output", func(t *testing.T) {
		node, err := NewTransformProcess(map[string]interface{}{
			"expression": "payload",
		})
		require.NoError(t, err)

		transform := node.(*TransformProcess)
		msg := types.NewMessage("test", types.DataTypeString)

		outputs, err := transform.Process(ctx, msg)
		require.NoError(t, err)
		require.Len(t, outputs, 1)

		transformed, ok := outputs[0].GetMeta("transformed")
		assert.True(t, ok)
		assert.Equal(t, true, transformed)

		expr, ok := outputs[0].GetMeta("expression")
		assert.True(t, ok)
		assert.Equal(t, "payload", expr)
	})
}

func TestTransformProcessPorts(t *testing.T) {
	node, err := NewTransformProcess(map[string]interface{}{})
	require.NoError(t, err)

	inputs, outputs := node.Ports()

	assert.Len(t, inputs, 1)
	assert.Equal(t, "input", inputs[0].ID)
	assert.Len(t, outputs, 1)
	assert.Equal(t, "output", outputs[0].ID)
}

func TestTransformProcessValidate(t *testing.T) {
	t.Run("valid transform passes", func(t *testing.T) {
		node, err := NewTransformProcess(map[string]interface{}{
			"expression": "payload.value",
		})
		require.NoError(t, err)

		transform := node.(*TransformProcess)
		assert.NoError(t, transform.Validate())
	})
}

func TestTransformProcessConfigSchema(t *testing.T) {
	node, err := NewTransformProcess(map[string]interface{}{})
	require.NoError(t, err)

	transform := node.(*TransformProcess)
	schema := transform.GetConfigSchema()

	assert.Contains(t, schema.Properties, "expression")
	assert.Contains(t, schema.Required, "expression")
}
