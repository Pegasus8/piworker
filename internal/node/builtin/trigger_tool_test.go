package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/toolschema"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	_ node.TriggerNode      = (*ToolTrigger)(nil)
	_ node.Emitter          = (*ToolTrigger)(nil)
	_ node.ToolProvider     = (*ToolTrigger)(nil)
	_ node.ConfigurableNode = (*ToolTrigger)(nil)
)

// uniqueToolName returns a tool name no other test uses (the registry of
// names is process-wide).
func uniqueToolName() string { return "test_" + strings.ReplaceAll(uuid.NewString(), "-", "") }

func validToolConfig(name string) map[string]any {
	return map[string]any{
		"toolName":    name,
		"title":       "Open the garage",
		"description": "Opens the left or right garage door and reports whether it moved.",
		"inputSchema": map[string]any{
			"type":     "object",
			"required": []any{"door"},
			"properties": map[string]any{
				"door": map[string]any{"type": "string", "enum": []any{"left", "right"}},
				"note": map[string]any{"type": "string", "maxLength": float64(20)},
			},
		},
	}
}

func newToolTrigger(t *testing.T, cfg map[string]any) *ToolTrigger {
	t.Helper()
	n, err := NewToolTrigger(cfg)
	require.NoError(t, err)
	require.NoError(t, n.Validate())
	return n.(*ToolTrigger)
}

func TestToolTriggerInfo(t *testing.T) {
	info, ok := node.DefaultRegistry.Get("trigger-tool")
	require.True(t, ok, "trigger-tool is registered")
	assert.Equal(t, types.NodeCategoryInput, info.Category)
	assert.Equal(t, node.RiskSafe, info.Risk, "the effective risk is computed per flow")
	assert.Empty(t, info.Inputs)
	require.Len(t, info.Outputs, 1)
	assert.Equal(t, "output", info.Outputs[0].ID)
	assert.NotEmpty(t, info.Documentation)

	require.NotNil(t, info.Config)
	props := info.Config.Properties
	assert.ElementsMatch(t, []string{"toolName", "description", "inputSchema"}, info.Config.Required)
	assert.Equal(t, "json", props["inputSchema"].Type)
	assert.Equal(t, "json", props["outputSchema"].Type)
	assert.Equal(t, "integer", props["timeoutMs"].Type)
	assert.Equal(t, "milliseconds", props["timeoutMs"].Unit)
	assert.Equal(t, 1000.0, *props["timeoutMs"].Minimum)
	assert.Equal(t, 30000.0, *props["timeoutMs"].Maximum)
	assert.Equal(t, []string{"none", "client", "admin"}, props["confirm"].Enum)
	assert.Equal(t, "milliseconds", props["cooldownMs"].Unit)
	assert.Equal(t, 1.0, *props["maxConcurrent"].Minimum)
	assert.Equal(t, 16.0, *props["maxConcurrent"].Maximum)
	// No default to seed new nodes with: unset means 1, or 4 for read-only
	// tools, which only the flow's risk analysis knows.
	assert.Nil(t, props["maxConcurrent"].Default)
	assert.Contains(t, props["maxConcurrent"].Description, "read-only")
	assert.Contains(t, info.Documentation, "4 for read-only tools")

	// The editor seeds new nodes from the defaults: an empty closed schema.
	_, err := toolschema.Compile(mustJSON(t, props["inputSchema"].Default))
	require.NoError(t, err)
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return data
}

func TestToolTriggerSpecDefaults(t *testing.T) {
	name := uniqueToolName()
	spec := newToolTrigger(t, validToolConfig(name)).ToolSpec()
	assert.Equal(t, name, spec.Name)
	assert.Equal(t, "Open the garage", spec.Title)
	assert.Equal(t, "Opens the left or right garage door and reports whether it moved.", spec.Description)
	require.NotNil(t, spec.InputSchema)
	assert.Contains(t, string(spec.InputSchema.JSON()), `"additionalProperties":false`)
	assert.Nil(t, spec.OutputSchema)
	assert.Equal(t, 10*time.Second, spec.Timeout)
	assert.Equal(t, node.ToolConfirmNone, spec.Confirm)
	assert.False(t, spec.Idempotent)
	assert.Equal(t, 0, spec.MaxConcurrent, "unset: the flow's risk analysis picks 1, or 4 for read-only tools")
	assert.Equal(t, time.Duration(0), spec.Cooldown)
}

// An explicit 1 is kept apart from an unset value, so a read-only tool the
// admin limited to one call at a time stays limited.
func TestToolTriggerExplicitMaxConcurrent(t *testing.T) {
	for _, v := range []any{float64(1), 1, nil, ""} {
		cfg := validToolConfig(uniqueToolName())
		cfg["maxConcurrent"] = v
		want := 1
		if v == nil || v == "" {
			want = 0
		}
		assert.Equalf(t, want, newToolTrigger(t, cfg).ToolSpec().MaxConcurrent, "%#v", v)
	}
}

func TestToolTriggerSpecFromConfig(t *testing.T) {
	cfg := validToolConfig(uniqueToolName())
	cfg["outputSchema"] = map[string]any{"type": "object", "properties": map[string]any{"moved": map[string]any{"type": "boolean"}}}
	cfg["timeoutMs"] = float64(20000)
	cfg["confirm"] = "admin"
	cfg["idempotent"] = true
	cfg["maxConcurrent"] = float64(4)
	cfg["cooldownMs"] = 60000 // Go callers may pass ints
	spec := newToolTrigger(t, cfg).ToolSpec()
	assert.JSONEq(t, `{"type":"object","properties":{"moved":{"type":"boolean"}}}`, string(spec.OutputSchema))
	assert.Equal(t, 20*time.Second, spec.Timeout)
	assert.Equal(t, node.ToolConfirmAdmin, spec.Confirm)
	assert.True(t, spec.Idempotent)
	assert.Equal(t, 4, spec.MaxConcurrent)
	assert.Equal(t, time.Minute, spec.Cooldown)
}

// Schemas may also be stored as text holding a JSON object.
func TestToolTriggerAcceptsSchemasAsJSONText(t *testing.T) {
	cfg := validToolConfig(uniqueToolName())
	cfg["inputSchema"] = `{"type":"object","properties":{"n":{"type":"integer","minimum":1,"maximum":3}}}`
	cfg["outputSchema"] = `{"type":"object"}`
	spec := newToolTrigger(t, cfg).ToolSpec()
	assert.Empty(t, spec.InputSchema.Validate(map[string]any{"n": float64(2)}))
	assert.NotEmpty(t, spec.InputSchema.Validate(map[string]any{"n": float64(9)}))
	assert.JSONEq(t, `{"type":"object"}`, string(spec.OutputSchema))
}

func TestToolTriggerRejectsInvalidConfig(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value any // nil deletes the key
		want  string
	}{
		{"missing name", "toolName", nil, "toolName"},
		{"empty name", "toolName", "", "toolName"},
		{"name with space", "toolName", "open garage", "toolName"},
		{"name with slash", "toolName", "a/b", "toolName"},
		{"name too long", "toolName", strings.Repeat("a", 129), "toolName"},
		{"reserved prefix", "toolName", "pw_get_result", "pw_"},
		{"reserved prefix any case", "toolName", "PW_status", "pw_"},
		{"name not text", "toolName", float64(12), "toolName"},
		{"missing description", "description", nil, "description"},
		{"short description", "description", "Opens it.", "20 characters"},
		{"padded description", "description", "   Opens it.             ", "20 characters"},
		{"description not text", "description", true, "description"},
		{"title too long", "title", strings.Repeat("t", 129), "title"},
		{"title not text", "title", float64(1), "title"},
		{"missing input schema", "inputSchema", nil, "inputSchema"},
		{"input schema outside subset", "inputSchema", map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"anyOf": []any{}}}}, "/properties/x/anyOf"},
		{"input schema not object type", "inputSchema", map[string]any{"type": "string"}, `"object"`},
		{"input schema text not json", "inputSchema", "type: object", "inputSchema"},
		{"input schema array", "inputSchema", []any{}, "inputSchema"},
		{"output schema not object type", "outputSchema", map[string]any{"type": "string"}, "outputSchema"},
		{"output schema text array", "outputSchema", "[]", "outputSchema"},
		{"output schema number", "outputSchema", float64(1), "outputSchema"},
		{"timeout too short", "timeoutMs", float64(999), "timeoutMs"},
		{"timeout too long", "timeoutMs", float64(30001), "timeoutMs"},
		{"timeout fraction", "timeoutMs", 1500.5, "timeoutMs"},
		{"timeout text", "timeoutMs", "10s", "timeoutMs"},
		{"confirm unknown", "confirm", "maybe", "confirm"},
		{"confirm case", "confirm", "Admin", "confirm"},
		{"idempotent text", "idempotent", "yes", "idempotent"},
		{"no concurrency", "maxConcurrent", float64(0), "maxConcurrent"},
		{"too much concurrency", "maxConcurrent", float64(17), "maxConcurrent"},
		{"negative cooldown", "cooldownMs", float64(-1), "cooldownMs"},
		{"cooldown over an hour", "cooldownMs", float64(3600001), "cooldownMs"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := validToolConfig(uniqueToolName())
			if c.value == nil {
				delete(cfg, c.key)
			} else {
				cfg[c.key] = c.value
			}
			_, err := NewToolTrigger(cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want)
		})
	}
}

func TestToolTriggerReportsEveryConfigProblem(t *testing.T) {
	cfg := validToolConfig("pw_bad")
	cfg["description"] = "short"
	cfg["inputSchema"] = map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "date"}}}
	_, err := NewToolTrigger(cfg)
	require.Error(t, err)
	for _, want := range []string{"toolName", "description", "/properties/x/type"} {
		assert.Contains(t, err.Error(), want)
	}
	var schemaErr *toolschema.Error
	require.True(t, errors.As(err, &schemaErr), "the schema issues stay reachable: %v", err)
	assert.Equal(t, "/properties/x/type", schemaErr.Issues[0].Pointer)
}

// The registry derives no schema from a probe (the factory refuses an empty
// config), so a nil config is an error, not a panic.
func TestToolTriggerNilConfig(t *testing.T) {
	_, err := NewToolTrigger(nil)
	require.Error(t, err)
}

func TestToolTriggerClaimsItsNameWhileRunning(t *testing.T) {
	name := uniqueToolName()
	a := newToolTrigger(t, validToolConfig(name))
	b := newToolTrigger(t, validToolConfig(name))
	out := make(chan *types.Message, 1)

	require.NoError(t, a.Start(context.Background(), out))
	err := b.Start(context.Background(), out)
	require.Error(t, err)
	assert.True(t, errors.Is(err, node.ErrResourceConflict), "%v", err)
	assert.Contains(t, err.Error(), name)

	err = a.Start(context.Background(), out)
	require.Error(t, err, "already running")
	assert.False(t, errors.Is(err, node.ErrResourceConflict))

	require.NoError(t, a.Stop())
	require.NoError(t, b.Start(context.Background(), out))
	require.NoError(t, a.Stop(), "stopping a stopped trigger is a no-op")
	assert.True(t, toolNames.taken(name), "a stale Stop must not free the name")
	require.NoError(t, b.Stop())
	assert.False(t, toolNames.taken(name))

	// The same instance can start again (flow restart).
	require.NoError(t, b.Start(context.Background(), out))
	require.NoError(t, b.Stop())
}

func toolMessage(payload any) *types.Message {
	msg := types.NewMessage(payload, types.DataTypeObject)
	msg.SetMeta("correlationID", "run-1")
	msg.SetMeta("actor", "editor")
	return msg
}

func TestToolTriggerEmit(t *testing.T) {
	name := uniqueToolName()
	trig := newToolTrigger(t, validToolConfig(name))
	ctx := context.Background()
	err := trig.Emit(ctx, toolMessage(map[string]any{"door": "left"}))
	require.Error(t, err, "not running")

	out := make(chan *types.Message, 1)
	require.NoError(t, trig.Start(ctx, out))
	defer func() { _ = trig.Stop() }()

	require.NoError(t, trig.Emit(ctx, toolMessage(map[string]any{"door": "left"})))
	got := <-out
	assert.Equal(t, "output", got.SourcePort)
	assert.Equal(t, map[string]any{"door": "left"}, got.Payload)
	for key, want := range map[string]any{"toolName": name, "correlationID": "run-1", "actor": "editor"} {
		v, ok := got.GetMeta(key)
		assert.True(t, ok, key)
		assert.Equal(t, want, v, key)
	}
	assert.Equal(t, name, got.Topic)
}

// The runtime passes validated arguments; the trigger checks them again so
// its flow never sees data outside the schema.
func TestToolTriggerEmitRefusesInvalidArguments(t *testing.T) {
	trig := newToolTrigger(t, validToolConfig(uniqueToolName()))
	out := make(chan *types.Message, 1)
	require.NoError(t, trig.Start(context.Background(), out))
	defer func() { _ = trig.Stop() }()

	for _, payload := range []any{
		map[string]any{"door": "middle"},
		map[string]any{},
		map[string]any{"door": "left", "extra": true},
		"left",
		nil,
	} {
		err := trig.Emit(context.Background(), toolMessage(payload))
		require.Errorf(t, err, "%#v", payload)
		assert.Contains(t, err.Error(), "invalid tool arguments")
	}
	err := trig.Emit(context.Background(), toolMessage(map[string]any{"door": "middle"}))
	assert.Contains(t, err.Error(), "/door")
	assert.Len(t, out, 0, "nothing was emitted")
}

func TestToolTriggerEmitHonoursContexts(t *testing.T) {
	t.Run("caller", func(t *testing.T) {
		trig := newToolTrigger(t, validToolConfig(uniqueToolName()))
		out := make(chan *types.Message) // nobody reads: Emit blocks
		require.NoError(t, trig.Start(context.Background(), out))
		defer func() { _ = trig.Stop() }()

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		err := trig.Emit(ctx, toolMessage(map[string]any{"door": "left"}))
		assert.ErrorIs(t, err, context.DeadlineExceeded)

		cancelled, cancelNow := context.WithCancel(context.Background())
		cancelNow()
		assert.ErrorIs(t, trig.Emit(cancelled, toolMessage(map[string]any{"door": "left"})), context.Canceled)
	})
	t.Run("trigger", func(t *testing.T) {
		trig := newToolTrigger(t, validToolConfig(uniqueToolName()))
		runCtx, stopRun := context.WithCancel(context.Background())
		out := make(chan *types.Message)
		require.NoError(t, trig.Start(runCtx, out))
		defer func() { _ = trig.Stop() }()

		done := make(chan error, 1)
		go func() { done <- trig.Emit(context.Background(), toolMessage(map[string]any{"door": "left"})) }()
		time.Sleep(20 * time.Millisecond)
		stopRun()
		select {
		case err := <-done:
			require.Error(t, err)
			assert.Contains(t, err.Error(), "not running")
		case <-time.After(2 * time.Second):
			t.Fatal("Emit did not return when the trigger's context ended")
		}
	})
	t.Run("stopped", func(t *testing.T) {
		trig := newToolTrigger(t, validToolConfig(uniqueToolName()))
		out := make(chan *types.Message, 1)
		require.NoError(t, trig.Start(context.Background(), out))
		require.NoError(t, trig.Stop())
		err := trig.Emit(context.Background(), toolMessage(map[string]any{"door": "left"}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not running")
	})
}

func TestToolTriggerProcessIsANoOp(t *testing.T) {
	trig := newToolTrigger(t, validToolConfig(uniqueToolName()))
	msgs, err := trig.Process(context.Background(), toolMessage(map[string]any{"door": "left"}))
	assert.NoError(t, err)
	assert.Empty(t, msgs)
}
