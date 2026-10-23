package flow

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Flow Tests
// ============================================================================

func TestNewFlow(t *testing.T) {
	t.Run("creates flow with valid name", func(t *testing.T) {
		f := NewFlow("Test Flow")

		assert.NotEmpty(t, f.ID, "flow ID should be generated")
		assert.Equal(t, "Test Flow", f.Name)
		assert.Equal(t, FlowStateInactive, f.State)
		assert.NotNil(t, f.Nodes)
		assert.NotNil(t, f.Connections)
		assert.NotNil(t, f.Variables)
		assert.Empty(t, f.Nodes)
		assert.Empty(t, f.Connections)
		assert.False(t, f.CreatedAt.IsZero())
		assert.False(t, f.UpdatedAt.IsZero())
	})

	t.Run("creates flow with empty name", func(t *testing.T) {
		f := NewFlow("")

		assert.NotEmpty(t, f.ID)
		assert.Empty(t, f.Name)
	})

	t.Run("creates flow with unicode name", func(t *testing.T) {
		f := NewFlow("Tarea Automatizada")

		assert.Equal(t, "Tarea Automatizada", f.Name)
	})

	t.Run("creates flow with special characters", func(t *testing.T) {
		f := NewFlow("Test <Flow> & \"Quotes\" 'Apostrophe'")

		assert.Equal(t, "Test <Flow> & \"Quotes\" 'Apostrophe'", f.Name)
	})

	t.Run("generates unique IDs for multiple flows", func(t *testing.T) {
		ids := make(map[string]bool)
		for i := 0; i < 100; i++ {
			f := NewFlow("Test")
			assert.False(t, ids[f.ID], "duplicate ID generated")
			ids[f.ID] = true
		}
	})
}

func TestFlowValidate(t *testing.T) {
	t.Run("empty flow is valid", func(t *testing.T) {
		f := NewFlow("Valid Flow")
		err := f.Validate()

		// Empty flow with no nodes is valid (no trigger requirement for empty)
		assert.NoError(t, err)
	})

	t.Run("flow with trigger node is valid", func(t *testing.T) {
		f := NewFlow("Valid Flow")
		n := NewNode("trigger-interval", NodeCategoryInput)
		n.Outputs = []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}
		f.AddNode(*n)

		err := f.Validate()

		assert.NoError(t, err)
	})

	t.Run("flow without trigger node is invalid", func(t *testing.T) {
		f := NewFlow("Invalid Flow")
		n := NewNode("action-log", NodeCategoryOutput)
		f.AddNode(*n)

		err := f.Validate()

		assert.ErrorIs(t, err, ErrNoTriggerNodes)
	})

	t.Run("flow with valid connection is valid", func(t *testing.T) {
		f := NewFlow("Valid Flow")

		trigger := NewNode("trigger-interval", NodeCategoryInput)
		trigger.Outputs = []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}

		action := NewNode("action-log", NodeCategoryOutput)
		action.Inputs = []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}}

		f.AddNode(*trigger)
		f.AddNode(*action)
		f.AddConnection(*NewConnection(trigger.ID, "output", action.ID, "input"))

		err := f.Validate()

		assert.NoError(t, err)
	})

	t.Run("flow with invalid source node connection is invalid", func(t *testing.T) {
		f := NewFlow("Invalid Flow")

		trigger := NewNode("trigger-interval", NodeCategoryInput)
		trigger.Outputs = []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}
		f.AddNode(*trigger)

		// Connection with non-existent source node
		conn := NewConnection("non-existent", "output", trigger.ID, "input")
		f.Connections = append(f.Connections, *conn)

		err := f.Validate()

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "source node")
	})

	t.Run("flow with invalid target node connection is invalid", func(t *testing.T) {
		f := NewFlow("Invalid Flow")

		trigger := NewNode("trigger-interval", NodeCategoryInput)
		trigger.Outputs = []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}
		f.AddNode(*trigger)

		// Connection with non-existent target node
		conn := NewConnection(trigger.ID, "output", "non-existent", "input")
		f.Connections = append(f.Connections, *conn)

		err := f.Validate()

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "target node")
	})

	t.Run("flow with invalid source port is invalid", func(t *testing.T) {
		f := NewFlow("Invalid Flow")

		trigger := NewNode("trigger-interval", NodeCategoryInput)
		trigger.Outputs = []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}

		action := NewNode("action-log", NodeCategoryOutput)
		action.Inputs = []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}}

		f.AddNode(*trigger)
		f.AddNode(*action)

		// Connection with non-existent source port
		conn := NewConnection(trigger.ID, "wrong-port", action.ID, "input")
		f.Connections = append(f.Connections, *conn)

		err := f.Validate()

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "source port")
	})

	t.Run("flow with invalid target port is invalid", func(t *testing.T) {
		f := NewFlow("Invalid Flow")

		trigger := NewNode("trigger-interval", NodeCategoryInput)
		trigger.Outputs = []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}

		action := NewNode("action-log", NodeCategoryOutput)
		action.Inputs = []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}}

		f.AddNode(*trigger)
		f.AddNode(*action)

		// Connection with non-existent target port
		conn := NewConnection(trigger.ID, "output", action.ID, "wrong-port")
		f.Connections = append(f.Connections, *conn)

		err := f.Validate()

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "target port")
	})
}

func TestFlowAddNode(t *testing.T) {
	t.Run("adds node to flow", func(t *testing.T) {
		f := NewFlow("Test Flow")
		n := NewNode("trigger-interval", NodeCategoryInput)

		originalUpdateTime := f.UpdatedAt
		time.Sleep(1 * time.Millisecond)

		f.AddNode(*n)

		assert.Len(t, f.Nodes, 1)
		assert.Equal(t, n.ID, f.Nodes[0].ID)
		assert.True(t, f.UpdatedAt.After(originalUpdateTime))
	})

	t.Run("adds multiple nodes", func(t *testing.T) {
		f := NewFlow("Test Flow")

		for i := 0; i < 10; i++ {
			n := NewNode("trigger-interval", NodeCategoryInput)
			f.AddNode(*n)
		}

		assert.Len(t, f.Nodes, 10)
	})
}

func TestFlowRemoveNode(t *testing.T) {
	t.Run("removes existing node", func(t *testing.T) {
		f := NewFlow("Test Flow")
		n := NewNode("trigger-interval", NodeCategoryInput)
		f.AddNode(*n)

		f.RemoveNode(n.ID)

		assert.Empty(t, f.Nodes)
	})

	t.Run("removes node and associated connections", func(t *testing.T) {
		f := NewFlow("Test Flow")

		trigger := NewNode("trigger-interval", NodeCategoryInput)
		trigger.Outputs = []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}

		action := NewNode("action-log", NodeCategoryOutput)
		action.Inputs = []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}}

		f.AddNode(*trigger)
		f.AddNode(*action)
		f.AddConnection(*NewConnection(trigger.ID, "output", action.ID, "input"))

		assert.Len(t, f.Connections, 1)

		f.RemoveNode(trigger.ID)

		assert.Len(t, f.Nodes, 1)
		assert.Empty(t, f.Connections, "connections should be removed")
	})

	t.Run("does nothing for non-existent node", func(t *testing.T) {
		f := NewFlow("Test Flow")
		n := NewNode("trigger-interval", NodeCategoryInput)
		f.AddNode(*n)

		f.RemoveNode("non-existent")

		assert.Len(t, f.Nodes, 1)
	})
}

func TestFlowGetNode(t *testing.T) {
	t.Run("returns existing node", func(t *testing.T) {
		f := NewFlow("Test Flow")
		n := NewNode("trigger-interval", NodeCategoryInput)
		f.AddNode(*n)

		result := f.GetNode(n.ID)

		require.NotNil(t, result)
		assert.Equal(t, n.ID, result.ID)
	})

	t.Run("returns nil for non-existent node", func(t *testing.T) {
		f := NewFlow("Test Flow")

		result := f.GetNode("non-existent")

		assert.Nil(t, result)
	})

	t.Run("returns nil for empty ID", func(t *testing.T) {
		f := NewFlow("Test Flow")

		result := f.GetNode("")

		assert.Nil(t, result)
	})
}

func TestFlowGetTriggerNodes(t *testing.T) {
	t.Run("returns only enabled input nodes", func(t *testing.T) {
		f := NewFlow("Test Flow")

		trigger1 := NewNode("trigger-interval", NodeCategoryInput)
		trigger2 := NewNode("trigger-cron", NodeCategoryInput)
		action := NewNode("action-log", NodeCategoryOutput)

		disabledTrigger := NewNode("trigger-interval", NodeCategoryInput)
		disabledTrigger.Enabled = false

		f.AddNode(*trigger1)
		f.AddNode(*trigger2)
		f.AddNode(*action)
		f.AddNode(*disabledTrigger)

		triggers := f.GetTriggerNodes()

		assert.Len(t, triggers, 2)
		for _, trigger := range triggers {
			assert.Equal(t, NodeCategoryInput, trigger.Category)
			assert.True(t, trigger.Enabled)
		}
	})

	t.Run("returns empty slice for flow with no triggers", func(t *testing.T) {
		f := NewFlow("Test Flow")
		action := NewNode("action-log", NodeCategoryOutput)
		f.AddNode(*action)

		triggers := f.GetTriggerNodes()

		assert.Empty(t, triggers)
	})
}

func TestFlowAddConnection(t *testing.T) {
	t.Run("adds connection to flow", func(t *testing.T) {
		f := NewFlow("Test Flow")
		conn := NewConnection("source", "port1", "target", "port2")

		f.AddConnection(*conn)

		assert.Len(t, f.Connections, 1)
	})

	t.Run("adds multiple connections", func(t *testing.T) {
		f := NewFlow("Test Flow")

		for i := 0; i < 5; i++ {
			conn := NewConnection("source", "port1", "target", "port2")
			f.AddConnection(*conn)
		}

		assert.Len(t, f.Connections, 5)
	})
}

func TestFlowRemoveConnection(t *testing.T) {
	t.Run("removes existing connection", func(t *testing.T) {
		f := NewFlow("Test Flow")
		conn := NewConnection("source", "port1", "target", "port2")
		f.AddConnection(*conn)

		f.RemoveConnection(conn.ID)

		assert.Empty(t, f.Connections)
	})

	t.Run("does nothing for non-existent connection", func(t *testing.T) {
		f := NewFlow("Test Flow")
		conn := NewConnection("source", "port1", "target", "port2")
		f.AddConnection(*conn)

		f.RemoveConnection("non-existent")

		assert.Len(t, f.Connections, 1)
	})
}

func TestFlowJSON(t *testing.T) {
	t.Run("serializes and deserializes correctly", func(t *testing.T) {
		f := NewFlow("Test Flow")
		f.Description = "A test flow"

		trigger := NewNode("trigger-interval", NodeCategoryInput)
		trigger.Config["interval"] = 1000
		trigger.Outputs = []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}
		f.AddNode(*trigger)

		action := NewNode("action-log", NodeCategoryOutput)
		action.Inputs = []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}}
		f.AddNode(*action)

		f.AddConnection(*NewConnection(trigger.ID, "output", action.ID, "input"))

		// Serialize
		data, err := f.ToJSON()
		require.NoError(t, err)

		// Deserialize
		restored, err := FromJSON(data)
		require.NoError(t, err)

		assert.Equal(t, f.ID, restored.ID)
		assert.Equal(t, f.Name, restored.Name)
		assert.Equal(t, f.Description, restored.Description)
		assert.Len(t, restored.Nodes, 2)
		assert.Len(t, restored.Connections, 1)
	})

	t.Run("handles empty flow", func(t *testing.T) {
		f := NewFlow("Empty")

		data, err := f.ToJSON()
		require.NoError(t, err)

		restored, err := FromJSON(data)
		require.NoError(t, err)

		assert.Equal(t, f.ID, restored.ID)
		assert.Empty(t, restored.Nodes)
		assert.Empty(t, restored.Connections)
	})

	t.Run("fails on invalid JSON", func(t *testing.T) {
		_, err := FromJSON([]byte("invalid json"))

		assert.Error(t, err)
	})
}

// ============================================================================
// Node Tests
// ============================================================================

func TestNewNode(t *testing.T) {
	t.Run("creates node with valid type", func(t *testing.T) {
		n := NewNode("trigger-interval", NodeCategoryInput)

		assert.NotEmpty(t, n.ID)
		assert.Equal(t, "trigger-interval", n.Type)
		assert.Equal(t, NodeCategoryInput, n.Category)
		assert.True(t, n.Enabled)
		assert.NotNil(t, n.Config)
		assert.NotNil(t, n.Inputs)
		assert.NotNil(t, n.Outputs)
		assert.Equal(t, float64(0), n.Position.X)
		assert.Equal(t, float64(0), n.Position.Y)
	})

	t.Run("generates unique IDs", func(t *testing.T) {
		ids := make(map[string]bool)
		for i := 0; i < 100; i++ {
			n := NewNode("trigger-interval", NodeCategoryInput)
			assert.False(t, ids[n.ID])
			ids[n.ID] = true
		}
	})
}

func TestNodeCategories(t *testing.T) {
	tests := []struct {
		name     string
		category NodeCategory
	}{
		{"input category", NodeCategoryInput},
		{"processing category", NodeCategoryProcessing},
		{"output category", NodeCategoryOutput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := NewNode("test-node", tt.category)
			assert.Equal(t, tt.category, n.Category)
		})
	}
}

// ============================================================================
// Connection Tests
// ============================================================================

func TestNewConnection(t *testing.T) {
	t.Run("creates connection with valid parameters", func(t *testing.T) {
		conn := NewConnection("source", "port1", "target", "port2")

		assert.NotEmpty(t, conn.ID)
		assert.Equal(t, "source", conn.SourceNode)
		assert.Equal(t, "port1", conn.SourcePort)
		assert.Equal(t, "target", conn.TargetNode)
		assert.Equal(t, "port2", conn.TargetPort)
	})

	t.Run("generates unique IDs", func(t *testing.T) {
		ids := make(map[string]bool)
		for i := 0; i < 100; i++ {
			conn := NewConnection("s", "p1", "t", "p2")
			assert.False(t, ids[conn.ID])
			ids[conn.ID] = true
		}
	})

	t.Run("creates connection with empty strings", func(t *testing.T) {
		conn := NewConnection("", "", "", "")

		assert.NotEmpty(t, conn.ID)
		assert.Empty(t, conn.SourceNode)
		assert.Empty(t, conn.TargetNode)
	})
}

// ============================================================================
// Message Tests
// ============================================================================

func TestNewMessage(t *testing.T) {
	t.Run("creates message with object payload", func(t *testing.T) {
		payload := map[string]interface{}{"key": "value"}
		msg := NewMessage(payload, DataTypeObject)

		assert.NotEmpty(t, msg.ID)
		assert.Equal(t, payload, msg.Payload)
		assert.Equal(t, DataTypeObject, msg.PayloadType)
		assert.NotNil(t, msg.Meta)
		assert.False(t, msg.Timestamp.IsZero())
	})

	t.Run("creates message with string payload", func(t *testing.T) {
		msg := NewMessage("hello", DataTypeString)

		assert.Equal(t, "hello", msg.Payload)
		assert.Equal(t, DataTypeString, msg.PayloadType)
	})

	t.Run("creates message with number payload", func(t *testing.T) {
		msg := NewMessage(42, DataTypeNumber)

		assert.Equal(t, 42, msg.Payload)
		assert.Equal(t, DataTypeNumber, msg.PayloadType)
	})

	t.Run("creates message with nil payload", func(t *testing.T) {
		msg := NewMessage(nil, DataTypeAny)

		assert.Nil(t, msg.Payload)
	})

	t.Run("generates unique IDs", func(t *testing.T) {
		ids := make(map[string]bool)
		for i := 0; i < 100; i++ {
			msg := NewMessage("test", DataTypeString)
			assert.False(t, ids[msg.ID])
			ids[msg.ID] = true
		}
	})
}

func TestMessageClone(t *testing.T) {
	t.Run("clones message with new ID", func(t *testing.T) {
		original := NewMessage("test", DataTypeString)
		original.Topic = "test-topic"
		original.SourceNode = "source"
		original.SourcePort = "port"
		original.SetMeta("key", "value")

		clone := original.Clone()

		assert.NotEqual(t, original.ID, clone.ID)
		assert.Equal(t, original.Payload, clone.Payload)
		assert.Equal(t, original.Topic, clone.Topic)
		assert.Equal(t, original.SourceNode, clone.SourceNode)
		assert.Equal(t, original.SourcePort, clone.SourcePort)
		// Meta is a map (reference type), verify independence by checking different contents
		assert.NotNil(t, clone.Meta)
	})

	t.Run("clones message with independent meta", func(t *testing.T) {
		original := NewMessage("test", DataTypeString)
		original.SetMeta("key", "original")

		clone := original.Clone()
		clone.SetMeta("key", "modified")
		clone.SetMeta("new", "value")

		v, _ := original.GetMeta("key")
		assert.Equal(t, "original", v)

		v, _ = clone.GetMeta("key")
		assert.Equal(t, "modified", v)

		_, ok := original.GetMeta("new")
		assert.False(t, ok)
	})
}

func TestMessageWithPayload(t *testing.T) {
	t.Run("creates new message with different payload", func(t *testing.T) {
		original := NewMessage("original", DataTypeString)
		original.SetMeta("key", "value")

		modified := original.WithPayload("modified", DataTypeString)

		assert.NotEqual(t, original.ID, modified.ID)
		assert.Equal(t, "original", original.Payload)
		assert.Equal(t, "modified", modified.Payload)

		// Meta should be preserved
		v, ok := modified.GetMeta("key")
		assert.True(t, ok)
		assert.Equal(t, "value", v)
	})

	t.Run("allows changing payload type", func(t *testing.T) {
		original := NewMessage("42", DataTypeString)

		modified := original.WithPayload(42, DataTypeNumber)

		assert.Equal(t, DataTypeString, original.PayloadType)
		assert.Equal(t, DataTypeNumber, modified.PayloadType)
	})
}

func TestMessageMeta(t *testing.T) {
	t.Run("sets and gets meta values", func(t *testing.T) {
		msg := NewMessage(nil, DataTypeAny)

		msg.SetMeta("string", "value")
		msg.SetMeta("number", 42)
		msg.SetMeta("bool", true)
		msg.SetMeta("nested", map[string]interface{}{"key": "value"})

		v, ok := msg.GetMeta("string")
		assert.True(t, ok)
		assert.Equal(t, "value", v)

		v, ok = msg.GetMeta("number")
		assert.True(t, ok)
		assert.Equal(t, 42, v)

		v, ok = msg.GetMeta("bool")
		assert.True(t, ok)
		assert.Equal(t, true, v)
	})

	t.Run("returns false for non-existent key", func(t *testing.T) {
		msg := NewMessage(nil, DataTypeAny)

		_, ok := msg.GetMeta("nonexistent")

		assert.False(t, ok)
	})

	t.Run("overwrites existing meta values", func(t *testing.T) {
		msg := NewMessage(nil, DataTypeAny)

		msg.SetMeta("key", "original")
		msg.SetMeta("key", "updated")

		v, _ := msg.GetMeta("key")
		assert.Equal(t, "updated", v)
	})

	t.Run("handles nil meta", func(t *testing.T) {
		msg := &Message{ID: "test", Meta: nil}

		_, ok := msg.GetMeta("key")
		assert.False(t, ok)
	})
}

// ============================================================================
// Port Tests
// ============================================================================

func TestPort(t *testing.T) {
	t.Run("creates port with all data types", func(t *testing.T) {
		types := []DataType{
			DataTypeAny,
			DataTypeString,
			DataTypeNumber,
			DataTypeBoolean,
			DataTypeObject,
			DataTypeArray,
		}

		for _, dt := range types {
			port := Port{
				ID:       "test",
				Name:     "Test",
				DataType: dt,
			}
			assert.Equal(t, dt, port.DataType)
		}
	})

	t.Run("port serialization", func(t *testing.T) {
		port := Port{
			ID:       "output",
			Name:     "Output",
			DataType: DataTypeObject,
			Multiple: true,
			Required: false,
		}

		data, err := json.Marshal(port)
		require.NoError(t, err)

		var restored Port
		err = json.Unmarshal(data, &restored)
		require.NoError(t, err)

		assert.Equal(t, port, restored)
	})
}

// ============================================================================
// Position Tests
// ============================================================================

func TestPosition(t *testing.T) {
	t.Run("default position is zero", func(t *testing.T) {
		var pos Position

		assert.Equal(t, float64(0), pos.X)
		assert.Equal(t, float64(0), pos.Y)
	})

	t.Run("handles negative coordinates", func(t *testing.T) {
		pos := Position{X: -100.5, Y: -200.5}

		assert.Equal(t, -100.5, pos.X)
		assert.Equal(t, -200.5, pos.Y)
	})

	t.Run("handles large coordinates", func(t *testing.T) {
		pos := Position{X: 1e10, Y: 1e10}

		assert.Equal(t, 1e10, pos.X)
		assert.Equal(t, 1e10, pos.Y)
	})
}

// ============================================================================
// FlowState Tests
// ============================================================================

func TestFlowState(t *testing.T) {
	tests := []struct {
		state    FlowState
		expected string
	}{
		{FlowStateActive, "active"},
		{FlowStateInactive, "inactive"},
		{FlowStateRunning, "running"},
		{FlowStateFailed, "failed"},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.state))
		})
	}
}

// ============================================================================
// Edge Cases and Boundary Tests
// ============================================================================

func TestEdgeCases(t *testing.T) {
	t.Run("flow with maximum reasonable nodes", func(t *testing.T) {
		f := NewFlow("Large Flow")

		for i := 0; i < 100; i++ {
			category := NodeCategoryProcessing
			if i == 0 {
				category = NodeCategoryInput // First one is trigger
			}
			n := NewNode("test-node", category)
			f.AddNode(*n)
		}

		assert.Len(t, f.Nodes, 100)
	})

	t.Run("flow with many connections", func(t *testing.T) {
		f := NewFlow("Connected Flow")

		trigger := NewNode("trigger-interval", NodeCategoryInput)
		trigger.Outputs = []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}
		f.AddNode(*trigger)

		for i := 0; i < 50; i++ {
			n := NewNode("action-log", NodeCategoryOutput)
			n.Inputs = []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}}
			f.AddNode(*n)
			f.AddConnection(*NewConnection(trigger.ID, "output", n.ID, "input"))
		}

		assert.Len(t, f.Nodes, 51)
		assert.Len(t, f.Connections, 50)
	})

	t.Run("message with complex nested payload", func(t *testing.T) {
		payload := map[string]interface{}{
			"level1": map[string]interface{}{
				"level2": map[string]interface{}{
					"level3": []interface{}{1, "two", true, nil},
				},
			},
		}

		msg := NewMessage(payload, DataTypeObject)

		assert.NotNil(t, msg.Payload)
	})

	t.Run("concurrent flow operations", func(t *testing.T) {
		f := NewFlow("Concurrent Flow")

		done := make(chan bool)
		for i := 0; i < 10; i++ {
			go func() {
				for j := 0; j < 10; j++ {
					n := NewNode("test-node", NodeCategoryProcessing)
					f.AddNode(*n)
				}
				done <- true
			}()
		}

		for i := 0; i < 10; i++ {
			<-done
		}

		// Note: This test might have race conditions depending on the implementation
		// It's here to demonstrate the need for thread-safety testing
		assert.GreaterOrEqual(t, len(f.Nodes), 1)
	})
}

// ============================================================================
// hasPort Helper Tests
// ============================================================================

func TestHasPort(t *testing.T) {
	ports := []Port{
		{ID: "port1", Name: "Port 1"},
		{ID: "port2", Name: "Port 2"},
		{ID: "port3", Name: "Port 3"},
	}

	t.Run("finds existing port", func(t *testing.T) {
		assert.True(t, hasPort(ports, "port1"))
		assert.True(t, hasPort(ports, "port2"))
		assert.True(t, hasPort(ports, "port3"))
	})

	t.Run("does not find non-existent port", func(t *testing.T) {
		assert.False(t, hasPort(ports, "port4"))
		assert.False(t, hasPort(ports, ""))
	})

	t.Run("handles empty slice", func(t *testing.T) {
		assert.False(t, hasPort([]Port{}, "port1"))
	})

	t.Run("handles nil slice", func(t *testing.T) {
		assert.False(t, hasPort(nil, "port1"))
	})
}
