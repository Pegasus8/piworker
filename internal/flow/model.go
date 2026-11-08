// Package flow provides the core data models and runtime for flow-based automation.
package flow

import (
	"encoding/json"
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
	Enabled  bool                   `json:"enabled"`  // Whether this node is active
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
	CreatedAt   time.Time              `json:"createdAt"`
	UpdatedAt   time.Time              `json:"updatedAt"`

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

// validate runs the structural checks and requires at least one enabled node
// for which isTrigger reports true.
func (f *Flow) validate(isTrigger func(*Node) bool) error {
	// An empty flow is valid (for initial creation)
	if len(f.Nodes) == 0 {
		return nil
	}

	nodeMap := make(map[string]*Node, len(f.Nodes))
	for i := range f.Nodes {
		n := &f.Nodes[i]
		if n.ID == "" {
			return NewValidationError("node at index %d (type %s) has an empty ID", i, n.Type)
		}
		if _, dup := nodeMap[n.ID]; dup {
			return NewValidationError("duplicate node ID %s", n.ID)
		}
		nodeMap[n.ID] = n
	}

	// If there are nodes, there must be at least one trigger node
	hasTrigger := false
	for i := range f.Nodes {
		if f.Nodes[i].Enabled && isTrigger(&f.Nodes[i]) {
			hasTrigger = true
			break
		}
	}
	if !hasTrigger {
		return ErrNoTriggerNodes
	}

	// Validate all connections reference existing nodes and ports
	connIDs := make(map[string]struct{}, len(f.Connections))
	adjacency := make(map[string][]string)
	for _, conn := range f.Connections {
		if conn.ID != "" {
			if _, dup := connIDs[conn.ID]; dup {
				return NewValidationError("duplicate connection ID %s", conn.ID)
			}
			connIDs[conn.ID] = struct{}{}
		}

		sourceNode, ok := nodeMap[conn.SourceNode]
		if !ok {
			return NewValidationError("connection %s references non-existent source node %s", conn.ID, conn.SourceNode)
		}
		targetNode, ok := nodeMap[conn.TargetNode]
		if !ok {
			return NewValidationError("connection %s references non-existent target node %s", conn.ID, conn.TargetNode)
		}
		if conn.SourceNode == conn.TargetNode {
			return NewValidationError("connection %s connects node %s to itself", conn.ID, conn.SourceNode)
		}

		// Validate ports exist
		if !hasPort(sourceNode.Outputs, conn.SourcePort) {
			return NewValidationError("connection %s references non-existent source port %s", conn.ID, conn.SourcePort)
		}
		if !hasPort(targetNode.Inputs, conn.TargetPort) {
			return NewValidationError("connection %s references non-existent target port %s", conn.ID, conn.TargetPort)
		}
		adjacency[conn.SourceNode] = append(adjacency[conn.SourceNode], conn.TargetNode)
	}

	if cycle := findCycle(f.Nodes, adjacency); cycle != "" {
		return NewValidationError("flow contains a cycle through node %s", cycle)
	}

	return nil
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
