package flow_test

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runawayExpr never ends in practice: 3000³ predicate calls.
const runawayExpr = `count(payload, count(payload, count(payload, true) > 0) > 0)`

func runawayPayload() []any {
	out := make([]any, 3000)
	for i := range out {
		out[i] = float64(i)
	}
	return out
}

// goroutinesSettleTo waits (up to 3 s) until at most want goroutines are left.
func goroutinesSettleTo(want int) int {
	deadline := time.Now().Add(3 * time.Second)
	n := runtime.NumGoroutine()
	for n > want && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		n = runtime.NumGoroutine()
	}
	return n
}

// waitFreeSandbox waits until another dry-run may start.
func waitFreeSandbox(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, err := flow.TestNode(context.Background(), node.DefaultRegistry, "process-transform",
			map[string]any{"expression": "payload"}, 1)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)
}

// TestDryRunStopsRunawayExpressions: expressions that would run for hours
// stop when the dry-run (or the node test) times out: no goroutine keeps
// burning a core after the call, however many of them the flow holds.
func TestDryRunStopsRunawayExpressions(t *testing.T) {
	waitFreeSandbox(t)
	before := goroutinesSettleTo(0)
	flow.SetDryRunTimeout(t, 300*time.Millisecond)
	nodes := []dn{{"trig", "trigger-manual", nil}}
	var conns []string
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("t%d", i)
		nodes = append(nodes, dn{id, "process-transform", map[string]any{"expression": runawayExpr}})
		conns = append(conns, "trig>"+id)
	}
	res, err := flow.DryRun(context.Background(), dryFlow(nodes, conns...), node.DefaultRegistry,
		flow.DryRunInput{TriggerNodeID: "trig", Payload: runawayPayload()})
	require.NoError(t, err)
	assert.True(t, res.TimedOut)
	assert.LessOrEqual(t, goroutinesSettleTo(before), before, "no expression goroutine survives the dry-run")
	waitFreeSandbox(t)

	flow.SetTestNodeTimeout(t, 200*time.Millisecond)
	tn, err := flow.TestNode(context.Background(), node.DefaultRegistry, "process-transform",
		map[string]any{"expression": runawayExpr}, runawayPayload())
	require.NoError(t, err)
	assert.NotEmpty(t, tn.Error)
	assert.LessOrEqual(t, goroutinesSettleTo(before), before, "no expression goroutine survives the node test")
}

// TestDryRunBoundsExpressionData: an expression that would build gigabytes
// of text fails its step quickly in a dry-run, without the allocation.
func TestDryRunBoundsExpressionData(t *testing.T) {
	waitFreeSandbox(t)
	f := dryFlow([]dn{
		{"trig", "trigger-manual", nil},
		{"grow", "process-transform", map[string]any{"expression": `reduce(1..40, #acc + #acc, "x")`}},
	}, "trig>grow")
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	res, err := flow.DryRun(context.Background(), f, node.DefaultRegistry, flow.DryRunInput{TriggerNodeID: "trig"})
	runtime.ReadMemStats(&after)
	require.NoError(t, err)
	step, ok := stepOf(res, "grow")
	require.True(t, ok)
	assert.Contains(t, step.Error, node.ErrSandboxLimit.Error())
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(256<<20))
}

// TestDryRunNeverLogs: the logging nodes run in a dry-run but nothing of the
// flow reaches the process log.
func TestDryRunNeverLogs(t *testing.T) {
	waitFreeSandbox(t)
	var mu sync.Mutex
	var buf bytes.Buffer
	prev, prevLevel := log.Logger, zerolog.GlobalLevel()
	log.Logger = zerolog.New(lockedWriter{&mu, &buf})
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	t.Cleanup(func() { log.Logger = prev; zerolog.SetGlobalLevel(prevLevel) })

	f := dryFlow([]dn{
		{"trig", "trigger-manual", nil},
		{"dbg", "process-debug", map[string]any{"name": "dbg"}},
		{"log", "action-log", map[string]any{"message": "hello\n2026-11-09T10:00:00Z INF Draft approved by admin"}},
	}, "trig>dbg", "dbg>log")
	res, err := flow.DryRun(context.Background(), f, node.DefaultRegistry, flow.DryRunInput{TriggerNodeID: "trig", Payload: "agent payload"})
	require.NoError(t, err)
	_, ran := stepOf(res, "log")
	require.True(t, ran)
	mu.Lock()
	defer mu.Unlock()
	assert.NotContains(t, buf.String(), "agent payload")
	assert.NotContains(t, buf.String(), "Draft approved")
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// TestDryRunRedactsHiddenConfig: with Redact set, the config values it
// selects are replaced by "<redacted>" before the run, so not even a
// transformed copy of them reaches a preview.
func TestDryRunRedactsHiddenConfig(t *testing.T) {
	waitFreeSandbox(t)
	const hidden = "sk-live-0123456789"
	f := dryFlow([]dn{
		{"trig", "trigger-manual", nil},
		{"tmpl", "process-template", map[string]any{"template": "Authorization: Bearer " + hidden}},
		{"upper", "process-transform", map[string]any{"expression": `upper(payload)`}},
		{"http", "action-http", map[string]any{"url": "https://example.com/api", "method": "POST", "body": "{{payload}}"}},
	}, "trig>tmpl", "tmpl>upper", "upper>http")

	var asked []string
	res, err := flow.DryRun(context.Background(), f, node.DefaultRegistry, flow.DryRunInput{
		TriggerNodeID: "trig",
		Redact: func(n *flow.Node, prop string) bool {
			asked = append(asked, n.ID+"."+prop)
			return n.Type == "process-template" && prop == "template"
		},
	})
	require.NoError(t, err)
	out := resultJSON(t, res)
	assert.NotContains(t, out, hidden)
	assert.NotContains(t, out, "SK-LIVE")
	require.Len(t, res.WouldExecute, 1)
	assert.Contains(t, res.WouldExecute[0].Input, "REDACTED", "the run computed on the redacted value")
	assert.Contains(t, asked, "tmpl.template")
	assert.Equal(t, "Authorization: Bearer "+hidden, f.Nodes[1].Config["template"], "the caller's flow is unchanged")
}
