// Package types provides shared type definitions for the PiWorker flow system.
// This package exists to break import cycles between the flow and node packages.
package types

import (
	"time"

	"github.com/google/uuid"
)

// DataType represents the type of data a port can handle.
type DataType string

const (
	DataTypeAny     DataType = "any"
	DataTypeString  DataType = "string"
	DataTypeNumber  DataType = "number"
	DataTypeBoolean DataType = "boolean"
	DataTypeObject  DataType = "object"
	DataTypeArray   DataType = "array"
)

// NodeCategory represents the functional category of a node.
type NodeCategory string

const (
	// NodeCategoryInput represents trigger/input nodes that initiate flow execution.
	NodeCategoryInput NodeCategory = "input"
	// NodeCategoryProcessing represents nodes that transform or process data.
	NodeCategoryProcessing NodeCategory = "processing"
	// NodeCategoryOutput represents nodes that perform actions or produce output.
	NodeCategoryOutput NodeCategory = "output"
)

// Port represents an input or output connection point on a node.
type Port struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	DataType DataType `json:"dataType"`
	Multiple bool     `json:"multiple"` // Whether this port can have multiple connections
	Required bool     `json:"required"` // Whether this port must be connected (for inputs)
}

// Message represents data flowing between nodes.
type Message struct {
	ID          string                 `json:"id"`
	Payload     interface{}            `json:"payload"`     // The actual data
	PayloadType DataType               `json:"payloadType"` // Type hint for the payload
	Topic       string                 `json:"topic"`       // Optional topic for routing
	Meta        map[string]interface{} `json:"meta"`        // Additional metadata
	Timestamp   time.Time              `json:"timestamp"`
	SourceNode  string                 `json:"sourceNode"` // ID of the node that produced this message
	SourcePort  string                 `json:"sourcePort"` // ID of the port that produced this message
}

// NewMessage creates a new Message with a generated UUID and timestamp.
func NewMessage(payload interface{}, payloadType DataType) *Message {
	return &Message{
		ID:          uuid.New().String(),
		Payload:     payload,
		PayloadType: payloadType,
		Meta:        make(map[string]interface{}),
		Timestamp:   time.Now(),
	}
}

// Clone creates a deep copy of the message.
func (m *Message) Clone() *Message {
	clone := &Message{
		ID:          uuid.New().String(),
		Payload:     deepCopyValue(m.Payload),
		PayloadType: m.PayloadType,
		Topic:       m.Topic,
		Meta:        make(map[string]interface{}),
		Timestamp:   time.Now(),
		SourceNode:  m.SourceNode,
		SourcePort:  m.SourcePort,
	}

	// Deep copy meta
	for k, v := range m.Meta {
		clone.Meta[k] = deepCopyValue(v)
	}

	return clone
}

// deepCopyValue performs a deep copy of common types.
// For map[string]interface{} and []interface{}, it recursively copies.
// For other types (primitives, structs), it returns the value as-is.
func deepCopyValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case map[string]interface{}:
		return deepCopyMap(val)
	case []interface{}:
		return deepCopySlice(val)
	default:
		// Primitives and other types are safe to copy directly
		return v
	}
}

// deepCopyMap creates a deep copy of a map[string]interface{}.
func deepCopyMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	cp := make(map[string]interface{}, len(m))
	for k, v := range m {
		cp[k] = deepCopyValue(v)
	}
	return cp
}

// deepCopySlice creates a deep copy of a []interface{}.
func deepCopySlice(s []interface{}) []interface{} {
	if s == nil {
		return nil
	}
	cp := make([]interface{}, len(s))
	for i, v := range s {
		cp[i] = deepCopyValue(v)
	}
	return cp
}

// WithPayload returns a new message with the updated payload.
func (m *Message) WithPayload(payload interface{}, payloadType DataType) *Message {
	clone := m.Clone()
	clone.Payload = payload
	clone.PayloadType = payloadType
	return clone
}

// SetMeta sets a metadata value on the message.
func (m *Message) SetMeta(key string, value interface{}) {
	if m.Meta == nil {
		m.Meta = make(map[string]interface{})
	}
	m.Meta[key] = value
}

// GetMeta retrieves a metadata value from the message.
func (m *Message) GetMeta(key string) (interface{}, bool) {
	if m.Meta == nil {
		return nil, false
	}
	v, ok := m.Meta[key]
	return v, ok
}

// User represents a user in the system.
// This type is defined in types to avoid import cycles between api and storage.
type User struct {
	Username string `json:"username"`
	// PasswordHash is the bcrypt hash of the password (never exposed in JSON).
	PasswordHash string `json:"-"`
}
