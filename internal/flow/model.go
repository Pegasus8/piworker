// Package flow provides the core data models and runtime for flow-based automation.
package flow

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/google/uuid"
)

// FlowState represents the current state of a flow.
type FlowState string

const (
	// FlowStateActive indicates the flow is saved and ready to be deployed.
	FlowStateActive FlowState = "active"
	// FlowStateInactive indicates the flow is disabled and won't run.
	FlowStateInactive FlowState = "inactive"
	// FlowStateRunning indicates the flow is currently executing.
	FlowStateRunning FlowState = "running"
	// FlowStateFailed indicates the flow encountered an error.
	FlowStateFailed FlowState = "failed"
)

// Type aliases for backward compatibility and convenience.
// These allow using flow.DataType, flow.NodeCategory, etc. without breaking existing code.
type (
	DataType     = types.DataType
	NodeCategory = types.NodeCategory
	Port         = types.Port
	Message      = types.Message
)

// Re-export constants for backward compatibility.
const (
	DataTypeAny     = types.DataTypeAny
	DataTypeString  = types.DataTypeString
	DataTypeNumber  = types.DataTypeNumber
	DataTypeBoolean = types.DataTypeBoolean
	DataTypeObject  = types.DataTypeObject
	DataTypeArray   = types.DataTypeArray

	NodeCategoryInput      = types.NodeCategoryInput
	NodeCategoryProcessing = types.NodeCategoryProcessing
	NodeCategoryOutput     = types.NodeCategoryOutput
)

// NewMessage is a convenience wrapper around types.NewMessage.
var NewMessage = types.NewMessage

// Position represents the X,Y coordinates of a node in the visual editor.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Node represents a single processing unit in a flow.
type Node struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`     // The registered node type (e.g., "trigger-interval")
	Category NodeCategory           `json:"category"` // input, processing, or output
	Position Position               `json:"position"` // Visual position in the editor
	Config   map[string]interface{} `json:"config"`   // Node-specific configuration
	Inputs   []Port                 `json:"inputs"`   // Input ports
	Outputs  []Port                 `json:"outputs"`  // Output ports
	Enabled  bool                   `json:"enabled"`  // Whether this node is active; true when absent in JSON
}

// UnmarshalJSON decodes a node, treating an absent (or null) "enabled" field
// as true. Definitions written by hand, by older editors or by agents often
// omit it, and decoding that as false would silently drop the node.
func (n *Node) UnmarshalJSON(data []byte) error {
	type plainNode Node // same fields, no methods: avoids recursing into UnmarshalJSON
	decoded := plainNode{Enabled: true}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*n = Node(decoded)
	return nil
}

// Connection represents a link between two nodes.
type Connection struct {
	ID         string `json:"id"`
	SourceNode string `json:"sourceNode"` // ID of the source node
	SourcePort string `json:"sourcePort"` // ID of the source port
	TargetNode string `json:"targetNode"` // ID of the target node
	TargetPort string `json:"targetPort"` // ID of the target port
}

// Flow represents a complete automation workflow.
type Flow struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Nodes       []Node                 `json:"nodes"`
	Connections []Connection           `json:"connections"`
	State       FlowState              `json:"state"`
	Variables   map[string]interface{} `json:"variables,omitempty"` // Legacy definition metadata; runtime variables use the scoped variable store.
	// Version counts the saved definitions of the flow (1 when created). It
	// is server-controlled: storage assigns it on every definition change
	// (not on state changes) and ignores any value a client or agent sends.
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	mu sync.RWMutex `json:"-"` // Protects concurrent access to fields
}

// NewFlow creates a new Flow with a generated UUID and timestamps.
func NewFlow(name string) *Flow {
	now := time.Now()
	return &Flow{
		ID:          uuid.New().String(),
		Name:        name,
		Nodes:       make([]Node, 0),
		Connections: make([]Connection, 0),
		State:       FlowStateInactive,
		Variables:   make(map[string]interface{}),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// NewNode creates a new Node with a generated UUID.
func NewNode(nodeType string, category NodeCategory) *Node {
	return &Node{
		ID:       uuid.New().String(),
		Type:     nodeType,
		Category: category,
		Position: Position{X: 0, Y: 0},
		Config:   make(map[string]interface{}),
		Inputs:   make([]Port, 0),
		Outputs:  make([]Port, 0),
		Enabled:  true,
	}
}

// NewConnection creates a new Connection with a generated UUID.
func NewConnection(sourceNode, sourcePort, targetNode, targetPort string) *Connection {
	return &Connection{
		ID:         uuid.New().String(),
		SourceNode: sourceNode,
		SourcePort: sourcePort,
		TargetNode: targetNode,
		TargetPort: targetPort,
	}
}

// AddNode adds a node to the flow.
func (f *Flow) AddNode(node Node) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Nodes = append(f.Nodes, node)
	f.UpdatedAt = time.Now()
}

// RemoveNode removes a node and its connections from the flow.
func (f *Flow) RemoveNode(nodeID string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Remove the node
	nodes := make([]Node, 0, len(f.Nodes))
	for _, n := range f.Nodes {
		if n.ID != nodeID {
			nodes = append(nodes, n)
		}
	}
	f.Nodes = nodes

	// Remove connections involving this node
	connections := make([]Connection, 0, len(f.Connections))
	for _, c := range f.Connections {
		if c.SourceNode != nodeID && c.TargetNode != nodeID {
			connections = append(connections, c)
		}
	}
	f.Connections = connections
	f.UpdatedAt = time.Now()
}

// AddConnection adds a connection to the flow.
func (f *Flow) AddConnection(conn Connection) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Connections = append(f.Connections, conn)
	f.UpdatedAt = time.Now()
}

// RemoveConnection removes a connection from the flow.
func (f *Flow) RemoveConnection(connID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	connections := make([]Connection, 0, len(f.Connections))
	for _, c := range f.Connections {
		if c.ID != connID {
			connections = append(connections, c)
		}
	}
	f.Connections = connections
	f.UpdatedAt = time.Now()
}

// GetNode returns a node by ID, or nil if not found.
func (f *Flow) GetNode(nodeID string) *Node {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for i := range f.Nodes {
		if f.Nodes[i].ID == nodeID {
			return &f.Nodes[i]
		}
	}
	return nil
}

// Validate checks the flow's structure (unique non-empty IDs, existing nodes
// and ports, no self-loops or cycles) and requires at least one enabled
// trigger. Whether a node is a trigger is decided by its registered type, never
// by the client-supplied Category field; unregistered types are never
// triggers.
func (f *Flow) Validate(reg *node.Registry) error {
	return f.validate(func(n *Node) bool {
		info, ok := reg.Get(n.Type)
		return ok && info.Category == NodeCategoryInput
	})
}

// ConfigIssues checks the config of every enabled node against its registered
// type's schema (node.CheckConfig: types, enums, bounds) and returns the
// problems with flow-level paths such as "nodes[3].config.interval". Disabled
// nodes are never instantiated and are skipped, as are unregistered types
// (instantiating them reports the unknown type). It is Lint's config check.
func (f *Flow) ConfigIssues(reg *node.Registry) []node.ConfigIssue {
	if reg == nil {
		return nil
	}
	var issues []node.ConfigIssue
	for _, fd := range configFindings(f, reg) {
		issues = append(issues, node.ConfigIssue{Path: fd.legacyPath(), Message: fd.msg})
	}
	return issues
}

// CheckConfig returns a *ValidationError listing every ConfigIssues entry, or
// nil when all node configs match their schemas. Deploy calls it so a mistyped
// value fails visibly instead of a node silently using its default.
func (f *Flow) CheckConfig(reg *node.Registry) error {
	issues := f.ConfigIssues(reg)
	if len(issues) == 0 {
		return nil
	}
	msgs := make([]string, len(issues))
	for i, issue := range issues {
		msgs[i] = issue.String()
	}
	return NewValidationError("%s", strings.Join(msgs, "; "))
}

// validate runs the structural checks (structureFindings, which Lint shares)
// on the ports stored on the nodes, requiring at least one enabled node for
// which isTrigger reports true, and returns the first problem's error.
func (f *Flow) validate(isTrigger func(*Node) bool) error {
	return firstError(structureFindings(f, func(i int) bool { return isTrigger(&f.Nodes[i]) }, storedPorts(f)))
}

// findCycle returns the ID of a node on a cycle, or "" if the graph is acyclic.
// Iterative DFS with white/grey/black colouring, so a deep chain cannot blow
// the goroutine stack.
func findCycle(nodes []Node, adjacency map[string][]string) string {
	const (
		white = iota
		grey
		black
	)
	color := make(map[string]int, len(nodes))
	type frame struct {
		id   string
		next int
	}
	for i := range nodes {
		root := nodes[i].ID
		if color[root] != white {
			continue
		}
		stack := []frame{{id: root}}
		color[root] = grey
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			succ := adjacency[top.id]
			if top.next >= len(succ) {
				color[top.id] = black
				stack = stack[:len(stack)-1]
				continue
			}
			nxt := succ[top.next]
			top.next++
			switch color[nxt] {
			case grey:
				return nxt
			case white:
				color[nxt] = grey
				stack = append(stack, frame{id: nxt})
			}
		}
	}
	return ""
}

// hasPort checks if a port with the given ID exists in the slice.
func hasPort(ports []Port, portID string) bool {
	for _, p := range ports {
		if p.ID == portID {
			return true
		}
	}
	return false
}

// ToJSON serializes the flow to JSON.
func (f *Flow) ToJSON() ([]byte, error) {
	return json.Marshal(f)
}

// FromJSON deserializes a flow from JSON.
func FromJSON(data []byte) (*Flow, error) {
	var flow Flow
	if err := json.Unmarshal(data, &flow); err != nil {
		return nil, err
	}
	return &flow, nil
}
