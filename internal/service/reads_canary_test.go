package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// canaryField is one config value planted by the read canary test.
type canaryField struct {
	nodeType, property string
	// marks are the texts that must (visible) or must not (hidden) appear
	// in the view.
	marks   []string
	visible bool
}

// canaryFlow builds one node per registered node type whose config holds a
// unique canary in every property: strings hold a canary string, maps and
// lists a map or list containing canaries (as a key and as a value), numbers
// a unique number. Every node also gets a property its schema does not
// declare. It returns the flow and the planted fields.
func canaryFlow(t *testing.T) (*flow.Flow, []canaryField) {
	t.Helper()
	infos := node.DefaultRegistry.List()
	require.NotEmpty(t, infos)
	sort.Slice(infos, func(a, b int) bool { return infos[a].Type < infos[b].Type })

	seq := 0
	next := func() string { seq++; return fmt.Sprintf("CNRY%05dX", seq) }
	nextNumber := func() float64 { seq++; return float64(7_300_000 + seq) }

	f := flow.NewFlow("canaries")
	var fields []canaryField
	for i, info := range infos {
		cfg := map[string]any{}
		var props []string
		if info.Config != nil {
			for name := range info.Config.Properties {
				props = append(props, name)
			}
		}
		sort.Strings(props)
		for _, name := range props {
			p := info.Config.Properties[name]
			field := canaryField{nodeType: info.Type, property: name, visible: p.AgentVisible && !p.Sensitive}
			switch p.Type {
			case "number", "integer":
				n := nextNumber()
				cfg[name] = n
				field.marks = []string{fmt.Sprintf("%.0f", n)}
			case "boolean":
				continue // two values: nothing unique to look for
			case "object", "json":
				key, val := next(), next()
				cfg[name] = map[string]any{key: val, "nested": map[string]any{"list": []any{val + "n"}}}
				field.marks = []string{key, val, val + "n"}
			case "array":
				val := next()
				cfg[name] = []any{val, map[string]any{"inner": val + "i"}}
				field.marks = []string{val, val + "i"}
			default: // string and untyped
				val := next()
				cfg[name] = val
				field.marks = []string{val}
			}
			fields = append(fields, field)
		}
		// Undeclared properties are hidden, whatever they hold.
		undeclared, undeclaredMap := next(), next()
		cfg["zzUndeclared"] = undeclared
		cfg["zzUndeclaredMap"] = map[string]any{"v": undeclaredMap}
		fields = append(fields,
			canaryField{nodeType: info.Type, property: "zzUndeclared", marks: []string{undeclared}},
			canaryField{nodeType: info.Type, property: "zzUndeclaredMap", marks: []string{undeclaredMap}},
		)

		n := flow.NewNode(info.Type, info.Category)
		n.ID = "n-" + info.Type
		n.Position = flow.Position{X: float64(i), Y: 1}
		n.Config = cfg
		f.AddNode(*n)
	}

	// A type no registry knows: all of its config is hidden.
	unknown := flow.NewNode("not-a-registered-type", flow.NodeCategoryProcessing)
	unknown.ID = "n-unknown"
	val := next()
	unknown.Config = map[string]any{"message": val, "level": "info"}
	f.AddNode(*unknown)
	fields = append(fields,
		canaryField{nodeType: unknown.Type, property: "message", marks: []string{val}},
		canaryField{nodeType: unknown.Type, property: "level", marks: []string{"info"}},
	)
	return f, fields
}

// The read redaction canary (docs/design/mcp.md §13): for every registered
// node type, GetFlow shows exactly the properties marked AgentVisible and not
// Sensitive; everything else, undeclared properties and whole nested values
// included, is replaced by the "<redacted>" sentinel.
func TestGetFlowRedactionCanaries(t *testing.T) {
	e := newReadsEnv(t)
	f, fields := canaryFlow(t)
	e.save(f)

	for _, actor := range []Actor{readsAgent(storage.ScopeRead), EditorActor()} {
		view, err := e.reads.GetFlow(context.Background(), actor, f.ID)
		require.NoError(t, err)
		out := mustJSON(t, view)

		configs := map[string]map[string]any{}
		for _, n := range view.Nodes {
			require.Falsef(t, n.ConfigOmitted, "config of %s omitted", n.ID)
			configs[n.Type] = n.Config
		}
		visible, hidden := 0, 0
		for _, fld := range fields {
			cfg, ok := configs[fld.nodeType]
			require.Truef(t, ok, "node of type %s missing from the view", fld.nodeType)
			got, present := cfg[fld.property]
			require.Truef(t, present, "%s.%s missing from the view", fld.nodeType, fld.property)
			if fld.visible {
				visible++
				for _, m := range fld.marks {
					assert.Containsf(t, out, m, "visible %s.%s must be shown", fld.nodeType, fld.property)
				}
				continue
			}
			hidden++
			assert.Equalf(t, redactedMark, got, "hidden %s.%s must be the sentinel, nested values included", fld.nodeType, fld.property)
			for _, m := range fld.marks {
				if fld.nodeType == "not-a-registered-type" && m == "info" {
					continue // a common word: the sentinel check above covers it
				}
				assert.NotContainsf(t, out, m, "hidden %s.%s leaked", fld.nodeType, fld.property)
			}
		}
		// The test is only meaningful if both kinds were planted.
		assert.Greater(t, visible, 20)
		assert.Greater(t, hidden, 40)
		assert.False(t, strings.Contains(out, `"zzUndeclared":"CNRY`), "undeclared properties are hidden")
	}
}

// The secret canary (docs/design/mcp.md §13): a value in the secret store,
// referenced by the flow and also leaked into the flow's name, a visible
// setting, the token name of a run's actor, node errors and a payload
// echoed in an error (the recorder stores no payloads), never appears in
// any read view, literally, URL-encoded, base64-encoded or escaped.
func TestReadsSecretCanary(t *testing.T) {
	e := newReadsEnv(t)
	const value = "Cn4ry-S3cret/Value+9&q"
	require.NoError(t, e.secrets.Set("API_KEY", value))

	f := manualFlow("canary " + value)
	httpN := flow.NewNode("action-http", flow.NodeCategoryOutput)
	httpN.ID = "http"
	httpN.Config = map[string]any{
		"method":  "POST",
		"url":     "https://api.example.com/v1?key={{secret.API_KEY}}",
		"headers": map[string]any{"Authorization": "Bearer {{secret.API_KEY}}"},
	}
	debug := flow.NewNode("process-debug", flow.NodeCategoryProcessing)
	debug.ID = "debug"
	debug.Config = map[string]any{"name": "uses {{secret.API_KEY}}, i.e. " + value}
	f.AddNode(*httpN)
	f.AddNode(*debug)
	e.save(f)

	var events []storage.NodeEventRecord
	for name, leak := range secretLeaks(value) {
		events = append(events, storage.NodeEventRecord{NodeID: "http", NodeType: "action-http", Phase: "error", Error: name + ": " + leak})
	}
	e.addRun(f.ID, "run-canary", "token:bot "+value, 0, time.Second, "error", events...)

	ctx := context.Background()
	actor := readsAgent(storage.ScopeRead)
	flows, err := e.reads.ListFlows(ctx, actor)
	require.NoError(t, err)
	view, err := e.reads.GetFlow(ctx, actor, f.ID)
	require.NoError(t, err)
	runs, err := e.reads.ListRuns(ctx, actor, f.ID, 10, "")
	require.NoError(t, err)
	run, err := e.reads.GetRun(ctx, actor, f.ID, "run-canary")
	require.NoError(t, err)
	prompt, err := e.reads.DiagnoseFlow(ctx, actor, f.ID)
	require.NoError(t, err)
	require.Len(t, run.Events, len(events))

	outputs := map[string]string{
		"ListFlows":    leakText(t, flows),
		"GetFlow":      leakText(t, view),
		"ListRuns":     leakText(t, runs),
		"GetRun":       leakText(t, run),
		"DiagnoseFlow": prompt,
	}
	for name, out := range outputs {
		for form, text := range secretForms(value) {
			assert.NotContainsf(t, out, text, "%s leaked the secret as %s", name, form)
		}
	}
	// References are names, not values: they stay where visible.
	assert.Contains(t, outputs["GetFlow"], "uses {{secret.API_KEY}}, i.e. "+redactedMark)
	assert.Equal(t, "POST", view.Nodes[2].Config["method"])
	assert.Equal(t, redactedMark, view.Nodes[2].Config["headers"])
}
