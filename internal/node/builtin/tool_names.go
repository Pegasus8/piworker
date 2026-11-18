package builtin

import (
	"fmt"
	"sync"

	"github.com/Pegasus8/piworker/internal/node"
)

// toolNameRegistry makes MCP tool names unique across running flows, the way
// webhook.DefaultHub does for webhook paths: a trigger-tool claims its name in
// Start and releases it in Stop. A taken name fails the deploy with
// node.ErrResourceConflict (HTTP 409, ADR-0006).
type toolNameRegistry struct {
	mu     sync.Mutex
	owners map[string]any
}

func newToolNameRegistry() *toolNameRegistry {
	return &toolNameRegistry{owners: make(map[string]any)}
}

// toolNames is the process-wide registry used by trigger-tool.
var toolNames = newToolNameRegistry()

// claim reserves name for owner. Claiming a name owner already holds succeeds.
func (r *toolNameRegistry) claim(name string, owner any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, taken := r.owners[name]; taken && current != owner {
		return fmt.Errorf("%w: tool name %q is used by another running flow", node.ErrResourceConflict, name)
	}
	r.owners[name] = owner
	return nil
}

// release frees name if owner holds it; otherwise it does nothing, so a
// stale instance stopping late cannot free a newer instance's name.
func (r *toolNameRegistry) release(name string, owner any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.owners[name] == owner {
		delete(r.owners, name)
	}
}

// taken reports whether name is claimed.
func (r *toolNameRegistry) taken(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.owners[name]
	return ok
}
