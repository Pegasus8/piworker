package flow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	flowvars "github.com/Pegasus8/piworker/internal/vars"
)

// TestNodeTimeout caps one node test.
const TestNodeTimeout = 5 * time.Second

// testNodeTimeout is TestNodeTimeout; tests lower it (export_test.go).
var testNodeTimeout = TestNodeTimeout

// testNodeGrace is how long a node test waits, past its deadline, for a node
// to return before it reports a timeout (the node keeps the sandbox slot
// until it does return).
const testNodeGrace = 250 * time.Millisecond

// ErrTestNodeForbidden: only processing nodes whose type and instance risk
// are safe can be tested (the service maps it to forbidden).
var ErrTestNodeForbidden = errors.New("only safe processing nodes can be tested")

// TestNodeResult is the outcome of a node test, bounded like a dry-run.
type TestNodeResult struct {
	// Issues are the config's problems deploy would refuse (config_type at
	// "/config/{prop}", invalid_config at "/config"); when there are any,
	// the node did not run.
	Issues  []Issue       `json:"issues"`
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
// sandbox: isolated, empty variable stores, an empty secret store, a
// node.Sandbox bounding the data it generates, a TestNodeTimeout deadline.
// The input and the config are at most DryRunMaxInputBytes of JSON each
// (else ErrDryRunInput). The config is checked as deploy checks it (Lint's
// collectors: node.CheckConfig, then the factory and Validate): its problems
// are the result's Issues (config_type at "/config/{prop}", invalid_config at
// "/config") and the node does not run then (no error). An unknown type
// wraps ErrInvalidNodeType; any other
// category or a risk that is not safe (type or instance) wraps
// ErrTestNodeForbidden. A node error or panic, an output over the per-value
// cap, is the result's Error, not an error of TestNode. TestNode returns at
// its deadline (plus a short grace) even if the node ignores cancellation.
// It shares the one-at-a-time sandbox slot with DryRun (busy: an error
// wrapping ErrBusy); work still running when it returns (the node, a
// goroutine it started with node.Go) keeps the slot until it returns. The
// node is closed once it returned.
func TestNode(ctx context.Context, reg *node.Registry, nodeType string, config map[string]any, input any) (TestNodeResult, error) {
	info, ok := reg.Get(nodeType)
	if !ok {
		return TestNodeResult{}, fmt.Errorf("%w: %s", ErrInvalidNodeType, nodeType)
	}
	if info.Category != NodeCategoryProcessing || info.Risk != node.RiskSafe {
		return TestNodeResult{}, fmt.Errorf("%w: %s is a %s node of risk %s", ErrTestNodeForbidden, nodeType, info.Category, info.Risk)
	}
	if err := checkInputSize("the input", input); err != nil {
		return TestNodeResult{}, err
	}
	if err := checkInputSize("the config", config); err != nil {
		return TestNodeResult{}, err
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
	held := true
	defer func() {
		if held {
			release()
		}
	}()

	// The config is checked by Lint's collectors, as deploy checks a node:
	// config typing, then the factory and Validate.
	probe := &Flow{Nodes: []Node{{ID: "node", Type: nodeType, Config: cfg, Enabled: true}}}
	if fds := configFindings(probe, reg); len(fds) > 0 {
		return testNodeIssues(fds), nil
	}
	instances, fds := instantiateNodes(probe, reg, nil)
	if len(fds) > 0 {
		closeInstances(instances)
		return testNodeIssues(fds), nil
	}
	inst := instances[0]
	if _, isTrigger := inst.(node.TriggerNode); isTrigger {
		closeNode(inst)
		return TestNodeResult{}, fmt.Errorf("%w: %s is a trigger", ErrTestNodeForbidden, nodeType)
	}
	_, ports := inst.Ports()

	runCtx, cancel := context.WithTimeout(ctx, testNodeTimeout)
	defer cancel()
	sb := newSandbox()
	nodeCtx := sandboxContext(runCtx, flowvars.NewStore(), flowvars.NewStore(), sb)
	msg := types.NewMessage(input, payloadDataType(input)).Clone()
	msg.SetMeta("actor", "test-node")

	type outcome struct {
		outs []*types.Message
		err  error
	}
	done := make(chan outcome, 1)
	start := time.Now()
	go func() {
		outs, err := sandboxProcess(nodeCtx, sb, inst, msg)
		done <- outcome{outs, err}
	}()
	var out outcome
	returned := true
	select {
	case out = <-done:
	case <-runCtx.Done():
		select {
		case out = <-done:
		case <-time.After(testNodeGrace):
			returned = false
		}
	}
	res := TestNodeResult{Issues: []Issue{}, Outputs: []PortPreview{}, DurationMs: durationMs(time.Since(start))}
	sess := &dryRunSession{} // for its bounded previews

	if !returned {
		// The node ignores cancellation: report the timeout now, close the
		// node and free the slot once it returns.
		held = false
		go func() {
			<-done
			closeNode(inst)
			sb.Wait()
			release()
		}()
		res.TimedOut = ctx.Err() == nil
		res.Error = fmt.Sprintf("the node did not finish within %s", testNodeTimeout)
		return res, ctx.Err()
	}
	closeNode(inst)
	if sb.Busy() {
		held = false
		go func() {
			sb.Wait()
			release()
		}()
	}

	if out.err != nil {
		res.Error = sess.cut(out.err.Error())
		res.TimedOut = ctx.Err() == nil && errors.Is(runCtx.Err(), context.DeadlineExceeded)
	}
	budget := DryRunMaxResultBytes - jsonLen(TestNodeResult{Issues: []Issue{}, Outputs: []PortPreview{}, Error: res.Error, DurationMs: res.DurationMs, Truncated: true, TimedOut: true})
	for _, p := range sess.outputPreviews(out.outs, ports) {
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

// testNodeIssues is the result of a node test whose config deploy would
// refuse: the issues, at "/config/{prop}" (config typing) or "/config" (the
// factory or Validate), each message cut to DryRunPreviewBytes; the node did
// not run. The messages are not hidden: the whole config is the caller's.
func testNodeIssues(fds []finding) TestNodeResult {
	res := TestNodeResult{Issues: []Issue{}, Outputs: []PortPreview{}}
	sess := &dryRunSession{} // for its bounded previews
	for _, fd := range fds {
		path := "/config"
		if fd.prop != "" {
			path += "/" + pointerEscape(fd.prop)
		}
		res.Issues = append(res.Issues, Issue{Path: path, Code: fd.code, Severity: fd.sev, Message: sess.cut(fd.msg)})
	}
	res.Truncated = sess.previewCut.Load()
	return res
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
