package flow_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	_ "github.com/Pegasus8/piworker/internal/node/builtin"
	"github.com/Pegasus8/piworker/internal/secrets"
	flowvars "github.com/Pegasus8/piworker/internal/vars"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTestNodeRunsTransform: a safe processing node runs once on the input.
func TestTestNodeRunsTransform(t *testing.T) {
	res, err := flow.TestNode(context.Background(), node.DefaultRegistry, "process-transform",
		map[string]any{"expression": `{"tripled": payload.x * 3}`}, map[string]any{"x": 2})
	require.NoError(t, err)
	assert.Empty(t, res.Error)
	assert.False(t, res.TimedOut)
	require.Len(t, res.Outputs, 1)
	assert.Equal(t, "output", res.Outputs[0].Port)
	assert.JSONEq(t, `{"tripled":6}`, res.Outputs[0].Preview)
	assert.GreaterOrEqual(t, res.DurationMs, 0.0)
}

// TestTestNodeRefusesUnsafeTypes: outputs, triggers and anything not safe
// are forbidden; unknown types are invalid.
func TestTestNodeRefusesUnsafeTypes(t *testing.T) {
	for typ, cfg := range map[string]map[string]any{
		"action-http":    {"url": "https://example.com"},
		"action-log":     nil, // safe, but an output node
		"trigger-manual": nil,
		"trigger-tool":   nil,
		"action-command": {"command": "true"},
	} {
		t.Run(typ, func(t *testing.T) {
			_, err := flow.TestNode(context.Background(), node.DefaultRegistry, typ, cfg, nil)
			require.ErrorIs(t, err, flow.ErrTestNodeForbidden)
		})
	}
	_, err := flow.TestNode(context.Background(), node.DefaultRegistry, "no-such-type", nil, nil)
	require.ErrorIs(t, err, flow.ErrInvalidNodeType)
}

// TestTestNodeRefusesUnsafeInstance: a safe processing type whose instance
// risk is raised (by a rule of InstanceRisk) is refused too.
func TestTestNodeRefusesUnsafeInstance(t *testing.T) {
	// No builtin safe processing type escalates today, so declare one: the
	// instance rules key on the type name, and "action-http" with a dynamic
	// destination is privileged whatever its declared risk.
	reg := node.NewRegistry()
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "action-http", Category: flow.NodeCategoryProcessing, Risk: node.RiskSafe},
		func(cfg map[string]any) (node.Node, error) {
			return node.DefaultRegistry.Create("process-transform", map[string]any{"expression": "payload"})
		}))

	_, err := flow.TestNode(context.Background(), reg, "action-http", map[string]any{"url": "{{payload.url}}"}, nil)
	require.ErrorIs(t, err, flow.ErrTestNodeForbidden)
	_, err = flow.TestNode(context.Background(), reg, "action-http", map[string]any{"url": "https://example.com"}, nil)
	require.NoError(t, err, "the same type with a fixed destination stays safe")
}

// TestTestNodeChecksConfigLikeDeploy: schema mismatches are validation
// errors, factory and Validate refusals wrap ErrInvalidNodeConfig.
func TestTestNodeChecksConfigLikeDeploy(t *testing.T) {
	_, err := flow.TestNode(context.Background(), node.DefaultRegistry, "process-transform", map[string]any{"expression": 42}, nil)
	var verr *flow.ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Contains(t, err.Error(), "config.expression")

	_, err = flow.TestNode(context.Background(), node.DefaultRegistry, "process-transform", map[string]any{"expression": "payload +"}, nil)
	require.ErrorIs(t, err, flow.ErrInvalidNodeConfig)
}

// TestTestNodeIsIsolated: the node sees empty stores and changes none of the
// real ones.
func TestTestNodeIsIsolated(t *testing.T) {
	const canary = "CANARY-test-node-91c2"
	require.NoError(t, secrets.DefaultStore.Set("tn_canary", canary))
	t.Cleanup(func() { _ = secrets.DefaultStore.Delete("tn_canary") })
	require.NoError(t, flowvars.DefaultStore.Set("tn_global", canary))
	t.Cleanup(func() { _ = flowvars.DefaultStore.Delete("tn_global") })

	res, err := flow.TestNode(context.Background(), node.DefaultRegistry, "process-template",
		map[string]any{"template": "{{secret.tn_canary}}|{{vars.tn_global}}|{{payload}}"}, "in")
	require.NoError(t, err)
	require.Len(t, res.Outputs, 1)
	assert.NotContains(t, res.Outputs[0].Preview, canary)

	res, err = flow.TestNode(context.Background(), node.DefaultRegistry, "get-var",
		map[string]any{"key": "tn_global", "scope": "global"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "null", res.Outputs[0].Preview, "real global variables are not visible")

	for _, scope := range []string{"global", "flow"} {
		res, err = flow.TestNode(context.Background(), node.DefaultRegistry, "set-var",
			map[string]any{"key": "tn_written", "value": "payload", "scope": scope}, "x")
		require.NoError(t, err)
		assert.Empty(t, res.Error, "scope %s works in the sandbox", scope)
	}
	_, found := flowvars.DefaultStore.Get("tn_written")
	assert.False(t, found, "the real global store is untouched")
}

// TestTestNodeTimesOut: a node that outlives the timeout reports timedOut.
func TestTestNodeTimesOut(t *testing.T) {
	assert.Equal(t, 5*time.Second, flow.TestNodeTimeout)
	flow.SetTestNodeTimeout(t, 100*time.Millisecond)
	start := time.Now()
	res, err := flow.TestNode(context.Background(), node.DefaultRegistry, "process-delay", map[string]any{"delay": 60000}, 1)
	require.NoError(t, err)
	assert.True(t, res.TimedOut)
	assert.NotEmpty(t, res.Error)
	assert.Empty(t, res.Outputs)
	assert.Less(t, time.Since(start), 3*time.Second)
}

// TestTestNodeBoundsResult: previews are cut to 2 KiB (truncated).
func TestTestNodeBoundsResult(t *testing.T) {
	res, err := flow.TestNode(context.Background(), node.DefaultRegistry, "process-transform",
		map[string]any{"expression": "payload"}, strings.Repeat("x", 10000))
	require.NoError(t, err)
	assert.True(t, res.Truncated)
	require.Len(t, res.Outputs, 1)
	assert.LessOrEqual(t, len(res.Outputs[0].Preview), flow.DryRunPreviewBytes)
}

// TestTestNodeReportsNodeErrors: a node error is the result's error.
func TestTestNodeReportsNodeErrors(t *testing.T) {
	res, err := flow.TestNode(context.Background(), node.DefaultRegistry, "process-transform",
		map[string]any{"expression": `payload.a.b()`}, map[string]any{})
	require.NoError(t, err)
	assert.NotEmpty(t, res.Error)
	assert.Empty(t, res.Outputs)
}

// TestTestNodeKeepsCallerConfig: the node gets a copy of the config.
func TestTestNodeKeepsCallerConfig(t *testing.T) {
	cfg := map[string]any{"expression": "payload", "nested": map[string]any{"a": 1}}
	_, err := flow.TestNode(context.Background(), node.DefaultRegistry, "process-transform", cfg, 1)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"expression": "payload", "nested": map[string]any{"a": 1}}, cfg)
}

// TestTestNodeBoundsExpressionWork: the reviewers' expressions — quadratic
// string growth and doubling — fail quickly at the sandbox's data limits
// instead of running on (or allocating gigabytes) after the call.
func TestTestNodeBoundsExpressionWork(t *testing.T) {
	waitFreeSandbox(t)
	before := stableGoroutines()
	for _, src := range []string{
		`len(reduce(1..300000, #acc + "xxxxxxxxxx", ""))`,
		`len(reduce(1..32, #acc + #acc, "x"))`,
	} {
		start := time.Now()
		res, err := flow.TestNode(context.Background(), node.DefaultRegistry, "process-transform",
			map[string]any{"expression": src}, nil)
		require.NoError(t, err, src)
		assert.Contains(t, res.Error, node.ErrSandboxLimit.Error(), src)
		assert.False(t, res.TimedOut, src)
		assert.Less(t, time.Since(start), 2*time.Second, src)
	}
	assert.LessOrEqual(t, goroutinesSettleTo(before), before)
}
