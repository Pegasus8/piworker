package node

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/toolschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubTool struct{ spec ToolSpec }

func (s stubTool) ToolSpec() ToolSpec { return s.spec }

var _ ToolProvider = stubTool{}

func TestToolConfirmModes(t *testing.T) {
	assert.Equal(t, "none", ToolConfirmNone)
	assert.Equal(t, "client", ToolConfirmClient)
	assert.Equal(t, "admin", ToolConfirmAdmin)
	assert.Equal(t, []string{"none", "client", "admin"}, ToolConfirmModes())
}

func TestToolSpecCarriesTheCompiledSchema(t *testing.T) {
	schema, err := toolschema.Compile(json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}}}`))
	require.NoError(t, err)
	var p ToolProvider = stubTool{spec: ToolSpec{
		Name:          "garage_open",
		Title:         "Open the garage",
		Description:   "Opens the garage door and reports its state.",
		InputSchema:   schema,
		OutputSchema:  json.RawMessage(`{"type":"object"}`),
		Timeout:       10 * time.Second,
		Confirm:       ToolConfirmAdmin,
		Idempotent:    true,
		MaxConcurrent: 1,
		Cooldown:      time.Minute,
	}}
	spec := p.ToolSpec()
	assert.Equal(t, "garage_open", spec.Name)
	assert.Empty(t, spec.InputSchema.Validate(map[string]any{"n": float64(1)}))
	assert.NotEmpty(t, spec.InputSchema.Validate(map[string]any{"n": "1"}))
}

func TestToolSpecEffectiveMaxConcurrent(t *testing.T) {
	assert.Equal(t, 1, ToolDefaultMaxConcurrent)
	assert.Equal(t, 4, ToolReadOnlyMaxConcurrent)
	cases := []struct {
		set      int
		readOnly bool
		want     int
	}{
		{0, false, 1},
		{0, true, 4},
		{1, true, 1},
		{1, false, 1},
		{8, false, 8},
		{8, true, 8},
	}
	for _, c := range cases {
		spec := ToolSpec{MaxConcurrent: c.set}
		assert.Equalf(t, c.want, spec.EffectiveMaxConcurrent(c.readOnly), "%+v", c)
	}
}
