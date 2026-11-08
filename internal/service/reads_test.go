package service

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readsEnv is a Reads over a real store, runtime manager and registry.
type readsEnv struct {
	t       *testing.T
	store   *storage.SQLiteStore
	manager *flow.RuntimeManager
	secrets *secrets.Store
	reads   *Reads
	clock   *fakeClock
}

func newReadsEnv(t *testing.T, customize ...func(*ReadsOptions)) *readsEnv {
	t.Helper()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "reads.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	e := &readsEnv{
		t:       t,
		store:   store,
		manager: flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop()),
		secrets: secrets.NewStore(),
		clock:   &fakeClock{t: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)},
	}
	t.Cleanup(e.manager.StopAll)
	opts := ReadsOptions{
		Flows:    store,
		History:  store,
		Runtimes: e.manager,
		Registry: node.DefaultRegistry,
		Secrets:  e.secrets,
		Settings: store,
		Now:      e.clock.now,
	}
	for _, c := range customize {
		c(&opts)
	}
	e.reads = NewReads(opts)
	return e
}

// save stores f as a new flow definition.
func (e *readsEnv) save(f *flow.Flow) *flow.Flow {
	e.t.Helper()
	require.NoError(e.t, e.store.CreateFlow(f))
	return f
}

// run deploys a stored flow.
func (e *readsEnv) run(f *flow.Flow) {
	e.t.Helper()
	require.NoError(e.t, e.manager.Deploy(context.Background(), f))
	require.NoError(e.t, e.store.UpdateFlowState(f.ID, flow.FlowStateRunning))
}

// readsAgent is an agent actor with the given scopes.
func readsAgent(scopes ...string) Actor {
	return Actor{Kind: ActorAgent, TokenID: "tok-1", TokenName: "reader", Scopes: scopes}
}

// manualFlow is a minimal valid flow: trigger-manual → action-log.
func manualFlow(name string) *flow.Flow {
	f := flow.NewFlow(name)
	trig := flow.NewNode("trigger-manual", flow.NodeCategoryInput)
	trig.ID = "trigger"
	logN := flow.NewNode("action-log", flow.NodeCategoryOutput)
	logN.ID = "log"
	logN.Config = map[string]any{"level": "info", "message": "hello"}
	f.AddNode(*trig)
	f.AddNode(*logN)
	f.AddConnection(*flow.NewConnection("trigger", "output", "log", "input"))
	return f
}

// readsToolFlow publishes tool toolName: trigger-tool → action-reply.
func readsToolFlow(name, toolName string) *flow.Flow {
	f := flow.NewFlow(name)
	tool := flow.NewNode("trigger-tool", flow.NodeCategoryInput)
	tool.ID = "tool"
	tool.Config = map[string]any{
		"toolName":    toolName,
		"description": "Replies with the arguments it was called with.",
		"inputSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{"door": map[string]any{"type": "string", "enum": []any{"left", "right"}}},
		},
	}
	reply := flow.NewNode("action-reply", flow.NodeCategoryOutput)
	reply.ID = "reply"
	f.AddNode(*tool)
	f.AddNode(*reply)
	f.AddConnection(*flow.NewConnection("tool", "output", "reply", "input"))
	return f
}

// mustJSON encodes v without HTML escaping, so a leak check cannot miss a
// value whose "<", ">" or "&" json.Marshal would escape.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := marshalNoEscape(v)
	require.NoError(t, err)
	return string(data)
}

// leakText is what a leak check searches: v's JSON text and, decoded, every
// string and object key in it, so a value the JSON encoding escapes (quotes,
// backslashes) is still found.
func leakText(t *testing.T, v any) string {
	t.Helper()
	data := mustJSON(t, v)
	var doc any
	require.NoError(t, json.Unmarshal([]byte(data), &doc))
	var b strings.Builder
	b.WriteString(data)
	var walk func(any)
	walk = func(x any) {
		switch x := x.(type) {
		case string:
			b.WriteString("\n" + x)
		case map[string]any:
			for k, e := range x {
				b.WriteString("\n" + k)
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(doc)
	return b.String()
}

// Every read view needs the read scope; the editor may read everything.
func TestReadsNeedTheReadScope(t *testing.T) {
	e := newReadsEnv(t)
	f := e.save(manualFlow("perm"))
	require.NoError(t, e.store.UpsertFlowRun("run-1", f.ID, time.Now()))
	ctx := context.Background()

	calls := map[string]func(Actor) error{
		"ListFlows": func(a Actor) error { _, err := e.reads.ListFlows(ctx, a); return err },
		"GetFlow":   func(a Actor) error { _, err := e.reads.GetFlow(ctx, a, f.ID); return err },
		"ListRuns":  func(a Actor) error { _, err := e.reads.ListRuns(ctx, a, f.ID, 10, ""); return err },
		"GetRun":    func(a Actor) error { _, err := e.reads.GetRun(ctx, a, f.ID, "run-1"); return err },
		"SystemStatus": func(a Actor) error {
			_, err := e.reads.SystemStatus(ctx, a)
			return err
		},
	}
	refused := map[string]Actor{
		"no scopes":         readsAgent(),
		"call scope only":   readsAgent(storage.ScopeCall),
		"author and deploy": readsAgent(storage.ScopeAuthor, storage.ScopeDeploy),
		"unknown kind":      {Kind: "robot", Scopes: []string{storage.ScopeRead}},
		"zero actor":        {},
	}
	allowed := map[string]Actor{
		"read scope": readsAgent(storage.ScopeRead),
		"editor":     EditorActor(),
	}
	for name, call := range calls {
		for who, actor := range refused {
			te := requireKind(t, call(actor), ErrPermissionDenied)
			assert.Equalf(t, "forbidden", te.Code(), "%s by %s", name, who)
		}
		for who, actor := range allowed {
			assert.NoErrorf(t, call(actor), "%s by %s", name, who)
		}
	}
}

func TestListFlowsStatesToolsAndLastRun(t *testing.T) {
	e := newReadsEnv(t)
	stopped := e.save(manualFlow("stopped"))
	running := e.save(readsToolFlow("running", "reads_open_door"))
	e.run(running)
	failed := e.save(manualFlow("failed"))
	require.NoError(t, e.store.UpdateFlowState(failed.ID, flow.FlowStateFailed))
	// Stored as running but not deployed (e.g. a restore that failed
	// before its state was written): not running, so failed.
	lost := e.save(manualFlow("lost"))
	require.NoError(t, e.store.UpdateFlowState(lost.ID, flow.FlowStateRunning))

	start := time.Now()
	require.NoError(t, e.store.UpsertFlowRun("old", stopped.ID, start))
	require.NoError(t, e.store.FinishFlowRun("old", "success", start.Add(time.Second), 2, 0))
	require.NoError(t, e.store.UpsertFlowRun("new", stopped.ID, start.Add(time.Minute)))
	require.NoError(t, e.store.FinishFlowRun("new", "error", start.Add(2*time.Minute), 2, 1))

	list, err := e.reads.ListFlows(context.Background(), readsAgent(storage.ScopeRead))
	require.NoError(t, err)
	assert.Equal(t, 4, list.Total)
	assert.False(t, list.Truncated)
	byName := map[string]FlowSummary{}
	for _, s := range list.Flows {
		byName[s.Name] = s
	}
	require.Len(t, byName, 4)

	assert.Equal(t, FlowStateStopped, byName["stopped"].State)
	assert.Equal(t, FlowStateRunning, byName["running"].State)
	assert.Equal(t, FlowStateFailed, byName["failed"].State)
	assert.Equal(t, FlowStateFailed, byName["lost"].State)

	assert.Equal(t, []string{"reads_open_door"}, byName["running"].Tools)
	assert.Empty(t, byName["stopped"].Tools)
	assert.Equal(t, 2, byName["stopped"].NodeCount)
	assert.Equal(t, stopped.ID, byName["stopped"].ID)

	last := byName["stopped"].LastRun
	require.NotNil(t, last)
	assert.Equal(t, "new", last.ID)
	assert.Equal(t, "error", last.Status)
	assert.Nil(t, byName["failed"].LastRun)

	// Stable camelCase field names.
	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(mustJSON(t, list)), &raw))
	assert.Contains(t, raw, "flows")
	assert.Contains(t, raw, "total")
	assert.Contains(t, raw, "truncated")
	first := raw["flows"].([]any)[0].(map[string]any)
	for _, key := range []string{"id", "name", "state", "nodeCount", "tools"} {
		assert.Containsf(t, first, key, "flow summary field %s", key)
	}
	// tools is a list even when empty.
	assert.Contains(t, mustJSON(t, byName["stopped"]), `"tools":[]`)
}

func TestListFlowsIsCapped(t *testing.T) {
	e := newReadsEnv(t)
	for i := range maxListedFlows + 3 {
		e.save(manualFlow(fmt.Sprintf("flow-%03d", i)))
	}
	list, err := e.reads.ListFlows(context.Background(), EditorActor())
	require.NoError(t, err)
	assert.Len(t, list.Flows, maxListedFlows)
	assert.Equal(t, maxListedFlows+3, list.Total)
	assert.True(t, list.Truncated)

	again, err := e.reads.ListFlows(context.Background(), EditorActor())
	require.NoError(t, err)
	assert.Equal(t, mustJSON(t, list), mustJSON(t, again), "the order is deterministic")
}

func TestListFlowsRedactsSecretsInNames(t *testing.T) {
	e := newReadsEnv(t)
	require.NoError(t, e.secrets.Set("API", "hunter2-Value!"))
	e.save(manualFlow("uses hunter2-Value! oops"))
	list, err := e.reads.ListFlows(context.Background(), EditorActor())
	require.NoError(t, err)
	out := leakText(t, list)
	assert.NotContains(t, out, "hunter2-Value!")
	assert.Contains(t, out, "uses <redacted> oops")
}
