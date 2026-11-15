package llm

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestPrepareSchemaClosesEveryObjectAndCopies(t *testing.T) {
	in := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"tags": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "object", "properties": map[string]any{"k": map[string]any{"type": "string"}}},
			},
			"open": map[string]any{"type": "object", "additionalProperties": true},
			"either": map[string]any{"anyOf": []any{
				map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "number"}}},
				map[string]any{"type": "null"},
			}},
		},
		"$defs": map[string]any{"point": map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "number"}}}},
	}
	out, err := PrepareSchema(in)
	if err != nil {
		t.Fatal(err)
	}
	if out["additionalProperties"] != false {
		t.Errorf("root not closed: %v", out["additionalProperties"])
	}
	props := out["properties"].(map[string]any)
	item := props["tags"].(map[string]any)["items"].(map[string]any)
	if item["additionalProperties"] != false {
		t.Errorf("array item not closed")
	}
	if props["open"].(map[string]any)["additionalProperties"] != true {
		t.Errorf("an explicit additionalProperties was changed")
	}
	alt := props["either"].(map[string]any)["anyOf"].([]any)[0].(map[string]any)
	if alt["additionalProperties"] != false {
		t.Errorf("anyOf branch not closed")
	}
	def := out["$defs"].(map[string]any)["point"].(map[string]any)
	if def["additionalProperties"] != false {
		t.Errorf("$defs entry not closed")
	}
	// The input is untouched (a deep copy).
	if _, ok := in["additionalProperties"]; ok {
		t.Fatal("PrepareSchema changed its input")
	}
}

func TestPrepareSchemaRejects(t *testing.T) {
	cases := map[string]map[string]any{
		"nil":       nil,
		"not root":  {"type": "array", "items": map[string]any{"type": "string"}},
		"no type":   {"properties": map[string]any{}},
		"too large": {"type": "object", "description": strings.Repeat("x", 70*1024)},
		"invalid":   {"type": "object", "properties": map[string]any{"a": map[string]any{"type": 12}}},
	}
	for name, schema := range cases {
		if _, err := PrepareSchema(schema); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestValidateOutput(t *testing.T) {
	schema, err := PrepareSchema(map[string]any{
		"type":       "object",
		"properties": map[string]any{"category": map[string]any{"enum": []any{"a", "b"}}, "confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1}},
		"required":   []any{"category", "confidence"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateOutput(schema, map[string]any{"category": "a", "confidence": 0.5}); err != nil {
		t.Fatalf("valid answer: %v", err)
	}
	bad := []any{
		map[string]any{"category": "c", "confidence": 0.5},
		map[string]any{"category": "a"},
		map[string]any{"category": "a", "confidence": 0.5, "extra": 1},
		"text",
	}
	for _, v := range bad {
		err := ValidateOutput(schema, v)
		if !errors.Is(err, ErrInvalidAnswer) {
			t.Errorf("%v: want ErrInvalidAnswer, got %v", v, err)
		} else if len(err.Error()) > 512+len(ErrInvalidAnswer.Error())+2 {
			t.Errorf("error too long: %d bytes", len(err.Error()))
		}
	}
}

func TestStrictCompatible(t *testing.T) {
	all := map[string]any{
		"type":       "object",
		"properties": map[string]any{"a": map[string]any{"type": "string"}, "b": map[string]any{"type": "object", "properties": map[string]any{"c": map[string]any{"type": "number"}}, "required": []any{"c"}}},
		"required":   []any{"a", "b"},
	}
	if !StrictCompatible(all) {
		t.Error("every property required: should be strict")
	}
	optional := map[string]any{
		"type":       "object",
		"properties": map[string]any{"a": map[string]any{"type": "string"}, "b": map[string]any{"type": "string"}},
		"required":   []string{"a"},
	}
	if StrictCompatible(optional) {
		t.Error("an optional property: not strict")
	}
	nested := map[string]any{
		"type":       "object",
		"properties": map[string]any{"list": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{}}}}},
		"required":   []any{"list"},
	}
	if StrictCompatible(nested) {
		t.Error("a nested optional property: not strict")
	}
}

func TestParseJSONAnswer(t *testing.T) {
	cases := map[string]any{
		`{"a": 1}`:                          map[string]any{"a": float64(1)},
		"  ```json\n{\"a\": [1, 2]}\n```  ": map[string]any{"a": []any{float64(1), float64(2)}},
		"```\n[1]\n```":                     []any{float64(1)},
		"Here you go: {\"ok\": true} hope it helps.": map[string]any{"ok": true},
		"[note] then {\"x\": \"y\"}":                 map[string]any{"x": "y"},
		// A failed candidate that closes is skipped as a whole.
		"See {this} and {\"x\": [1]}":                 map[string]any{"x": []any{float64(1)}},
		"{\"note\": \"a } in text\" bad} {\"ok\": 1}": map[string]any{"ok": float64(1)},
	}
	for text, want := range cases {
		got, err := ParseJSONAnswer(text)
		if err != nil {
			t.Errorf("%q: %v", text, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %#v, want %#v", text, got, want)
		}
	}
	// A malformed answer is invalid, never a value nested inside it: the
	// object's trailing comma must not turn the answer into its "items".
	for _, text := range []string{"", "no json here", `"just a string"`, "{broken",
		`{"summary":"x","items":[{"a":1}],}`,
		"```json\n{\"summary\": \"x\", \"items\": [1, 2] x}\n```",
		`Sure: {"a": {"b": 1}, oops}`,
		`[{"a":1}, {"b":2},]`,
	} {
		if _, err := ParseJSONAnswer(text); !errors.Is(err, ErrInvalidAnswer) {
			t.Errorf("%q: want ErrInvalidAnswer, got %v", text, err)
		}
	}
}
