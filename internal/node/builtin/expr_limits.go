package builtin

import (
	"fmt"

	"github.com/expr-lang/expr/file"
	"github.com/expr-lang/expr/parser/lexer"
)

// Bounds on an expression's source, checked by compileExpr before expr
// parses it (docs/design/mcp.md §10). expr's parser recurses once per
// bracket with no depth limit, and parentheses create no AST node, so
// MaxNodes never stops "((((…1…))))": a 510 KB expression (what fits in an
// agent's flow) grew a goroutine stack to 512 MiB and overflowed the 250 MB
// stack limit of 32-bit builds, a fatal error no recover catches.
const (
	// MaxExprBytes caps an expression's length.
	MaxExprBytes = 16 << 10
	// MaxExprNesting caps how deeply brackets ( [ { nest in an expression.
	MaxExprNesting = 256
)

// checkExprSource refuses an expression over MaxExprBytes or nesting
// brackets deeper than MaxExprNesting. Brackets are counted on expr's own
// tokens (its lexer does not recurse), so brackets in strings and comments
// do not count and unclosed ones do. A lexing error is left for expr.Compile
// to report: the parser stops there, at a depth already checked.
func checkExprSource(src string) error {
	if len(src) > MaxExprBytes {
		return fmt.Errorf("expression is too long (%d bytes; at most %d)", len(src), MaxExprBytes)
	}
	lex := lexer.New()
	lex.Reset(file.NewSource(src))
	depth := 0
	for {
		tok, err := lex.Next()
		if err != nil { // io.EOF, or a lexing error expr.Compile reports
			return nil
		}
		if tok.Kind != lexer.Bracket {
			continue
		}
		switch tok.Value {
		case "(", "[", "{":
			depth++
			if depth > MaxExprNesting {
				return fmt.Errorf("expression nests brackets too deeply (more than %d levels)", MaxExprNesting)
			}
		default:
			if depth > 0 {
				depth--
			}
		}
	}
}
