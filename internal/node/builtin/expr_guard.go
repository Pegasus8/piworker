package builtin

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/builtin"
	"github.com/expr-lang/expr/conf"
	"github.com/expr-lang/expr/vm/runtime"
)

// The expression guard bounds what an evaluation can do with work and data
// the expr VM does not account for (docs/design/mcp.md §10):
//
//   - every predicate body (the loops of filter, map, count, reduce, …) first
//     calls __pw_tick, which stops the evaluation once its context ends, so a
//     timed-out evaluation does not keep burning a core in the background;
//   - every "+" whose operands may be strings goes through __pw_add, and the
//     string-growing builtins (repeat, join, replace, toJSON, string,
//     toBase64) through guarded versions: in a sandbox (node.SandboxFrom)
//     each result is sized before it exists and charged to the sandbox
//     (per-value cap and budget), so an expression cannot build gigabytes of
//     text; outside one they compute exactly what the builtins compute.
//
// The guard of one evaluation is the env value exprGuardName; compileExpr
// rewrites the program to pass it to these functions.
const exprGuardName = "__pw_guard"

// errExprGuardMissing: the guard value was shadowed (let __pw_guard = …):
// the evaluation fails closed.
var errExprGuardMissing = errors.New("the expression guard is missing (" + exprGuardName + " is reserved)")

// exprGuard is the per-evaluation state of the guard functions.
type exprGuard struct {
	done <-chan struct{} // the evaluation's context
	ctx  context.Context
	sb   *node.Sandbox // nil outside a sandbox
	// repeatBytes emulates the VM's memory budget for repeat, which the VM
	// charges only for its builtin (the guarded one replaces it).
	repeatBytes uint
}

func newExprGuard(ctx context.Context, sb *node.Sandbox) *exprGuard {
	return &exprGuard{done: ctx.Done(), ctx: ctx, sb: sb}
}

func guardOf(v any) (*exprGuard, error) {
	g, ok := v.(*exprGuard)
	if !ok || g == nil {
		return nil, errExprGuardMissing
	}
	return g, nil
}

// tick stops the evaluation once its context ended.
func (g *exprGuard) tick() error {
	select {
	case <-g.done:
		return fmt.Errorf("expression evaluation stopped: %w", g.ctx.Err())
	default:
		return nil
	}
}

// charge spends n bytes of the sandbox (nothing outside one).
func (g *exprGuard) charge(n int64) error {
	if g.sb == nil {
		return nil
	}
	return g.sb.Charge(n)
}

// checkResult bounds a guarded builtin's result in a sandbox.
func (g *exprGuard) checkResult(v any) error {
	if g.sb == nil {
		return nil
	}
	return g.sb.CheckValue(v)
}

// exprGuardOptions are the compile options that install the guard.
func exprGuardOptions() []expr.Option {
	opts := []expr.Option{
		expr.Function("__pw_tick", func(params ...any) (any, error) {
			g, err := guardOf(first(params))
			if err != nil {
				return nil, err
			}
			return nil, g.tick()
		}),
		expr.Function("__pw_add", guardedAdd),
	}
	for name, fn := range guardedBuiltins {
		opts = append(opts, expr.Function(name, fn))
	}
	return append(opts, expr.Patch(exprGuardPatcher{}))
}

func first(params []any) any {
	if len(params) == 0 {
		return nil
	}
	return params[0]
}

// guardedAdd is "+" for operands that may be strings: (guard, a, b).
func guardedAdd(params ...any) (any, error) {
	if len(params) != 3 {
		return nil, fmt.Errorf("invalid operation: + takes 2 operands")
	}
	g, err := guardOf(params[0])
	if err != nil {
		return nil, err
	}
	if err := g.tick(); err != nil {
		return nil, err
	}
	a, b := params[1], params[2]
	if sa, ok := a.(string); ok {
		if sb, ok := b.(string); ok {
			if err := g.charge(int64(len(sa)) + int64(len(sb))); err != nil {
				return nil, err
			}
			return sa + sb, nil
		}
	}
	return runtime.Add(a, b), nil
}

// guardedBuiltins replace the string-growing builtins: each is called with
// the guard first, sizes its result before computing it, then runs the real
// builtin.
var guardedBuiltins = map[string]func(params ...any) (any, error){
	"repeat":   guardBuiltin("repeat", repeatSize),
	"join":     guardBuiltin("join", joinSize),
	"replace":  guardBuiltin("replace", replaceSize),
	"toJSON":   guardBuiltin("toJSON", indentedJSONSize),
	"string":   guardBuiltin("string", valueSize),
	"toBase64": guardBuiltin("toBase64", base64Size),
}

// sizeFunc returns the size, in bytes, of a builtin's result for args
// (limit+1 or more when it is over limit), or ok=false when it cannot tell
// (the builtin then fails on its arguments itself).
type sizeFunc func(args []any, limit int64) (size int64, ok bool)

func guardBuiltin(name string, size sizeFunc) func(params ...any) (any, error) {
	fn := builtin.Builtins[builtin.Index[name]]
	return func(params ...any) (any, error) {
		if len(params) == 0 {
			return nil, errExprGuardMissing
		}
		g, err := guardOf(params[0])
		if err != nil {
			return nil, err
		}
		if err := g.tick(); err != nil {
			return nil, err
		}
		args := params[1:]
		if name == "repeat" {
			// The VM charges repeat's result to its memory budget; keep
			// that limit (checked before the string exists).
			if n, ok := repeatSize(args, int64(conf.DefaultMemoryBudget)); ok {
				g.repeatBytes += uint(n)
				if g.repeatBytes >= conf.DefaultMemoryBudget {
					return nil, errors.New("memory budget exceeded")
				}
			}
		}
		if g.sb != nil {
			if n, ok := size(args, g.sb.MaxValueBytes()); ok {
				if err := g.charge(n); err != nil {
					return nil, err
				}
			}
		}
		out, err := callBuiltin(fn, args)
		if err != nil {
			return nil, err
		}
		if err := g.checkResult(out); err != nil {
			return nil, err
		}
		return out, nil
	}
}

// callBuiltin runs an expr builtin the way the VM would.
func callBuiltin(fn *builtin.Function, args []any) (any, error) {
	switch {
	case fn.Fast != nil:
		if len(args) != 1 {
			return nil, fmt.Errorf("invalid number of arguments for %s (expected 1, got %d)", fn.Name, len(args))
		}
		return fn.Fast(args[0]), nil
	case fn.Func != nil:
		return fn.Func(args...)
	case fn.Safe != nil:
		out, _, err := fn.Safe(args...)
		return out, err
	}
	return nil, fmt.Errorf("builtin %s cannot be called", fn.Name)
}

// mulSize is a*b, or limit+1 when it would be over limit (or overflow).
func mulSize(a, b, limit int64) int64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	if a > (limit+1)/b {
		return limit + 1
	}
	return a * b
}

func repeatSize(args []any, limit int64) (int64, bool) {
	if len(args) != 2 {
		return 0, false
	}
	s, ok := args[0].(string)
	if !ok {
		return 0, false
	}
	n, ok := intArg(args[1])
	if !ok {
		return 0, false
	}
	return mulSize(int64(len(s)), n, limit), true
}

func intArg(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int64:
		return x, true
	case int32:
		return int64(x), true
	case float64:
		return int64(x), true
	}
	rv := reflect.ValueOf(v)
	if rv.CanInt() {
		return rv.Int(), true
	}
	if rv.CanUint() {
		return int64(rv.Uint()), true
	}
	return 0, false
}

func joinSize(args []any, limit int64) (int64, bool) {
	if len(args) == 0 || len(args) > 2 {
		return 0, false
	}
	glue := ""
	if len(args) == 2 {
		g, ok := args[1].(string)
		if !ok {
			return 0, false
		}
		glue = g
	}
	var n, count int64
	add := func(s string) bool {
		n += int64(len(s))
		if count > 0 {
			n += int64(len(glue))
		}
		count++
		return n <= limit
	}
	switch list := args[0].(type) {
	case []string:
		for _, s := range list {
			if !add(s) {
				return limit + 1, true
			}
		}
	case []any:
		for _, item := range list {
			s, ok := item.(string)
			if !ok {
				return 0, false
			}
			if !add(s) {
				return limit + 1, true
			}
		}
	default:
		return 0, false
	}
	return n, true
}

func replaceSize(args []any, limit int64) (int64, bool) {
	if len(args) != 3 && len(args) != 4 {
		return 0, false
	}
	s, ok1 := args[0].(string)
	old, ok2 := args[1].(string)
	repl, ok3 := args[2].(string)
	if !ok1 || !ok2 || !ok3 {
		return 0, false
	}
	var count int64
	if old == "" {
		count = int64(utf8.RuneCountInString(s)) + 1
	} else {
		count = int64(strings.Count(s, old))
	}
	if len(args) == 4 {
		max, ok := intArg(args[3])
		if !ok {
			return 0, false
		}
		if max >= 0 && max < count {
			count = max
		}
	}
	grow := int64(len(repl)) - int64(len(old))
	if grow <= 0 {
		return int64(len(s)), true
	}
	extra := mulSize(count, grow, limit)
	if extra > limit {
		return limit + 1, true
	}
	return int64(len(s)) + extra, true
}

func base64Size(args []any, _ int64) (int64, bool) {
	if len(args) != 1 {
		return 0, false
	}
	s, ok := args[0].(string)
	if !ok {
		return 0, false
	}
	return (int64(len(s)) + 2) / 3 * 4, true
}

func valueSize(args []any, limit int64) (int64, bool) {
	if len(args) != 1 {
		return 0, false
	}
	return node.EstimateSize(args[0], limit), true
}

// indentedJSONSize sizes toJSON's result (json.MarshalIndent with two-space
// indent): every value of a nested structure is preceded by a newline and
// its depth's indentation, which an unindented estimate misses.
func indentedJSONSize(args []any, limit int64) (int64, bool) {
	if len(args) != 1 {
		return 0, false
	}
	e := indentEstimator{limit: limit}
	e.add(args[0], 0)
	return e.n, true
}

type indentEstimator struct{ n, limit int64 }

func (e *indentEstimator) add(v any, depth int64) {
	if e.n > e.limit {
		return
	}
	switch x := v.(type) {
	case map[string]any:
		e.n += 2 + 2*depth + 1
		for k, item := range x {
			if e.n > e.limit {
				return
			}
			e.n += int64(len(k)) + 5 + 2*(depth+1) + 1
			e.add(item, depth+1)
		}
	case []any:
		e.n += 2 + 2*depth + 1
		for _, item := range x {
			if e.n > e.limit {
				return
			}
			e.n += 2*(depth+1) + 2
			e.add(item, depth+1)
		}
	default:
		// Other values: their unindented size, doubled for any nested
		// indentation an estimate cannot see.
		e.n += 2 * node.EstimateSize(v, e.limit)
	}
}

// exprGuardPatcher rewrites a checked expression tree to call the guard: a
// tick at the start of every predicate body, "+" that may concatenate
// strings through __pw_add, and the guard as the first argument of the
// guarded builtins.
type exprGuardPatcher struct{}

func guardIdent() ast.Node { return &ast.IdentifierNode{Value: exprGuardName} }

func (exprGuardPatcher) Visit(n *ast.Node) {
	switch x := (*n).(type) {
	case *ast.PredicateNode:
		x.Node = &ast.SequenceNode{Nodes: []ast.Node{
			&ast.CallNode{Callee: &ast.IdentifierNode{Value: "__pw_tick"}, Arguments: []ast.Node{guardIdent()}},
			x.Node,
		}}
	case *ast.BinaryNode:
		if x.Operator == "+" && mayConcat(x.Left, x.Right) {
			ast.Patch(n, &ast.CallNode{
				Callee:    &ast.IdentifierNode{Value: "__pw_add"},
				Arguments: []ast.Node{guardIdent(), x.Left, x.Right},
			})
		}
	case *ast.CallNode:
		if id, ok := x.Callee.(*ast.IdentifierNode); ok {
			if _, guarded := guardedBuiltins[id.Value]; guarded {
				x.Arguments = append([]ast.Node{guardIdent()}, x.Arguments...)
			}
		}
	}
}

// mayConcat reports whether a "+" may concatenate strings: neither operand
// is known to be something else (numbers, times, durations add in place).
func mayConcat(left, right ast.Node) bool {
	return mayBeString(left.Type()) && mayBeString(right.Type())
}

func mayBeString(t reflect.Type) bool {
	return t == nil || t.Kind() == reflect.Interface || t.Kind() == reflect.String
}
