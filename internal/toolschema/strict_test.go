package toolschema

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeCorpus(t *testing.T) {
	assert.Equal(t, []string{"-x", "--a=b", "..", "/", " ", "\n", "$(", ";", "|", "`"}, Probes())
	p := Probes()
	p[0] = "changed"
	assert.Equal(t, "-x", Probes()[0], "Probes returns a copy")
}

const strictSchema = `{
	"type": "object",
	"properties": {
		"color":   {"type": "string", "enum": ["red", "green"]},
		"slug":    {"type": "string", "pattern": "^[a-z][a-z0-9_]*$"},
		"free":    {"type": "string"},
		"loose":   {"type": "string", "pattern": "^.*$"},
		"dashy":   {"type": "string", "pattern": "^[a-z-]+$"},
		"acked":   {"type": "string", "pattern": "^[a-z-]+$", "x-piworker-allow": ["-x"]},
		"path":    {"type": "string", "pattern": "^[a-z./]+$", "x-piworker-allow": ["/", ".."]},
		"nosemi":  {"type": "string", "pattern": "^[^;|]*$"},
		"bounded": {"type": "integer", "minimum": 0, "maximum": 10},
		"lower":   {"type": "number", "minimum": 0},
		"upper":   {"type": "integer", "maximum": 5},
		"open":    {"type": "number"},
		"level":   {"type": "integer", "enum": [1, 2]},
		"flag":    {"type": "boolean"},
		"tags":    {"type": "array", "maxItems": 3, "items": {"type": "string"}},
		"ids":     {"type": "array", "maxItems": 3, "items": {"type": "integer", "minimum": 1, "maximum": 9}},
		"opts":    {"type": "object", "properties": {
			"host": {"type": "string"},
			"port": {"type": "integer", "minimum": 1, "maximum": 65535},
			"in":   {"type": "object", "properties": {"x": {"type": "string", "enum": ["a"]}}}
		}}
	}
}`

func TestRequireStrictForEveryLeaf(t *testing.T) {
	s := mustCompile(t, strictSchema)
	for _, paths := range [][]string{nil, {}} {
		got := issuesByPointer(s.RequireStrictFor(paths))
		assert.ElementsMatch(t,
			[]string{"/free", "/loose", "/dashy", "/nosemi", "/lower", "/upper", "/open", "/tags", "/opts/host"},
			keys(got), "%v", got)

		assert.Contains(t, got["/free"], "enum or an anchored pattern")
		assert.Contains(t, got["/lower"], "minimum and maximum")
		assert.Contains(t, got["/upper"], "minimum and maximum")
		assert.Contains(t, got["/open"], "minimum and maximum")
		assert.Contains(t, got["/tags"], "items")
		assert.Contains(t, got["/opts/host"], "enum or an anchored pattern")

		// The message names the probes the pattern accepts.
		assert.Contains(t, got["/dashy"], strconv.Quote("-x"))
		assert.NotContains(t, got["/dashy"], strconv.Quote(".."))
		assert.Contains(t, got["/nosemi"], strconv.Quote("\n"))
		assert.Contains(t, got["/nosemi"], strconv.Quote("$("))
		assert.NotContains(t, got["/nosemi"], strconv.Quote(";"))
		for _, probe := range Probes() {
			if probe != "\n" { // ^.*$ does not match a line break
				assert.Contains(t, got["/loose"], strconv.Quote(probe))
			}
		}
		assert.Contains(t, got["/loose"], "x-piworker-allow")
	}
}

func TestRequireStrictForSelectedPaths(t *testing.T) {
	s := mustCompile(t, strictSchema)
	assert.Empty(t, s.RequireStrictFor([]string{"/slug", "/color", "/bounded", "/level", "/flag", "/acked", "/path", "/ids"}))

	got := issuesByPointer(s.RequireStrictFor([]string{"/free", "/slug"}))
	assert.Equal(t, []string{"/free"}, keys(got))

	// An object selects every leaf below it; the root selects everything.
	got = issuesByPointer(s.RequireStrictFor([]string{"/opts"}))
	assert.Equal(t, []string{"/opts/host"}, keys(got))
	assert.Len(t, s.RequireStrictFor([]string{""}), 9)

	// Array elements select the array's items.
	got = issuesByPointer(s.RequireStrictFor([]string{"/tags/0"}))
	assert.Equal(t, []string{"/tags"}, keys(got))

	// Duplicates are reported once.
	assert.Len(t, s.RequireStrictFor([]string{"/free", "/free", ""}), 9)
}

func TestRequireStrictForUnknownPaths(t *testing.T) {
	s := mustCompile(t, strictSchema)
	for _, path := range []string{"/nope", "/opts/nope", "/slug/x", "free", "/flag/0"} {
		got := issuesByPointer(s.RequireStrictFor([]string{path}))
		require.Containsf(t, got, path, "%v", got)
		assert.Contains(t, got[path], "not a property")
	}
}

func TestRequireStrictForEscapedNames(t *testing.T) {
	s := mustCompile(t, `{"type":"object","properties":{"a/b":{"type":"string"},"m~n":{"type":"string","enum":["x"]}}}`)
	got := issuesByPointer(s.RequireStrictFor(nil))
	assert.Equal(t, []string{"/a~1b"}, keys(got))
	assert.Len(t, s.RequireStrictFor([]string{"/a~1b"}), 1)
	assert.Empty(t, s.RequireStrictFor([]string{"/m~0n"}))
}

func TestRequireStrictForAllowanceIsPerProbe(t *testing.T) {
	s := mustCompile(t, `{"type":"object","properties":{"p":{"type":"string","pattern":"^[a-z./-]+$","x-piworker-allow":["/"]}}}`)
	got := issuesByPointer(s.RequireStrictFor(nil))
	msg := got["/p"]
	assert.NotContains(t, msg, strconv.Quote("/")+",")
	for _, probe := range []string{"-x", ".."} {
		assert.Contains(t, msg, strconv.Quote(probe))
	}
	assert.Equal(t, 1, strings.Count(msg, "accepts"))
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
