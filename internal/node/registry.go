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
