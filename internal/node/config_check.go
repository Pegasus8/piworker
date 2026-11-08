package node

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ConfigIssue is one configuration value that does not match its schema.
type ConfigIssue struct {
	// Path of the value inside the node config, e.g. "interval".
	Path string `json:"path"`
	// Message says what was expected and what was found, e.g.
	// `expected number (milliseconds), got string "5s"`.
	Message string `json:"message"`
}

// String renders the issue as "path: message".
func (i ConfigIssue) String() string { return i.Path + ": " + i.Message }

// maxShownValue caps how much of an offending string is quoted in a message.
const maxShownValue = 32

// CheckConfig checks the values in cfg against the types, enums and bounds
// declared by schema, and returns the problems sorted by path (nil when there
// are none). It lets a deploy fail with an actionable message instead of a
// node silently falling back to a default for a mistyped value.
//
// Rules:
//   - keys the schema does not declare, and properties without a Type, are
//     not checked;
//   - null, and "" for any non-string type or enum, mean "unset" (the editor
//     stores "" for cleared fields and nodes then use their default);
//   - "number" accepts any finite number, "integer" a whole number;
//   - enum values match exactly, or in any case when the property is
//     CaseInsensitive (the node normalises case itself);
//   - Minimum/Maximum are inclusive;
//   - Required is left to the node, which knows its defaults.
//
// Values of Sensitive properties are never quoted in messages.
func CheckConfig(schema *ConfigSchema, cfg map[string]any) []ConfigIssue {
	if schema == nil || len(cfg) == 0 {
		return nil
	}
	keys := make([]string, 0, len(cfg))
	for k := range cfg {
		if _, declared := schema.Properties[k]; declared {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var issues []ConfigIssue
	for _, k := range keys {
		if msg := checkValue(schema.Properties[k], cfg[k]); msg != "" {
			issues = append(issues, ConfigIssue{Path: k, Message: msg})
		}
	}
	return issues
}

// checkValue returns a message describing why v does not satisfy p, or "".
func checkValue(p ConfigProperty, v any) string {
	if v == nil || p.Type == "" {
		return ""
	}
	if s, ok := v.(string); ok && s == "" && (p.Type != "string" || len(p.Enum) > 0) {
		return ""
	}
	unit := ""
	if p.Unit != "" {
		unit = " (" + p.Unit + ")"
	}
	mismatch := func() string {
		return fmt.Sprintf("expected %s%s, got %s", p.Type, unit, describeValue(v, p.Sensitive))
	}

	switch p.Type {
	case "string":
		s, ok := v.(string)
		if !ok {
			return mismatch()
		}
		return checkEnum(p, s)
	case "boolean":
		if _, ok := v.(bool); !ok {
			return mismatch()
		}
	case "object":
		if !isKind(v, reflect.Map) {
			return mismatch()
		}
	case "array":
		if !isKind(v, reflect.Slice, reflect.Array) {
			return mismatch()
		}
	case "number", "integer":
		n, ok := toFloat(v)
		if !ok {
			return mismatch()
		}
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return fmt.Sprintf("expected a finite number%s, got %v", unit, n)
		}
		if p.Type == "integer" && n != math.Trunc(n) {
			return mismatch()
		}
		if p.Minimum != nil && n < *p.Minimum {
			return fmt.Sprintf("expected %s >= %s%s, got %s", p.Type, formatNumber(*p.Minimum), unit, formatNumber(n))
		}
		if p.Maximum != nil && n > *p.Maximum {
			return fmt.Sprintf("expected %s <= %s%s, got %s", p.Type, formatNumber(*p.Maximum), unit, formatNumber(n))
		}
	}
	return ""
}

// checkEnum validates a string against the property's enum, if any.
func checkEnum(p ConfigProperty, s string) string {
	if len(p.Enum) == 0 {
		return ""
	}
	for _, allowed := range p.Enum {
		if s == allowed || (p.CaseInsensitive && strings.EqualFold(s, allowed)) {
			return ""
		}
	}
	quoted := make([]string, len(p.Enum))
	for i, allowed := range p.Enum {
		quoted[i] = strconv.Quote(allowed)
	}
	anyCase := ""
	if p.CaseInsensitive {
		anyCase = " (any case)"
	}
	got := "a value outside the list"
	if !p.Sensitive {
		got = quote(s)
	}
	return fmt.Sprintf("expected one of %s%s, got %s", strings.Join(quoted, ", "), anyCase, got)
}

// describeValue names v's JSON type and, unless sensitive, a short rendering
// of its value: `string "5s"`, `number 12.5`, `boolean true`, `object`.
func describeValue(v any, sensitive bool) string {
	var kind, shown string
	switch val := v.(type) {
	case string:
		kind, shown = "string", quote(val)
	case bool:
		kind, shown = "boolean", strconv.FormatBool(val)
	default:
		if n, ok := toFloat(v); ok {
			kind, shown = "number", formatNumber(n)
		} else if isKind(v, reflect.Map) {
			kind = "object"
		} else if isKind(v, reflect.Slice, reflect.Array) {
			kind = "array"
		} else {
			kind = fmt.Sprintf("%T", v)
		}
	}
	if sensitive || shown == "" {
		return kind
	}
	return kind + " " + shown
}

// quote %q-quotes s, truncated to maxShownValue runes.
func quote(s string) string {
	if utf8.RuneCountInString(s) <= maxShownValue {
		return strconv.Quote(s)
	}
	runes := []rune(s)
	return strconv.Quote(string(runes[:maxShownValue])) + "…"
}

func formatNumber(n float64) string {
	return strconv.FormatFloat(n, 'f', -1, 64)
}

// toFloat converts any Go numeric value (or json.Number) to float64.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return 0, false
}

func isKind(v any, kinds ...reflect.Kind) bool {
	k := reflect.ValueOf(v).Kind()
	for _, want := range kinds {
		if k == want {
			return true
		}
	}
	return false
}
