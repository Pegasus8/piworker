// Package types provides shared type definitions for the PiWorker flow system.
// This package exists to break import cycles between the flow and node packages.
package types

import (
	"reflect"
	"slices"
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

// deepCopyValue performs a deep copy of a payload/meta value so that a clone
// never shares mutable backing storage (maps, slices) with the original.
// The JSON-shaped types and the typed collections nodes commonly produce are
// handled on a fast path; any other map, slice or array is copied recursively
// via reflection. Scalars, strings, structs (by value), pointers, channels and
// functions are returned as-is.
func deepCopyValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case map[string]interface{}:
		return deepCopyMap(val)
	case []interface{}:
		return deepCopySlice(val)
	case map[string]string:
		if val == nil {
			return val
		}
		cp := make(map[string]string, len(val))
		for k, s := range val {
			cp[k] = s
		}
		return cp
	case []map[string]interface{}:
		if val == nil {
			return val
		}
		cp := make([]map[string]interface{}, len(val))
		for i, m := range val {
			cp[i] = deepCopyMap(m)
		}
		return cp
	case []string:
		return slices.Clone(val)
	case []byte:
		return slices.Clone(val)
	case []float64:
		return slices.Clone(val)
	case []int:
		return slices.Clone(val)
	case []int64:
		return slices.Clone(val)
	case []bool:
		return slices.Clone(val)
	case string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64,
		float32, float64, time.Time, time.Duration:
		return v
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array:
		return deepCopyReflect(rv).Interface()
	default:
		return v
	}
}

// deepCopyReflect recursively copies maps, slices, arrays and interface values
// of arbitrary element types. Other kinds are returned unchanged.
func deepCopyReflect(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(deepCopyReflect(v.Elem()))
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), deepCopyReflect(iter.Value()))
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(deepCopyReflect(v.Index(i)))
		}
		return out
	case reflect.Array:
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(deepCopyReflect(v.Index(i)))
		}
		return out
	default:
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
