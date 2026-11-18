package builtin

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// IntervalTrigger Tests
// ============================================================================

func TestNewIntervalTrigger(t *testing.T) {
	t.Run("creates trigger with default interval", func(t *testing.T) {
		trigger, err := NewIntervalTrigger(nil)

		assert.NoError(t, err)
		assert.NotNil(t, trigger)
	})

	t.Run("creates trigger with custom interval", func(t *testing.T) {
		config := map[string]interface{}{
			"interval": 5000,
		}

		trigger, err := NewIntervalTrigger(config)

		assert.NoError(t, err)
		assert.NotNil(t, trigger)
	})

	t.Run("enforces minimum interval", func(t *testing.T) {
		config := map[string]interface{}{
			"interval": 50, // Below minimum
		}

		trigger, err := NewIntervalTrigger(config)

		assert.NoError(t, err)
		assert.NotNil(t, trigger)

		// The trigger should have enforced minimum
		intervalTrigger := trigger.(*IntervalTrigger)
		assert.GreaterOrEqual(t, intervalTrigger.interval, 100*time.Millisecond)
	})
}

func TestIntervalTriggerStartStop(t *testing.T) {
	t.Run("starts and fires messages", func(t *testing.T) {
		config := map[string]interface{}{
			"interval": 100, // 100ms for fast testing
		}

		trigger, err := NewIntervalTrigger(config)
		require.NoError(t, err)

		intervalTrigger := trigger.(*IntervalTrigger)
		out := make(chan *flow.Message, 10)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		err = intervalTrigger.Start(ctx, out)
		require.NoError(t, err)

		// Wait for at least one message
		select {
		case msg := <-out:
			assert.NotNil(t, msg)
			assert.NotEmpty(t, msg.ID)
			assert.Equal(t, "output", msg.SourcePort)

			// Check payload
			payload, ok := msg.Payload.(map[string]interface{})
			require.True(t, ok)
			assert.Contains(t, payload, "timestamp")
			assert.Contains(t, payload, "count")
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timeout waiting for message")
		}

		err = intervalTrigger.Stop()
		assert.NoError(t, err)
	})

	t.Run("stops cleanly", func(t *testing.T) {
		config := map[string]interface{}{
			"interval": 100,
		}

		trigger, err := NewIntervalTrigger(config)
		require.NoError(t, err)

		intervalTrigger := trigger.(*IntervalTrigger)
		out := make(chan *flow.Message, 10)
		ctx := context.Background()

		err = intervalTrigger.Start(ctx, out)
		require.NoError(t, err)

		time.Sleep(150 * time.Millisecond) // Let it fire once

		err = intervalTrigger.Stop()
		assert.NoError(t, err)

		// Drain channel
		drainChannel(out)

		// Should not receive more messages after stop
		time.Sleep(200 * time.Millisecond)
		select {
		case <-out:
			t.Fatal("received message after stop")
		default:
			// Expected
		}
	})

	t.Run("cannot start twice", func(t *testing.T) {
		config := map[string]interface{}{
			"interval": 1000,
		}

		trigger, err := NewIntervalTrigger(config)
		require.NoError(t, err)

		intervalTrigger := trigger.(*IntervalTrigger)
		out := make(chan *flow.Message, 10)
		ctx := context.Background()

		err = intervalTrigger.Start(ctx, out)
		require.NoError(t, err)
		defer intervalTrigger.Stop()

		err = intervalTrigger.Start(ctx, out)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already running")
	})

	t.Run("stop on non-running is safe", func(t *testing.T) {
		config := map[string]interface{}{
			"interval": 1000,
		}

		trigger, err := NewIntervalTrigger(config)
		require.NoError(t, err)

		intervalTrigger := trigger.(*IntervalTrigger)

		err = intervalTrigger.Stop()

		assert.NoError(t, err)
	})

	t.Run("stops on context cancellation", func(t *testing.T) {
		config := map[string]interface{}{
			"interval": 100,
		}

		trigger, err := NewIntervalTrigger(config)
		require.NoError(t, err)

		intervalTrigger := trigger.(*IntervalTrigger)
		out := make(chan *flow.Message, 10)
		ctx, cancel := context.WithCancel(context.Background())

		err = intervalTrigger.Start(ctx, out)
		require.NoError(t, err)

		// Let it fire once
		<-out

		cancel()
		time.Sleep(50 * time.Millisecond)

		// The goroutine should have exited
	})
}

func TestIntervalTriggerValidate(t *testing.T) {
	t.Run("valid configuration passes", func(t *testing.T) {
		config := map[string]interface{}{
			"interval": 1000,
		}

		trigger, err := NewIntervalTrigger(config)
		require.NoError(t, err)

		err = trigger.Validate()

		assert.NoError(t, err)
	})
}

func TestIntervalTriggerPorts(t *testing.T) {
	trigger, err := NewIntervalTrigger(nil)
	require.NoError(t, err)

	inputs, outputs := trigger.Ports()

	assert.Empty(t, inputs)
	assert.Len(t, outputs, 1)
	assert.Equal(t, "output", outputs[0].ID)
}

func TestIntervalTriggerConfigSchema(t *testing.T) {
	trigger, err := NewIntervalTrigger(nil)
	require.NoError(t, err)

	intervalTrigger := trigger.(*IntervalTrigger)
	schema := intervalTrigger.GetConfigSchema()

	assert.Contains(t, schema.Properties, "interval")
	assert.Contains(t, schema.Required, "interval")
}

// ============================================================================
// LogAction Tests
// ============================================================================

func TestNewLogAction(t *testing.T) {
	t.Run("creates action with default config", func(t *testing.T) {
		action, err := NewLogAction(nil)

		assert.NoError(t, err)
		assert.NotNil(t, action)
	})

	t.Run("creates action with custom config", func(t *testing.T) {
		config := map[string]interface{}{
			"level":   "debug",
			"message": "Custom message",
		}

		action, err := NewLogAction(config)

		assert.NoError(t, err)
		assert.NotNil(t, action)
	})
}

func TestLogActionProcess(t *testing.T) {
	t.Run("processes message and passes through", func(t *testing.T) {
		action, err := NewLogAction(nil)
		require.NoError(t, err)

		msg := flow.NewMessage("test payload", flow.DataTypeString)
		ctx := context.Background()

		outputs, err := action.Process(ctx, msg)

		assert.NoError(t, err)
		assert.Len(t, outputs, 1)
		assert.Equal(t, msg.Payload, outputs[0].Payload)
	})

	t.Run("handles different payload types", func(t *testing.T) {
		action, err := NewLogAction(nil)
		require.NoError(t, err)

		testCases := []struct {
			name    string
			payload interface{}
		}{
			{"string", "test string"},
			{"number", 42},
			{"boolean", true},
			{"map", map[string]interface{}{"key": "value"}},
			{"slice", []interface{}{1, 2, 3}},
			{"nil", nil},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				msg := flow.NewMessage(tc.payload, flow.DataTypeAny)
				ctx := context.Background()

				outputs, err := action.Process(ctx, msg)

				assert.NoError(t, err)
				assert.Len(t, outputs, 1)
			})
		}
	})
}

func TestLogActionValidate(t *testing.T) {
	t.Run("valid level passes", func(t *testing.T) {
		validLevels := []string{"debug", "info", "warn", "error"}

		for _, level := range validLevels {
			t.Run(level, func(t *testing.T) {
				config := map[string]interface{}{"level": level}
				action, err := NewLogAction(config)
				require.NoError(t, err)

				err = action.Validate()

				assert.NoError(t, err)
			})
		}
	})

	t.Run("invalid level fails", func(t *testing.T) {
		config := map[string]interface{}{"level": "invalid"}
		action, err := NewLogAction(config)
		require.NoError(t, err)

		err = action.Validate()

		assert.Error(t, err)
	})
}

func TestLogActionPorts(t *testing.T) {
	action, err := NewLogAction(nil)
	require.NoError(t, err)

	inputs, outputs := action.Ports()

	assert.Len(t, inputs, 1)
	assert.Equal(t, "input", inputs[0].ID)
	assert.Len(t, outputs, 1)
	assert.Equal(t, "output", outputs[0].ID)
}

// ============================================================================
// DelayProcess Tests
// ============================================================================

func TestNewDelayProcess(t *testing.T) {
	t.Run("creates delay with default config", func(t *testing.T) {
		delay, err := NewDelayProcess(nil)

		assert.NoError(t, err)
		assert.NotNil(t, delay)
	})

	t.Run("creates delay with custom config", func(t *testing.T) {
		config := map[string]interface{}{
			"delay": 500,
		}

		delay, err := NewDelayProcess(config)

		assert.NoError(t, err)
		assert.NotNil(t, delay)
	})

	t.Run("enforces minimum delay", func(t *testing.T) {
		config := map[string]interface{}{
			"delay": -100, // Negative
		}

		delay, err := NewDelayProcess(config)

		assert.NoError(t, err)
		delayProc := delay.(*DelayProcess)
		assert.GreaterOrEqual(t, delayProc.delay, time.Duration(0))
	})

	t.Run("enforces maximum delay", func(t *testing.T) {
		config := map[string]interface{}{
			"delay": 1000000, // Very large
		}

		delay, err := NewDelayProcess(config)

		assert.NoError(t, err)
		delayProc := delay.(*DelayProcess)
		assert.LessOrEqual(t, delayProc.delay, 5*time.Minute)
	})
}

func TestDelayProcessProcess(t *testing.T) {
	t.Run("delays message by configured time", func(t *testing.T) {
		config := map[string]interface{}{
			"delay": 100, // 100ms
		}

		delay, err := NewDelayProcess(config)
		require.NoError(t, err)

		msg := flow.NewMessage("test", flow.DataTypeString)
		ctx := context.Background()

		start := time.Now()
		outputs, err := delay.Process(ctx, msg)
		elapsed := time.Since(start)

		assert.NoError(t, err)
		assert.Len(t, outputs, 1)
		assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond)
		assert.Less(t, elapsed, 200*time.Millisecond)
	})

	t.Run("zero delay passes through immediately", func(t *testing.T) {
		config := map[string]interface{}{
			"delay": 0,
		}

		delay, err := NewDelayProcess(config)
		require.NoError(t, err)

		msg := flow.NewMessage("test", flow.DataTypeString)
		ctx := context.Background()

		start := time.Now()
		outputs, err := delay.Process(ctx, msg)
		elapsed := time.Since(start)

		assert.NoError(t, err)
		assert.Len(t, outputs, 1)
		assert.Less(t, elapsed, 50*time.Millisecond)
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		config := map[string]interface{}{
			"delay": 5000, // 5 seconds
		}

		delay, err := NewDelayProcess(config)
		require.NoError(t, err)

		msg := flow.NewMessage("test", flow.DataTypeString)
		ctx, cancel := context.WithCancel(context.Background())

		// Cancel after short delay
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		start := time.Now()
		_, err = delay.Process(ctx, msg)
		elapsed := time.Since(start)

		assert.Error(t, err)
		assert.Less(t, elapsed, 500*time.Millisecond)
	})

	t.Run("adds delay metadata", func(t *testing.T) {
		config := map[string]interface{}{
			"delay": 50,
		}

		delay, err := NewDelayProcess(config)
		require.NoError(t, err)

		msg := flow.NewMessage("test", flow.DataTypeString)
		ctx := context.Background()

		outputs, err := delay.Process(ctx, msg)

		assert.NoError(t, err)
		require.Len(t, outputs, 1)

		delayed, ok := outputs[0].GetMeta("delayed")
		assert.True(t, ok)
		assert.Equal(t, true, delayed)
	})
}

func TestDelayProcessValidate(t *testing.T) {
	t.Run("valid delay passes", func(t *testing.T) {
		config := map[string]interface{}{
			"delay": 1000,
		}

		delay, err := NewDelayProcess(config)
		require.NoError(t, err)

		err = delay.Validate()

		assert.NoError(t, err)
	})
}

func TestDelayProcessPorts(t *testing.T) {
	delay, err := NewDelayProcess(nil)
	require.NoError(t, err)

	inputs, outputs := delay.Ports()

	assert.Len(t, inputs, 1)
	assert.Equal(t, "input", inputs[0].ID)
	assert.Len(t, outputs, 1)
	assert.Equal(t, "output", outputs[0].ID)
}

// ============================================================================
// CommandAction Tests
// ============================================================================

func TestNewCommandAction(t *testing.T) {
	t.Run("creates action with default config", func(t *testing.T) {
		action, err := NewCommandAction(nil)

		assert.NoError(t, err)
		assert.NotNil(t, action)
	})

	t.Run("creates action with custom config", func(t *testing.T) {
		config := map[string]interface{}{
			"command": "echo test",
			"shell":   "/bin/bash",
			"timeout": 10,
		}

		action, err := NewCommandAction(config)

		assert.NoError(t, err)
		assert.NotNil(t, action)
	})

	t.Run("enforces timeout limits", func(t *testing.T) {
		config := map[string]interface{}{
			"command": "echo test",
			"timeout": 10000, // Above max
		}

		action, err := NewCommandAction(config)

		assert.NoError(t, err)
		cmdAction := action.(*CommandAction)
		assert.LessOrEqual(t, cmdAction.timeout, time.Hour)
	})
}

func TestCommandActionProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	t.Run("executes simple command", func(t *testing.T) {
		config := map[string]interface{}{
			"command": "echo hello",
		}

		action, err := NewCommandAction(config)
		require.NoError(t, err)

		msg := flow.NewMessage(nil, flow.DataTypeAny)
		ctx := context.Background()

		outputs, err := action.Process(ctx, msg)

		assert.NoError(t, err)
		require.Len(t, outputs, 1)

		payload, ok := outputs[0].Payload.(map[string]interface{})
		require.True(t, ok)

		assert.Equal(t, true, payload["success"])
		assert.Equal(t, "hello", payload["stdout"])
		assert.Equal(t, 0, payload["exitCode"])
	})

	t.Run("captures stderr", func(t *testing.T) {
		config := map[string]interface{}{
			"command": "echo error >&2",
		}

		action, err := NewCommandAction(config)
		require.NoError(t, err)

		msg := flow.NewMessage(nil, flow.DataTypeAny)
		ctx := context.Background()

		outputs, err := action.Process(ctx, msg)

		assert.NoError(t, err)
		require.Len(t, outputs, 1)

		payload := outputs[0].Payload.(map[string]interface{})
		assert.Equal(t, "error", payload["stderr"])
	})

	t.Run("handles command failure", func(t *testing.T) {
		config := map[string]interface{}{
			"command": "exit 1",
		}

		action, err := NewCommandAction(config)
		require.NoError(t, err)

		msg := flow.NewMessage(nil, flow.DataTypeAny)
		ctx := context.Background()

		outputs, err := action.Process(ctx, msg)

		assert.NoError(t, err) // Process doesn't return error, it's in payload
		require.Len(t, outputs, 1)

		payload := outputs[0].Payload.(map[string]interface{})
		assert.Equal(t, false, payload["success"])
		assert.Equal(t, 1, payload["exitCode"])
	})

	t.Run("expands payload variable", func(t *testing.T) {
		config := map[string]interface{}{
			"command": "echo {{payload}}",
		}

		action, err := NewCommandAction(config)
		require.NoError(t, err)

		msg := flow.NewMessage("test-value", flow.DataTypeString)
		ctx := context.Background()

		outputs, err := action.Process(ctx, msg)

		assert.NoError(t, err)
		require.Len(t, outputs, 1)

		payload := outputs[0].Payload.(map[string]interface{})
		assert.Equal(t, "test-value", payload["stdout"])
	})

	t.Run("fails on empty command", func(t *testing.T) {
		config := map[string]interface{}{
			"command": "",
		}

		action, err := NewCommandAction(config)
		require.NoError(t, err)

		msg := flow.NewMessage(nil, flow.DataTypeAny)
		ctx := context.Background()

		_, err = action.Process(ctx, msg)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no command specified")
	})

	t.Run("respects timeout", func(t *testing.T) {
		config := map[string]interface{}{
			"command": "sleep 10",
			"timeout": 1, // 1 second
		}

		action, err := NewCommandAction(config)
		require.NoError(t, err)

		msg := flow.NewMessage(nil, flow.DataTypeAny)
		ctx := context.Background()

		start := time.Now()
		outputs, err := action.Process(ctx, msg)
		elapsed := time.Since(start)

		assert.NoError(t, err)
		require.Len(t, outputs, 1)

		payload := outputs[0].Payload.(map[string]interface{})
		assert.Equal(t, false, payload["success"])
		assert.Contains(t, payload["error"], "timed out")
		assert.Less(t, elapsed, 5*time.Second)
	})
}

func TestCommandActionValidate(t *testing.T) {
	t.Run("valid command passes", func(t *testing.T) {
		config := map[string]interface{}{
			"command": "echo test",
		}

		action, err := NewCommandAction(config)
		require.NoError(t, err)

		err = action.Validate()

		assert.NoError(t, err)
	})

	t.Run("empty command fails", func(t *testing.T) {
		config := map[string]interface{}{
			"command": "",
		}

		action, err := NewCommandAction(config)
		require.NoError(t, err)

		err = action.Validate()

		assert.Error(t, err)
	})
}

func TestCommandActionPorts(t *testing.T) {
	action, err := NewCommandAction(nil)
	require.NoError(t, err)

	inputs, outputs := action.Ports()

	assert.Len(t, inputs, 1)
	assert.Equal(t, "input", inputs[0].ID)
	assert.Len(t, outputs, 1)
	assert.Equal(t, "output", outputs[0].ID)
}

// ============================================================================
// CronTrigger Tests
// ============================================================================

func TestNewCronTrigger(t *testing.T) {
	t.Run("creates trigger with default expression", func(t *testing.T) {
		trigger, err := NewCronTrigger(nil)

		assert.NoError(t, err)
		assert.NotNil(t, trigger)
	})

	t.Run("creates trigger with custom expression", func(t *testing.T) {
		config := map[string]interface{}{
			"expression": "0 0 * * *", // Midnight daily
		}

		trigger, err := NewCronTrigger(config)

		assert.NoError(t, err)
		assert.NotNil(t, trigger)
	})
}

func TestCronTriggerValidate(t *testing.T) {
	t.Run("valid expression passes", func(t *testing.T) {
		validExpressions := []string{
			"* * * * *",     // Every minute
			"*/5 * * * *",   // Every 5 minutes
			"0 0 * * *",     // Midnight daily
			"0 0 1 * *",     // First of month
			"0 0 * * 0",     // Every Sunday
			"30 4 1,15 * 5", // Complex
		}

		for _, expr := range validExpressions {
			t.Run(expr, func(t *testing.T) {
				config := map[string]interface{}{"expression": expr}
				trigger, err := NewCronTrigger(config)
				require.NoError(t, err)

				err = trigger.Validate()

				assert.NoError(t, err)
			})
		}
	})

	t.Run("invalid expression fails", func(t *testing.T) {
		invalidExpressions := []string{
			"not a cron",
			"* * *",         // Too few fields
			"* * * * * * *", // Too many fields
			"60 * * * *",    // Invalid minute
		}

		for _, expr := range invalidExpressions {
			t.Run(expr, func(t *testing.T) {
				config := map[string]interface{}{"expression": expr}
				// With early validation, NewCronTrigger should fail for invalid expressions
				_, err := NewCronTrigger(config)

				assert.Error(t, err, "expected error for invalid expression: %s", expr)
			})
		}
	})
}

func TestCronTriggerStartStop(t *testing.T) {
	t.Run("starts and stops", func(t *testing.T) {
		config := map[string]interface{}{
			"expression": "* * * * *",
		}

		trigger, err := NewCronTrigger(config)
		require.NoError(t, err)

		cronTrigger := trigger.(*CronTrigger)
		out := make(chan *flow.Message, 10)
		ctx := context.Background()

		err = cronTrigger.Start(ctx, out)
		require.NoError(t, err)

		err = cronTrigger.Stop()
		assert.NoError(t, err)
	})

	t.Run("cannot start twice", func(t *testing.T) {
		config := map[string]interface{}{
			"expression": "* * * * *",
		}

		trigger, err := NewCronTrigger(config)
		require.NoError(t, err)

		cronTrigger := trigger.(*CronTrigger)
		out := make(chan *flow.Message, 10)
		ctx := context.Background()

		err = cronTrigger.Start(ctx, out)
		require.NoError(t, err)
		defer cronTrigger.Stop()

		err = cronTrigger.Start(ctx, out)

		assert.Error(t, err)
	})

	t.Run("stop on non-running is safe", func(t *testing.T) {
		config := map[string]interface{}{
			"expression": "* * * * *",
		}

		trigger, err := NewCronTrigger(config)
		require.NoError(t, err)

		cronTrigger := trigger.(*CronTrigger)

		err = cronTrigger.Stop()

		assert.NoError(t, err)
	})

	t.Run("fails with invalid expression at creation", func(t *testing.T) {
		config := map[string]interface{}{
			"expression": "invalid",
		}

		// With early validation, error occurs at creation time
		_, err := NewCronTrigger(config)

		assert.Error(t, err)
	})
}

func TestCronTriggerPorts(t *testing.T) {
	trigger, err := NewCronTrigger(nil)
	require.NoError(t, err)

	inputs, outputs := trigger.Ports()

	assert.Empty(t, inputs)
	assert.Len(t, outputs, 1)
	assert.Equal(t, "output", outputs[0].ID)
}

// ============================================================================
// Node Registration Tests
// ============================================================================

func TestNodeRegistration(t *testing.T) {
	// Verify all builtin nodes are registered via init()

	t.Run("interval trigger is registered", func(t *testing.T) {
		info, exists := node.DefaultRegistry.Get("trigger-interval")

		assert.True(t, exists)
		assert.Equal(t, "Interval Timer", info.Name)
		assert.Equal(t, flow.NodeCategoryInput, info.Category)
	})

	t.Run("cron trigger is registered", func(t *testing.T) {
		info, exists := node.DefaultRegistry.Get("trigger-cron")

		assert.True(t, exists)
		assert.Equal(t, "Cron Schedule", info.Name)
	})

	t.Run("log action is registered", func(t *testing.T) {
		info, exists := node.DefaultRegistry.Get("action-log")

		assert.True(t, exists)
		assert.Equal(t, "Log", info.Name)
	})

	t.Run("command action is registered", func(t *testing.T) {
		info, exists := node.DefaultRegistry.Get("action-command")

		assert.True(t, exists)
		assert.Equal(t, "Execute Command", info.Name)
	})

	t.Run("delay process is registered", func(t *testing.T) {
		info, exists := node.DefaultRegistry.Get("process-delay")

		assert.True(t, exists)
		assert.Equal(t, "Delay", info.Name)
		assert.Equal(t, flow.NodeCategoryProcessing, info.Category)
	})
}

// ============================================================================
// BaseNode Tests
// ============================================================================

func TestBaseNode(t *testing.T) {
	t.Run("GetConfigString returns correct value", func(t *testing.T) {
		config := map[string]interface{}{
			"key": "value",
		}
		base := node.NewBaseNode(config)

		result := base.GetConfigString("key", "default")

		assert.Equal(t, "value", result)
	})

	t.Run("GetConfigString returns default for missing key", func(t *testing.T) {
		base := node.NewBaseNode(nil)

		result := base.GetConfigString("missing", "default")

		assert.Equal(t, "default", result)
	})

	t.Run("GetConfigInt returns correct value", func(t *testing.T) {
		config := map[string]interface{}{
			"int":     42,
			"int64":   int64(84),
			"float64": float64(168),
		}
		base := node.NewBaseNode(config)

		assert.Equal(t, 42, base.GetConfigInt("int", 0))
		assert.Equal(t, 84, base.GetConfigInt("int64", 0))
		assert.Equal(t, 168, base.GetConfigInt("float64", 0))
	})

	t.Run("GetConfigInt returns default for missing key", func(t *testing.T) {
		base := node.NewBaseNode(nil)

		result := base.GetConfigInt("missing", 99)

		assert.Equal(t, 99, result)
	})

	t.Run("GetConfigFloat returns correct value", func(t *testing.T) {
		config := map[string]interface{}{
			"float64": 3.14,
			"int":     42,
		}
		base := node.NewBaseNode(config)

		assert.Equal(t, 3.14, base.GetConfigFloat("float64", 0))
		assert.Equal(t, 42.0, base.GetConfigFloat("int", 0))
	})

	t.Run("GetConfigBool returns correct value", func(t *testing.T) {
		config := map[string]interface{}{
			"true":  true,
			"false": false,
		}
		base := node.NewBaseNode(config)

		assert.True(t, base.GetConfigBool("true", false))
		assert.False(t, base.GetConfigBool("false", true))
	})

	t.Run("Configure updates config", func(t *testing.T) {
		base := node.NewBaseNode(nil)

		newConfig := map[string]interface{}{"key": "new"}
		err := base.Configure(newConfig)

		assert.NoError(t, err)
		assert.Equal(t, "new", base.GetConfigString("key", ""))
	})

	t.Run("GetConfig returns config", func(t *testing.T) {
		config := map[string]interface{}{"key": "value"}
		base := node.NewBaseNode(config)

		result := base.GetConfig()

		assert.Equal(t, config, result)
	})
}

// ============================================================================
// Concurrent Node Tests
// ============================================================================

func TestNodesConcurrency(t *testing.T) {
	t.Run("concurrent interval trigger messages", func(t *testing.T) {
		config := map[string]interface{}{"interval": 50}
		trigger, err := NewIntervalTrigger(config)
		require.NoError(t, err)

		intervalTrigger := trigger.(*IntervalTrigger)
		out := make(chan *flow.Message, 100)
		ctx, cancel := context.WithCancel(context.Background())

		err = intervalTrigger.Start(ctx, out)
		require.NoError(t, err)

		// Collect messages concurrently
		var wg sync.WaitGroup
		messageCount := 0
		var mu sync.Mutex

		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for msg := range out {
					if msg != nil {
						mu.Lock()
						messageCount++
						mu.Unlock()
					}
				}
			}()
		}

		time.Sleep(200 * time.Millisecond)
		cancel()
		intervalTrigger.Stop()
		close(out)

		wg.Wait()

		mu.Lock()
		assert.Greater(t, messageCount, 0)
		mu.Unlock()
	})
}

// ============================================================================
// Helper Functions
// ============================================================================

func drainChannel(ch <-chan *flow.Message) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// ============================================================================
// Node Info Tests
// ============================================================================

func TestNodeInfoFunctions(t *testing.T) {
	t.Run("IntervalTriggerInfo returns complete info", func(t *testing.T) {
		info := IntervalTriggerInfo()

		assert.Equal(t, "trigger-interval", info.Type)
		assert.Equal(t, "Interval Timer", info.Name)
		assert.NotEmpty(t, info.Description)
		assert.Equal(t, flow.NodeCategoryInput, info.Category)
		assert.Empty(t, info.Inputs)
		assert.Len(t, info.Outputs, 1)
		assert.Equal(t, "timer", info.Icon)
	})

	t.Run("LogActionInfo returns complete info", func(t *testing.T) {
		info := LogActionInfo()

		assert.Equal(t, "action-log", info.Type)
		assert.Equal(t, "Log", info.Name)
		assert.Equal(t, flow.NodeCategoryOutput, info.Category)
		assert.Len(t, info.Inputs, 1)
		assert.Len(t, info.Outputs, 1)
	})

	t.Run("DelayProcessInfo returns complete info", func(t *testing.T) {
		info := DelayProcessInfo()

		assert.Equal(t, "process-delay", info.Type)
		assert.Equal(t, "Delay", info.Name)
		assert.Equal(t, flow.NodeCategoryProcessing, info.Category)
	})

	t.Run("CommandActionInfo returns complete info", func(t *testing.T) {
		info := CommandActionInfo()

		assert.Equal(t, "action-command", info.Type)
		assert.Equal(t, "Execute Command", info.Name)
		assert.Equal(t, "terminal", info.Icon)
	})

	t.Run("CronTriggerInfo returns complete info", func(t *testing.T) {
		info := CronTriggerInfo()

		assert.Equal(t, "trigger-cron", info.Type)
		assert.Equal(t, "Cron Schedule", info.Name)
		assert.Equal(t, flow.NodeCategoryInput, info.Category)
	})
}

// ============================================================================
// Variable Expansion Tests
// ============================================================================

func TestCommandActionVariableExpansion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	t.Run("expands topic variable", func(t *testing.T) {
		config := map[string]interface{}{
			"command": "echo {{topic}}",
		}

		action, err := NewCommandAction(config)
		require.NoError(t, err)

		msg := flow.NewMessage(nil, flow.DataTypeAny)
		msg.Topic = "test-topic"
		ctx := context.Background()

		outputs, err := action.Process(ctx, msg)

		assert.NoError(t, err)
		payload := outputs[0].Payload.(map[string]interface{})
		assert.Equal(t, "test-topic", payload["stdout"])
	})

	t.Run("expands timestamp variable", func(t *testing.T) {
		config := map[string]interface{}{
			"command": "echo {{timestamp}}",
		}

		action, err := NewCommandAction(config)
		require.NoError(t, err)

		msg := flow.NewMessage(nil, flow.DataTypeAny)
		ctx := context.Background()

		outputs, err := action.Process(ctx, msg)

		assert.NoError(t, err)
		payload := outputs[0].Payload.(map[string]interface{})
		// Should be a numeric timestamp
		assert.NotEmpty(t, payload["stdout"])
	})

	t.Run("expands meta variables", func(t *testing.T) {
		config := map[string]interface{}{
			"command": "echo {{meta.custom}}",
		}

		action, err := NewCommandAction(config)
		require.NoError(t, err)

		msg := flow.NewMessage(nil, flow.DataTypeAny)
		msg.SetMeta("custom", "meta-value")
		ctx := context.Background()

		outputs, err := action.Process(ctx, msg)

		assert.NoError(t, err)
		payload := outputs[0].Payload.(map[string]interface{})
		assert.Equal(t, "meta-value", payload["stdout"])
	})

	t.Run("handles missing variables gracefully", func(t *testing.T) {
		config := map[string]interface{}{
			"command": "echo {{meta.missing}}",
		}

		action, err := NewCommandAction(config)
		require.NoError(t, err)

		msg := flow.NewMessage(nil, flow.DataTypeAny)
		ctx := context.Background()

		outputs, err := action.Process(ctx, msg)

		assert.NoError(t, err)
		payload := outputs[0].Payload.(map[string]interface{})
		// The placeholder should remain or be empty
		stdout := payload["stdout"].(string)
		assert.True(t, stdout == "" || strings.Contains(stdout, "missing"))
	})
}
