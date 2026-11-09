package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/toolschema"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleArgSchema exercises every kind of argSchema.
func sampleArgSchema() *argSchema {
	pos := object("A position.", prop("x", number("X.")), prop("y", number("Y."))).require("x", "y")
	return object("Sample.",
		prop("name", text("A name.", 1, 5)),
		prop("id", pattern("An id.", `^[a-z]{1,8}$`, 8)),
		prop("kind", enum("A kind.", "a", "b")),
		prop("count", integer("A count.", 1, 10)),
		prop("on", boolean("A switch.")),
		prop("at", pos),
		prop("tags", array("Tags.", text("A tag.", 0, 3), 2)),
		prop("config", freeObject("Free-form.")),
		prop("payload", anyValue("Any JSON value.")),
		prop("a~b/c", boolean("An odd name.")),
	).require("name")
}

// The advertised JSON is a JSON Schema clients read as the checks do:
// closed objects, one type per schema, the bounds that are enforced.
func TestArgSchemaJSON(t *testing.T) {
	data := sampleArgSchema().JSON()
	var s jsonschema.Schema
	require.NoError(t, json.Unmarshal(data, &s))
	_, err := s.Resolve(nil)
	require.NoError(t, err, "a valid JSON Schema")
	assert.JSONEq(t, `{"type":"object","description":"Sample.","additionalProperties":false,"required":["name"],"properties":{
		"name":{"type":"string","description":"A name.","minLength":1,"maxLength":5},
		"id":{"type":"string","description":"An id.","pattern":"^[a-z]{1,8}$","maxLength":8},
		"kind":{"type":"string","description":"A kind.","enum":["a","b"]},
		"count":{"type":"integer","description":"A count.","minimum":1,"maximum":10},
		"on":{"type":"boolean","description":"A switch."},
		"at":{"type":"object","description":"A position.","additionalProperties":false,"required":["x","y"],
			"properties":{"x":{"type":"number","description":"X."},"y":{"type":"number","description":"Y."}}},
		"tags":{"type":"array","description":"Tags.","maxItems":2,"items":{"type":"string","description":"A tag.","maxLength":3}},
		"config":{"type":"object","description":"Free-form."},
		"payload":{"description":"Any JSON value."},
		"a~b/c":{"type":"boolean","description":"An odd name."}}}`, string(data))
	assert.Equal(t, string(data), string(sampleArgSchema().JSON()), "deterministic")
}

func decodeJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(s), &m))
	return m
}

// check reports every problem with its argument's pointer, and never
// quotes a value.
func TestArgSchemaCheck(t *testing.T) {
	s := sampleArgSchema()
	assert.Empty(t, s.check(decodeJSON(t, `{"name":"ab","id":"abc","kind":"b","count":3,"on":true,"at":{"x":1.5,"y":-2},
		"tags":["x","yz"],"config":{"anything":{"deep":[null,1]}},"payload":[1,"x",null],"a~b/c":false}`)))
	assert.Empty(t, s.check(decodeJSON(t, `{"name":"héllo"}`)), "lengths count characters")
	assert.Empty(t, s.check(decodeJSON(t, `{"name":"x","payload":null}`)), "any value, null included")

	for _, tc := range []struct {
		args string
		want []toolschema.Issue
	}{
		{`{}`, []toolschema.Issue{{Pointer: "/name", Message: "missing required property"}}},
		{`{"name":"x","extra":1,"Name":"y"}`, []toolschema.Issue{
			{Pointer: "/Name", Message: "unknown property"}, {Pointer: "/extra", Message: "unknown property"}}},
		{`{"name":7}`, []toolschema.Issue{{Pointer: "/name", Message: "expected string, got number"}}},
		{`{"name":""}`, []toolschema.Issue{{Pointer: "/name", Message: "must be at least 1 characters"}}},
		{`{"name":"secret-value"}`, []toolschema.Issue{{Pointer: "/name", Message: "must be at most 5 characters"}}},
		{`{"name":"x","id":"ABC"}`, []toolschema.Issue{{Pointer: "/id", Message: "must match the pattern ^[a-z]{1,8}$"}}},
		{`{"name":"x","kind":"c"}`, []toolschema.Issue{{Pointer: "/kind", Message: "must be one of: a, b"}}},
		{`{"name":"x","count":2.5}`, []toolschema.Issue{{Pointer: "/count", Message: "expected integer, got number"}}},
		{`{"name":"x","count":11}`, []toolschema.Issue{{Pointer: "/count", Message: "must be from 1 to 10"}}},
		{`{"name":"x","on":"true"}`, []toolschema.Issue{{Pointer: "/on", Message: "expected boolean, got string"}}},
		{`{"name":"x","at":{"x":1}}`, []toolschema.Issue{{Pointer: "/at/y", Message: "missing required property"}}},
		{`{"name":"x","at":[1,2]}`, []toolschema.Issue{{Pointer: "/at", Message: "expected object, got array"}}},
		{`{"name":"x","tags":["a","b","c"]}`, []toolschema.Issue{{Pointer: "/tags", Message: "has 3 items, at most 2 allowed (maxItems)"}}},
		{`{"name":"x","tags":["a","long"]}`, []toolschema.Issue{{Pointer: "/tags/1", Message: "must be at most 3 characters"}}},
		{`{"name":"x","config":"x"}`, []toolschema.Issue{{Pointer: "/config", Message: "expected object, got string"}}},
		{`{"name":"x","config":null}`, []toolschema.Issue{{Pointer: "/config", Message: "expected object, got null"}}},
		{`{"name":"x","a~b/c":1}`, []toolschema.Issue{{Pointer: "/a~0b~1c", Message: "expected boolean, got number"}}},
	} {
		got := s.check(decodeJSON(t, tc.args))
		assert.Equal(t, tc.want, got, tc.args)
		assert.NotContains(t, issuesText(got), "secret-value", "values are never quoted")
	}

	// An unknown property's name is shown shortened.
	long := strings.Repeat("k", 300)
	got := s.check(map[string]any{"name": "x", long: 1})
	require.Len(t, got, 1)
	assert.Less(t, len(got[0].Pointer), 100)
}

// check stops after maxIssues problems, saying so.
func TestArgSchemaCheckIsBounded(t *testing.T) {
	s := object("Many.", prop("list", array("L.", boolean("B."), 500)))
	list := make([]any, 500)
	for i := range list {
		list[i] = "not a boolean"
	}
	got := s.check(map[string]any{"list": list})
	require.Len(t, got, maxIssues+1)
	assert.Contains(t, got[maxIssues].Message, "too many problems")
}

func issuesText(issues []toolschema.Issue) string {
	var b strings.Builder
	for _, i := range issues {
		b.WriteString(i.String())
		b.WriteByte('\n')
	}
	return b.String()
}
