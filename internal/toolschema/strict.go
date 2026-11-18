package toolschema

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// probes is the corpus a strict string's pattern must reject: option
// injection (-x, --a=b), path traversal (.., /), whitespace and line breaks,
// and shell metacharacters ($(, ;, |, backtick).
var probes = []string{"-x", "--a=b", "..", "/", " ", "\n", "$(", ";", "|", "`"}

// Probes returns a copy of the probe corpus. Each probe stands for a class
// of values, and a pattern accepts a probe when it accepts any value of the
// class (see patternProbes):
//
//   - "-x": a value starting with - (an option flag);
//   - "--a=b": a value starting with -- (a long option);
//   - "..": a value with a .. path segment (the whole value, or between
//     slashes, at the start or at the end);
//   - "/": a / anywhere;
//   - " ": whitespace anywhere (space, tab and every other Unicode space
//     that is not a line break);
//   - "\n": a line break anywhere (\n, \r, U+0085, U+2028, U+2029);
//   - "$(", ";", "|", "`": a $, ;, | or backtick anywhere.
func Probes() []string {
	return append([]string(nil), probes...)
}

// probeList renders the corpus for messages.
func probeList() string {
	parts := make([]string, len(probes))
	for i, p := range probes {
		parts[i] = strconv.Quote(p)
	}
	return strings.Join(parts, ", ")
}

// RequireStrictFor checks that the properties at paths cannot carry option
// flags, path traversal or shell syntax, for arguments that reach a
// privileged node. paths are JSON pointers of properties in the arguments
// ("/host", "/opts/host"); nil or empty means every leaf property. An object
// selects every leaf below it and an array (or one of its elements) its
// items.
//
// A string needs an enum, or a pattern (always anchored) that rejects every
// probe of the corpus (see Probes) other than those the property lists in
// x-piworker-allow. A probe is judged on every value the pattern accepts,
// not just as a whole value: ^[a-z].*$ rejects "/" but accepts "a/b", so it
// accepts the "/" probe. An integer or number needs an enum, or both minimum
// and maximum. Booleans are always fine. The result has one issue per offending
// property (naming the probes its pattern accepts) and one per path that is
// not a property, sorted by pointer; nil when everything is strict.
func (s *Schema) RequireStrictFor(paths []string) []Issue {
	targets := map[string]*node{}
	var issues []Issue
	if len(paths) == 0 {
		collectLeaves(s.root, "", targets)
	}
	for _, p := range paths {
		n, ptr, ok := s.lookup(p)
		if !ok {
			issues = append(issues, Issue{Pointer: p, Message: "not a property of the tool's input schema"})
			continue
		}
		collectLeaves(n, ptr, targets)
	}
	for ptr, n := range targets {
		if msg := strictProblem(n); msg != "" {
			issues = append(issues, Issue{Pointer: ptr, Message: msg})
		}
	}
	sort.SliceStable(issues, func(i, j int) bool { return issues[i].Pointer < issues[j].Pointer })
	return issues
}

// lookup resolves a JSON pointer into the arguments to the schema node it
// selects and that node's canonical pointer. A pointer to an array element
// selects the array.
func (s *Schema) lookup(p string) (*node, string, bool) {
	if p == "" {
		return s.root, "", true
	}
	if !strings.HasPrefix(p, "/") {
		return nil, "", false
	}
	segments := strings.Split(p[1:], "/")
	cur, ptr := s.root, ""
	for i, seg := range segments {
		switch cur.typ {
		case "object":
			name := unescapePointer(seg)
			child := cur.props[name]
			if child == nil {
				return nil, "", false
			}
			cur, ptr = child, ptr+"/"+escapePointer(name)
		case "array":
			if _, err := strconv.ParseUint(seg, 10, 32); err != nil || i != len(segments)-1 {
				return nil, "", false
			}
			return cur, ptr, true
		default:
			return nil, "", false
		}
	}
	return cur, ptr, true
}

// collectLeaves adds every non-object node under n, keyed by pointer.
func collectLeaves(n *node, ptr string, into map[string]*node) {
	if n.typ != "object" {
		into[ptr] = n
		return
	}
	for name, child := range n.props {
		collectLeaves(child, ptr+"/"+escapePointer(name), into)
	}
}

// strictProblem explains why a leaf is not strict, or returns "".
func strictProblem(n *node) string {
	switch n.typ {
	case "array":
		if msg := strictProblem(n.items); msg != "" {
			return "items: " + msg
		}
	case "string":
		if n.enum != nil {
			return ""
		}
		if n.re == nil {
			return "needs an enum or an anchored pattern: it reaches a privileged node"
		}
		found, err := patternProbes(n.pattern)
		if err != nil {
			return "pattern cannot be analysed: " + err.Error()
		}
		var accepted []string
		for _, probe := range found {
			if !slices.Contains(n.allow, probe) {
				accepted = append(accepted, strconv.Quote(probe))
			}
		}
		if len(accepted) > 0 {
			return fmt.Sprintf("pattern accepts the probe(s) %s and the value reaches a privileged node: tighten the pattern, or acknowledge them in %s",
				strings.Join(accepted, ", "), allowKeyword)
		}
	case "integer", "number":
		if n.enum == nil && (n.minimum == nil || n.maximum == nil) {
			return "needs both minimum and maximum (or an enum): it reaches a privileged node"
		}
	}
	return ""
}
