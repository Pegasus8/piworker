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
//     builtins that build data through guarded versions (guardedBuiltins).
//     In a sandbox (node.SandboxFrom) the string-growing ones (repeat, join,
//     replace, toJSON, string, toBase64) and the array-building ones
//     (concat, flatten) size their result before it exists, and the ones
//     whose result is at most a few times their input (upper, lower,
//     split, splitAfter, fromBase64, fromJSON, keys, values, toPairs, uniq)
//     charge it once it exists, to the sandbox (per-value cap and budget),
//     so an expression cannot build gigabytes of data; outside one they
//     compute what the builtins compute;
//   - in every flow, the builtins the VM charges to its memory budget only
//     after they built their result (repeat, concat, flatten) are checked
//     against that budget before they run, and uniq, which is quadratic,
//     checks the evaluation's context as it goes.
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
	// vmMemory emulates the VM's memory budget for the builtins it charges
	// to it (repeat, concat, flatten): the VM charges only its own
	// builtins, after they ran, and the guarded ones replace them.
	vmMemory uint
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

// guardedBuiltins replace the builtins that build data: each is called with
// the guard first, bounds its result (builtinGuard), then runs the real
// builtin.
var guardedBuiltins = map[string]func(params ...any) (any, error){
	// Strings that may be far larger than their arguments.
	"repeat":   guardBuiltin("repeat", builtinGuard{size: repeatSize, vmUnits: repeatSize}),
	"join":     guardBuiltin("join", builtinGuard{size: joinSize}),
	"replace":  guardBuiltin("replace", builtinGuard{size: replaceSize}),
	"toJSON":   guardBuiltin("toJSON", builtinGuard{size: indentedJSONSize}),
	"string":   guardBuiltin("string", builtinGuard{size: valueSize}),
	"toBase64": guardBuiltin("toBase64", builtinGuard{size: base64Size}),
	// Arrays that may be far larger than their arguments (copies of one
	// array, arrays nesting the same array): items the VM charges only
	// once they exist.
	"concat":  guardBuiltin("concat", builtinGuard{size: concatSize, vmUnits: concatItems}),
	"flatten": guardBuiltin("flatten", builtinGuard{size: valueSize, vmUnits: flattenItems}),
	// Data at most a few times the size of its input.
	"upper":      guardBuiltin("upper", builtinGuard{}),
	"lower":      guardBuiltin("lower", builtinGuard{}),
	"split":      guardBuiltin("split", builtinGuard{}),
	"splitAfter": guardBuiltin("splitAfter", builtinGuard{}),
	"fromBase64": guardBuiltin("fromBase64", builtinGuard{}),
	"fromJSON":   guardBuiltin("fromJSON", builtinGuard{}),
	"keys":       guardBuiltin("keys", builtinGuard{}),
	"values":     guardBuiltin("values", builtinGuard{}),
	"toPairs":    guardBuiltin("toPairs", builtinGuard{}),
	"uniq":       guardedUniq,
}

// sizeFunc returns the size, in bytes, of a builtin's result for args
// (limit+1 or more when it is over limit), or ok=false when it cannot tell
// (the builtin then fails on its arguments itself).
type sizeFunc func(args []any, limit int64) (size int64, ok bool)

// builtinGuard is how the guard bounds a builtin.
type builtinGuard struct {
	// size sizes the result before it exists, in a sandbox, where it is
	// charged; nil: the result is charged once it exists (a builtin whose
	// result is at most a few times the size of its input).
	size sizeFunc
	// vmUnits is what the VM would charge its memory budget for the
	// result, checked before the builtin runs, in every flow: the VM
	// charges its own builtin only after it built the result (and the
	// guarded one not at all).
	vmUnits sizeFunc
}

// guardArgs returns the guard and the builtin's own arguments of a guarded
// call, after a tick.
func guardArgs(params []any) (*exprGuard, []any, error) {
	if len(params) == 0 {
		return nil, nil, errExprGuardMissing
	}
	g, err := guardOf(params[0])
	if err != nil {
		return nil, nil, err
	}
	if err := g.tick(); err != nil {
		return nil, nil, err
	}
	return g, params[1:], nil
}

// spendVM spends n units of the emulated VM memory budget, failing as the
// VM does once it is spent (which ends the evaluation).
func (g *exprGuard) spendVM(n int64) error {
	if n >= int64(conf.DefaultMemoryBudget) || g.vmMemory+uint(n) >= conf.DefaultMemoryBudget {
		return errors.New("memory budget exceeded")
	}
	g.vmMemory += uint(n)
	return nil
}

// chargeResult bounds a builtin's result once it exists, in a sandbox:
// charged (sized like its JSON) when it was not sized before, else checked
// against the per-value cap.
func (g *exprGuard) chargeResult(v any, charge bool) error {
	switch {
	case g.sb == nil:
		return nil
	case charge:
		return g.sb.ChargeValue(v)
	default:
		return g.checkResult(v)
	}
}

func guardBuiltin(name string, bound builtinGuard) func(params ...any) (any, error) {
	fn := builtin.Builtins[builtin.Index[name]]
	return func(params ...any) (any, error) {
		g, args, err := guardArgs(params)
		if err != nil {
			return nil, err
		}
		if bound.vmUnits != nil {
			if n, ok := bound.vmUnits(args, int64(conf.DefaultMemoryBudget)); ok {
				if err := g.spendVM(n); err != nil {
					return nil, err
				}
			}
		}
		if g.sb != nil && bound.size != nil {
			if n, ok := bound.size(args, g.sb.MaxValueBytes()); ok {
				if err := g.charge(n); err != nil {
					return nil, err
				}
			}
		}
		out, err := callBuiltin(fn, args)
		if err != nil {
			return nil, err
		}
		if err := g.chargeResult(out, bound.size == nil); err != nil {
			return nil, err
		}
		return out, nil
	}
}

// guardedUniq is expr's uniq (the same quadratic algorithm, so the same
// result) that checks the evaluation's context for every item.
func guardedUniq(params ...any) (any, error) {
	g, args, err := guardArgs(params)
	if err != nil {
		return nil, err
	}
	if len(args) != 1 {
		return callBuiltin(builtin.Builtins[builtin.Index["uniq"]], args) // its error
	}
	v, ok := arrayArg(args[0])
	if !ok {
		return callBuiltin(builtin.Builtins[builtin.Index["uniq"]], args) // its error
	}
	ret := []any{}
	for i := 0; i < v.Len(); i++ {
		if err := g.tick(); err != nil {
			return nil, err
		}
		item := v.Index(i).Interface()
		seen := false
		for _, r := range ret {
			if runtime.Equal(item, r) {
				seen = true
				break
			}
		}
		if !seen {
			ret = append(ret, item)
		}
	}
	if err := g.chargeResult(ret, true); err != nil {
		return nil, err
	}
	return ret, nil
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

// arrayArg is v as an array or slice (ok=false for anything else).
func arrayArg(v any) (reflect.Value, bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return reflect.Value{}, false
	}
	return rv, true
}

// concatItems is the number of items concat returns.
func concatItems(args []any, limit int64) (int64, bool) {
	if len(args) == 0 {
		return 0, false
	}
	var n int64
	for _, arg := range args {
		v, ok := arrayArg(arg)
		if !ok {
			return 0, false
		}
		n += int64(v.Len())
		if n > limit {
			return limit + 1, true
		}
	}
	return n, true
}

// concatSize sizes concat's result like its JSON: its arguments' items.
func concatSize(args []any, limit int64) (int64, bool) {
	if _, ok := concatItems(args, limit); !ok {
		return 0, false
	}
	var n int64 = 2
	for _, arg := range args {
		n += node.EstimateSize(arg, limit)
		if n > limit {
			return limit + 1, true
		}
	}
	return n, true
}

// flattenItems counts what flatten goes through: the items it returns and
// the nested arrays it opens (arrays nesting the same array twice double
// both at every level).
func flattenItems(args []any, limit int64) (int64, bool) {
	if len(args) != 1 {
		return 0, false
	}
	v, ok := arrayArg(args[0])
	if !ok {
		return 0, false
	}
	var n int64
	var walk func(v reflect.Value, depth int) bool
	walk = func(v reflect.Value, depth int) bool {
		if depth > builtin.MaxDepth {
			return false // flatten fails itself
		}
		for i := 0; i < v.Len() && n <= limit; i++ {
			n++
			item := derefValue(v.Index(i))
			if item.Kind() == reflect.Slice || item.Kind() == reflect.Array {
				if !walk(item, depth+1) {
					return false
				}
			}
		}
		return true
	}
	if !walk(v, 0) {
		return 0, false
	}
	return min(n, limit+1), true
}

// derefValue follows pointers and interfaces, as flatten does.
func derefValue(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return v
		}
		v = v.Elem()
	}
	return v
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
