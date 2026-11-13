package flow

import (
	"sort"

	"github.com/Pegasus8/piworker/internal/node"
)

// ConnectionsUsed returns the broker connections (ADR-0026) the enabled
// nodes of f name (node.ConnectionRequirer), sorted, each once. Nodes that
// cannot be built are left out (they cannot run either). It builds the
// nodes as Lint does and closes them before it returns; nothing starts.
// The editor's lifecycle uses it to restart the running flows that use a
// connection when the connection changes, and to refuse deleting a
// connection a running flow uses (ADR-0029).
func ConnectionsUsed(f *Flow, reg *node.Registry) []string {
	out := []string{}
	if f == nil || reg == nil {
		return out
	}
	instances, _ := instantiateNodes(f, reg, nil)
	defer closeInstances(instances)
	set := map[string]bool{}
	for _, in := range instances {
		if req, ok := in.(node.ConnectionRequirer); ok {
			for _, name := range req.RequiredConnections() {
				set[name] = true
			}
		}
	}
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
