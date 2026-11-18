package toolschema

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validateSchema = `{
	"type": "object",
	"required": ["name", "count"],
	"properties": {
		"name":  {"type": "string", "minLength": 2, "maxLength": 8, "pattern": "^[a-z]+$"},
		"mode":  {"type": "string", "enum": ["on", "off"]},
		"free":  {"type": "string"},
		"count": {"type": "integer", "minimum": 1, "maximum": 10},
		"ratio": {"type": "number", "minimum": 0, "maximum": 1},
		"level": {"type": "integer", "enum": [1, 2, 3]},
		"flag":  {"type": "boolean"},
		"tags":  {"type": "array", "maxItems": 3, "items": {"type": "string", "enum": ["a", "b"]}},
		"opts":  {"type": "object", "required": ["port"], "properties": {
			"port": {"type": "integer", "minimum": 1, "maximum": 65535},
			"deep": {"type": "object", "properties": {"x": {"type": "boolean"}}}
		}},
		"a/b": {"type": "string"},
		"m~n": {"type": "string"}
	}
}`

func issuesByPointer(issues []Issue) map[string]string {
	out := map[string]string{}
	for _, i := range issues {
		out[i.Pointer] = i.Message
	}
	return out
}

func TestValidateAcceptsValidArguments(t *testing.T) {
	s := mustCompile(t, validateSchema)
	args := map[string]any{
		"name":  "abc",
		"mode":  "on",
		"free":  strings.Repeat("é", 256), // 256 characters (runes), not bytes
		"count": float64(3),
		"ratio": 0.5,
		"level": float64(2),
		"flag":  false,
		"tags":  []any{"a", "b", "a"},
		"opts":  map[string]any{"port": float64(22), "deep": map[string]any{"x": true}},
		"a/b":   "x",
		"m~n":   "y",
	}
	assert.Empty(t, s.Validate(args))
}

func TestValidateAcceptsWholeNumbersAsIntegers(t *testing.T) {
	s := mustCompile(t, validateSchema)
	for _, n := range []any{float64(3), 3, int64(3), uint8(3), json.Number("3"), json.Number("3.0")} {
		assert.Emptyf(t, s.Validate(map[string]any{"name": "ab", "count": n}), "%T %v", n, n)
	}
}

func TestValidateNilArgumentsAreAnEmptyObject(t *testing.T) {
	s := mustCompile(t, `{"type":"object","properties":{"a":{"type":"string"}}}`)
	assert.Empty(t, s.Validate(nil))

	s = mustCompile(t, validateSchema)
	got := issuesByPointer(s.Validate(nil))
	assert.Contains(t, got["/name"], "missing required")
	assert.Contains(t, got["/count"], "missing required")
}

func TestValidateReportsProblemsWithPointers(t *testing.T) {
	s := mustCompile(t, validateSchema)
	base := func() map[string]any { return map[string]any{"name": "abc", "count": float64(3)} }
	cases := []struct {
		name    string
		key     string
		value   any
		pointer string
		want    string
	}{
		{"unknown property", "extra", "x", "/extra", "unknown property"},
		{"string type", "name", float64(1), "/name", "expected string, got number"},
		{"null", "name", nil, "/name", "expected string, got null"},
		{"too short", "name", "a", "/name", "shorter than 2"},
		{"too long", "name", "abcdefghi", "/name", "longer than 8"},
		{"pattern", "name", "ABC", "/name", "does not match the pattern ^[a-z]+$"},
		{"forced max length", "free", strings.Repeat("x", 257), "/free", "longer than 256"},
		{"enum", "mode", "auto", "/mode", `must be one of "on", "off"`},
		{"integer fraction", "count", 3.5, "/count", "expected integer, got number 3.5"},
		{"integer as string", "count", "3", "/count", "expected integer, got string"},
		{"minimum", "count", float64(0), "/count", ">= 1 (minimum)"},
		{"maximum", "count", float64(11), "/count", "<= 10 (maximum)"},
		{"number type", "ratio", "0.5", "/ratio", "expected number, got string"},
		{"number maximum", "ratio", 1.5, "/ratio", "maximum"},
		{"integer enum", "level", float64(4), "/level", "must be one of 1, 2, 3"},
		{"boolean", "flag", "true", "/flag", "expected boolean, got string"},
		{"array type", "tags", "a", "/tags", "expected array, got string"},
		{"max items", "tags", []any{"a", "a", "a", "a"}, "/tags", "at most 3"},
		{"item enum", "tags", []any{"a", "c"}, "/tags/1", "must be one of"},
		{"item type", "tags", []any{true}, "/tags/0", "expected string, got boolean"},
		{"object type", "opts", []any{}, "/opts", "expected object, got array"},
		{"nested missing", "opts", map[string]any{}, "/opts/port", "missing required"},
		{"nested unknown", "opts", map[string]any{"port": float64(1), "x": 1}, "/opts/x", "unknown property"},
		{"nested bound", "opts", map[string]any{"port": float64(70000)}, "/opts/port", "maximum"},
		{"deep type", "opts", map[string]any{"port": float64(1), "deep": map[string]any{"x": "no"}}, "/opts/deep/x", "expected boolean"},
		{"escaped slash", "a/b", float64(1), "/a~1b", "expected string"},
		{"escaped tilde", "m~n", float64(1), "/m~0n", "expected string"},
		{"go type", "flag", struct{}{}, "/flag", "expected boolean"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := base()
			args[c.key] = c.value
			got := issuesByPointer(s.Validate(args))
			require.Containsf(t, got, c.pointer, "issues: %v", got)
			assert.Contains(t, got[c.pointer], c.want)
		})
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	s := mustCompile(t, validateSchema)
	issues := s.Validate(map[string]any{"name": float64(1), "zzz": 1, "flag": 1})
	got := issuesByPointer(issues)
	assert.Len(t, got, 4, "%v", issues) // /count missing, /flag, /name, /zzz
	assert.Contains(t, got["/count"], "missing required")
	assert.Contains(t, got["/zzz"], "unknown property")
	assert.Contains(t, got["/name"], "expected string")
	assert.Contains(t, got["/flag"], "expected boolean")

	// Stable order across calls.
	assert.Equal(t, issues, s.Validate(map[string]any{"name": float64(1), "zzz": 1, "flag": 1}))
}

// Messages quote at most the first 64 characters of an argument.
func TestValidateNeverEchoesLongValues(t *testing.T) {
	s := mustCompile(t, `{"type":"object","properties":{
		"mode":{"type":"string","enum":["on","off"]},
		"name":{"type":"string","maxLength":4096,"pattern":"^[a-z]*$"}
	}}`)
	secret := "SECRET" + strings.Repeat("x", 100) + "TAIL"
	for _, args := range []map[string]any{
		{"mode": secret},
		{"name": secret},
		{secret: "x"},
	} {
		for _, issue := range s.Validate(args) {
			assert.NotContains(t, issue.Message, "TAIL")
			assert.LessOrEqual(t, len(issue.Message), 200, issue.Message)
		}
	}
}

func TestValidateCapsTheNumberOfIssues(t *testing.T) {
	s := mustCompile(t, `{"type":"object"}`)
	args := map[string]any{}
	for i := 0; i < 500; i++ {
		args[strings.Repeat("k", i+1)] = i
	}
	issues := s.Validate(args)
	assert.LessOrEqual(t, len(issues), maxIssues+1)
	assert.Contains(t, issues[len(issues)-1].Message, "too many")
}

// Long enums are listed partially.
func TestValidateShortensLongEnums(t *testing.T) {
	values := make([]string, 50)
	for i := range values {
		values[i] = `"v` + strings.Repeat("x", i%3) + string(rune('a'+i%26)) + string(rune('a'+i/26)) + `"`
	}
	s := mustCompile(t, `{"type":"object","properties":{"e":{"type":"string","enum":[`+strings.Join(values, ",")+`]}}}`)
	got := issuesByPointer(s.Validate(map[string]any{"e": "nope"}))
	assert.Contains(t, got["/e"], "more)")
}

// format is an annotation only (design §5): it is advertised, not enforced.
func TestValidateIgnoresFormat(t *testing.T) {
	s := mustCompile(t, `{"type":"object","properties":{"s":{"type":"string","format":"email"},"d":{"type":"string","format":"date-time"}}}`)
	assert.Empty(t, s.Validate(map[string]any{"s": "not an email", "d": "yesterday"}))
	assert.Contains(t, string(s.JSON()), `"format":"email"`)
}

// One compiled schema serves every call of a tool at once.
func TestSchemaIsSafeForConcurrentUse(t *testing.T) {
	s := mustCompile(t, validateSchema)
	done := make(chan struct{})
	for i := 0; i < 16; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 50; j++ {
				valid := map[string]any{"name": "abc", "count": float64(1 + (i+j)%10), "tags": []any{"a"}}
				if issues := s.Validate(valid); len(issues) != 0 {
					t.Errorf("unexpected issues: %v", issues)
				}
				if issues := s.Validate(map[string]any{"name": "ABC", "count": float64(99)}); len(issues) != 2 {
					t.Errorf("want 2 issues, got %v", issues)
				}
				_ = s.RequireStrictFor(nil)
				_ = s.JSON()
			}
		}(i)
	}
	for i := 0; i < 16; i++ {
		<-done
	}
}

// Unknown property names come from the caller: their pointers are shortened
// like values.
func TestValidateShortensUnknownPropertyPointers(t *testing.T) {
	s := mustCompile(t, `{"type":"object"}`)
	key := strings.Repeat("k", 100) + "TAIL"
	issues := s.Validate(map[string]any{key: 1})
	require.Len(t, issues, 1)
	assert.NotContains(t, issues[0].Pointer, "TAIL")
	assert.True(t, strings.HasPrefix(issues[0].Pointer, "/kkkk"), issues[0].Pointer)
	assert.Contains(t, issues[0].Message, "unknown property")
}

// jsonschema-go validates the normalized schema again after the subset
// validator: a gap in one is caught by the other.
func TestValidateHasASecondOpinion(t *testing.T) {
	s := mustCompile(t, `{"type":"object","properties":{"n":{"type":"integer","maximum":5}}}`)
	s.root.props["n"].maximum = nil // simulate a bug in the subset validator
	issues := s.Validate(map[string]any{"n": float64(6)})
	require.Len(t, issues, 1)
	assert.Equal(t, "", issues[0].Pointer)
	assert.Contains(t, issues[0].Message, "do not satisfy the schema")
}
