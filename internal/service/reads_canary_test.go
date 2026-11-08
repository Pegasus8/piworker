package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

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
