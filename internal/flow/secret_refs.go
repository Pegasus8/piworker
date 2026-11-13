package flow

import "sort"

// SecretReference is one {{secret.NAME}} reference written in a node's
// settings: which secret, where, and whether the node is enabled (a
// disabled node does not run, but its reference is still there).
type SecretReference struct {
	Name     string `json:"name"`
	NodeID   string `json:"nodeId"`
	NodeType string `json:"nodeType"`
	Setting  string `json:"setting"`
	Enabled  bool   `json:"enabled"`
}

// SecretReferences returns every secret reference of f's node settings
// (nested values included, any setting: whether the node resolves it there
// is the node's business), in node order, then setting name, then secret
// name, each (node, setting, name) once. It is what the Secrets dialog
// shows as "used by". Never nil.
func SecretReferences(f *Flow) []SecretReference {
	out := []SecretReference{}
	if f == nil {
		return out
	}
	for i := range f.Nodes {
		n := &f.Nodes[i]
		props := make([]string, 0, len(n.Config))
		for prop := range n.Config {
			props = append(props, prop)
		}
		sort.Strings(props)
		for _, prop := range props {
			names := map[string]bool{}
			collectSecretNames(n.Config[prop], names)
			sorted := make([]string, 0, len(names))
			for name := range names {
				if name != "" {
					sorted = append(sorted, name)
				}
			}
			sort.Strings(sorted)
			for _, name := range sorted {
				out = append(out, SecretReference{Name: name, NodeID: n.ID, NodeType: n.Type, Setting: prop, Enabled: n.Enabled})
			}
		}
	}
	return out
}
