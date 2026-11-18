// Package toolschema compiles and enforces the closed JSON Schema subset that
// PiWorker accepts for the input of MCP tools (ADR-0013, docs/design/mcp.md
// §5). Agents supply data only; the schema is the contract that data must
// meet before a flow sees it, so the subset is small enough to be checked
// completely:
//
//   - the root is `type: object`; every object is closed
//     (`additionalProperties: false`, forced when absent) and has
//     `properties` and `required`; objects nest at most MaxDepth levels;
//   - a property has exactly one type: string, integer, number, boolean, an
//     array of one of those with a mandatory `maxItems`, or an object;
//   - strings: `enum`, `pattern` (anchored `^…$`, in the regular expression
//     syntax Go and JavaScript share), `minLength`, `maxLength`
//     (DefaultMaxLength when absent), `format` (an annotation only);
//   - integers and numbers: `minimum`, `maximum`, `enum`;
//   - `title`, `description`, `default` (must validate) and `examples`;
//   - `x-piworker-allow` on strings: probes of the corpus (see Probes) the
//     admin accepts, stripped from the schema advertised to clients.
//
// Everything else (`$ref`, `$defs`, composition, conditionals,
// `patternProperties`, `dependent*`, `unevaluated*`, boolean schemas,
// untyped schemas, type lists, `x-mcp-header`, unknown keywords…) is
// rejected with the JSON pointer of the offending keyword.
//
// This package must not import internal/node or internal/flow.
package toolschema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// Limits of the subset.
const (
	// MaxSchemaBytes caps the raw schema document.
	MaxSchemaBytes = 64 << 10
	// MaxDepth is how many object levels may nest, the root included.
	MaxDepth = 3
	// MaxProperties caps the properties of one object.
	MaxProperties = 64
	// MaxEnumValues caps an enum.
	MaxEnumValues = 256
	// MaxItemsLimit is the largest maxItems an array may declare.
	MaxItemsLimit = 100
	// DefaultMaxLength is forced on strings that declare no maxLength.
	DefaultMaxLength = 256
	// MaxLengthLimit is the largest maxLength a string may declare.
	MaxLengthLimit = 4096
	// MaxPatternLength caps a pattern's source.
	MaxPatternLength = 1024
	// MaxPropertyNameLength caps a property name.
	MaxPropertyNameLength = 128
)

// Issue is one problem, located by a JSON pointer: into the schema document
// for Compile, into the arguments for Validate, and the argument's property
// for RequireStrictFor.
type Issue struct {
	Pointer string `json:"pointer"`
	Message string `json:"message"`
}

// String renders the issue as "pointer: message" ("(root)" for "").
func (i Issue) String() string {
	ptr := i.Pointer
	if ptr == "" {
		ptr = "(root)"
	}
	return ptr + ": " + i.Message
}

// Error is returned by Compile for a schema outside the subset. It lists
// every problem found, sorted by pointer.
type Error struct {
	Issues []Issue
}

func (e *Error) Error() string {
	parts := make([]string, len(e.Issues))
	for i, issue := range e.Issues {
		parts[i] = issue.String()
	}
	return "invalid tool input schema: " + strings.Join(parts, "; ")
}

// Schema is a compiled tool input schema. It is immutable and safe for
// concurrent use.
type Schema struct {
	root      *node
	canonical []byte
	resolved  *jsonschema.Resolved
}

// node is one schema of the subset, already checked.
type node struct {
	typ         string // object, array, string, integer, number, boolean
	title       string
	description string
	hasDefault  bool
	def         any
	examples    []any

	// object
	props    map[string]*node
	required []string

	// array
	items    *node
	maxItems int

	// string
	enum      []any
	pattern   string
	re        *regexp.Regexp
	minLength int
	maxLength int
	format    string
	allow     []string

	// integer, number
	minimum *float64
	maximum *float64
}

// Compile checks raw against the subset and returns the compiled schema, or
// an *Error listing every problem.
func Compile(raw json.RawMessage) (*Schema, error) {
	if len(raw) > MaxSchemaBytes {
		return nil, &Error{Issues: []Issue{{Message: fmt.Sprintf("schema is larger than 64 KiB (%d bytes)", len(raw))}}}
	}
	doc, issue := decodeDocument(raw)
	if issue != nil {
		return nil, &Error{Issues: []Issue{*issue}}
	}

	c := &compiler{}
	root := c.schema(doc, "", 1, roleRoot)
	if len(c.issues) == 0 {
		c.checkDefaults(root, "")
	}
	if len(c.issues) > 0 {
		sort.SliceStable(c.issues, func(i, j int) bool { return c.issues[i].Pointer < c.issues[j].Pointer })
		return nil, &Error{Issues: c.issues}
	}

	canonical, err := marshalCanonical(root.canonical())
	if err != nil {
		return nil, &Error{Issues: []Issue{{Message: "schema cannot be encoded: " + err.Error()}}}
	}
	// jsonschema-go resolves the normalized schema as a second opinion and
	// validates arguments again after Validate (defence in depth).
	var js jsonschema.Schema
	if err := json.Unmarshal(canonical, &js); err != nil {
		return nil, &Error{Issues: []Issue{{Message: "schema cannot be resolved: " + err.Error()}}}
	}
	resolved, err := js.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true})
	if err != nil {
		return nil, &Error{Issues: []Issue{{Message: "schema cannot be resolved: " + err.Error()}}}
	}
	return &Schema{root: root, canonical: canonical, resolved: resolved}, nil
}

// JSON returns the normalized schema advertised to clients: canonical
// (sorted keys, normalized numbers), every object closed, every string
// bounded, x-piworker-allow removed. The caller owns the returned bytes.
func (s *Schema) JSON() json.RawMessage {
	return bytes.Clone(s.canonical)
}

type role int

const (
	roleRoot role = iota
	roleProperty
	roleItems
)

// Keywords accepted per type.
var (
	commonKeywords = []string{"type", "title", "description", "default", "examples"}
	typeKeywords   = map[string][]string{
		"object":  {"properties", "required", "additionalProperties"},
		"array":   {"items", "maxItems"},
		"string":  {"enum", "pattern", "minLength", "maxLength", "format", allowKeyword},
		"integer": {"enum", "minimum", "maximum"},
		"number":  {"enum", "minimum", "maximum"},
		"boolean": {},
	}
	// subsetKeywords are accepted for some type, just not every type.
	subsetKeywords = func() map[string]bool {
		m := map[string]bool{}
		for _, kws := range typeKeywords {
			for _, k := range kws {
				m[k] = true
			}
		}
		return m
	}()
)

// allowKeyword lists the probes a string property accepts on purpose.
const allowKeyword = "x-piworker-allow"

func keywordAllowed(typ, kw string) bool {
	return slices.Contains(commonKeywords, kw) || slices.Contains(typeKeywords[typ], kw)
}

func unsupportedKeyword(typ, kw string) string {
	switch {
	case kw == allowKeyword:
		return allowKeyword + " only applies to string properties"
	case subsetKeywords[kw]:
		return fmt.Sprintf("%s is not supported for type %s", quoteShort(kw), typ)
	case strings.HasPrefix(kw, "x-"):
		return fmt.Sprintf("extension keyword %s is not supported", quoteShort(kw))
	default:
		return fmt.Sprintf("keyword %s is not supported (only a closed subset of JSON Schema is accepted)", quoteShort(kw))
	}
}

// compiler collects the issues of one Compile.
type compiler struct {
	issues []Issue
}

func (c *compiler) add(ptr, format string, args ...any) {
	c.issues = append(c.issues, Issue{Pointer: ptr, Message: fmt.Sprintf(format, args...)})
}

// schema checks one schema object and returns its node, or nil when it is
// unusable (the reason is recorded).
func (c *compiler) schema(v any, ptr string, depth int, r role) *node {
	m, ok := v.(map[string]any)
	if !ok {
		if _, isBool := v.(bool); isBool {
			c.add(ptr, "boolean schemas are not supported: use an object with a type")
		} else {
			c.add(ptr, "must be a JSON object (a schema)")
		}
		return nil
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// Keywords outside the subset are reported whatever the type (even a
	// missing one); keywords of the subset are checked against the type below.
	before := len(c.issues)
	for _, k := range keys {
		if !slices.Contains(commonKeywords, k) && !subsetKeywords[k] {
			c.add(ptr+"/"+escapePointer(k), "%s", unsupportedKeyword("", k))
		}
	}

	rawType, ok := m["type"]
	if !ok {
		if r == roleRoot {
			c.add(ptr, `a type is required: the root schema must be "type": "object"`)
		} else {
			c.add(ptr, "a type is required: string, integer, number, boolean, array or object")
		}
		return nil
	}
	typ, ok := rawType.(string)
	if !ok {
		if _, isList := rawType.([]any); isList {
			c.add(ptr+"/type", "must be a single type, not a list of types")
		} else {
			c.add(ptr+"/type", "type must be a string")
		}
		return nil
	}
	if _, known := typeKeywords[typ]; !known {
		c.add(ptr+"/type", "unsupported type %s: use string, integer, number, boolean, array or object", quoteShort(typ))
		return nil
	}
	switch {
	case r == roleRoot && typ != "object":
		c.add(ptr+"/type", `the root schema's type must be "object"`)
		return nil
	case r == roleItems && (typ == "object" || typ == "array"):
		c.add(ptr+"/type", "array items must be a primitive type: string, integer, number or boolean")
		return nil
	case typ == "object" && depth > MaxDepth:
		c.add(ptr, "nesting depth exceeds %d object levels (the root included)", MaxDepth)
		return nil
	}

	n := &node{typ: typ}
	var rawRequired any
	requiredSet := false
	hasMaxItems, hasItems, hasMaxLength := false, false, false

	for _, k := range keys {
		kp := ptr + "/" + escapePointer(k)
		val := m[k]
		if !keywordAllowed(typ, k) {
			if subsetKeywords[k] {
				c.add(kp, "%s", unsupportedKeyword(typ, k))
			}
			continue
		}
		switch k {
		case "type":
		case "title":
			n.title = c.str(val, kp, k)
		case "description":
			n.description = c.str(val, kp, k)
		case "format":
			n.format = c.str(val, kp, k)
		case "default":
			n.hasDefault, n.def = true, normalize(val)
		case "examples":
			if a, ok := val.([]any); ok {
				n.examples = normalize(a).([]any)
			} else {
				c.add(kp, "examples must be an array")
			}
		case "properties":
			n.props = c.properties(val, kp, depth)
		case "required":
			rawRequired, requiredSet = val, true
		case "additionalProperties":
			if b, ok := val.(bool); !ok || b {
				c.add(kp, "additionalProperties must be false: tool arguments are closed")
			}
		case "items":
			hasItems = true
			n.items = c.schema(val, kp, depth, roleItems)
		case "maxItems":
			if i, ok := c.nonNegInt(val, kp, k); ok {
				hasMaxItems = true
				if i > MaxItemsLimit {
					c.add(kp, "maxItems must be at most %d", MaxItemsLimit)
				}
				n.maxItems = i
			}
		case "minLength":
			n.minLength, _ = c.nonNegInt(val, kp, k)
		case "maxLength":
			if i, ok := c.nonNegInt(val, kp, k); ok {
				hasMaxLength = true
				if i > MaxLengthLimit {
					c.add(kp, "maxLength must be at most %d", MaxLengthLimit)
				}
				n.maxLength = i
			}
		case "pattern":
			n.pattern, n.re = c.pattern(val, kp)
		case "minimum":
			n.minimum = c.number(val, kp, k)
		case "maximum":
			n.maximum = c.number(val, kp, k)
		case "enum":
			n.enum = c.enum(typ, val, kp)
		case allowKeyword:
			n.allow = c.allow(val, kp)
		}
	}

	switch typ {
	case "object":
		if n.props == nil {
			n.props = map[string]*node{}
		}
		if requiredSet {
			// Check against the declared names, not the properties that
			// compiled, so a broken property is reported once.
			declared, _ := m["properties"].(map[string]any)
			n.required = c.required(rawRequired, ptr+"/required", declared)
		}
	case "array":
		if !hasItems {
			c.add(ptr, "arrays need items: a primitive schema for their elements")
		}
		if !hasMaxItems {
			c.add(ptr, "arrays need maxItems (at most %d)", MaxItemsLimit)
		}
	case "string":
		if !hasMaxLength {
			n.maxLength = DefaultMaxLength
		}
		if n.minLength > n.maxLength {
			if hasMaxLength {
				c.add(ptr+"/minLength", "minLength must not exceed maxLength (%d)", n.maxLength)
			} else {
				c.add(ptr+"/minLength", "minLength exceeds the default maxLength of %d: set maxLength", DefaultMaxLength)
			}
		}
	case "integer", "number":
		if n.minimum != nil && n.maximum != nil && *n.minimum > *n.maximum {
			c.add(ptr+"/minimum", "minimum must not exceed maximum")
		}
	}

	if len(c.issues) > before {
		return nil
	}
	// Enum values must also meet the other constraints of their schema, or
	// they could be advertised but never accepted.
	for i, e := range n.enum {
		for _, issue := range validateValue(n, e, "", true) {
			c.add(fmt.Sprintf("%s/enum/%d", ptr, i), "enum value is invalid%s", describeAt(issue))
		}
	}
	if len(c.issues) > before {
		return nil
	}
	return n
}

// properties checks a properties object.
func (c *compiler) properties(v any, ptr string, depth int) map[string]*node {
	m, ok := v.(map[string]any)
	if !ok {
		c.add(ptr, "properties must be a JSON object")
		return nil
	}
	if len(m) > MaxProperties {
		c.add(ptr, "an object may declare at most %d properties (got %d)", MaxProperties, len(m))
		return nil
	}
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	props := make(map[string]*node, len(m))
	for _, name := range names {
		pp := ptr + "/" + escapePointer(name)
		switch {
		case name == "":
			c.add(pp, "property name must not be empty")
			continue
		case len(name) > MaxPropertyNameLength:
			c.add(pp, "property name is longer than %d bytes", MaxPropertyNameLength)
			continue
		}
		if child := c.schema(m[name], pp, depth+1, roleProperty); child != nil {
			props[name] = child
		}
	}
	return props
}

// required checks a required list against the declared properties.
func (c *compiler) required(v any, ptr string, declared map[string]any) []string {
	list, ok := v.([]any)
	if !ok {
		c.add(ptr, "required must be an array of property names")
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(list))
	for i, item := range list {
		ip := fmt.Sprintf("%s/%d", ptr, i)
		name, ok := item.(string)
		switch {
		case !ok:
			c.add(ip, "required entries must be strings")
		case seen[name]:
			c.add(ip, "duplicate required property %s", quoteShort(name))
		case !hasKey(declared, name):
			c.add(ip, "required property %s is not declared in properties", quoteShort(name))
		default:
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func (c *compiler) str(v any, ptr, kw string) string {
	s, ok := v.(string)
	if !ok {
		c.add(ptr, "%s must be a string", kw)
	}
	return s
}

// nonNegInt reads a non-negative whole number (2.0 counts).
func (c *compiler) nonNegInt(v any, ptr, kw string) (int, bool) {
	f, ok := jsonFloat(v)
	if !ok || f < 0 || f != math.Trunc(f) || f > math.MaxInt32 {
		c.add(ptr, "%s must be a non-negative integer", kw)
		return 0, false
	}
	return int(f), true
}

func (c *compiler) number(v any, ptr, kw string) *float64 {
	f, ok := jsonFloat(v)
	if !ok {
		c.add(ptr, "%s must be a number", kw)
		return nil
	}
	return &f
}

// pattern checks that a pattern compiles and is anchored: it starts with ^,
// ends with $, has no top-level alternation, and its parse tree begins at
// the start of the text and ends at its end (so flags such as (?m) or an
// escaped \$ cannot unanchor it). It must also use only the syntax Go and
// JavaScript share (portabilityProblem): clients read it as ECMA-262.
func (c *compiler) pattern(v any, ptr string) (string, *regexp.Regexp) {
	p, ok := v.(string)
	if !ok {
		c.add(ptr, "pattern must be a string")
		return "", nil
	}
	if len(p) > MaxPatternLength {
		c.add(ptr, "pattern is too long (at most %d bytes)", MaxPatternLength)
		return "", nil
	}
	re, err := regexp.Compile(p)
	if err != nil {
		c.add(ptr, "pattern is not a valid regular expression: %v", err)
		return "", nil
	}
	if msg := anchoringProblem(p); msg != "" {
		c.add(ptr, "%s", msg)
		return "", nil
	}
	if msg := portabilityProblem(p); msg != "" {
		c.add(ptr, "%s", msg)
		return "", nil
	}
	return p, re
}

func (c *compiler) enum(typ string, v any, ptr string) []any {
	list, ok := v.([]any)
	if !ok {
		c.add(ptr, "enum must be an array")
		return nil
	}
	switch {
	case len(list) == 0:
		c.add(ptr, "enum must list at least one value")
		return nil
	case len(list) > MaxEnumValues:
		c.add(ptr, "enum may list at most %d values (got %d)", MaxEnumValues, len(list))
		return nil
	}
	out := make([]any, 0, len(list))
	for i, item := range list {
		ip := fmt.Sprintf("%s/%d", ptr, i)
		var value any
		switch typ {
		case "string":
			s, ok := item.(string)
			if !ok {
				c.add(ip, "enum values of a string must be strings")
				continue
			}
			value = s
		case "integer", "number":
			f, ok := jsonFloat(item)
			if !ok {
				c.add(ip, "enum values of a %s must be numbers", typ)
				continue
			}
			if typ == "integer" && f != math.Trunc(f) {
				c.add(ip, "enum values of an integer must be integers")
				continue
			}
			value = f
		}
		if slices.Contains(out, value) {
			c.add(ip, "duplicate enum value")
			continue
		}
		out = append(out, value)
	}
	return out
}

func (c *compiler) allow(v any, ptr string) []string {
	list, ok := v.([]any)
	if !ok {
		c.add(ptr, "%s must be an array of probe strings", allowKeyword)
		return nil
	}
	out := make([]string, 0, len(list))
	for i, item := range list {
		s, ok := item.(string)
		if !ok || !slices.Contains(probes, s) {
			c.add(fmt.Sprintf("%s/%d", ptr, i), "must be a probe of the corpus: %s", probeList())
			continue
		}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// checkDefaults validates every default against its own schema.
func (c *compiler) checkDefaults(n *node, ptr string) {
	if n == nil {
		return
	}
	if n.hasDefault {
		for _, issue := range validateValue(n, n.def, "", false) {
			c.add(ptr+"/default", "default is invalid%s", describeAt(issue))
		}
	}
	names := make([]string, 0, len(n.props))
	for name := range n.props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		c.checkDefaults(n.props[name], ptr+"/properties/"+escapePointer(name))
	}
	c.checkDefaults(n.items, ptr+"/items")
}

// describeAt renders a validation issue of a default or enum value: the
// value's inner pointer (if any) and the message.
func describeAt(issue Issue) string {
	if issue.Pointer == "" {
		return ": " + issue.Message
	}
	return " at " + issue.Pointer + ": " + issue.Message
}

func hasKey(m map[string]any, k string) bool {
	_, ok := m[k]
	return ok
}

// canonical renders the node as the schema advertised to clients.
func (n *node) canonical() map[string]any {
	m := map[string]any{"type": n.typ}
	if n.title != "" {
		m["title"] = n.title
	}
	if n.description != "" {
		m["description"] = n.description
	}
	if n.hasDefault {
		m["default"] = n.def
	}
	if n.examples != nil {
		m["examples"] = n.examples
	}
	if n.enum != nil {
		m["enum"] = n.enum
	}
	switch n.typ {
	case "object":
		m["additionalProperties"] = false
		props := make(map[string]any, len(n.props))
		for name, child := range n.props {
			props[name] = child.canonical()
		}
		m["properties"] = props
		if len(n.required) > 0 {
			m["required"] = n.required
		}
	case "array":
		m["items"] = n.items.canonical()
		m["maxItems"] = n.maxItems
	case "string":
		m["maxLength"] = n.maxLength
		if n.minLength > 0 {
			m["minLength"] = n.minLength
		}
		if n.pattern != "" {
			m["pattern"] = n.pattern
		}
		if n.format != "" {
			m["format"] = n.format
		}
	case "integer", "number":
		if n.minimum != nil {
			m["minimum"] = *n.minimum
		}
		if n.maximum != nil {
			m["maximum"] = *n.maximum
		}
	}
	return m
}

// marshalCanonical encodes v with sorted keys and without HTML escaping.
func marshalCanonical(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// normalize converts json.Number values to float64, recursively, so that
// numbers compare and encode the same whatever their spelling (1e2, 100.0).
func normalize(v any) any {
	switch t := v.(type) {
	case json.Number:
		if f, ok := jsonFloat(t); ok {
			return f
		}
		return t
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = normalize(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normalize(val)
		}
		return out
	}
	return v
}

// jsonFloat converts a JSON number (json.Number, float or integer) to a
// finite float64.
func jsonFloat(v any) (float64, bool) {
	var f float64
	switch t := v.(type) {
	case json.Number:
		parsed, err := strconv.ParseFloat(string(t), 64)
		if err != nil {
			return 0, false
		}
		f = parsed
	case float64:
		f = t
	case float32:
		f = float64(t)
	case int:
		f = float64(t)
	case int8:
		f = float64(t)
	case int16:
		f = float64(t)
	case int32:
		f = float64(t)
	case int64:
		f = float64(t)
	case uint:
		f = float64(t)
	case uint8:
		f = float64(t)
	case uint16:
		f = float64(t)
	case uint32:
		f = float64(t)
	case uint64:
		f = float64(t)
	default:
		return 0, false
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}
