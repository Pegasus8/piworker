package toolschema

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxIssues caps the issues Validate reports for one call.
const maxIssues = 64

// maxShownRunes caps how much of a string argument a message quotes.
const maxShownRunes = 64

// maxShownEnum caps how many enum values a message lists.
const maxShownEnum = 10

// validateValue checks v against n; skipEnum leaves out n's own enum (used
// to check enum values against the rest of their schema).
func validateValue(n *node, v any, ptr string, skipEnum bool) []Issue {
	vr := &validator{}
	vr.value(n, v, ptr, skipEnum)
	return vr.issues
}

type validator struct {
	issues []Issue
	full   bool
}

func (vr *validator) add(ptr, format string, args ...any) {
	if vr.full {
		return
	}
	if len(vr.issues) == maxIssues {
		vr.issues = append(vr.issues, Issue{Message: fmt.Sprintf("too many problems: only the first %d are listed", maxIssues)})
		vr.full = true
		return
	}
	vr.issues = append(vr.issues, Issue{Pointer: ptr, Message: fmt.Sprintf(format, args...)})
}

func (vr *validator) value(n *node, v any, ptr string, skipEnum bool) {
	if vr.full {
		return
	}
	switch n.typ {
	case "object":
		vr.object(n, v, ptr)
	case "array":
		vr.array(n, v, ptr)
	case "string":
		vr.string(n, v, ptr, skipEnum)
	case "integer", "number":
		vr.number(n, v, ptr, skipEnum)
	case "boolean":
		if _, ok := v.(bool); !ok {
			vr.add(ptr, "expected boolean, got %s", kindOf(v))
		}
	}
}

func (vr *validator) object(n *node, v any, ptr string) {
	m, ok := v.(map[string]any)
	if !ok {
		vr.add(ptr, "expected object, got %s", kindOf(v))
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if n.props[k] == nil {
			vr.add(ptr+"/"+escapePointer(k), "unknown property")
		}
	}
	for _, name := range n.required {
		if _, ok := m[name]; !ok {
			vr.add(ptr+"/"+escapePointer(name), "missing required property")
		}
	}
	for _, k := range keys {
		if child := n.props[k]; child != nil {
			vr.value(child, m[k], ptr+"/"+escapePointer(k), false)
		}
	}
}

func (vr *validator) array(n *node, v any, ptr string) {
	a, ok := v.([]any)
	if !ok {
		vr.add(ptr, "expected array, got %s", kindOf(v))
		return
	}
	if len(a) > n.maxItems {
		vr.add(ptr, "has %d items, at most %d allowed (maxItems)", len(a), n.maxItems)
		return
	}
	for i, item := range a {
		vr.value(n.items, item, ptr+"/"+strconv.Itoa(i), false)
	}
}

func (vr *validator) string(n *node, v any, ptr string, skipEnum bool) {
	s, ok := v.(string)
	if !ok {
		vr.add(ptr, "expected string, got %s", kindOf(v))
		return
	}
	if n.enum != nil && !skipEnum {
		for _, e := range n.enum {
			if e == s {
				return
			}
		}
		vr.add(ptr, "must be one of %s (got %s)", enumList(n.enum), quoteShort(s))
		return
	}
	count := utf8.RuneCountInString(s)
	if count > n.maxLength {
		vr.add(ptr, "is longer than %d characters (maxLength); got %d", n.maxLength, count)
		return
	}
	if count < n.minLength {
		vr.add(ptr, "is shorter than %d characters (minLength); got %d", n.minLength, count)
	}
	if n.re != nil && !n.re.MatchString(s) {
		vr.add(ptr, "does not match the pattern %s", n.pattern)
	}
}

func (vr *validator) number(n *node, v any, ptr string, skipEnum bool) {
	f, ok := jsonFloat(v)
	if !ok {
		vr.add(ptr, "expected %s, got %s", n.typ, kindOf(v))
		return
	}
	if n.typ == "integer" && f != math.Trunc(f) {
		vr.add(ptr, "expected integer, got number %s", formatNumber(f))
		return
	}
	if n.enum != nil && !skipEnum {
		for _, e := range n.enum {
			if e == f {
				return
			}
		}
		vr.add(ptr, "must be one of %s (got %s)", enumList(n.enum), formatNumber(f))
		return
	}
	if n.minimum != nil && f < *n.minimum {
		vr.add(ptr, "must be >= %s (minimum); got %s", formatNumber(*n.minimum), formatNumber(f))
	}
	if n.maximum != nil && f > *n.maximum {
		vr.add(ptr, "must be <= %s (maximum); got %s", formatNumber(*n.maximum), formatNumber(f))
	}
}

// kindOf names v's JSON type without quoting it.
func kindOf(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	}
	if f, ok := jsonFloat(v); ok {
		return "number " + formatNumber(f)
	}
	if _, ok := v.(json.Number); ok {
		return "number"
	}
	return "an unsupported value"
}

// enumList renders an enum for a message, at most maxShownEnum values.
func enumList(enum []any) string {
	shown := enum
	if len(shown) > maxShownEnum {
		shown = shown[:maxShownEnum]
	}
	parts := make([]string, 0, len(shown)+1)
	for _, e := range shown {
		switch t := e.(type) {
		case string:
			parts = append(parts, quoteShort(t))
		case float64:
			parts = append(parts, formatNumber(t))
		}
	}
	if extra := len(enum) - len(shown); extra > 0 {
		parts = append(parts, fmt.Sprintf("… (%d more)", extra))
	}
	return strings.Join(parts, ", ")
}

// quoteShort quotes s, truncated to maxShownRunes runes.
func quoteShort(s string) string {
	if utf8.RuneCountInString(s) <= maxShownRunes {
		return strconv.Quote(s)
	}
	runes := []rune(s)
	return strconv.Quote(string(runes[:maxShownRunes])) + "…"
}

func formatNumber(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}
