package flow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	flowvars "github.com/Pegasus8/piworker/internal/vars"
)

// TestNodeTimeout caps one node test.
const TestNodeTimeout = 5 * time.Second

// testNodeTimeout is TestNodeTimeout; tests lower it (export_test.go).
var testNodeTimeout = TestNodeTimeout

// ErrTestNodeForbidden: only processing nodes whose type and instance risk
// are safe can be tested (the service maps it to forbidden).
var ErrTestNodeForbidden = errors.New("only safe processing nodes can be tested")

// TestNodeResult is the outcome of a node test, bounded like a dry-run.
type TestNodeResult struct {
	Outputs []PortPreview `json:"outputs"`
	// Error is the node's error, cut to DryRunPreviewBytes.
	Error      string  `json:"error,omitempty"`
	DurationMs float64 `json:"durationMs"`
	// Truncated: a preview was cut or outputs were left out to keep the
	// result within DryRunMaxResultBytes.
	Truncated bool `json:"truncated"`
	// TimedOut: the node did not finish within TestNodeTimeout.
	TimedOut bool `json:"timedOut"`
}

// TestNode runs one processing node of a safe type once on input, in a
// sandbox: isolated, empty variable stores and an empty secret store, a
// TestNodeTimeout deadline. The config is checked as deploy checks it
// (node.CheckConfig, then the factory and Validate): a mismatch is a
// *ValidationError, a refusal of the factory or Validate wraps
// ErrInvalidNodeConfig. An unknown type wraps ErrInvalidNodeType; any other
// category or a risk that is not safe (type or instance) wraps
// ErrTestNodeForbidden. A node error or panic is the result's Error, not an
// error of TestNode. It shares the one-at-a-time sandbox slot with DryRun
// (busy: an error wrapping ErrBusy). The node is closed afterwards.
func TestNode(ctx context.Context, reg *node.Registry, nodeType string, config map[string]any, input any) (TestNodeResult, error) {
	info, ok := reg.Get(nodeType)
	if !ok {
		return TestNodeResult{}, fmt.Errorf("%w: %s", ErrInvalidNodeType, nodeType)
	}
	if info.Category != NodeCategoryProcessing || info.Risk != node.RiskSafe {
		return TestNodeResult{}, fmt.Errorf("%w: %s is a %s node of risk %s", ErrTestNodeForbidden, nodeType, info.Category, info.Risk)
	}
	cfg := cloneConfig(config)
	if risk := InstanceRisk(&Node{Type: nodeType, Config: cfg}, reg); risk != node.RiskSafe {
		return TestNodeResult{}, fmt.Errorf("%w: this %s configuration is of risk %s", ErrTestNodeForbidden, nodeType, risk)
	}
	if err := ctx.Err(); err != nil {
		return TestNodeResult{}, err
	}
	release, err := acquireSandbox()
	if err != nil {
		return TestNodeResult{}, err
	}
	defer release()

	if issues := node.CheckConfig(info.Config, cfg); len(issues) > 0 {
		msgs := make([]string, len(issues))
		for i, issue := range issues {
			msgs[i] = "config." + issue.String()
		}
		return TestNodeResult{}, NewValidationError("%s", strings.Join(msgs, "; "))
	}
	inst, err := reg.Create(nodeType, cfg)
	if err != nil {
		return TestNodeResult{}, fmt.Errorf("%w: %w", ErrInvalidNodeConfig, err)
	}
	defer closeNode(inst)
	if _, isTrigger := inst.(node.TriggerNode); isTrigger {
		return TestNodeResult{}, fmt.Errorf("%w: %s is a trigger", ErrTestNodeForbidden, nodeType)
	}
	if err := inst.Validate(); err != nil {
		return TestNodeResult{}, fmt.Errorf("%w: %w", ErrInvalidNodeConfig, err)
	}
	_, ports := inst.Ports()

	runCtx, cancel := context.WithTimeout(ctx, testNodeTimeout)
	defer cancel()
	runCtx = sandboxContext(runCtx, flowvars.NewStore(), flowvars.NewStore())
	msg := types.NewMessage(input, payloadDataType(input)).Clone()
	msg.SetMeta("actor", "test-node")

	sess := &dryRunSession{} // for its bounded previews
	start := time.Now()
	outs, perr := processSafely(runCtx, inst, msg)
	res := TestNodeResult{Outputs: []PortPreview{}, DurationMs: durationMs(time.Since(start))}
	if perr != nil {
		res.Error = sess.cut(perr.Error())
		res.TimedOut = ctx.Err() == nil && errors.Is(runCtx.Err(), context.DeadlineExceeded)
	}

	budget := DryRunMaxResultBytes - jsonLen(TestNodeResult{Outputs: []PortPreview{}, Error: res.Error, DurationMs: res.DurationMs, Truncated: true, TimedOut: true})
	for _, p := range sess.outputPreviews(outs, ports) {
		n := jsonLen(p) + 1
		if n > budget {
			res.Truncated = true
			break
		}
		budget -= n
		res.Outputs = append(res.Outputs, p)
	}
	res.Truncated = res.Truncated || sess.previewCut.Load()
	return res, ctx.Err()
}

// cloneConfig deep-copies a node config (a test must not let the node keep
// or change the caller's map).
func cloneConfig(cfg map[string]any) map[string]any {
	if cfg == nil {
		return map[string]any{}
	}
	out, _ := types.NewMessage(cfg, types.DataTypeObject).Clone().Payload.(map[string]any)
	return out
}
