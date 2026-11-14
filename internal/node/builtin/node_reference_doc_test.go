package builtin

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

var updateNodeReference = flag.Bool("update", false, "rewrite docs/guide/nodes.md from the node registry instead of comparing with it")

const nodeReferencePath = "../../../docs/guide/nodes.md"

// TestNodeReferenceDocIsCurrent keeps the user guide's node reference equal to
// what the registry declares, so a new node or a changed setting cannot ship
// without its documentation. Regenerate with:
//
//	go test -run TestNodeReferenceDocIsCurrent ./internal/node/builtin -update
func TestNodeReferenceDocIsCurrent(t *testing.T) {
	want := renderNodeReference(node.DefaultRegistry.List())
	path := filepath.FromSlash(nodeReferencePath)
	if *updateNodeReference {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the node reference: %v (create it with -update)", err)
	}
	if string(got) != want {
		t.Fatalf("%s does not match the node registry; regenerate it with\n\tgo test -run TestNodeReferenceDocIsCurrent ./internal/node/builtin -update", nodeReferencePath)
	}
}

func renderNodeReference(infos []node.NodeTypeInfo) string {
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	var b strings.Builder
	b.WriteString("# Node reference\n\n")
	b.WriteString("<!-- Generated from the node registry. Do not edit by hand: run\n")
	b.WriteString("     go test -run TestNodeReferenceDocIsCurrent ./internal/node/builtin -update -->\n\n")
	b.WriteString("Every node PiWorker ships, with its ports and settings. The editor shows the\n")
	b.WriteString("same information, plus examples, behind the **?** button of each node's\n")
	b.WriteString("configuration dialog. New to PiWorker? Start with\n")
	b.WriteString("[Getting started](getting-started.md) and [Concepts](concepts.md).\n\n")
	b.WriteString("**Risk** says what a node can affect: `safe` nodes only compute, read local\n")
	b.WriteString("state or log; `external` nodes send data to other services; `privileged`\n")
	b.WriteString("nodes act on the device itself (commands, files, databases, hardware). It\n")
	b.WriteString("decides how [AI agents](agents.md) may use a flow.\n\n")

	sections := []struct {
		category types.NodeCategory
		title    string
		intro    string
	}{
		{types.NodeCategoryInput, "Triggers", "A trigger starts a flow: it has no input and emits a message when something happens."},
		{types.NodeCategoryProcessing, "Processing", "Processing nodes change, route or hold the messages that pass through them."},
		{types.NodeCategoryOutput, "Actions", "Actions do something with a message: notify, call a service, write, switch a pin."},
	}
	for _, s := range sections {
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", s.title, s.intro)
		b.WriteString("| Node | What it does |\n|------|--------------|\n")
		for _, info := range infos {
			if info.Category == s.category {
				fmt.Fprintf(&b, "| [%s](#%s) | %s |\n", info.Name, anchor(info), cell(info.Description))
			}
		}
		b.WriteString("\n")
		for _, info := range infos {
			if info.Category == s.category {
				renderNode(&b, info)
			}
		}
	}
	return b.String()
}

func anchor(info node.NodeTypeInfo) string { return info.Type }

func renderNode(b *strings.Builder, info node.NodeTypeInfo) {
	fmt.Fprintf(b, "<a id=\"%s\"></a>\n\n### %s\n\n", anchor(info), info.Name)
	fmt.Fprintf(b, "%s.\n\n", strings.TrimSuffix(info.Description, "."))
	fmt.Fprintf(b, "- **Type:** `%s`\n- **Risk:** `%s`\n", info.Type, info.Risk)
	fmt.Fprintf(b, "- **Inputs:** %s\n- **Outputs:** %s\n\n", ports(info.Inputs), ports(info.Outputs))
	if info.Config == nil || len(info.Config.Properties) == 0 {
		b.WriteString("No settings.\n\n")
		return
	}
	names := make([]string, 0, len(info.Config.Properties))
	for name := range info.Config.Properties {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		a, c := info.Config.Properties[names[i]], info.Config.Properties[names[j]]
		if a.Title != c.Title {
			return a.Title < c.Title
		}
		return names[i] < names[j]
	})
	required := map[string]bool{}
	for _, name := range info.Config.Required {
		required[name] = true
	}
	b.WriteString("| Setting | Key | Type | Default | Description |\n|---------|-----|------|---------|-------------|\n")
	for _, name := range names {
		p := info.Config.Properties[name]
		title := p.Title
		if title == "" {
			title = name
		}
		if required[name] {
			title += " (required)"
		}
		fmt.Fprintf(b, "| %s | `%s` | %s | %s | %s |\n", cell(title), name, propType(p), propDefault(p), cell(p.Description))
	}
	b.WriteString("\n")
}

func ports(list []types.Port) string {
	if len(list) == 0 {
		return "none"
	}
	ids := make([]string, 0, len(list))
	for _, p := range list {
		ids = append(ids, "`"+p.ID+"`")
	}
	return strings.Join(ids, ", ")
}

func propType(p node.ConfigProperty) string {
	if len(p.Enum) > 0 {
		choices := make([]string, 0, len(p.Enum))
		for _, e := range p.Enum {
			if e == "" {
				e = "(empty)"
			}
			choices = append(choices, "`"+e+"`")
		}
		return strings.Join(choices, ", ")
	}
	kind := p.Type
	if kind == "" {
		kind = "any"
	}
	if p.Unit != "" {
		kind += " (" + p.Unit + ")"
	}
	return kind
}

func propDefault(p node.ConfigProperty) string {
	if p.Default == nil {
		return "–"
	}
	if s, ok := p.Default.(string); ok {
		if s == "" {
			return "–"
		}
		return "`" + cell(s) + "`"
	}
	raw, err := json.Marshal(p.Default)
	if err != nil {
		return "–"
	}
	return "`" + cell(string(raw)) + "`"
}

// cell makes text safe inside a Markdown table cell.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.Join(strings.Fields(s), " ")
}
