package node

import (
	"fmt"
	"sync"

	"github.com/Pegasus8/piworker/internal/types"
)

// NodeFactory is a function that creates a new instance of a node.
type NodeFactory func(config map[string]interface{}) (Node, error)

// NodeTypeInfo contains metadata about a registered node type.
type NodeTypeInfo struct {
	Type          string             `json:"type"`                    // Unique identifier (e.g., "trigger-interval")
	Name          string             `json:"name"`                    // Human-readable name (e.g., "Interval Timer")
	Description   string             `json:"description"`             // Brief description of what the node does
	Documentation string             `json:"documentation,omitempty"` // Detailed markdown documentation
	Category      types.NodeCategory `json:"category"`                // input, processing, or output
	Inputs        []types.Port       `json:"inputs"`                  // Input port definitions
	Outputs       []types.Port       `json:"outputs"`                 // Output port definitions
	Config        *ConfigSchema      `json:"config,omitempty"`        // Configuration schema
	Icon          string             `json:"icon,omitempty"`          // Icon name for UI
	Risk          Risk               `json:"risk"`                    // What the node can affect; every registered type declares it
}

// Risk classifies what a node type can affect outside the flow. It drives the
// risk of MCP tools built from flows and the agent policies.
type Risk string

const (
	// RiskSafe nodes only compute, read local state or log.
	RiskSafe Risk = "safe"
	// RiskExternal nodes send data to other services (HTTP, MQTT, e-mail,
	// chat/notification services).
	RiskExternal Risk = "external"
	// RiskPrivileged nodes act on the host itself: shell commands, files,
	// databases and hardware (GPIO, I2C).
	RiskPrivileged Risk = "privileged"
)

// Valid reports whether r is one of the declared risk levels.
func (r Risk) Valid() bool {
	switch r {
	case RiskSafe, RiskExternal, RiskPrivileged:
		return true
	}
	return false
}

// Registry manages node type registration and instantiation.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]NodeFactory
	types     map[string]NodeTypeInfo
}

// NewRegistry creates a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]NodeFactory),
		types:     make(map[string]NodeTypeInfo),
	}
}

// Register adds a new node type to the registry.
// The factory function will be called to create new instances of the node.
func (r *Registry) Register(info NodeTypeInfo, factory NodeFactory) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[info.Type]; exists {
		return fmt.Errorf("node type %q already registered", info.Type)
	}

	// Derive the config schema from a probe instance when the info doesn't supply
	// one, so a node's GetConfigSchema() is the single source of truth (the UI
	// reads info.Config). Nodes that require configuration to construct keep the
	// schema they provide explicitly.
	if info.Config == nil {
		if probe := probeNode(factory); probe != nil {
			if cn, ok := probe.(ConfigurableNode); ok {
				schema := cn.GetConfigSchema()
				info.Config = &schema
			}
		}
	}

	r.factories[info.Type] = factory
	r.types[info.Type] = info

	return nil
}

// probeNode creates a throwaway instance with no config to read its declared
// schema. It returns nil if the node requires configuration to construct (in
// which case the caller-provided info is used as-is).
func probeNode(factory NodeFactory) (n Node) {
	defer func() {
		if recover() != nil {
			n = nil
		}
	}()
	probe, err := factory(nil)
	if err != nil {
		return nil
	}
	return probe
}

// Derive returns a new registry with the same node types and infos, whose
// factories are chosen by override: it is called once per type with the
// type's info and registered factory and returns the factory the new registry
// uses, or nil to keep the original (a nil override keeps every original).
// The infos are copied as registered (no factory is called again), and r is
// never modified, before or after: the dry-run builds its stubs this way
// without touching the global registry.
func (r *Registry) Derive(override func(info NodeTypeInfo, factory NodeFactory) NodeFactory) *Registry {
	r.mu.RLock()
	derived := &Registry{
		factories: make(map[string]NodeFactory, len(r.factories)),
		types:     make(map[string]NodeTypeInfo, len(r.types)),
	}
	for typ, factory := range r.factories {
		derived.factories[typ] = factory
		derived.types[typ] = r.types[typ]
	}
	r.mu.RUnlock()

	// override runs without r's lock, so it may read r (Get, List).
	if override != nil {
		for typ, factory := range derived.factories {
			if replacement := override(derived.types[typ], factory); replacement != nil {
				derived.factories[typ] = replacement
			}
		}
	}
	return derived
}

// Unregister removes a node type from the registry.
func (r *Registry) Unregister(nodeType string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.factories, nodeType)
	delete(r.types, nodeType)
}

// Create instantiates a new node of the given type with the provided configuration.
func (r *Registry) Create(nodeType string, config map[string]interface{}) (Node, error) {
	r.mu.RLock()
	factory, exists := r.factories[nodeType]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unknown node type: %s", nodeType)
	}

	return factory(config)
}

// Get returns information about a registered node type.
func (r *Registry) Get(nodeType string) (NodeTypeInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, exists := r.types[nodeType]
	return info, exists
}

// List returns information about all registered node types.
func (r *Registry) List() []NodeTypeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]NodeTypeInfo, 0, len(r.types))
	for _, info := range r.types {
		result = append(result, info)
	}

	return result
}

// ListByCategory returns all node types in the specified category.
func (r *Registry) ListByCategory(category types.NodeCategory) []NodeTypeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]NodeTypeInfo, 0)
	for _, info := range r.types {
		if info.Category == category {
			result = append(result, info)
		}
	}

	return result
}

// Has checks if a node type is registered.
func (r *Registry) Has(nodeType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.factories[nodeType]
	return exists
}

// Count returns the number of registered node types.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.factories)
}

// DefaultRegistry is the global node registry.
var DefaultRegistry = NewRegistry()

// Register adds a node type to the default registry.
func Register(info NodeTypeInfo, factory NodeFactory) error {
	return DefaultRegistry.Register(info, factory)
}

// Create instantiates a node from the default registry.
func Create(nodeType string, config map[string]interface{}) (Node, error) {
	return DefaultRegistry.Create(nodeType, config)
}

// List returns all node types from the default registry.
func List() []NodeTypeInfo {
	return DefaultRegistry.List()
}
