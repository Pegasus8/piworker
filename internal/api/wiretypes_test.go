package api

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/events"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var updateWireTypes = flag.Bool("update", false, "rewrite web/src/types/wire.gen.ts from the Go response types instead of comparing with it")

// wireTypes are the response structs whose JSON the editor reads into a
// hand-written TypeScript type (web/src/types/index.ts). Their shapes are
// generated into wire.gen.ts, and wire.check.ts makes vue-tsc fail when a
// hand-written type stops matching: add a struct here and its pair there.
var wireTypes = []any{
	storage.FlowRun{},
	storage.NodeEventRecord{},
	events.Event{},
	service.DeploymentView{},
	service.VersionEntry{},
	service.VersionList{},
	flow.CallResult{},
	TLSInfo{},
	// LLM connections, memory, knowledge base and the flow assistant
	// (docs/design/llm.md §6).
	LLMConnectionsResponse{},
	LLMConnectionTest{},
	LLMModelsResponse{},
	LLMConversationsResponse{},
	LLMConversation{},
	KnowledgeCollectionsResponse{},
	KnowledgeDocumentPage{},
	service.AssistantFlowResult{},
}

const wireTypesPath = "../../web/src/types/wire.gen.ts"

// The generated wire types are current: after changing a response struct,
// run `go test -run TestWireTypesAreCurrent ./internal/api -update` and fix
// what vue-tsc then reports in web/.
func TestWireTypesAreCurrent(t *testing.T) {
	want := generateWireTypes(t, wireTypes)
	if *updateWireTypes {
		require.NoError(t, os.WriteFile(filepath.FromSlash(wireTypesPath), []byte(want), 0o644))
		return
	}
	got, err := os.ReadFile(filepath.FromSlash(wireTypesPath))
	require.NoError(t, err)
	assert.Equal(t, want, string(got), "wire.gen.ts is stale: run go test -run TestWireTypesAreCurrent ./internal/api -update")
}

func TestWireTypeRendering(t *testing.T) {
	type inner struct {
		N int `json:"n"`
	}
	type sample struct {
		ID       string          `json:"id"`
		When     time.Time       `json:"when"`
		Maybe    *time.Time      `json:"maybe,omitempty"`
		Nullable *int            `json:"nullable"`
		Tags     []string        `json:"tags"`
		Inner    *inner          `json:"inner,omitempty"`
		Raw      json.RawMessage `json:"raw,omitempty"`
		Any      any             `json:"any,omitempty"`
		ByName   map[string]int  `json:"byName"`
		Ratio    float64         `json:"ratio"`
		Hidden   string          `json:"-"`
		private  string
	}
	out := generateWireTypes(t, []any{sample{}})
	assert.Contains(t, out, "export interface sample {\n  id: string\n  when: string\n  maybe?: string\n  nullable: number | null\n  tags: string[]\n  inner?: inner\n  raw?: unknown\n  any?: unknown\n  byName: Record<string, number>\n  ratio: number\n}\n")
	assert.Contains(t, out, "export interface inner {\n  n: number\n}\n")
	assert.NotContains(t, out, "Hidden")
	assert.NotContains(t, out, "private")
}

// generateWireTypes renders the TypeScript interfaces of roots and of every
// struct they contain, in the order first met.
func generateWireTypes(t *testing.T, roots []any) string {
	t.Helper()
	g := &wireGen{seen: map[reflect.Type]bool{}, names: map[string]reflect.Type{}}
	for _, r := range roots {
		g.add(t, reflect.TypeOf(r))
	}
	var b strings.Builder
	b.WriteString("// Code generated from the server's Go response types. DO NOT EDIT.\n")
	b.WriteString("// Regenerate: go test -run TestWireTypesAreCurrent ./internal/api -update\n")
	b.WriteString("//\n// The JSON shapes the server sends. The types the interface uses are the\n")
	b.WriteString("// hand-written ones in index.ts; wire.check.ts keeps both in step.\n")
	for _, typ := range g.order {
		b.WriteString("\n/** " + typ.PkgPath()[strings.LastIndex(typ.PkgPath(), "/")+1:] + "." + typ.Name() + " */\n")
		b.WriteString("export interface " + typ.Name() + " {\n")
		for _, f := range g.fields(t, typ) {
			b.WriteString("  " + f + "\n")
		}
		b.WriteString("}\n")
	}
	return b.String()
}

type wireGen struct {
	seen  map[reflect.Type]bool
	names map[string]reflect.Type
	order []reflect.Type
}

func (g *wireGen) add(t *testing.T, typ reflect.Type) {
	if g.seen[typ] {
		return
	}
	if other, taken := g.names[typ.Name()]; taken && other != typ {
		t.Fatalf("two wire types are named %s: %s and %s", typ.Name(), other.PkgPath(), typ.PkgPath())
	}
	g.seen[typ] = true
	g.names[typ.Name()] = typ
	g.order = append(g.order, typ)
	g.fields(t, typ) // registers nested structs
}

var (
	timeType = reflect.TypeOf(time.Time{})
	rawType  = reflect.TypeOf(json.RawMessage{})
)

// fields renders the JSON fields of struct typ, embedded structs inlined, in
// declaration order.
func (g *wireGen) fields(t *testing.T, typ reflect.Type) []string {
	var out []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := f.Tag.Get("json")
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" && f.Type.Kind() == reflect.Struct {
			out = append(out, g.fields(t, f.Type)...)
			continue
		}
		if !f.IsExported() || name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		optional := strings.Contains(opts, "omitempty")
		ts := g.tsType(t, f.Type)
		if f.Type.Kind() == reflect.Pointer && !optional {
			ts += " | null"
		}
		if optional {
			name += "?"
		}
		out = append(out, name+": "+ts)
	}
	return out
}

func (g *wireGen) tsType(t *testing.T, typ reflect.Type) string {
	switch {
	case typ == timeType:
		return "string"
	case typ == rawType:
		return "unknown"
	}
	switch typ.Kind() {
	case reflect.Pointer:
		return g.tsType(t, typ.Elem())
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Interface:
		return "unknown"
	case reflect.Slice, reflect.Array:
		elem := g.tsType(t, typ.Elem())
		if strings.Contains(elem, " ") {
			elem = "(" + elem + ")"
		}
		return elem + "[]"
	case reflect.Map:
		return "Record<string, " + g.tsType(t, typ.Elem()) + ">"
	case reflect.Struct:
		g.add(t, typ)
		return typ.Name()
	}
	t.Fatalf("no TypeScript type for %s", typ)
	return ""
}
