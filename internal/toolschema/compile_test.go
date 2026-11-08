package toolschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustCompile compiles raw and fails the test on error.
func mustCompile(t *testing.T, raw string) *Schema {
	t.Helper()
	s, err := Compile(json.RawMessage(raw))
	require.NoError(t, err, raw)
	require.NotNil(t, s)
	return s
}

// compileIssues compiles raw, requires a *Error and returns its issues.
func compileIssues(t *testing.T, raw string) []Issue {
	t.Helper()
	s, err := Compile(json.RawMessage(raw))
	require.Error(t, err, raw)
	assert.Nil(t, s)
	var ce *Error
	require.True(t, errors.As(err, &ce), "error %v is not a *toolschema.Error", err)
	require.NotEmpty(t, ce.Issues)
	return ce.Issues
}

// hasIssueAt reports whether issues contain one at pointer whose message
// contains want.
func hasIssueAt(issues []Issue, pointer, want string) bool {
	for _, i := range issues {
		if i.Pointer == pointer && strings.Contains(i.Message, want) {
			return true
		}
	}
	return false
}

func TestCompileAcceptsTheSubset(t *testing.T) {
	cases := map[string]string{
		"empty object":      `{"type":"object"}`,
		"explicit closed":   `{"type":"object","additionalProperties":false,"properties":{}}`,
		"annotations":       `{"type":"object","title":"Open","description":"Opens it","examples":[{}],"default":{}}`,
		"string":            `{"type":"object","properties":{"s":{"type":"string","minLength":1,"maxLength":10,"format":"email","title":"S","description":"d","default":"a","examples":["b"]}},"required":["s"]}`,
		"string enum":       `{"type":"object","properties":{"s":{"type":"string","enum":["a","b"]}}}`,
		"anchored pattern":  `{"type":"object","properties":{"s":{"type":"string","pattern":"^[a-z][a-z0-9_]*$"}}}`,
		"grouped alternate": `{"type":"object","properties":{"s":{"type":"string","pattern":"^(on|off)$"}}}`,
		"integer":           `{"type":"object","properties":{"n":{"type":"integer","minimum":0,"maximum":10,"default":3,"enum":[1,3,5]}}}`,
		"whole float int":   `{"type":"object","properties":{"n":{"type":"integer","enum":[2.0],"default":2.0}}}`,
		"number":            `{"type":"object","properties":{"n":{"type":"number","minimum":-1.5,"maximum":1.5}}}`,
		"boolean":           `{"type":"object","properties":{"b":{"type":"boolean","default":true}}}`,
		"array":             `{"type":"object","properties":{"a":{"type":"array","items":{"type":"string","enum":["x","y"]},"maxItems":5,"default":["x"]}}}`,
		"array max cap":     `{"type":"object","properties":{"a":{"type":"array","items":{"type":"integer"},"maxItems":100}}}`,
		"nested depth 3":    `{"type":"object","properties":{"a":{"type":"object","properties":{"b":{"type":"object","properties":{"c":{"type":"string"}},"required":["c"]}}}}}`,
		"allow probes":      `{"type":"object","properties":{"p":{"type":"string","pattern":"^[a-z/.]+$","x-piworker-allow":["/",".."]}}}`,
		"long maxLength":    `{"type":"object","properties":{"s":{"type":"string","maxLength":4096,"minLength":300}}}`,
		"enum then length":  `{"type":"object","properties":{"s":{"type":"string","enum":["abc"],"minLength":3,"maxLength":3}}}`,
		"weird prop names":  `{"type":"object","properties":{"a/b":{"type":"string"},"m~n":{"type":"boolean"}}}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			mustCompile(t, raw)
		})
	}
}

// Keywords outside the subset are refused wherever they appear: at the root,
// inside a property and inside array items.
func TestCompileRejectsKeywordsEverywhere(t *testing.T) {
	keywords := map[string]string{
		"$ref":                  `"#/$defs/x"`,
		"$defs":                 `{"x":{"type":"string"}}`,
		"definitions":           `{"x":{"type":"string"}}`,
		"$id":                   `"https://example.com/s"`,
		"$schema":               `"https://json-schema.org/draft/2020-12/schema"`,
		"$anchor":               `"a"`,
		"$dynamicRef":           `"#a"`,
		"$dynamicAnchor":        `"a"`,
		"$comment":              `"c"`,
		"$vocabulary":           `{}`,
		"anyOf":                 `[{"type":"string"}]`,
		"oneOf":                 `[{"type":"string"}]`,
		"allOf":                 `[{"type":"string"}]`,
		"not":                   `{"type":"string"}`,
		"if":                    `{"type":"string"}`,
		"then":                  `{"type":"string"}`,
		"else":                  `{"type":"string"}`,
		"const":                 `"x"`,
		"patternProperties":     `{"^x$":{"type":"string"}}`,
		"propertyNames":         `{"type":"string"}`,
		"dependentRequired":     `{"a":["b"]}`,
		"dependentSchemas":      `{"a":{"type":"object"}}`,
		"dependencies":          `{"a":["b"]}`,
		"unevaluatedProperties": `false`,
		"unevaluatedItems":      `false`,
		"minProperties":         `1`,
		"maxProperties":         `1`,
		"multipleOf":            `2`,
		"exclusiveMinimum":      `0`,
		"exclusiveMaximum":      `9`,
		"contains":              `{"type":"string"}`,
		"prefixItems":           `[{"type":"string"}]`,
		"additionalItems":       `false`,
		"uniqueItems":           `true`,
		"minItems":              `1`,
		"contentEncoding":       `"base64"`,
		"contentMediaType":      `"text/plain"`,
		"contentSchema":         `{"type":"string"}`,
		"readOnly":              `true`,
		"writeOnly":             `true`,
		"deprecated":            `true`,
		"nullable":              `true`,
		"x-mcp-header":          `"X-Thing"`,
		"x-unknown":             `1`,
		"bogus":                 `1`,
	}
	containers := []struct {
		name    string
		wrap    func(kw string) string
		pointer string
	}{
		{"root", func(kw string) string { return `{"type":"object",` + kw + `}` }, ""},
		{"property", func(kw string) string {
			return `{"type":"object","properties":{"p":{"type":"string",` + kw + `}}}`
		}, "/properties/p"},
		{"items", func(kw string) string {
			return `{"type":"object","properties":{"a":{"type":"array","maxItems":3,"items":{"type":"string",` + kw + `}}}}`
		}, "/properties/a/items"},
		{"nested object", func(kw string) string {
			return `{"type":"object","properties":{"o":{"type":"object","properties":{},` + kw + `}}}`
		}, "/properties/o"},
	}
	for kw, value := range keywords {
		for _, c := range containers {
			t.Run(c.name+"/"+kw, func(t *testing.T) {
				raw := c.wrap(fmt.Sprintf("%q:%s", kw, value))
				issues := compileIssues(t, raw)
				pointer := c.pointer + "/" + escapePointer(kw)
				assert.Truef(t, hasIssueAt(issues, pointer, kw), "want an issue at %s naming %s, got %v", pointer, kw, issues)
			})
		}
	}
}

func TestCompileRejectsOutsideTheSubset(t *testing.T) {
	cases := []struct {
		name, raw, pointer, want string
	}{
		// Documents.
		{"not json", `{"type":`, "", "invalid JSON"},
		{"empty", ``, "", "invalid JSON"},
		{"trailing data", `{"type":"object"} {}`, "", "invalid JSON"},
		{"duplicate key", `{"type":"object","type":"object"}`, "/type", "duplicate key"},
		{"nested duplicate key", `{"type":"object","properties":{"a":{"type":"string","type":"integer"}}}`, "/properties/a/type", "duplicate key"},
		// Root.
		{"null root", `null`, "", "must be a JSON object"},
		{"true root", `true`, "", "boolean schemas"},
		{"false root", `false`, "", "boolean schemas"},
		{"array root", `[]`, "", "must be a JSON object"},
		{"untyped root", `{}`, "", "type"},
		{"string root", `{"type":"string"}`, "/type", `must be "object"`},
		{"type array root", `{"type":["object"]}`, "/type", "single type"},
		{"additional true", `{"type":"object","additionalProperties":true}`, "/additionalProperties", "false"},
		{"additional schema", `{"type":"object","additionalProperties":{"type":"string"}}`, "/additionalProperties", "false"},
		{"additional empty schema", `{"type":"object","additionalProperties":{}}`, "/additionalProperties", "false"},
		{"properties not object", `{"type":"object","properties":[]}`, "/properties", "object"},
		{"required not array", `{"type":"object","properties":{"a":{"type":"string"}},"required":"a"}`, "/required", "array"},
		{"required not string", `{"type":"object","properties":{"a":{"type":"string"}},"required":[1]}`, "/required/0", "string"},
		{"required undeclared", `{"type":"object","properties":{"a":{"type":"string"}},"required":["b"]}`, "/required/0", "not declared"},
		{"required duplicate", `{"type":"object","properties":{"a":{"type":"string"}},"required":["a","a"]}`, "/required/1", "duplicate"},
		{"empty property name", `{"type":"object","properties":{"":{"type":"string"}}}`, "/properties/", "name"},
		// Property schemas.
		{"boolean property", `{"type":"object","properties":{"a":true}}`, "/properties/a", "boolean schemas"},
		{"false property", `{"type":"object","properties":{"a":false}}`, "/properties/a", "boolean schemas"},
		{"untyped property", `{"type":"object","properties":{"a":{}}}`, "/properties/a", "type"},
		{"untyped with keywords", `{"type":"object","properties":{"a":{"maxLength":3}}}`, "/properties/a", "type"},
		{"type array", `{"type":"object","properties":{"a":{"type":["string","null"]}}}`, "/properties/a/type", "single type"},
		{"null type", `{"type":"object","properties":{"a":{"type":"null"}}}`, "/properties/a/type", "unsupported type"},
		{"unknown type", `{"type":"object","properties":{"a":{"type":"date"}}}`, "/properties/a/type", "unsupported type"},
		{"type not string", `{"type":"object","properties":{"a":{"type":1}}}`, "/properties/a/type", "type"},
		{"schema not object", `{"type":"object","properties":{"a":"string"}}`, "/properties/a", "JSON object"},
		{"too deep", `{"type":"object","properties":{"a":{"type":"object","properties":{"b":{"type":"object","properties":{"c":{"type":"object"}}}}}}}`, "/properties/a/properties/b/properties/c", "depth"},
		// Arrays.
		{"array without maxItems", `{"type":"object","properties":{"a":{"type":"array","items":{"type":"string"}}}}`, "/properties/a", "maxItems"},
		{"array maxItems too big", `{"type":"object","properties":{"a":{"type":"array","maxItems":101,"items":{"type":"string"}}}}`, "/properties/a/maxItems", "100"},
		{"array maxItems negative", `{"type":"object","properties":{"a":{"type":"array","maxItems":-1,"items":{"type":"string"}}}}`, "/properties/a/maxItems", "integer"},
		{"array maxItems fraction", `{"type":"object","properties":{"a":{"type":"array","maxItems":1.5,"items":{"type":"string"}}}}`, "/properties/a/maxItems", "integer"},
		{"array without items", `{"type":"object","properties":{"a":{"type":"array","maxItems":3}}}`, "/properties/a", "items"},
		{"tuple items", `{"type":"object","properties":{"a":{"type":"array","maxItems":3,"items":[{"type":"string"}]}}}`, "/properties/a/items", "JSON object"},
		{"boolean items", `{"type":"object","properties":{"a":{"type":"array","maxItems":3,"items":true}}}`, "/properties/a/items", "boolean schemas"},
		{"array of arrays", `{"type":"object","properties":{"a":{"type":"array","maxItems":3,"items":{"type":"array","maxItems":3,"items":{"type":"string"}}}}}`, "/properties/a/items/type", "primitive"},
		{"array of objects", `{"type":"object","properties":{"a":{"type":"array","maxItems":3,"items":{"type":"object"}}}}`, "/properties/a/items/type", "primitive"},
		{"array enum", `{"type":"object","properties":{"a":{"type":"array","maxItems":3,"items":{"type":"string"},"enum":[["x"]]}}}`, "/properties/a/enum", "enum"},
		// Strings.
		{"unanchored pattern", `{"type":"object","properties":{"s":{"type":"string","pattern":"[0-9]+"}}}`, "/properties/s/pattern", "anchored"},
		{"no end anchor", `{"type":"object","properties":{"s":{"type":"string","pattern":"^[0-9]+"}}}`, "/properties/s/pattern", "anchored"},
		{"no start anchor", `{"type":"object","properties":{"s":{"type":"string","pattern":"[0-9]+$"}}}`, "/properties/s/pattern", "anchored"},
		{"top-level alternation", `{"type":"object","properties":{"s":{"type":"string","pattern":"^a|b$"}}}`, "/properties/s/pattern", "alternation"},
		{"anchored alternatives", `{"type":"object","properties":{"s":{"type":"string","pattern":"^a$|^b$"}}}`, "/properties/s/pattern", "alternation"},
		{"escaped dollar", `{"type":"object","properties":{"s":{"type":"string","pattern":"^abc\\$"}}}`, "/properties/s/pattern", "anchored"},
		{"multiline flag", `{"type":"object","properties":{"s":{"type":"string","pattern":"^(?m)a$"}}}`, "/properties/s/pattern", "anchored"},
		{"invalid regexp", `{"type":"object","properties":{"s":{"type":"string","pattern":"^(a$"}}}`, "/properties/s/pattern", "regular expression"},
		{"pattern not string", `{"type":"object","properties":{"s":{"type":"string","pattern":1}}}`, "/properties/s/pattern", "string"},
		{"minLength > maxLength", `{"type":"object","properties":{"s":{"type":"string","minLength":5,"maxLength":4}}}`, "/properties/s/minLength", "maxLength"},
		{"minLength over forced max", `{"type":"object","properties":{"s":{"type":"string","minLength":300}}}`, "/properties/s/minLength", "maxLength"},
		{"maxLength too big", `{"type":"object","properties":{"s":{"type":"string","maxLength":4097}}}`, "/properties/s/maxLength", "4096"},
		{"maxLength negative", `{"type":"object","properties":{"s":{"type":"string","maxLength":-1}}}`, "/properties/s/maxLength", "integer"},
		{"maxLength fraction", `{"type":"object","properties":{"s":{"type":"string","maxLength":2.5}}}`, "/properties/s/maxLength", "integer"},
		{"minLength string", `{"type":"object","properties":{"s":{"type":"string","minLength":"1"}}}`, "/properties/s/minLength", "integer"},
		{"format not string", `{"type":"object","properties":{"s":{"type":"string","format":1}}}`, "/properties/s/format", "string"},
		// Enums.
		{"enum not array", `{"type":"object","properties":{"s":{"type":"string","enum":"a"}}}`, "/properties/s/enum", "array"},
		{"enum empty", `{"type":"object","properties":{"s":{"type":"string","enum":[]}}}`, "/properties/s/enum", "at least one"},
		{"enum type mismatch", `{"type":"object","properties":{"s":{"type":"string","enum":["a",1]}}}`, "/properties/s/enum/1", "string"},
		{"integer enum fraction", `{"type":"object","properties":{"n":{"type":"integer","enum":[1,1.5]}}}`, "/properties/n/enum/1", "integer"},
		{"number enum string", `{"type":"object","properties":{"n":{"type":"number","enum":["1"]}}}`, "/properties/n/enum/0", "number"},
		{"enum duplicate", `{"type":"object","properties":{"s":{"type":"string","enum":["a","a"]}}}`, "/properties/s/enum/1", "duplicate"},
		{"enum fails pattern", `{"type":"object","properties":{"s":{"type":"string","enum":["a","B"],"pattern":"^[a-z]$"}}}`, "/properties/s/enum/1", "pattern"},
		{"enum fails bounds", `{"type":"object","properties":{"n":{"type":"integer","enum":[1,20],"maximum":10}}}`, "/properties/n/enum/1", "maximum"},
		{"enum on boolean", `{"type":"object","properties":{"b":{"type":"boolean","enum":[true]}}}`, "/properties/b/enum", "not supported"},
		// Numbers.
		{"minimum > maximum", `{"type":"object","properties":{"n":{"type":"number","minimum":5,"maximum":4}}}`, "/properties/n/minimum", "maximum"},
		{"minimum not number", `{"type":"object","properties":{"n":{"type":"number","minimum":"1"}}}`, "/properties/n/minimum", "number"},
		{"string keyword on number", `{"type":"object","properties":{"n":{"type":"number","maxLength":3}}}`, "/properties/n/maxLength", "not supported"},
		{"number keyword on string", `{"type":"object","properties":{"s":{"type":"string","minimum":3}}}`, "/properties/s/minimum", "not supported"},
		{"pattern on integer", `{"type":"object","properties":{"n":{"type":"integer","pattern":"^1$"}}}`, "/properties/n/pattern", "not supported"},
		// Annotations.
		{"title not string", `{"type":"object","title":1}`, "/title", "string"},
		{"description not string", `{"type":"object","properties":{"a":{"type":"string","description":[]}}}`, "/properties/a/description", "string"},
		{"examples not array", `{"type":"object","properties":{"a":{"type":"string","examples":"x"}}}`, "/properties/a/examples", "array"},
		// Defaults must validate.
		{"default wrong type", `{"type":"object","properties":{"a":{"type":"string","default":1}}}`, "/properties/a/default", "string"},
		{"default too long", `{"type":"object","properties":{"a":{"type":"string","maxLength":2,"default":"abc"}}}`, "/properties/a/default", "longer"},
		{"default too long forced", `{"type":"object","properties":{"a":{"type":"string","default":"` + strings.Repeat("x", 257) + `"}}}`, "/properties/a/default", "longer"},
		{"default not in enum", `{"type":"object","properties":{"a":{"type":"string","enum":["x"],"default":"y"}}}`, "/properties/a/default", "one of"},
		{"default fraction", `{"type":"object","properties":{"a":{"type":"integer","default":1.5}}}`, "/properties/a/default", "integer"},
		{"default out of bounds", `{"type":"object","properties":{"a":{"type":"integer","minimum":2,"default":1}}}`, "/properties/a/default", "minimum"},
		{"default pattern", `{"type":"object","properties":{"a":{"type":"string","pattern":"^[a-z]+$","default":"A"}}}`, "/properties/a/default", "pattern"},
		{"default array too long", `{"type":"object","properties":{"a":{"type":"array","maxItems":1,"items":{"type":"string"},"default":["a","b"]}}}`, "/properties/a/default", "at most 1"},
		{"default object unknown", `{"type":"object","properties":{"a":{"type":"string"}},"default":{"b":"x"}}`, "/default", "unknown property"},
		{"default object missing", `{"type":"object","properties":{"a":{"type":"string"}},"required":["a"],"default":{}}`, "/default", "missing required"},
		{"default null", `{"type":"object","properties":{"a":{"type":"string","default":null}}}`, "/properties/a/default", "string"},
		// x-piworker-allow.
		{"allow on number", `{"type":"object","properties":{"n":{"type":"number","x-piworker-allow":["-x"]}}}`, "/properties/n/x-piworker-allow", "string"},
		{"allow unknown probe", `{"type":"object","properties":{"s":{"type":"string","x-piworker-allow":["rm"]}}}`, "/properties/s/x-piworker-allow/0", "probe"},
		{"allow not array", `{"type":"object","properties":{"s":{"type":"string","x-piworker-allow":"-x"}}}`, "/properties/s/x-piworker-allow", "array"},
		{"allow entry not string", `{"type":"object","properties":{"s":{"type":"string","x-piworker-allow":[1]}}}`, "/properties/s/x-piworker-allow/0", "probe"},
		{"allow at root", `{"type":"object","x-piworker-allow":["-x"]}`, "/x-piworker-allow", "string"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issues := compileIssues(t, c.raw)
			assert.Truef(t, hasIssueAt(issues, c.pointer, c.want), "want an issue at %q containing %q, got %v", c.pointer, c.want, issues)
		})
	}
}

func TestCompileCaps(t *testing.T) {
	t.Run("raw size", func(t *testing.T) {
		desc := strings.Repeat("x", MaxSchemaBytes)
		issues := compileIssues(t, `{"type":"object","description":"`+desc+`"}`)
		assert.True(t, hasIssueAt(issues, "", "64 KiB"), "%v", issues)
	})
	t.Run("enum values", func(t *testing.T) {
		values := make([]string, MaxEnumValues+1)
		for i := range values {
			values[i] = fmt.Sprintf("%q", fmt.Sprint("v", i))
		}
		issues := compileIssues(t, `{"type":"object","properties":{"s":{"type":"string","enum":[`+strings.Join(values, ",")+`]}}}`)
		assert.True(t, hasIssueAt(issues, "/properties/s/enum", "256"), "%v", issues)
	})
	t.Run("enum values at the cap", func(t *testing.T) {
		values := make([]string, MaxEnumValues)
		for i := range values {
			values[i] = fmt.Sprintf("%q", fmt.Sprint("v", i))
		}
		mustCompile(t, `{"type":"object","properties":{"s":{"type":"string","enum":[`+strings.Join(values, ",")+`]}}}`)
	})
	t.Run("properties per object", func(t *testing.T) {
		props := make([]string, MaxProperties+1)
		for i := range props {
			props[i] = fmt.Sprintf(`"p%d":{"type":"boolean"}`, i)
		}
		issues := compileIssues(t, `{"type":"object","properties":{`+strings.Join(props, ",")+`}}`)
		assert.True(t, hasIssueAt(issues, "/properties", "64"), "%v", issues)
		issues = compileIssues(t, `{"type":"object","properties":{"o":{"type":"object","properties":{`+strings.Join(props, ",")+`}}}}`)
		assert.True(t, hasIssueAt(issues, "/properties/o/properties", "64"), "%v", issues)
	})
	t.Run("pathological nesting", func(t *testing.T) {
		raw := strings.Repeat(`{"type":"object","properties":{"a":`, 2000) + `{"type":"string"}` + strings.Repeat(`}}`, 2000)
		_, err := Compile(json.RawMessage(raw))
		require.Error(t, err)
	})
	t.Run("pathological json nesting", func(t *testing.T) {
		raw := `{"type":"object","examples":` + strings.Repeat("[", 5000) + strings.Repeat("]", 5000) + `}`
		_, err := Compile(json.RawMessage(raw))
		require.Error(t, err)
	})
	t.Run("long property name", func(t *testing.T) {
		issues := compileIssues(t, `{"type":"object","properties":{"`+strings.Repeat("n", MaxPropertyNameLength+1)+`":{"type":"string"}}}`)
		assert.NotEmpty(t, issues)
	})
	t.Run("long pattern", func(t *testing.T) {
		issues := compileIssues(t, `{"type":"object","properties":{"s":{"type":"string","pattern":"^`+strings.Repeat("a", MaxPatternLength)+`$"}}}`)
		assert.True(t, hasIssueAt(issues, "/properties/s/pattern", "long"), "%v", issues)
	})
}

// Every problem is reported, not only the first, and the error text names
// the pointers.
func TestCompileReportsEveryIssue(t *testing.T) {
	issues := compileIssues(t, `{"type":"object","properties":{"a":{"type":"string","pattern":"x"},"b":{"anyOf":[]},"c":{"type":"array"}}}`)
	assert.True(t, hasIssueAt(issues, "/properties/a/pattern", "anchored"), "%v", issues)
	assert.True(t, hasIssueAt(issues, "/properties/b/anyOf", "anyOf"), "%v", issues)
	assert.True(t, hasIssueAt(issues, "/properties/c", "maxItems"), "%v", issues)

	_, err := Compile(json.RawMessage(`{"type":"object","properties":{"b":{"anyOf":[]}}}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/properties/b/anyOf")
}

func TestCompileIssuesAreSorted(t *testing.T) {
	issues := compileIssues(t, `{"type":"object","properties":{"z":{"type":"string","pattern":"x"},"a":{"anyOf":[]},"m":{"type":"array"}}}`)
	for i := 1; i < len(issues); i++ {
		assert.LessOrEqual(t, issues[i-1].Pointer, issues[i].Pointer, "%v", issues)
	}
}

func TestJSONIsCanonical(t *testing.T) {
	a := mustCompile(t, `{"type":"object","required":["b","a"],"properties":{"b":{"type":"integer","maximum":1e2,"minimum":0},"a":{"type":"string"}}}`)
	b := mustCompile(t, `{"properties":{"a":{"type":"string"},"b":{"minimum":0,"type":"integer","maximum":100.0}},"required":["a","b"],"additionalProperties":false,"type":"object"}`)
	assert.Equal(t, string(a.JSON()), string(b.JSON()))
	assert.JSONEq(t, `{
		"type":"object","additionalProperties":false,"required":["a","b"],
		"properties":{
			"a":{"type":"string","maxLength":256},
			"b":{"type":"integer","minimum":0,"maximum":100}
		}}`, string(a.JSON()))

	// Compiling the advertised schema again yields the same bytes.
	again := mustCompile(t, string(a.JSON()))
	assert.Equal(t, string(a.JSON()), string(again.JSON()))
}

func TestJSONForcesClosedObjectsAndBoundedStrings(t *testing.T) {
	s := mustCompile(t, `{"type":"object","properties":{
		"o":{"type":"object","properties":{"in":{"type":"object","properties":{"s":{"type":"string"}}}}},
		"tags":{"type":"array","maxItems":3,"items":{"type":"string"}},
		"e":{"type":"string","enum":["x"]},
		"long":{"type":"string","maxLength":1000}
	}}`)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(s.JSON(), &doc))
	assert.Equal(t, false, doc["additionalProperties"])
	props := doc["properties"].(map[string]any)
	o := props["o"].(map[string]any)
	assert.Equal(t, false, o["additionalProperties"])
	in := o["properties"].(map[string]any)["in"].(map[string]any)
	assert.Equal(t, false, in["additionalProperties"])
	assert.Equal(t, float64(256), in["properties"].(map[string]any)["s"].(map[string]any)["maxLength"])
	assert.Equal(t, float64(256), props["tags"].(map[string]any)["items"].(map[string]any)["maxLength"])
	assert.Equal(t, float64(256), props["e"].(map[string]any)["maxLength"])
	assert.Equal(t, float64(1000), props["long"].(map[string]any)["maxLength"])
}

func TestJSONStripsAllowAndKeepsAnnotations(t *testing.T) {
	s := mustCompile(t, `{"type":"object","title":"T","description":"D","properties":{
		"p":{"type":"string","pattern":"^[a-z/]+$","x-piworker-allow":["/"],"format":"uri-reference","examples":["a/b"],"default":"a"}
	}}`)
	out := string(s.JSON())
	assert.NotContains(t, out, "x-piworker-allow")
	assert.JSONEq(t, `{"type":"object","additionalProperties":false,"title":"T","description":"D","properties":{
		"p":{"type":"string","pattern":"^[a-z/]+$","format":"uri-reference","examples":["a/b"],"default":"a","maxLength":256}
	}}`, out)
}

func TestJSONReturnsACopy(t *testing.T) {
	s := mustCompile(t, `{"type":"object"}`)
	first := s.JSON()
	first[0] = 'X'
	assert.Equal(t, byte('{'), s.JSON()[0])
}

// Patterns are advertised to MCP clients, which read them as ECMA-262
// (JavaScript) regular expressions: only the syntax Go's RE2 and JavaScript
// (with the u flag) share is accepted.
func TestCompilePatternsArePortable(t *testing.T) {
	compilePattern := func(t *testing.T, pattern string) error {
		t.Helper()
		raw, err := json.Marshal(map[string]any{
			"type":       "object",
			"properties": map[string]any{"s": map[string]any{"type": "string", "pattern": pattern}},
		})
		require.NoError(t, err)
		_, err = Compile(raw)
		return err
	}
	for _, pattern := range []string{
		`^[a-z][a-z0-9_]*$`,
		`^[a-z\-]+$`,
		`^[-a-z]+$`,
		`^\p{L}+$`,
		`^\P{Lu}$`,
		`^[\p{L}\d_]+$`,
		`^a{2,3}$`,
		`^a{2}$`,
		`^a{2,}$`,
		`^a{2,3}?$`,
		`^(?:a|b)$`,
		`^\$\.\*\+\?\(\)\[\]\{\}\|\/\\\^$`,
		`^\x41\t\n\r\f\v$`,
		`^[^\]]+$`,
		`^[\[]$`,
		`^[\^a]$`,
		`^\d\D\w\W\s\S$`,
		`^\bfoo\b\B$`,
		`^a*?b+?c??$`,
		`^é+$`,
		`^[\x41-\x5A]$`,
		`^[{}()|$^.*+?/]$`,
		`^[\d-]$`,
		`^[a&&b]$`,
		`^[\w-]$`,
		`^[\w\-.]$`,
		`^[a-z-\d]$`,
	} {
		t.Run("accepts "+pattern, func(t *testing.T) {
			assert.NoError(t, compilePattern(t, pattern))
		})
	}
	for pattern, want := range map[string]string{
		`^(?i)[a-z]+$`:   "flags",
		`^(?i:a)$`:       "flags",
		`^(?s)a.*$`:      "flags",
		`^(?U)a*$`:       "flags",
		`^(?P<n>a)$`:     "named groups",
		`^(?<n>a)$`:      "named groups",
		`^a\z$`:          `\z`,
		`^\Aa$`:          `\A`,
		`^\Qa.b\E$`:      `\Q`,
		`^\pL+$`:         `\p{`,
		`^[\pL]$`:        `\p{`,
		`^\p{Greek}$`:    "Greek",
		`^[\p{Greek}]$`:  "Greek",
		`^\p{^L}$`:       "^L",
		`^\p{Any}$`:      "Any",
		`^[[:alpha:]]+$`: `\[`,
		`^[[]$`:          `\[`,
		`^a\-b$`:         `\-`,
		`^\x{41}$`:       `\x`,
		`^\a$`:           `\a`,
		`^\_$`:           `\_`,
		`^\#$`:           `\#`,
		`^\101$`:         `\1`,
		`^\0$`:           `\0`,
		`^[\a]$`:         `\a`,
		`^a{,3}$`:        `\{`,
		`^a{$`:           `\{`,
		`^a}$`:           `\}`,
		`^a]$`:           `\]`,
		`^[]a]$`:         `\]`,
		`^[^]a]$`:        `\]`,
		`^a\b+$`:         "quantifier",
		`^^*a$`:          "quantifier",
		`^a$?$`:          "quantifier",
		`^a\B{2}$`:       "quantifier",
		`^[\w-.]$`:       `\-`,
		`^[^a\d-z]$`:     `\-`,
		`^[\p{L}-/]$`:    `\-`,
	} {
		t.Run("rejects "+pattern, func(t *testing.T) {
			err := compilePattern(t, pattern)
			var ce *Error
			require.ErrorAs(t, err, &ce)
			require.Len(t, ce.Issues, 1, "%v", ce.Issues)
			assert.Equal(t, "/properties/s/pattern", ce.Issues[0].Pointer)
			assert.Contains(t, ce.Issues[0].Message, "JavaScript")
			assert.Contains(t, ce.Issues[0].Message, want)
		})
	}
}
