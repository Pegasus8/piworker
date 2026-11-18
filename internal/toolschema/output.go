package toolschema

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// CheckOutputSchema checks a tool's optional output schema and returns it in
// canonical form (sorted keys, normalized numbers). Output schemas describe
// what the flow replies and are advertised as is, so they are not limited to
// the input subset: the document must be a JSON object (at most 64 KiB,
// without duplicate keys) with "type": "object", as MCP requires, and must
// resolve as JSON Schema without remote references.
func CheckOutputSchema(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) > MaxSchemaBytes {
		return nil, fmt.Errorf("output schema is larger than 64 KiB (%d bytes)", len(raw))
	}
	doc, issue := decodeDocument(raw)
	if issue != nil {
		return nil, errors.New("output schema: " + issue.String())
	}
	m, ok := doc.(map[string]any)
	if !ok {
		return nil, errors.New(`output schema must be a JSON object with "type": "object"`)
	}
	if m["type"] != "object" {
		return nil, errors.New(`output schema must declare "type": "object"`)
	}
	canonical, err := marshalCanonical(normalize(m))
	if err != nil {
		return nil, fmt.Errorf("output schema cannot be encoded: %w", err)
	}
	var js jsonschema.Schema
	if err := json.Unmarshal(canonical, &js); err != nil {
		return nil, fmt.Errorf("invalid output schema: %w", err)
	}
	if _, err := js.Resolve(nil); err != nil {
		return nil, fmt.Errorf("invalid output schema: %w", err)
	}
	return canonical, nil
}
