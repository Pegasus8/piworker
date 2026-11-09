package flow_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	_ "github.com/Pegasus8/piworker/internal/node/builtin"
	"github.com/Pegasus8/piworker/internal/secrets"
	flowvars "github.com/Pegasus8/piworker/internal/vars"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dn is a node of a dry-run test flow.
type dn struct {
	id, typ string
	cfg     map[string]any
}

// dryFlow builds a flow from nodes and "a>b" connections (output → input).
func dryFlow(nodes []dn, conns ...string) *flow.Flow {
	f := flow.NewFlow("dry")
	for _, d := range nodes {
		n := flow.NewNode(d.typ, flow.NodeCategoryProcessing)
		n.ID, n.Config = d.id, d.cfg
		if n.Config == nil {
			n.Config = map[string]any{}
		}
		f.AddNode(*n)
	}
	for _, c := range conns {
		src, dst, _ := strings.Cut(c, ">")
		f.AddConnection(*flow.NewConnection(src, "output", dst, "input"))
	}
	return f
}

// hitServer counts requests and records the Authorization headers.
func hitServer(t *testing.T) (*httptest.Server, *atomic.Int32, func() []string) {
	t.Helper()
	var hits atomic.Int32
	var mu sync.Mutex
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), auths...) }
}

func stepOf(res flow.DryRunResult, nodeID string) (flow.TraceStep, bool) {
	for _, s := range res.Trace {
		if s.NodeID == nodeID {
			return s, true
		}
	}
	return flow.TraceStep{}, false
}

func resultJSON(t *testing.T, res flow.DryRunResult) string {
	t.Helper()
	data, err := json.Marshal(res)
	require.NoError(t, err)
	return string(data)
}

// TestDryRunStubsHTTPAndMocksContinue: trigger → transform → action-http →
// transform. The HTTP node never reaches the server; it is listed in
// wouldExecute with its input; without a mock nothing runs after it, with a
// mock the downstream node runs on the mock.
func TestDryRunStubsHTTPAndMocksContinue(t *testing.T) {
	srv, hits, _ := hitServer(t)
	f := dryFlow([]dn{
		{"trig", "trigger-manual", nil},
		{"double", "process-transform", map[string]any{"expression": `{"v": payload.x * 2}`}},
		{"http", "action-http", map[string]any{"url": srv.URL + "/api", "method": "POST", "body": "{{payload}}"}},
		{"after", "process-transform", map[string]any{"expression": `payload.status`}},
	}, "trig>double", "double>http", "http>after")

	res, err := flow.DryRun(context.Background(), f, node.DefaultRegistry, flow.DryRunInput{
		TriggerNodeID: "trig", Payload: map[string]any{"x": 2},
	})
	require.NoError(t, err)
	assert.Zero(t, hits.Load(), "the HTTP node never sends")
	assert.False(t, res.TimedOut)
	assert.False(t, res.Capped)

	require.Len(t, res.WouldExecute, 1)
	w := res.WouldExecute[0]
	assert.Equal(t, "http", w.NodeID)
	assert.Equal(t, "action-http", w.Type)
	assert.Equal(t, node.RiskExternal, w.Risk)
	assert.JSONEq(t, `{"v":4}`, w.Input)

	ids := make([]string, 0, len(res.Trace))
	for _, s := range res.Trace {
		ids = append(ids, s.NodeID)
	}
	assert.Equal(t, []string{"trig", "double", "http"}, ids, "execution order; nothing after an unmocked stub")
	trig, _ := stepOf(res, "trig")
	assert.True(t, trig.Stubbed, "the trigger is stubbed: the payload is injected at its output")
	require.Len(t, trig.Outputs, 1)
	assert.Equal(t, "output", trig.Outputs[0].Port)
	assert.JSONEq(t, `{"x":2}`, trig.Outputs[0].Preview)
	double, _ := stepOf(res, "double")
	assert.False(t, double.Stubbed)
	assert.JSONEq(t, `{"x":2}`, double.Input)
	require.Len(t, double.Outputs, 1)
	assert.Equal(t, "output", double.Outputs[0].Port)
	assert.JSONEq(t, `{"v":4}`, double.Outputs[0].Preview)
	stub, _ := stepOf(res, "http")
	assert.True(t, stub.Stubbed)
	assert.Empty(t, stub.Outputs)

	res, err = flow.DryRun(context.Background(), f, node.DefaultRegistry, flow.DryRunInput{
		TriggerNodeID: "trig", Payload: map[string]any{"x": 2},
		Mocks: map[string]any{"http": map[string]any{"status": 201}},
	})
	require.NoError(t, err)
	assert.Zero(t, hits.Load())
	stub, _ = stepOf(res, "http")
	require.Len(t, stub.Outputs, 1)
	assert.JSONEq(t, `{"status":201}`, stub.Outputs[0].Preview)
	after, ok := stepOf(res, "after")
	require.True(t, ok, "a mocked stub lets the run continue")
	assert.False(t, after.Stubbed)
	require.Len(t, after.Outputs, 1)
	assert.Equal(t, "201", after.Outputs[0].Preview)
}

// TestDryRunNeverSeesRealSecrets: a canary in secrets.DefaultStore never
// reaches a dry-run, whatever node references it (template-rendering and
// secret-reading types alike), while a normal runtime still resolves it.
func TestDryRunNeverSeesRealSecrets(t *testing.T) {
	const canary = "CANARY-dry-run-4b1d"
	require.NoError(t, secrets.DefaultStore.Set("dry_canary", canary))
	t.Cleanup(func() { _ = secrets.DefaultStore.Delete("dry_canary") })
	require.NoError(t, flowvars.DefaultStore.Set("dry_canary_var", canary))
	t.Cleanup(func() { _ = flowvars.DefaultStore.Delete("dry_canary_var") })
	ref := "{{secret.dry_canary}}"
	srv, hits, auths := hitServer(t)

	nodes := []dn{
		{"trig", "trigger-manual", nil},
		{"hook", "trigger-webhook", map[string]any{"path": "dry-canary-hook", "token": ref}},
		{"mqttin", "trigger-mqtt", map[string]any{"broker": "tcp://127.0.0.1:1", "topic": "t", "password": ref}},
		{"tmpl", "process-template", map[string]any{"template": "secret=" + ref + " var={{vars.dry_canary_var}} p={{payload}}"}},
		{"expr", "process-transform", map[string]any{"expression": `{"vars": vars, "p": payload}`}},
		{"http", "action-http", map[string]any{"url": srv.URL + "/x?k=" + ref, "allowPrivate": true,
			"headers": map[string]any{"Authorization": "Bearer " + ref}}},
		{"notify", "action-notify", map[string]any{"url": "https://hooks.example.com/" + ref}},
		{"tg", "action-telegram", map[string]any{"botToken": ref, "chatId": "1", "message": "{{payload}}"}},
		{"mail", "action-email", map[string]any{"host": "smtp.example.com", "from": "a@example.com", "to": "b@example.com",
			"username": ref, "password": ref, "body": "{{payload}} " + ref}},
		{"mqtt", "action-mqtt", map[string]any{"broker": "tcp://127.0.0.1:1", "topic": "t", "password": ref, "payload": ref}},
		{"db", "action-database", map[string]any{"driver": "sqlite", "dsn": ref, "query": "SELECT 1"}},
		{"after", "process-template", map[string]any{"template": "after " + ref + " {{payload}}"}},
	}
	conns := []string{"trig>tmpl", "tmpl>expr"}
	mocks := map[string]any{}
	for _, id := range []string{"http", "notify", "tg", "mail", "mqtt", "db"} {
		conns = append(conns, "expr>"+id, id+">after")
		mocks[id] = map[string]any{"mocked": id}
	}
	f := dryFlow(nodes, conns...)

	res, err := flow.DryRun(context.Background(), f, node.DefaultRegistry, flow.DryRunInput{
		TriggerNodeID: "trig", Payload: "hello", Mocks: mocks,
	})
	require.NoError(t, err)
	out := resultJSON(t, res)
	assert.NotContains(t, out, canary, "no real secret or global variable reaches the dry-run")
	assert.Zero(t, hits.Load())
	assert.Len(t, res.WouldExecute, 6)
	assert.Equal(t, []string{"dry_canary"}, res.MissingSecrets)
	tmpl, ok := stepOf(res, "tmpl")
	require.True(t, ok)
	require.Len(t, tmpl.Outputs, 1)
	assert.Contains(t, tmpl.Outputs[0].Preview, "var= ", "the global variable store is empty")
	after := 0
	for _, s := range res.Trace {
		if s.NodeID == "after" {
			after++
		}
	}
	assert.Equal(t, 6, after, "every mocked stub let the run continue")

	// A normal runtime resolves the same reference.
	real := dryFlow([]dn{
		{"trig", "trigger-manual", nil},
		{"http", "action-http", map[string]any{"url": srv.URL + "/x", "allowPrivate": true,
			"headers": map[string]any{"Authorization": "Bearer " + ref}}},
	}, "trig>http")
	m := flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop())
	defer m.StopAll()
	require.NoError(t, m.Deploy(context.Background(), real))
	rt, _ := m.GetRuntime(real.ID)
	require.NoError(t, rt.InjectTrigger("trig", "go"))
	require.Eventually(t, func() bool { return hits.Load() == 1 }, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"Bearer " + canary}, auths())
}

// TestDryRunVariablesAreIsolated: set-var writes only to the dry-run's own
// stores; the flow store is seeded from FlowVars; real variables are not
// visible.
func TestDryRunVariablesAreIsolated(t *testing.T) {
	require.NoError(t, flowvars.DefaultStore.Set("dry_real_global", "real"))
	t.Cleanup(func() { _ = flowvars.DefaultStore.Delete("dry_real_global") })

	f := dryFlow([]dn{
		{"trig", "trigger-manual", nil},
		{"setg", "set-var", map[string]any{"key": "dry_written", "value": "payload", "scope": "global"}},
		{"setf", "set-var", map[string]any{"key": "dry_written", "value": "payload", "scope": "flow"}},
		{"getg", "get-var", map[string]any{"key": "dry_real_global", "scope": "global"}},
		{"getf", "get-var", map[string]any{"key": "seed", "scope": "flow"}},
	}, "trig>setg", "setg>setf", "setf>getg", "getg>getf")
	t.Cleanup(func() { flowvars.DefaultFlows.Forget(f.ID) })

	res, err := flow.DryRun(context.Background(), f, node.DefaultRegistry, flow.DryRunInput{
		TriggerNodeID: "trig", Payload: "written-by-dry-run", FlowVars: map[string]any{"seed": 7},
	})
	require.NoError(t, err)
	getg, ok := stepOf(res, "getg")
	require.True(t, ok)
	assert.Equal(t, "null", getg.Outputs[0].Preview, "real global variables are not visible")
	getf, ok := stepOf(res, "getf")
	require.True(t, ok)
	assert.Equal(t, "7", getf.Outputs[0].Preview, "the flow store is seeded from FlowVars")

	_, found := flowvars.DefaultStore.Get("dry_written")
	assert.False(t, found, "the real global store is untouched")
	local, err := flowvars.DefaultFlows.Get(f.ID)
	require.NoError(t, err)
	_, found = local.Get("dry_written")
	assert.False(t, found, "the real flow store is untouched")
}

// TestDryRunTimesOut: a delay longer than the wall-clock cap ends the
// dry-run with timedOut, promptly.
func TestDryRunTimesOut(t *testing.T) {
	assert.Equal(t, 10*time.Second, flow.DryRunTimeout)
	flow.SetDryRunTimeout(t, 150*time.Millisecond)
	f := dryFlow([]dn{
		{"trig", "trigger-manual", nil},
		{"wait", "process-delay", map[string]any{"delay": 60000}},
		{"after", "process-transform", map[string]any{"expression": "payload"}},
	}, "trig>wait", "wait>after")

	start := time.Now()
	res, err := flow.DryRun(context.Background(), f, node.DefaultRegistry, flow.DryRunInput{TriggerNodeID: "trig", Payload: 1})
	require.NoError(t, err)
	assert.True(t, res.TimedOut)
	assert.Less(t, time.Since(start), 3*time.Second)
	_, ran := stepOf(res, "after")
	assert.False(t, ran)
}

// TestDryRunRefusesBadInput: the trigger must be an enabled input node of
// the flow, mocks must name stubbed nodes, and the flow has a node cap.
func TestDryRunRefusesBadInput(t *testing.T) {
	f := dryFlow([]dn{
		{"trig", "trigger-manual", nil},
		{"t", "process-transform", map[string]any{"expression": "payload"}},
		{"log", "action-log", nil},
	}, "trig>t", "t>log")
	for name, in := range map[string]flow.DryRunInput{
		"unknown trigger":      {TriggerNodeID: "nope"},
		"not an input node":    {TriggerNodeID: "t"},
		"mock of unknown node": {TriggerNodeID: "trig", Mocks: map[string]any{"nope": 1}},
		"mock of a real node":  {TriggerNodeID: "trig", Mocks: map[string]any{"t": 1}},
		"mock of the trigger":  {TriggerNodeID: "trig", Mocks: map[string]any{"trig": 1}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := flow.DryRun(context.Background(), f, node.DefaultRegistry, in)
			require.ErrorIs(t, err, flow.ErrDryRunInput)
		})
	}

	f.Nodes[0].Enabled = false
	_, err := flow.DryRun(context.Background(), f, node.DefaultRegistry, flow.DryRunInput{TriggerNodeID: "trig"})
	require.Error(t, err, "a disabled trigger cannot start a dry-run")

	big := []dn{{"trig", "trigger-manual", nil}}
	var conns []string
	prev := "trig"
	for i := 0; i < flow.DryRunMaxNodes; i++ {
		id := "n" + strings.Repeat("x", i+1)
		big = append(big, dn{id, "process-transform", map[string]any{"expression": "payload"}})
		conns = append(conns, prev+">"+id)
		prev = id
	}
	_, err = flow.DryRun(context.Background(), dryFlow(big, conns...), node.DefaultRegistry, flow.DryRunInput{TriggerNodeID: "trig"})
	require.ErrorIs(t, err, flow.ErrDryRunInput, "more than %d nodes", flow.DryRunMaxNodes)
}

// TestDryRunRefusesInvalidFlow: the flow is validated like a deploy first;
// failures wrap the deploy's own invalid-flow errors.
func TestDryRunRefusesInvalidFlow(t *testing.T) {
	f := dryFlow([]dn{
		{"trig", "trigger-manual", nil},
		{"t", "process-transform", map[string]any{"expression": 42}},
	}, "trig>t")
	_, err := flow.DryRun(context.Background(), f, node.DefaultRegistry, flow.DryRunInput{TriggerNodeID: "trig"})
	var verr *flow.ValidationError
	require.ErrorAs(t, err, &verr, "mistyped config, as deploy reports it")

	f = dryFlow([]dn{
		{"trig", "trigger-manual", nil},
		{"t", "process-transform", map[string]any{"expression": "payload +"}},
	}, "trig>t")
	_, err = flow.DryRun(context.Background(), f, node.DefaultRegistry, flow.DryRunInput{TriggerNodeID: "trig"})
	require.ErrorIs(t, err, flow.ErrInvalidNodeConfig, "a factory error, as deploy reports it")

	f = dryFlow([]dn{
		{"trig", "trigger-manual", nil},
		{"t", "process-transform", map[string]any{"expression": "payload"}},
	}, "trig>t", "t>missing")
	_, err = flow.DryRun(context.Background(), f, node.DefaultRegistry, flow.DryRunInput{TriggerNodeID: "trig"})
	require.ErrorAs(t, err, &verr, "a structural error, as deploy reports it")
}

// TestDryRunDoesNotModifyTheFlow: the caller's flow is left as it was (the
// runtime fills ports in on its own copy).
func TestDryRunDoesNotModifyTheFlow(t *testing.T) {
	f := dryFlow([]dn{
		{"trig", "trigger-manual", nil},
		{"t", "process-transform", map[string]any{"expression": "payload"}},
	}, "trig>t")
	before, err := json.Marshal(f)
	require.NoError(t, err)
	_, err = flow.DryRun(context.Background(), f, node.DefaultRegistry, flow.DryRunInput{TriggerNodeID: "trig", Payload: 1})
	require.NoError(t, err)
	after, err := json.Marshal(f)
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after))
}

// TestDryRunBoundsPreviews: each preview is at most 2 KiB and the whole
// result at most 64 KiB, flagged truncated.
func TestDryRunBoundsPreviews(t *testing.T) {
	big := strings.Repeat("é", 4000) // 8000 bytes
	f := dryFlow([]dn{
		{"trig", "trigger-manual", nil},
		{"t", "process-transform", map[string]any{"expression": "payload"}},
	}, "trig>t")
	res, err := flow.DryRun(context.Background(), f, node.DefaultRegistry, flow.DryRunInput{TriggerNodeID: "trig", Payload: big})
	require.NoError(t, err)
	assert.True(t, res.Truncated)
	step, ok := stepOf(res, "t")
	require.True(t, ok)
	assert.LessOrEqual(t, len(step.Input), flow.DryRunPreviewBytes)
	assert.LessOrEqual(t, len(step.Outputs[0].Preview), flow.DryRunPreviewBytes)
	assert.True(t, strings.HasSuffix(step.Input, "…"))

	// 40 nodes each showing a 2 KiB input and output preview: ~160 KiB.
	nodes := []dn{{"trig", "trigger-manual", nil}}
	var conns []string
	prev := "trig"
	for i := 0; i < 40; i++ {
		id := "n" + strings.Repeat("x", i+1)
		nodes = append(nodes, dn{id, "process-transform", map[string]any{"expression": "payload"}})
		conns = append(conns, prev+">"+id)
		prev = id
	}
	res, err = flow.DryRun(context.Background(), dryFlow(nodes, conns...), node.DefaultRegistry, flow.DryRunInput{TriggerNodeID: "trig", Payload: big})
	require.NoError(t, err)
	assert.True(t, res.Truncated)
	assert.LessOrEqual(t, len(resultJSON(t, res)), flow.DryRunMaxResultBytes)
	assert.NotEmpty(t, res.Trace)
	assert.Less(t, len(res.Trace), 41, "steps that do not fit are left out")
}

// TestDryRunErrorsAreTraced: a node error ends its branch and shows in the
// trace; the dry-run itself succeeds.
func TestDryRunErrorsAreTraced(t *testing.T) {
	f := dryFlow([]dn{
		{"trig", "trigger-manual", nil},
		{"t", "process-transform", map[string]any{"expression": `payload.a.b()`}},
	}, "trig>t")
	res, err := flow.DryRun(context.Background(), f, node.DefaultRegistry, flow.DryRunInput{TriggerNodeID: "trig", Payload: map[string]any{}})
	require.NoError(t, err)
	step, ok := stepOf(res, "t")
	require.True(t, ok)
	assert.NotEmpty(t, step.Error)
	assert.Empty(t, step.Outputs)
}

// TestDryRunCallerCancel: a cancelled caller context ends the dry-run with
// the context's error.
func TestDryRunCallerCancel(t *testing.T) {
	f := dryFlow([]dn{
		{"trig", "trigger-manual", nil},
		{"wait", "process-delay", map[string]any{"delay": 60000}},
	}, "trig>wait")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := flow.DryRun(ctx, f, node.DefaultRegistry, flow.DryRunInput{TriggerNodeID: "trig"})
	require.True(t, errors.Is(err, context.DeadlineExceeded), "got %v", err)
}
