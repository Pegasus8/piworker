package toolschema

import (
	"fmt"
	"regexp/syntax"
	"strings"
	"unicode"
	"unicode/utf8"
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

// portabilityProblem explains why a pattern (already known to compile as Go
// RE2) uses syntax that JavaScript does not share, or returns "". Schemas
// are advertised to MCP clients, which read pattern as an ECMA-262 regular
// expression (with the u flag when they validate), so a Go-only construct
// could stop a client from loading the tool list or make it disagree with
// the server. The common subset accepted here: literals; . ^ $ | * + ?
// (lazy too); {n} {n,} {n,m}; (...) and (?:...); classes [...] and [^...];
// \d \D \w \W \s \S \b \B \t \n \r \f \v \xHH; \p{…} and \P{…} with a
// Unicode general category; and \ before a punctuation character with a
// special meaning (also - inside a class).
func portabilityProblem(p string) string {
	const prefix = "pattern must use the regular expression syntax Go and JavaScript share (MCP clients read patterns as JavaScript): "
	inClass := false
	// afterAssertion: the previous token is ^, $, \b or \B, which Go lets
	// a quantifier repeat and JavaScript does not.
	afterAssertion := false
	for i := 0; i < len(p); {
		c := p[i]
		assertion := false
		switch {
		case c == '\\':
			n, msg := portableEscape(p[i:], inClass)
			if msg != "" {
				return prefix + msg
			}
			assertion = !inClass && (p[i+1] == 'b' || p[i+1] == 'B')
			// Go reads [\w-x] as \w, - and x; JavaScript refuses a range
			// from a class escape.
			if inClass && strings.IndexByte("dDwWsSpP", p[i+1]) >= 0 &&
				i+n+1 < len(p) && p[i+n] == '-' && p[i+n+1] != ']' {
				return prefix + `escape a - after a class escape such as \w as \- (or put the - last)`
			}
			i += n
		case inClass:
			switch c {
			case '[':
				return prefix + `escape [ inside a character class as \[ (POSIX classes such as [[:alpha:]] are Go-only)`
			case ']':
				inClass = false
			}
			i++
		case c == '[':
			i++
			if i < len(p) && p[i] == '^' {
				i++
			}
			if i < len(p) && p[i] == ']' {
				return prefix + `escape a ] right after [ or [^ as \]`
			}
			inClass = true
		case c == '(':
			if strings.HasPrefix(p[i:], "(?") && !strings.HasPrefix(p[i:], "(?:") {
				if strings.HasPrefix(p[i:], "(?P<") || strings.HasPrefix(p[i:], "(?<") {
					return prefix + "named groups are not supported: use (...) or (?:...)"
				}
				return prefix + "inline flags such as (?i) or (?s) are not supported: list both cases instead, e.g. [A-Za-z]"
			}
			i++
		case c == '*' || c == '+' || c == '?' || c == '{':
			if afterAssertion {
				return prefix + `a quantifier cannot follow ^, $, \b or \B`
			}
			if c != '{' {
				i++
				break
			}
			n := repetitionLength(p[i:])
			if n == 0 {
				return prefix + `escape a literal { as \{ (a repetition is {n}, {n,} or {n,m})`
			}
			i += n
		case c == '}':
			return prefix + `escape a literal } as \}`
		case c == ']':
			return prefix + `escape a literal ] as \]`
		default:
			assertion = c == '^' || c == '$'
			i++
		}
		afterAssertion = assertion
	}
	return ""
}

// portableEscape checks the escape at the start of s (s[0] is a backslash)
// and returns its length, or a problem.
func portableEscape(s string, inClass bool) (int, string) {
	if len(s) < 2 {
		return 0, `a pattern cannot end with \`
	}
	e := s[1]
	switch {
	case strings.IndexByte("dDwWsStnrfv", e) >= 0,
		(e == 'b' || e == 'B') && !inClass,
		strings.IndexByte(`^$\.*+?()[]{}|/`, e) >= 0,
		e == '-' && inClass:
		return 2, ""
	case e == 'x':
		if len(s) >= 4 && isHexDigit(s[2]) && isHexDigit(s[3]) {
			return 4, ""
		}
		return 0, `write \x with exactly two hex digits (\x41), not \x{…}`
	case e == 'p' || e == 'P':
		end := strings.IndexByte(s, '}')
		if len(s) < 3 || s[2] != '{' || end < 0 {
			return 0, fmt.Sprintf(`write \%c with braces and a general category, e.g. \%c{L}`, e, e)
		}
		if name := s[3:end]; unicode.Categories[name] == nil {
			return 0, fmt.Sprintf(`\%c{%s} is not a Unicode general category: use one such as \%c{L}, \%c{Lu} or \%c{Nd}`, e, name, e, e, e)
		}
		return end + 1, ""
	default:
		r, _ := utf8.DecodeRuneInString(s[1:])
		return 0, fmt.Sprintf(`the escape \%c is Go-only (escape only punctuation with a special meaning, use \xHH for other characters)`, r)
	}
}

// repetitionLength returns the length of the {n}, {n,} or {n,m} at the start
// of s, or 0.
func repetitionLength(s string) int {
	i := 1
	digits := func() int {
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		return i - start
	}
	if digits() == 0 {
		return 0
	}
	if i < len(s) && s[i] == ',' {
		i++
		digits()
	}
	if i < len(s) && s[i] == '}' {
		return i + 1
	}
	return 0
}

func isHexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}
