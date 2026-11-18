package node

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Bounds and non-finite messages must not reveal a Sensitive number either
// (e.g. a PIN or numeric key stored in a number field).
func TestCheckConfigBoundsNeverEchoSensitiveValues(t *testing.T) {
	lo, hi := 1000.0, 9999.0
	schema := &ConfigSchema{Properties: map[string]ConfigProperty{
		"pin":    {Type: "integer", Sensitive: true, Minimum: &lo, Maximum: &hi},
		"secret": {Type: "number", Sensitive: true},
	}}

	issues := issueMap(CheckConfig(schema, map[string]any{"pin": float64(123456)}))
	assert.Equal(t, `expected integer <= 9999, got a larger value`, issues["pin"])
	assert.NotContains(t, issues["pin"], "123456")

	issues = issueMap(CheckConfig(schema, map[string]any{"pin": float64(42)}))
	assert.Equal(t, `expected integer >= 1000, got a smaller value`, issues["pin"])
	assert.NotContains(t, issues["pin"], "42")

	for _, v := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
		issues = issueMap(CheckConfig(schema, map[string]any{"secret": v}))
		assert.Equal(t, `expected a finite number, got a non-finite value`, issues["secret"])
	}
}

// Non-sensitive properties keep quoting the offending value.
func TestCheckConfigBoundsStillShowPlainValues(t *testing.T) {
	lo := 1.0
	schema := &ConfigSchema{Properties: map[string]ConfigProperty{
		"count": {Type: "number", Minimum: &lo},
	}}
	issues := issueMap(CheckConfig(schema, map[string]any{"count": float64(0.5)}))
	assert.Equal(t, `expected number >= 1, got 0.5`, issues["count"])
	issues = issueMap(CheckConfig(schema, map[string]any{"count": math.Inf(1)}))
	assert.Equal(t, `expected a finite number, got +Inf`, issues["count"])
}
