package node

import (
	"fmt"

	"github.com/Pegasus8/piworker/internal/types"
)

// Outputs from a setting (ADR-0040, docs/design/llm.md §5): a type whose
// NodeTypeInfo.OutputsFrom names an array-of-strings setting has one extra
// output port per item, after its static Outputs. The node's Ports()
// returns them (deploy, Lint and the router use the instance's ports); the
// editor renders them from the node's config.

// ConfigOutputs returns the output ports a node of type info has with cfg:
// info.Outputs, then one port per item of the OutputsFrom setting (ID and
// name = the item, any data type, Multiple like a static output), in
// order. Items that could not be a port of their own are skipped: anything
// but a non-empty string, a repeated item, the ID of a static output and
// the runtime's error port (the node's Validate refuses such items;
// ConfigOutputs never fails). The result is never nil and never shares
// info.Outputs' array.
func ConfigOutputs(info NodeTypeInfo, cfg map[string]any) []types.Port {
	outputs := append([]types.Port{}, info.Outputs...)
	if info.OutputsFrom == "" {
		return outputs
	}
	seen := map[string]bool{types.ErrorPort: true}
	for _, p := range outputs {
		seen[p.ID] = true
	}
	for _, item := range stringItems(cfg[info.OutputsFrom]) {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		outputs = append(outputs, types.Port{ID: item, Name: item, DataType: types.DataTypeAny, Multiple: true})
	}
	return outputs
}

// stringItems returns the string items of a list setting ([]any as decoded
// from JSON, or []string), in order; other items are left out.
func stringItems(v any) []string {
	switch list := v.(type) {
	case []string:
		return list
	case []any:
		items := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := item.(string); ok {
				items = append(items, s)
			}
		}
		return items
	}
	return nil
}

// checkOutputsFrom reports why info's OutputsFrom cannot give ports: it must
// name an "array" setting of the type's (possibly derived) schema.
func checkOutputsFrom(info NodeTypeInfo) error {
	if info.OutputsFrom == "" {
		return nil
	}
	if info.Config == nil {
		return fmt.Errorf("node type %q: outputsFrom %q needs a config schema declaring it", info.Type, info.OutputsFrom)
	}
	p, ok := info.Config.Properties[info.OutputsFrom]
	if !ok {
		return fmt.Errorf("node type %q: outputsFrom %q is not a setting of its config schema", info.Type, info.OutputsFrom)
	}
	if p.Type != "array" {
		return fmt.Errorf("node type %q: outputsFrom %q must be an array setting, not %q", info.Type, info.OutputsFrom, p.Type)
	}
	return nil
}
