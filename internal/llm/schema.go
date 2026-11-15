package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
)

// MaxSchemaBytes bounds a structured-output schema once encoded.
const MaxSchemaBytes = 64 << 10

// maxQuoted bounds the text an error quotes (an answer, a validation
// failure, a provider's message).
const maxQuoted = 512

// jsonInstruction is appended to the system prompt of a Request with JSON
// (and no schema): models without structured outputs follow it well.
const jsonInstruction = "Answer with one JSON object and nothing else."

// PrepareSchema returns a deep copy of schema ready for structured outputs:
// every object schema without additionalProperties gets false (structured
// outputs require closed objects). The root must be an object schema
// ("type": "object"), the encoded schema at most MaxSchemaBytes, and the
// schema must resolve (JSON Schema 2020-12, local references only).
func PrepareSchema(schema map[string]any) (map[string]any, error) {
	if len(schema) == 0 {
		return nil, errors.New("the schema is empty")
	}
	if schema["type"] != "object" {
		return nil, errors.New(`the schema's root must be an object schema ("type": "object")`)
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("the schema cannot be encoded: %w", err)
	}
	if len(raw) > MaxSchemaBytes {
		return nil, fmt.Errorf("the schema is too large (at most %d KiB)", MaxSchemaBytes>>10)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("the schema cannot be decoded: %w", err)
	}
	walkSchemas(out, func(s map[string]any) {
		if isObjectSchema(s) {
			if _, ok := s["additionalProperties"]; !ok {
				s["additionalProperties"] = false
			}
		}
	})
	if _, err := resolveSchema(out); err != nil {
		return nil, err
	}
	return out, nil
}

// resolveSchema compiles schema for validation.
func resolveSchema(schema map[string]any) (*jsonschema.Resolved, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("the schema cannot be encoded: %w", err)
	}
	var js jsonschema.Schema
	if err := json.Unmarshal(raw, &js); err != nil {
		return nil, fmt.Errorf("invalid schema: %s", quote(err.Error()))
	}
	resolved, err := js.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("invalid schema: %s", quote(err.Error()))
	}
	return resolved, nil
}

// ValidateOutput validates a parsed answer against schema. Its errors wrap
// ErrInvalidAnswer and quote at most 512 bytes.
func ValidateOutput(schema map[string]any, value any) error {
	resolved, err := resolveSchema(schema)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidAnswer, err)
	}
	if err := resolved.Validate(value); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidAnswer, quote(err.Error()))
	}
	return nil
}

// StrictCompatible reports whether every object schema in schema lists all
// its properties as required: what OpenAI's strict structured outputs
// demand (an optional property must be nullable instead).
func StrictCompatible(schema map[string]any) bool {
	strict := true
	walkSchemas(schema, func(s map[string]any) {
		props, _ := s["properties"].(map[string]any)
		if len(props) == 0 {
			return
		}
		required := map[string]bool{}
		switch r := s["required"].(type) {
		case []any:
			for _, v := range r {
				if name, ok := v.(string); ok {
					required[name] = true
				}
			}
		case []string:
			for _, name := range r {
				required[name] = true
			}
		}
		for name := range props {
			if !required[name] {
				strict = false
			}
		}
	})
	return strict
}

// isObjectSchema reports whether s describes an object.
func isObjectSchema(s map[string]any) bool {
	switch t := s["type"].(type) {
	case string:
		return t == "object"
	case []any:
		for _, v := range t {
			if v == "object" {
				return true
			}
		}
		return false
	}
	_, hasProps := s["properties"]
	return hasProps
}

// walkSchemas calls fn for s and every subschema it holds.
func walkSchemas(s map[string]any, fn func(map[string]any)) {
	walkSchemasDepth(s, fn, 0)
}

// maxSchemaDepth bounds the walk (a schema is at most 64 KiB, but nesting is
// cheap to write).
const maxSchemaDepth = 128

func walkSchemasDepth(s map[string]any, fn func(map[string]any), depth int) {
	if s == nil || depth > maxSchemaDepth {
		return
	}
	fn(s)
	sub := func(v any) {
		if m, ok := v.(map[string]any); ok {
			walkSchemasDepth(m, fn, depth+1)
		}
	}
	for _, key := range []string{"items", "additionalProperties", "not", "if", "then", "else", "contains", "propertyNames", "unevaluatedItems", "unevaluatedProperties"} {
		sub(s[key])
	}
	for _, key := range []string{"properties", "patternProperties", "$defs", "definitions", "dependentSchemas"} {
		if m, ok := s[key].(map[string]any); ok {
			for _, v := range m {
				sub(v)
			}
		}
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf", "prefixItems"} {
		if list, ok := s[key].([]any); ok {
			for _, v := range list {
				sub(v)
			}
		}
	}
	// Draft-04 style tuple items.
	if list, ok := s["items"].([]any); ok {
		for _, v := range list {
			sub(v)
		}
	}
}

// maxJSONCandidates bounds the positions ParseJSONAnswer tries.
const maxJSONCandidates = 64

// ParseJSONAnswer parses a model's JSON answer: it trims the text, strips a
// Markdown code fence (```json ... ```) and decodes the first JSON object or
// array it finds (text around it is ignored). Errors wrap ErrInvalidAnswer.
func ParseJSONAnswer(text string) (any, error) {
	text = stripFence(strings.TrimSpace(text))
	tried := 0
	for i := 0; i < len(text) && tried < maxJSONCandidates; i++ {
		if text[i] != '{' && text[i] != '[' {
			continue
		}
		tried++
		dec := json.NewDecoder(strings.NewReader(text[i:]))
		var v any
		if err := dec.Decode(&v); err == nil {
			return v, nil
		}
	}
	if text == "" {
		return nil, fmt.Errorf("%w: the answer is empty", ErrInvalidAnswer)
	}
	return nil, fmt.Errorf("%w: the answer is not a JSON object or array: %s", ErrInvalidAnswer, quote(text))
}

// stripFence removes a surrounding Markdown code fence, if any.
func stripFence(text string) string {
	if !strings.HasPrefix(text, "```") {
		return text
	}
	body := text[3:]
	if nl := strings.IndexByte(body, '\n'); nl >= 0 {
		body = body[nl+1:] // drop the info string (json, JSON, …)
	} else {
		body = strings.TrimLeft(body, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	}
	if end := strings.LastIndex(body, "```"); end >= 0 {
		body = body[:end]
	}
	return strings.TrimSpace(body)
}

// quote returns s cut to maxQuoted bytes (at a rune boundary).
func quote(s string) string { return truncate(s, maxQuoted) }

// truncate cuts s to at most n bytes without splitting a rune.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
