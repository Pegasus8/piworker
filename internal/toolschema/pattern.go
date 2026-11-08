package toolschema

import (
	"regexp/syntax"
	"strings"
)

// anchoringProblem explains why a pattern (already known to compile) is not
// fully anchored, or returns "". JSON Schema patterns are unanchored
// searches: "[0-9]+" accepts "5 --exec=x". A pattern must therefore start
// with ^ and end with $, keep any alternation inside a group (^a|b$ means
// "starts with a or ends with b"), and its parse tree must run from the
// beginning to the end of the text, which also rules out an escaped \$ at
// the end and flags such as (?m) that turn $ into an end-of-line anchor.
func anchoringProblem(p string) string {
	const anchored = "pattern must be anchored: start with ^ and end with $ (e.g. ^[a-z0-9_]+$)"
	if !strings.HasPrefix(p, "^") || !strings.HasSuffix(p, "$") {
		return anchored
	}
	re, err := syntax.Parse(p, syntax.Perl)
	if err != nil {
		return "pattern is not a valid regular expression: " + err.Error()
	}
	switch {
	case re.Op == syntax.OpAlternate:
		return "pattern has a top-level alternation: put the alternatives inside a group, e.g. ^(a|b)$"
	case re.Op != syntax.OpConcat || len(re.Sub) < 2,
		re.Sub[0].Op != syntax.OpBeginText,
		re.Sub[len(re.Sub)-1].Op != syntax.OpEndText:
		return anchored
	}
	return ""
}
