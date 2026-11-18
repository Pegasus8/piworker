package toolschema

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckOutputSchemaAcceptsObjectSchemas(t *testing.T) {
	out, err := CheckOutputSchema(json.RawMessage(`{"type":"object","required":["ok"],"properties":{"ok":{"type":"boolean"},"n":{"type":"number","maximum":1e2}}}`))
	require.NoError(t, err)
	assert.Equal(t, `{"properties":{"n":{"maximum":100,"type":"number"},"ok":{"type":"boolean"}},"required":["ok"],"type":"object"}`, string(out))

	// Output schemas describe what the flow replies; they are advertised as
	// is and not limited to the input subset.
	_, err = CheckOutputSchema(json.RawMessage(`{"type":"object","properties":{"v":{"anyOf":[{"type":"string"},{"type":"null"}]}}}`))
	require.NoError(t, err)
}

func TestCheckOutputSchemaRejects(t *testing.T) {
	cases := map[string]struct{ raw, want string }{
		"not json":        {`{"type":`, "invalid JSON"},
		"duplicate key":   {`{"type":"object","type":"object"}`, "duplicate key"},
		"array":           {`[]`, "JSON object"},
		"boolean":         {`true`, "JSON object"},
		"untyped":         {`{}`, `"type": "object"`},
		"string":          {`{"type":"string"}`, `"type": "object"`},
		"type list":       {`{"type":["object","null"]}`, `"type": "object"`},
		"malformed":       {`{"type":"object","properties":5}`, "output schema"},
		"remote ref":      {`{"type":"object","properties":{"a":{"$ref":"https://example.com/s.json"}}}`, "output schema"},
		"bad pattern":     {`{"type":"object","properties":{"a":{"type":"string","pattern":"("}}}`, "output schema"},
		"too large":       {`{"type":"object","description":"` + strings.Repeat("x", MaxSchemaBytes) + `"}`, "64 KiB"},
		"trailing values": {`{"type":"object"} 1`, "invalid JSON"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := CheckOutputSchema(json.RawMessage(c.raw))
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want)
		})
	}
}
