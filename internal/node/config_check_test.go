package node

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func f64(v float64) *float64 { return &v }

func checkSchema() *ConfigSchema {
	return &ConfigSchema{Properties: map[string]ConfigProperty{
		"interval": {Type: "number", Unit: "milliseconds", Minimum: f64(100)},
		"timeout":  {Type: "number", Unit: "seconds", Minimum: f64(1), Maximum: f64(3600)},
		"count":    {Type: "integer", Minimum: f64(1)},
		"name":     {Type: "string"},
		"method":   {Type: "string", Enum: []string{"GET", "POST"}},
		"mode":     {Type: "string", Enum: []string{"", "Markdown"}},
		"enabled":  {Type: "boolean"},
		"headers":  {Type: "object"},
		"items":    {Type: "array"},
		"password": {Type: "string", Sensitive: true},
		"anything": {},
	}}
}

func issueMap(issues []ConfigIssue) map[string]string {
	out := make(map[string]string, len(issues))
	for _, i := range issues {
		out[i.Path] = i.Message
	}
	return out
}

func TestCheckConfigAcceptsValidValues(t *testing.T) {
	cfg := map[string]any{
		"interval": float64(1000),
		"timeout":  30, // Go callers may pass ints
		"count":    float64(3),
		"name":     "x",
		"method":   "POST",
		"mode":     "",
		"enabled":  false,
		"headers":  map[string]any{"A": "b"},
		"items":    []any{1, "two"},
		"password": "p",
		"anything": []string{"whatever"},
		"unknown":  "extra keys are not the schema's business",
	}
	assert.Empty(t, CheckConfig(checkSchema(), cfg))
}

func TestCheckConfigAcceptsTypedGoValues(t *testing.T) {
	cfg := map[string]any{
		"interval": int64(500),
		"count":    uint8(2),
		"headers":  map[string]string{"A": "b"},
		"items":    []string{"a"},
		"timeout":  json.Number("10"),
	}
	assert.Empty(t, CheckConfig(checkSchema(), cfg))
}

func TestCheckConfigTreatsNullAndEmptyStringAsUnset(t *testing.T) {
	// Older editors and hand-written definitions store "" for a cleared field;
	// nodes fall back to their default for both, so neither is a type error.
	cfg := map[string]any{
		"interval": nil,
		"timeout":  "",
		"count":    "",
		"enabled":  "",
		"headers":  "",
		"items":    nil,
		"method":   "", // not in the enum, but means "use the default"
	}
	assert.Empty(t, CheckConfig(checkSchema(), cfg))
}

func TestCheckConfigReportsWrongTypesWithUnits(t *testing.T) {
	issues := issueMap(CheckConfig(checkSchema(), map[string]any{
		"interval": "5s",
		"timeout":  true,
		"name":     12.5,
		"enabled":  "yes",
		"headers":  "Authorization: x",
		"items":    map[string]any{},
		"count":    []any{},
	}))
	assert.Equal(t, `expected number (milliseconds), got string "5s"`, issues["interval"])
	assert.Equal(t, `expected number (seconds), got boolean true`, issues["timeout"])
	assert.Equal(t, `expected string, got number 12.5`, issues["name"])
	assert.Equal(t, `expected boolean, got string "yes"`, issues["enabled"])
	assert.Equal(t, `expected object, got string "Authorization: x"`, issues["headers"])
	assert.Equal(t, `expected array, got object`, issues["items"])
	assert.Equal(t, `expected integer, got array`, issues["count"])
	assert.Len(t, issues, 7)
}

func TestCheckConfigIntegerRejectsFractions(t *testing.T) {
	issues := issueMap(CheckConfig(checkSchema(), map[string]any{"count": 1.5}))
	assert.Equal(t, `expected integer, got number 1.5`, issues["count"])
}

func TestCheckConfigRejectsNonFiniteNumbers(t *testing.T) {
	issues := issueMap(CheckConfig(checkSchema(), map[string]any{"interval": math.Inf(1), "timeout": math.NaN()}))
	assert.Contains(t, issues["interval"], "expected a finite number (milliseconds)")
	assert.Contains(t, issues["timeout"], "expected a finite number (seconds)")
}

func TestCheckConfigEnforcesBounds(t *testing.T) {
	issues := issueMap(CheckConfig(checkSchema(), map[string]any{
		"interval": 50,
		"timeout":  float64(7200),
		"count":    0,
	}))
	assert.Equal(t, `expected number >= 100 (milliseconds), got 50`, issues["interval"])
	assert.Equal(t, `expected number <= 3600 (seconds), got 7200`, issues["timeout"])
	assert.Equal(t, `expected integer >= 1, got 0`, issues["count"])

	assert.Empty(t, CheckConfig(checkSchema(), map[string]any{"interval": 100, "timeout": 3600, "count": 1}), "bounds are inclusive")
}

func TestCheckConfigEnum(t *testing.T) {
	issues := issueMap(CheckConfig(checkSchema(), map[string]any{"method": "FETCH", "mode": "html"}))
	assert.Equal(t, `expected one of "GET", "POST", got "FETCH"`, issues["method"])
	assert.Equal(t, `expected one of "", "Markdown", got "html"`, issues["mode"])

	// Enum values match exactly: most nodes compare them exactly and would
	// silently fall back to a default for "Markdown" written as "markdown".
	issues = issueMap(CheckConfig(checkSchema(), map[string]any{"method": "post", "mode": "markdown"}))
	assert.Equal(t, `expected one of "GET", "POST", got "post"`, issues["method"])
	assert.Equal(t, `expected one of "", "Markdown", got "markdown"`, issues["mode"])
}

func TestCheckConfigCaseInsensitiveEnum(t *testing.T) {
	// A node that normalises case declares it, and only then do other
	// spellings pass.
	schema := &ConfigSchema{Properties: map[string]ConfigProperty{
		"method": {Type: "string", Enum: []string{"GET", "POST"}, CaseInsensitive: true},
	}}
	assert.Empty(t, CheckConfig(schema, map[string]any{"method": "post"}))
	issues := issueMap(CheckConfig(schema, map[string]any{"method": "fetch"}))
	assert.Equal(t, `expected one of "GET", "POST" (any case), got "fetch"`, issues["method"])
}

func TestCheckConfigNeverEchoesSensitiveValues(t *testing.T) {
	issues := issueMap(CheckConfig(checkSchema(), map[string]any{"password": float64(123456)}))
	assert.Equal(t, `expected string, got number`, issues["password"])

	schema := &ConfigSchema{Properties: map[string]ConfigProperty{
		"token": {Type: "string", Sensitive: true, Enum: []string{"a"}},
	}}
	got := issueMap(CheckConfig(schema, map[string]any{"token": "hunter2"}))
	assert.NotContains(t, got["token"], "hunter2")
}

func TestCheckConfigTruncatesLongValues(t *testing.T) {
	long := strings.Repeat("x", 500)
	issues := issueMap(CheckConfig(checkSchema(), map[string]any{"interval": long}))
	assert.Less(t, len(issues["interval"]), 120)
	assert.Contains(t, issues["interval"], "…")
}

func TestCheckConfigIsSortedAndNilSafe(t *testing.T) {
	assert.Empty(t, CheckConfig(nil, map[string]any{"a": 1}))
	assert.Empty(t, CheckConfig(checkSchema(), nil))

	issues := CheckConfig(checkSchema(), map[string]any{"timeout": "a", "enabled": "b", "interval": "c"})
	require.Len(t, issues, 3)
	assert.Equal(t, []string{"enabled", "interval", "timeout"}, []string{issues[0].Path, issues[1].Path, issues[2].Path})
	assert.Equal(t, `enabled: expected boolean, got string "b"`, issues[0].String())
}
