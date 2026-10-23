package builtin

import (
	"context"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManualTrigger(t *testing.T) {
	t.Run("creates with default config", func(t *testing.T) {
		node, err := NewManualTrigger(map[string]interface{}{})
		require.NoError(t, err)
		require.NotNil(t, node)

		trigger := node.(*ManualTrigger)
		assert.Equal(t, "{}", trigger.payload)
		assert.Equal(t, "json", trigger.payloadType)
		assert.Equal(t, "manual", trigger.topic)
	})

	t.Run("creates with custom JSON payload", func(t *testing.T) {
		node, err := NewManualTrigger(map[string]interface{}{
			"payload":     `{"key": "value"}`,
			"payloadType": "json",
			"topic":       "custom",
		})
		require.NoError(t, err)

		trigger := node.(*ManualTrigger)
		assert.Equal(t, `{"key": "value"}`, trigger.payload)
		assert.Equal(t, "json", trigger.payloadType)
		assert.Equal(t, "custom", trigger.topic)
	})

	t.Run("creates with text payload", func(t *testing.T) {
		node, err := NewManualTrigger(map[string]interface{}{
			"payload":     "hello world",
			"payloadType": "text",
		})
		require.NoError(t, err)

		trigger := node.(*ManualTrigger)
		assert.Equal(t, "hello world", trigger.payload)
		assert.Equal(t, "text", trigger.payloadType)
	})

	t.Run("fails with invalid JSON payload", func(t *testing.T) {
		_, err := NewManualTrigger(map[string]interface{}{
			"payload":     "not valid json",
			"payloadType": "json",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid JSON")
	})
}

func TestManualTriggerPorts(t *testing.T) {
	node, err := NewManualTrigger(map[string]interface{}{})
	require.NoError(t, err)

	inputs, outputs := node.Ports()

	assert.Nil(t, inputs)
	assert.Len(t, outputs, 1)
	assert.Equal(t, "output", outputs[0].ID)
}

func TestManualTriggerStartStop(t *testing.T) {
	node, err := NewManualTrigger(map[string]interface{}{})
	require.NoError(t, err)

	trigger := node.(*ManualTrigger)
	out := make(chan *types.Message, 10)
	ctx := context.Background()

	// Start
	err = trigger.Start(ctx, out)
	require.NoError(t, err)
	assert.True(t, trigger.IsRunning())

	// Can't start again
	err = trigger.Start(ctx, out)
	require.Error(t, err)

	// Stop
	err = trigger.Stop()
	require.NoError(t, err)
	assert.False(t, trigger.IsRunning())

	// Stop again is idempotent
	err = trigger.Stop()
	require.NoError(t, err)
}

func TestManualTriggerInject(t *testing.T) {
	t.Run("inject with default JSON payload", func(t *testing.T) {
		node, err := NewManualTrigger(map[string]interface{}{
			"payload":     `{"test": "data"}`,
			"payloadType": "json",
			"topic":       "test-topic",
		})
		require.NoError(t, err)

		trigger := node.(*ManualTrigger)
		out := make(chan *types.Message, 10)
		ctx := context.Background()

		err = trigger.Start(ctx, out)
		require.NoError(t, err)
		defer trigger.Stop()

		// Inject
		err = trigger.Inject(nil)
		require.NoError(t, err)

		// Should receive message
		select {
		case msg := <-out:
			assert.Equal(t, "test-topic", msg.Topic)
			assert.Equal(t, "output", msg.SourcePort)

			payload := msg.Payload.(map[string]interface{})
			assert.NotNil(t, payload["data"])
			assert.Equal(t, uint64(1), payload["count"])
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for message")
		}
	})

	t.Run("inject with custom payload", func(t *testing.T) {
		node, err := NewManualTrigger(map[string]interface{}{})
		require.NoError(t, err)

		trigger := node.(*ManualTrigger)
		out := make(chan *types.Message, 10)
		ctx := context.Background()

		err = trigger.Start(ctx, out)
		require.NoError(t, err)
		defer trigger.Stop()

		// Inject with custom payload
		customPayload := map[string]interface{}{"custom": "data"}
		err = trigger.Inject(customPayload)
		require.NoError(t, err)

		select {
		case msg := <-out:
			payload := msg.Payload.(map[string]interface{})
			data := payload["data"].(map[string]interface{})
			assert.Equal(t, "data", data["custom"])
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for message")
		}
	})

	t.Run("inject with text payload", func(t *testing.T) {
		node, err := NewManualTrigger(map[string]interface{}{
			"payload":     "hello",
			"payloadType": "text",
		})
		require.NoError(t, err)

		trigger := node.(*ManualTrigger)
		out := make(chan *types.Message, 10)
		ctx := context.Background()

		err = trigger.Start(ctx, out)
		require.NoError(t, err)
		defer trigger.Stop()

		err = trigger.Inject(nil)
		require.NoError(t, err)

		select {
		case msg := <-out:
			payload := msg.Payload.(map[string]interface{})
			assert.Equal(t, "hello", payload["data"])
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for message")
		}
	})

	t.Run("inject fails when not running", func(t *testing.T) {
		node, err := NewManualTrigger(map[string]interface{}{})
		require.NoError(t, err)

		trigger := node.(*ManualTrigger)

		err = trigger.Inject(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not running")
	})

	t.Run("inject increments count", func(t *testing.T) {
		node, err := NewManualTrigger(map[string]interface{}{})
		require.NoError(t, err)

		trigger := node.(*ManualTrigger)
		out := make(chan *types.Message, 10)
		ctx := context.Background()

		err = trigger.Start(ctx, out)
		require.NoError(t, err)
		defer trigger.Stop()

		// Inject 3 times
		for i := 1; i <= 3; i++ {
			err = trigger.Inject(nil)
			require.NoError(t, err)

			select {
			case msg := <-out:
				payload := msg.Payload.(map[string]interface{})
				assert.Equal(t, uint64(i), payload["count"])
			case <-time.After(time.Second):
				t.Fatalf("timeout waiting for message %d", i)
			}
		}
	})
}

func TestManualTriggerConfigSchema(t *testing.T) {
	node, err := NewManualTrigger(map[string]interface{}{})
	require.NoError(t, err)

	trigger := node.(*ManualTrigger)
	schema := trigger.GetConfigSchema()

	assert.Contains(t, schema.Properties, "payload")
	assert.Contains(t, schema.Properties, "payloadType")
	assert.Contains(t, schema.Properties, "topic")
}
