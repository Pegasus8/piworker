package mcpserver

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Pegasus8/piworker/internal/toolschema"
)

// argSchema describes the arguments of a built-in tool whose input does not
// fit toolschema's closed subset (RawInput): it is both advertised (JSON)
// and enforced (check), so the two never drift apart. It uses only what
// clients and the checks read alike: one type per schema (or none, for any
// JSON value), closed objects unless free-form, and the bounds check
// enforces. Messages never quote argument values.
type argSchema struct {
	typ         string // object, array, string, integer, number, boolean; "" any JSON value
	description string

	// object
	props    []argProp
	required []string
	freeForm bool // any members of any type

	// array
	items    *argSchema
	maxItems int

	// string
	minLength, maxLength int
	pattern              *regexp.Regexp
	enum                 []string

	// integer
	minimum, maximum int
}

type argProp struct {
	name   string
	schema *argSchema
}

// maxIssues caps the problems check reports, as toolschema does.
const maxIssues = 64

func prop(name string, s *argSchema) argProp { return argProp{name: name, schema: s} }

// object is a closed object of props.
func object(description string, props ...argProp) *argSchema {
	return &argSchema{typ: "object", description: description, props: props}
}

// require marks properties of an object as required.
func (s *argSchema) require(names ...string) *argSchema {
	s.required = append(s.required, names...)
	return s
}

// freeObject is an object with any members (node settings, mocks).
func freeObject(description string) *argSchema {
	return &argSchema{typ: "object", description: description, freeForm: true}
}

// anyValue is any JSON value, null included (payloads).
func anyValue(description string) *argSchema { return &argSchema{description: description} }

func text(description string, minLength, maxLength int) *argSchema {
	return &argSchema{typ: "string", description: description, minLength: minLength, maxLength: maxLength}
}

// pattern is a string matching re (anchored), at most maxLength characters.
func pattern(description, re string, maxLength int) *argSchema {
	return &argSchema{typ: "string", description: description, pattern: regexp.MustCompile(re), maxLength: maxLength}
}

func enum(description string, values ...string) *argSchema {
	return &argSchema{typ: "string", description: description, enum: values}
}

func integer(description string, minimum, maximum int) *argSchema {
	return &argSchema{typ: "integer", description: description, minimum: minimum, maximum: maximum}
}

func number(description string) *argSchema {
	return &argSchema{typ: "number", description: description}
}

func boolean(description string) *argSchema {
	return &argSchema{typ: "boolean", description: description}
}

func array(description string, items *argSchema, maxItems int) *argSchema {
	return &argSchema{typ: "array", description: description, items: items, maxItems: maxItems}
}

// prop returns the schema of an object's property name (nil when none).
func (s *argSchema) prop(name string) *argSchema {
	for _, p := range s.props {
		if p.name == name {
			return p.schema
		}
	}
	return nil
}

// JSON is the schema as clients see it (keys sorted: deterministic).
func (s *argSchema) JSON() []byte {
	data, err := marshalNoEscape(s.doc())
	if err != nil {
		panic(fmt.Sprintf("mcpserver: argument schema: %v", err)) // only maps, strings and numbers
	}
	return data
}

func (s *argSchema) doc() map[string]any {
	d := map[string]any{}
	if s.typ != "" {
		d["type"] = s.typ
	}
	if s.description != "" {
		d["description"] = s.description
	}
	switch s.typ {
	case "object":
		if s.freeForm {
			break
		}
		props := map[string]any{}
		for _, p := range s.props {
			props[p.name] = p.schema.doc()
		}
		d["properties"] = props
		d["additionalProperties"] = false
		if len(s.required) > 0 {
			d["required"] = s.required
		}
	case "array":
		d["items"] = s.items.doc()
		d["maxItems"] = s.maxItems
	case "string":
		if s.enum != nil {
			d["enum"] = s.enum
			break
		}
		if s.minLength > 0 {
			d["minLength"] = s.minLength
		}
		if s.pattern != nil {
			d["pattern"] = s.pattern.String()
		}
		d["maxLength"] = s.maxLength
	case "integer":
		d["minimum"], d["maximum"] = s.minimum, s.maximum
	}
	return d
}

// check returns every problem of v (decoded JSON) against the schema, at
// most maxIssues and a last one saying more were left out.
func (s *argSchema) check(v any) []toolschema.Issue {
	c := &argChecker{}
	c.value(s, v, "")
	return c.issues
}

type argChecker struct {
	issues []toolschema.Issue
	full   bool
}

func (c *argChecker) add(ptr, format string, args ...any) {
	if c.full {
		return
	}
	if len(c.issues) == maxIssues {
		c.issues = append(c.issues, toolschema.Issue{Message: fmt.Sprintf("too many problems: only the first %d are listed", maxIssues)})
		c.full = true
		return
	}
	c.issues = append(c.issues, toolschema.Issue{Pointer: ptr, Message: fmt.Sprintf(format, args...)})
}

func (c *argChecker) value(s *argSchema, v any, ptr string) {
	if c.full {
		return
	}
	switch s.typ {
	case "":
	case "object":
		c.object(s, v, ptr)
	case "array":
		a, ok := v.([]any)
		if !ok {
			c.add(ptr, "expected array, got %s", jsonKind(v))
			return
		}
		if len(a) > s.maxItems {
			c.add(ptr, "has %d items, at most %d allowed (maxItems)", len(a), s.maxItems)
			return
		}
		for i, item := range a {
			c.value(s.items, item, ptr+"/"+strconv.Itoa(i))
		}
	case "string":
		c.string(s, v, ptr)
	case "integer":
		n, ok := v.(float64)
		if !ok || n != math.Trunc(n) {
			c.add(ptr, "expected integer, got %s", jsonKind(v))
			return
		}
		if n < float64(s.minimum) || n > float64(s.maximum) {
			c.add(ptr, "must be from %d to %d", s.minimum, s.maximum)
		}
	case "number":
		if _, ok := v.(float64); !ok {
			c.add(ptr, "expected number, got %s", jsonKind(v))
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			c.add(ptr, "expected boolean, got %s", jsonKind(v))
		}
	}
}

func (c *argChecker) object(s *argSchema, v any, ptr string) {
	m, ok := v.(map[string]any)
	if !ok {
		c.add(ptr, "expected object, got %s", jsonKind(v))
		return
	}
	if s.freeForm {
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if s.prop(k) == nil {
			c.add(ptr+"/"+escapePointer(shorten(k, 64)), "unknown property")
		}
	}
	for _, name := range s.required {
		if _, ok := m[name]; !ok {
			c.add(ptr+"/"+escapePointer(name), "missing required property")
		}
	}
	for _, k := range keys {
		if child := s.prop(k); child != nil {
			c.value(child, m[k], ptr+"/"+escapePointer(k))
		}
	}
}

func (c *argChecker) string(s *argSchema, v any, ptr string) {
	str, ok := v.(string)
	if !ok {
		c.add(ptr, "expected string, got %s", jsonKind(v))
		return
	}
	if s.enum != nil {
		for _, e := range s.enum {
			if e == str {
				return
			}
		}
		c.add(ptr, "must be one of: %s", strings.Join(s.enum, ", "))
		return
	}
	switch n := utf8.RuneCountInString(str); {
	case n < s.minLength:
		c.add(ptr, "must be at least %d characters", s.minLength)
	case n > s.maxLength:
		c.add(ptr, "must be at most %d characters", s.maxLength)
	case s.pattern != nil && !s.pattern.MatchString(str):
		c.add(ptr, "must match the pattern %s", s.pattern.String())
	}
}

// jsonKind names the JSON type of a decoded value.
func jsonKind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}

// escapePointer escapes a JSON pointer token (RFC 6901).
func escapePointer(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}
