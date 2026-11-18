package builtin

import (
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nested(open, close string, depth int) string {
	return strings.Repeat(open, depth) + "1" + strings.Repeat(close, depth)
}

// TestCompileExprBoundsNesting: expr's parser recurses once per bracket
// with no limit (parentheses create no AST node, so MaxNodes never fires);
// compileExpr refuses brackets nested deeper than 256 levels before
// parsing, counting unclosed ones too, and only brackets the expr lexer
// sees (not those in strings or comments).
func TestCompileExprBoundsNesting(t *testing.T) {
	for name, src := range map[string]string{
		"parentheses":         nested("(", ")", 257),
		"arrays":              nested("[", "]", 257),
		"maps":                strings.Repeat(`{"a": `, 257) + "1" + strings.Repeat("}", 257),
		"calls":               strings.Repeat("abs(", 257) + "1" + strings.Repeat(")", 257),
		"mixed":               strings.Repeat("([{a: ", 86) + "1" + strings.Repeat("}])", 86),
		"unclosed":            strings.Repeat("(", 257) + "1",
		"after a stray close": ")" + nested("(", ")", 257),
		"before a bad string": strings.Repeat("(", 300) + `"unterminated`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := compileExpr(src)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "nest")
		})
	}
	for name, src := range map[string]string{
		"at the limit":      nested("(", ")", 256),
		"in a string":       `"` + strings.Repeat("(", 1000) + `"`,
		"in a raw string":   "`" + strings.Repeat("[", 1000) + "`",
		"in a comment":      "/* " + strings.Repeat("{", 1000) + " */ 1",
		"sequential groups": strings.Repeat("(1) + ", 1000) + "1",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := compileExpr(src)
			require.NoError(t, err)
		})
	}
}

// TestCompileExprBoundsLength: an expression is at most 16 KiB.
func TestCompileExprBoundsLength(t *testing.T) {
	str := func(n int) string { return `"` + strings.Repeat("a", n-2) + `"` }
	_, err := compileExpr(str(16 << 10))
	require.NoError(t, err)
	_, err = compileExpr(str(16<<10 + 1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too long")
}

// TestCompileExprRefusesTheReportedNestingQuickly: 255,000 parentheses
// (510 KB, what fits in an agent's 512 KiB flow) grew a goroutine stack to
// 512 MiB on 64-bit builds and overflowed the 250 MB stack of 32-bit ones
// (a fatal error); every expr-backed node refuses it in milliseconds now,
// and the deepest nesting that fits in 16 KiB too.
func TestCompileExprRefusesTheReportedNestingQuickly(t *testing.T) {
	for name, src := range map[string]string{
		"reported": nested("(", ")", 255000),
		"16 KiB":   nested("(", ")", 8000),
	} {
		for typ, cfg := range map[string]map[string]any{
			"process-transform": {"expression": src},
			"process-filter":    {"condition": src},
			"process-switch":    {"condition": src},
			"set-var":           {"key": "k", "value": src},
			"process-join":      {"key": src},
			"action-database":   {"driver": "sqlite", "dsn": "file::memory:", "query": "SELECT ?", "params": src},
		} {
			t.Run(name+"/"+typ, func(t *testing.T) {
				start := time.Now()
				_, err := node.DefaultRegistry.Create(typ, cfg)
				require.Error(t, err)
				assert.Regexp(t, "too long|nest", err.Error())
				assert.Less(t, time.Since(start), time.Second)
			})
		}
	}
}
