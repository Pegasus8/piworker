package builtin

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/expr-lang/expr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// goroutinesBackTo waits (up to limit) until at most want goroutines are
// left and returns the last count.
func goroutinesBackTo(want int, limit time.Duration) int {
	deadline := time.Now().Add(limit)
	n := runtime.NumGoroutine()
	for n > want && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		n = runtime.NumGoroutine()
	}
	return n
}

func numbers(n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = float64(i)
	}
	return out
}

// nestedLoops never ends in practice: 3000³ predicate calls.
const nestedLoops = `count(payload, count(payload, count(payload, true) > 0) > 0)`

// TestEvalExprStopsAtTheDeadline: an evaluation that runs past its context
// stops (its goroutine exits) instead of burning a core in the background,
// inside and outside a sandbox.
func TestEvalExprStopsAtTheDeadline(t *testing.T) {
	program, err := compileExpr(nestedLoops)
	require.NoError(t, err)
	msg := types.NewMessage(numbers(3000), types.DataTypeArray)

	for _, sandboxed := range []bool{false, true} {
		t.Run(fmt.Sprintf("sandboxed=%v", sandboxed), func(t *testing.T) {
			before := runtime.NumGoroutine()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if sandboxed {
				ctx = node.WithSandbox(ctx, node.NewSandbox(1<<20, 32<<20))
			}
			_, err := evalExpr(ctx, program, msg)
			require.Error(t, err)
			assert.LessOrEqual(t, goroutinesBackTo(before, 2*time.Second), before,
				"the evaluation goroutine is gone soon after the deadline")
		})
	}
}

// TestEvalExprIsTrackedBySandbox: in a sandbox the evaluation goroutine is
// started with node.Go, so the sandbox stays busy until it really returned.
func TestEvalExprIsTrackedBySandbox(t *testing.T) {
	program, err := compileExpr(nestedLoops)
	require.NoError(t, err)
	msg := types.NewMessage(numbers(3000), types.DataTypeArray)
	sb := node.NewSandbox(1<<20, 32<<20)
	ctx, cancel := context.WithCancel(node.WithSandbox(context.Background(), sb))

	done := make(chan error, 1)
	go func() {
		_, err := evalExpr(ctx, program, msg)
		done <- err
	}()
	require.Eventually(t, sb.Busy, 2*time.Second, 5*time.Millisecond, "the evaluation is tracked")
	cancel()
	require.Error(t, <-done)
	waited := make(chan struct{})
	go func() { sb.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("the evaluation kept running after its context ended")
	}
}

// TestEvalExprSandboxBoundsData: in a sandbox, expressions cannot build huge
// strings or values: concatenation, the string-growing builtins and the
// result are checked against the sandbox's limits before the data exists.
func TestEvalExprSandboxBoundsData(t *testing.T) {
	kib := strings.Repeat("k", 1<<10)
	cases := map[string]string{
		"doubling":         `reduce(1..40, #acc + #acc, "x")`,
		"quadratic growth": `len(reduce(1..300000, #acc + "xxxxxxxxxx", ""))`,
		"repeat":           `repeat(payload, 1000000)`,
		"replace":          `replace(repeat("a", 2000), "", payload)`,
		"join":             `join(map(1..100000, payload), ",")`,
		"toJSON":           `toJSON(map(1..100000, payload))`,
		"string":           `string(map(1..100000, payload))`,
		"toBase64":         `toBase64(join(map(1..2000, payload)))`,
		"result":           `map(1..200000, payload)`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			program, err := compileExpr(src)
			require.NoError(t, err)
			sb := node.NewSandbox(1<<20, 32<<20)
			ctx := node.WithSandbox(context.Background(), sb)

			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			start := time.Now()
			_, err = evalExpr(ctx, program, types.NewMessage(kib, types.DataTypeString))
			runtime.ReadMemStats(&after)
			require.Error(t, err)
			// repeat hits expr's own memory budget, kept by the guard, first.
			assert.True(t, errors.Is(err, node.ErrSandboxLimit) || strings.Contains(err.Error(), "memory budget exceeded"),
				"a sandbox limit: %v", err)
			assert.Less(t, time.Since(start), 3*time.Second)
			assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(256<<20), "allocations stay bounded")
		})
	}
}

// TestEvalExprSandboxAllowsSmallWork: ordinary expressions are untouched by
// the sandbox's limits.
func TestEvalExprSandboxAllowsSmallWork(t *testing.T) {
	ctx := node.WithSandbox(context.Background(), node.NewSandbox(1<<20, 32<<20))
	for src, want := range map[string]any{
		`payload.a + " " + payload.b`:              "x y",
		`repeat("ab", 3)`:                          "ababab",
		`join(map(1..3, string(#)), "-")`:          "1-2-3",
		`toJSON([1, 2])`:                           "[\n  1,\n  2\n]",
		`reduce(1..100, #acc + "x", "")`:           strings.Repeat("x", 100),
		`count(1..1000, # % 2 == 0)`:               500,
		`replace("a.b.c", ".", "/")`:               "a/b/c",
		`toBase64("hi")`:                           "aGk=",
		`filter(1..5, # > 3) | map(# * 2) | sum()`: 18,
	} {
		program, err := compileExpr(src)
		require.NoError(t, err, src)
		got, err := evalExpr(ctx, program, types.NewMessage(map[string]any{"a": "x", "b": "y"}, types.DataTypeObject))
		require.NoError(t, err, src)
		assert.Equal(t, want, got, src)
	}
}

// TestCompileExprGuardKeepsSemantics: outside a sandbox the guarded program
// computes exactly what the plain expr program computes, and compile errors
// are the plain ones.
func TestCompileExprGuardKeepsSemantics(t *testing.T) {
	payload := map[string]any{
		"name": "ada", "last": "lovelace", "a": 2.0, "b": 3.0,
		"items": []any{
			map[string]any{"name": "x", "price": 50.0},
			map[string]any{"name": "y", "price": 150.0},
			map[string]any{"name": "z", "price": 250.0},
		},
		"nums": []any{1.0, 2.0, 3.0},
		"s":    "aaa",
	}
	exprs := []string{
		`payload.a + payload.b`,
		`"a" + "b"`,
		`1 + 2`,
		`1.5 + 2`,
		`payload.name + " " + payload.last`,
		`upper(payload.name + "x")`,
		`filter(payload.items, .price > 100)`,
		`map(payload.items, .name)`,
		`count(payload.items, .price > 100)`,
		`all(payload.items, .price > 10)`,
		`any(payload.items, .price > 200)`,
		`none(payload.items, .price > 1000)`,
		`one(payload.items, .price > 200)`,
		`find(payload.items, .price > 100).name`,
		`findIndex(payload.items, .price > 100)`,
		`findLast(payload.items, .price > 100).name`,
		`findLastIndex(payload.items, .price > 100)`,
		`sum(payload.items, .price)`,
		`groupBy(payload.items, .price > 100)`,
		`sortBy(payload.items, .price, "desc")`,
		`reduce(payload.nums, #acc + #, 0)`,
		`reduce(payload.nums, #acc + string(#), "")`,
		`map(payload.items, #index)`,
		`payload.items | filter(.price > 100) | map(.name)`,
		`len(filter(payload.items, .price > 100)) > 0`,
		`repeat("ab", 3)`,
		`repeat("ab", 600000)`,
		`join(["a", "b"], "-")`,
		`join(["a", "b"])`,
		`replace(payload.s, "a", "b")`,
		`replace(payload.s, "a", "b", 1)`,
		`toJSON({"a": 1})`,
		`payload.nums | toJSON()`,
		`string(123)`,
		`toBase64("hi")`,
		`let x = 2; x + 1`,
		`{"sum": payload.a + 1, "list": [payload.a + payload.b]}`,
		`payload.missing + 1`,
		`payload.name + 1`,
		`date("2024-01-02") + duration("1h") > date("2024-01-02")`,
		`"x" + 1`,
		`repeat(1, 2)`,
		`join(1)`,
		`foo(`,
		`map(1..3, filter(1..#, # > 1))`,
	}
	for _, src := range exprs {
		t.Run(src, func(t *testing.T) {
			plain, plainErr := expr.Compile(src, expr.AllowUndefinedVariables(), expr.MaxNodes(DefaultExprMaxNodes), expr.WithContext("ctx"))
			guarded, guardedErr := compileExpr(src)
			if plainErr != nil {
				require.Error(t, guardedErr)
				assert.Equal(t, plainErr.Error(), guardedErr.Error(), "the plain compile error")
				return
			}
			require.NoError(t, guardedErr)

			msg := types.NewMessage(payload, types.DataTypeObject)
			ctx := context.Background()
			env := map[string]any{"payload": msg.Payload, "vars": map[string]any{}, "flowVars": map[string]any{},
				"meta": map[string]any{}, "topic": msg.Topic, "timestamp": msg.Timestamp.Format(time.RFC3339), "id": msg.ID, "ctx": ctx}
			want, wantErr := expr.Run(plain, env)
			got, gotErr := evalExpr(ctx, guarded, msg)
			if wantErr != nil {
				require.Error(t, gotErr)
				return
			}
			require.NoError(t, gotErr)
			assert.Equal(t, want, got)
		})
	}
}

// TestExprGuardIsReserved: an expression cannot disable the guard by
// shadowing it; the evaluation fails closed.
func TestExprGuardIsReserved(t *testing.T) {
	program, err := compileExpr(`let ` + exprGuardName + ` = 1; count(1..3, true)`)
	if err != nil {
		return // refused at compile time: fine too
	}
	_, err = evalExpr(context.Background(), program, types.NewMessage(nil, types.DataTypeAny))
	require.Error(t, err)
}
