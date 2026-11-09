package builtin

import (
	"context"
	"encoding/base64"
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

// sandboxIdle waits (up to limit) until no goroutine sb tracks is running.
func sandboxIdle(sb *node.Sandbox, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for sb.Busy() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	return !sb.Busy()
}

// fanOut nests [x, x] depth times over two empty arrays: flatten visits
// 2^(depth+1) arrays and returns nothing.
func fanOut(depth int) string {
	var b strings.Builder
	b.WriteString("let x0 = [[], []]; ")
	for i := 1; i <= depth; i++ {
		fmt.Fprintf(&b, "let x%d = [x%d, x%d]; ", i, i-1, i-1)
	}
	fmt.Fprintf(&b, "len(flatten(x%d))", depth)
	return b.String()
}

// TestEvalExprBoundsArrayBuiltins: concat and flatten build their whole
// result before the VM charges it to its memory budget, and never check
// the evaluation's context: copies of a 999,990-element range allocated
// gigabytes past every deadline (an OOM kill on a Pi), and flatten over
// shared arrays ran for hours. They are sized before they run now, inside
// and outside a sandbox, and refused over the VM's budget.
func TestEvalExprBoundsArrayBuiltins(t *testing.T) {
	copies := strings.TrimSuffix(strings.Repeat("r, ", 30), ", ")
	cases := map[string]string{
		"concat":          "let r = 1..999990; len(concat(" + copies + "))",
		"flatten":         "let r = 1..999000; len(flatten([" + copies + "]))",
		"flatten fan-out": fanOut(40),
	}
	for name, src := range cases {
		for _, sandboxed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/sandboxed=%v", name, sandboxed), func(t *testing.T) {
				program, err := compileExpr(src)
				require.NoError(t, err)
				before := runtime.NumGoroutine()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				sb := node.NewSandbox(1<<20, 32<<20)
				if sandboxed {
					ctx = node.WithSandbox(ctx, sb)
				}
				var stats [2]runtime.MemStats
				runtime.ReadMemStats(&stats[0])
				start := time.Now()
				_, err = evalExpr(ctx, program, types.NewMessage(nil, types.DataTypeAny))
				require.Error(t, err)
				assert.Less(t, time.Since(start), time.Second, "refused before it runs: %v", err)
				assert.True(t, errors.Is(err, node.ErrSandboxLimit) || strings.Contains(err.Error(), "memory budget exceeded"),
					"a memory limit: %v", err)
				if sandboxed {
					assert.True(t, sandboxIdle(sb, time.Second), "nothing keeps running in the sandbox")
				} else {
					assert.LessOrEqual(t, goroutinesBackTo(before, time.Second), before, "nothing keeps running")
				}
				runtime.ReadMemStats(&stats[1])
				assert.Less(t, stats[1].TotalAlloc-stats[0].TotalAlloc, uint64(256<<20), "allocations stay bounded")
			})
		}
	}
}

// TestEvalExprUniqStopsAtTheDeadline: uniq is quadratic and never checked
// the context: uniq(1..40000) ran on for 20 s after a node test returned,
// holding the sandbox (every other dry-run and node test was busy).
func TestEvalExprUniqStopsAtTheDeadline(t *testing.T) {
	program, err := compileExpr(`len(uniq(1..40000))`)
	require.NoError(t, err)
	for _, sandboxed := range []bool{false, true} {
		t.Run(fmt.Sprintf("sandboxed=%v", sandboxed), func(t *testing.T) {
			before := runtime.NumGoroutine()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			sb := node.NewSandbox(1<<20, 32<<20)
			if sandboxed {
				ctx = node.WithSandbox(ctx, sb)
			}
			_, err := evalExpr(ctx, program, types.NewMessage(nil, types.DataTypeAny))
			require.Error(t, err)
			if sandboxed {
				assert.True(t, sandboxIdle(sb, time.Second), "the sandbox is free soon after the deadline")
			} else {
				assert.LessOrEqual(t, goroutinesBackTo(before, time.Second), before, "the evaluation stopped")
			}
		})
	}
}

// TestEvalExprSandboxChargesDerivedData: builtins that build data from
// their input (upper, lower, split, splitAfter, fromBase64, fromJSON, keys,
// values, toPairs, uniq) charge it to the sandbox: 300 upper-cased copies
// of a 1 MB string were 289 MiB against a 32 MiB budget, with no refusal.
func TestEvalExprSandboxChargesDerivedData(t *testing.T) {
	payload := map[string]any{
		"b64":  base64.StdEncoding.EncodeToString([]byte(strings.Repeat("b", 48<<10))),
		"json": "[" + strings.TrimSuffix(strings.Repeat("1,", 5000), ",") + "]",
	}
	const pairs = `let m = fromPairs(map(1..20000, [string(#), #])); `
	cases := map[string]struct {
		src    string
		budget int64
	}{
		"upper":      {src: `let s = repeat("a", 999999); len(map(1..300, upper(s)))`},
		"lower":      {src: `let s = repeat("A", 999999); len(map(1..300, lower(s)))`},
		"split":      {src: `let s = repeat("a,", 60000); len(map(1..300, split(s, ",")))`},
		"splitAfter": {src: `let s = repeat("a,", 60000); len(map(1..300, splitAfter(s, ",")))`},
		"fromBase64": {src: `len(map(1..2000, fromBase64(payload.b64)))`},
		"fromJSON":   {src: `len(map(1..2000, fromJSON(payload.json)))`, budget: 1 << 20},
		"keys":       {src: pairs + `len(map(1..300, keys(m)))`},
		"values":     {src: pairs + `len(map(1..300, values(m)))`},
		"toPairs":    {src: pairs + `len(map(1..300, toPairs(m)))`},
		"uniq":       {src: `len(map(1..100, uniq(1..500)))`, budget: 64 << 10},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			program, err := compileExpr(c.src)
			require.NoError(t, err)
			// A smaller budget than a dry-run's, so the allocations (a few
			// times the JSON-like sizes the sandbox counts) stay well
			// under the bound below.
			budget := c.budget
			if budget == 0 {
				budget = 8 << 20
			}
			sb := node.NewSandbox(1<<20, budget)
			ctx := node.WithSandbox(context.Background(), sb)
			var stats [2]runtime.MemStats
			runtime.ReadMemStats(&stats[0])
			start := time.Now()
			_, err = evalExpr(ctx, program, types.NewMessage(payload, types.DataTypeObject))
			runtime.ReadMemStats(&stats[1])
			require.Error(t, err)
			assert.ErrorIs(t, err, node.ErrSandboxLimit)
			// The bound catches a runaway evaluation (uniq ran for hours),
			// not slowness: under -race on a CI runner busy with the other
			// packages a case takes about three times as long as alone.
			assert.Less(t, time.Since(start), 10*time.Second)
			assert.Less(t, stats[1].TotalAlloc-stats[0].TotalAlloc, uint64(256<<20), "allocations stay bounded")
		})
	}
}

// TestGuardedBuiltinsKeepSemantics: outside a sandbox the newly guarded
// builtins compute exactly what expr's compute, errors included; in a
// sandbox small work is untouched.
func TestGuardedBuiltinsKeepSemantics(t *testing.T) {
	payload := map[string]any{"name": "ada", "a": 2.0, "nums": []any{1.0, 2.0, 2.0, 3.0}, "s": "a,b,c"}
	exprs := []string{
		`concat([1, 2], [3], payload.nums)`,
		`concat(payload.nums)`,
		`payload.nums | concat([9])`,
		`concat(1)`,
		`concat(payload.a)`,
		`flatten([1, [2, [3, [4]]], payload.nums])`,
		`flatten([])`,
		`flatten(payload.a)`,
		`uniq([1, 1.0, "1", 2, 2, payload.a])`,
		`uniq(payload.nums)`,
		`uniq(payload.a)`,
		`upper(payload.name)`,
		`lower("ÀB")`,
		`upper(payload.a)`,
		`split(payload.s, ",")`,
		`split(payload.s, ",", 2)`,
		`split(payload.s, "")`,
		`splitAfter(payload.s, ",")`,
		`splitAfter(payload.s, ",", 1)`,
		`fromBase64("aGk=")`,
		`fromBase64("!!")`,
		`fromJSON("{\"a\": [1, 2]}")`,
		`fromJSON("nope")`,
		`sort(keys(payload))`,
		`sort(values({"a": 1, "b": 2}))`,
		`len(toPairs(payload))`,
		`keys(payload.a)`,
		`len(uniq(1..300))`,
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
			for _, sandboxed := range []bool{false, true} {
				runCtx := ctx
				if sandboxed {
					runCtx = node.WithSandbox(ctx, node.NewSandbox(1<<20, 32<<20))
				}
				got, gotErr := evalExpr(runCtx, guarded, msg)
				if wantErr != nil {
					require.Error(t, gotErr, "sandboxed=%v", sandboxed)
					continue
				}
				require.NoError(t, gotErr, "sandboxed=%v", sandboxed)
				assert.Equal(t, want, got, "sandboxed=%v", sandboxed)
			}
		})
	}
}
