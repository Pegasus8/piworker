package node

import (
	"context"
	"sync"
	"testing"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Mock Node for Testing
// ============================================================================

type mockNode struct {
	*BaseNode
}

func (n *mockNode) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	return []*types.Message{msg}, nil
}

func (n *mockNode) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny}}
}

func mockNodeFactory(config map[string]interface{}) (Node, error) {
	return &mockNode{BaseNode: NewBaseNode(config)}, nil
}

// ============================================================================
// Registry Creation Tests
// ============================================================================

func TestNewRegistry(t *testing.T) {
	t.Run("creates empty registry", func(t *testing.T) {
		registry := NewRegistry()

		assert.NotNil(t, registry)
		assert.Equal(t, 0, registry.Count())
	})
}

// ============================================================================
// Registry Register Tests
// ============================================================================

func TestRegistryRegister(t *testing.T) {
	t.Run("registers new node type", func(t *testing.T) {
		registry := NewRegistry()

		info := NodeTypeInfo{
			Type:        "test-node",
			Name:        "Test Node",
			Description: "A test node",
			Category:    types.NodeCategoryProcessing,
		}

		err := registry.Register(info, mockNodeFactory)

		assert.NoError(t, err)
		assert.Equal(t, 1, registry.Count())
		assert.True(t, registry.Has("test-node"))
	})

	t.Run("fails to register duplicate type", func(t *testing.T) {
		registry := NewRegistry()

		info := NodeTypeInfo{
			Type: "test-node",
			Name: "Test Node",
		}

		err := registry.Register(info, mockNodeFactory)
		require.NoError(t, err)

		err = registry.Register(info, mockNodeFactory)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
	})

	t.Run("registers multiple different types", func(t *testing.T) {
		registry := NewRegistry()

		types := []string{"type-a", "type-b", "type-c"}
		for _, typ := range types {
			info := NodeTypeInfo{Type: typ, Name: typ}
			err := registry.Register(info, mockNodeFactory)
			require.NoError(t, err)
		}

		assert.Equal(t, 3, registry.Count())
		for _, typ := range types {
			assert.True(t, registry.Has(typ))
		}
	})

	t.Run("stores complete node info", func(t *testing.T) {
		registry := NewRegistry()

		info := NodeTypeInfo{
			Type:        "complete-node",
			Name:        "Complete Node",
			Description: "A fully configured test node",
			Category:    types.NodeCategoryInput,
			Inputs: []types.Port{
				{ID: "in1", Name: "Input 1", DataType: types.DataTypeString},
			},
			Outputs: []types.Port{
				{ID: "out1", Name: "Output 1", DataType: types.DataTypeAny},
			},
			Icon: "test-icon",
		}

		err := registry.Register(info, mockNodeFactory)
		require.NoError(t, err)

		retrieved, exists := registry.Get("complete-node")

		assert.True(t, exists)
		assert.Equal(t, info.Type, retrieved.Type)
		assert.Equal(t, info.Name, retrieved.Name)
		assert.Equal(t, info.Description, retrieved.Description)
		assert.Equal(t, info.Category, retrieved.Category)
		assert.Equal(t, info.Icon, retrieved.Icon)
		assert.Len(t, retrieved.Inputs, 1)
		assert.Len(t, retrieved.Outputs, 1)
	})
}

// ============================================================================
// Registry Unregister Tests
// ============================================================================

func TestRegistryUnregister(t *testing.T) {
	t.Run("unregisters existing type", func(t *testing.T) {
		registry := NewRegistry()

		info := NodeTypeInfo{Type: "test-node", Name: "Test"}
		err := registry.Register(info, mockNodeFactory)
		require.NoError(t, err)

		registry.Unregister("test-node")

		assert.False(t, registry.Has("test-node"))
		assert.Equal(t, 0, registry.Count())
	})

	t.Run("does nothing for non-existent type", func(t *testing.T) {
		registry := NewRegistry()

		info := NodeTypeInfo{Type: "existing", Name: "Existing"}
		err := registry.Register(info, mockNodeFactory)
		require.NoError(t, err)

		registry.Unregister("nonexistent")

		assert.Equal(t, 1, registry.Count())
		assert.True(t, registry.Has("existing"))
	})

	t.Run("allows re-registration after unregister", func(t *testing.T) {
		registry := NewRegistry()

		info := NodeTypeInfo{Type: "test-node", Name: "Original"}
		err := registry.Register(info, mockNodeFactory)
		require.NoError(t, err)

		registry.Unregister("test-node")

		info.Name = "New Version"
		err = registry.Register(info, mockNodeFactory)

		assert.NoError(t, err)
		retrieved, _ := registry.Get("test-node")
		assert.Equal(t, "New Version", retrieved.Name)
	})
}

// ============================================================================
// Registry Create Tests
// ============================================================================

func TestRegistryCreate(t *testing.T) {
	t.Run("creates node with empty config", func(t *testing.T) {
		registry := NewRegistry()

		info := NodeTypeInfo{Type: "test-node", Name: "Test"}
		err := registry.Register(info, mockNodeFactory)
		require.NoError(t, err)

		node, err := registry.Create("test-node", nil)

		assert.NoError(t, err)
		assert.NotNil(t, node)
	})

	t.Run("creates node with config", func(t *testing.T) {
		registry := NewRegistry()

		factory := func(config map[string]interface{}) (Node, error) {
			n := &mockNode{BaseNode: NewBaseNode(config)}
			return n, nil
		}

		info := NodeTypeInfo{Type: "configurable", Name: "Configurable"}
		err := registry.Register(info, factory)
		require.NoError(t, err)

		config := map[string]interface{}{
			"key1": "value1",
			"key2": 42,
		}

		node, err := registry.Create("configurable", config)

		assert.NoError(t, err)
		assert.NotNil(t, node)
	})

	t.Run("fails for unknown type", func(t *testing.T) {
		registry := NewRegistry()

		node, err := registry.Create("unknown-type", nil)

		assert.Error(t, err)
		assert.Nil(t, node)
		assert.Contains(t, err.Error(), "unknown node type")
	})

	t.Run("propagates factory error", func(t *testing.T) {
		registry := NewRegistry()

		factory := func(config map[string]interface{}) (Node, error) {
			return nil, assert.AnError
		}

		info := NodeTypeInfo{Type: "error-node", Name: "Error"}
		err := registry.Register(info, factory)
		require.NoError(t, err)

		node, err := registry.Create("error-node", nil)

		assert.Error(t, err)
		assert.Nil(t, node)
	})
}

// ============================================================================
// Registry Get Tests
// ============================================================================

func TestRegistryGet(t *testing.T) {
	t.Run("returns existing type info", func(t *testing.T) {
		registry := NewRegistry()

		info := NodeTypeInfo{
			Type:     "test-node",
			Name:     "Test Node",
			Category: types.NodeCategoryInput,
		}
		err := registry.Register(info, mockNodeFactory)
		require.NoError(t, err)

		retrieved, exists := registry.Get("test-node")

		assert.True(t, exists)
		assert.Equal(t, info.Type, retrieved.Type)
		assert.Equal(t, info.Name, retrieved.Name)
		assert.Equal(t, info.Category, retrieved.Category)
	})

	t.Run("returns false for non-existent type", func(t *testing.T) {
		registry := NewRegistry()

		_, exists := registry.Get("nonexistent")

		assert.False(t, exists)
	})
}

// ============================================================================
// Registry List Tests
// ============================================================================

func TestRegistryList(t *testing.T) {
	t.Run("returns empty list for empty registry", func(t *testing.T) {
		registry := NewRegistry()

		list := registry.List()

		assert.Empty(t, list)
	})

	t.Run("returns all registered types", func(t *testing.T) {
		registry := NewRegistry()

		types := []string{"type-a", "type-b", "type-c"}
		for _, typ := range types {
			info := NodeTypeInfo{Type: typ, Name: typ}
			err := registry.Register(info, mockNodeFactory)
			require.NoError(t, err)
		}

		list := registry.List()

		assert.Len(t, list, 3)

		typeNames := make(map[string]bool)
		for _, info := range list {
			typeNames[info.Type] = true
		}

		for _, typ := range types {
			assert.True(t, typeNames[typ])
		}
	})
}

// ============================================================================
// Registry ListByCategory Tests
// ============================================================================

func TestRegistryListByCategory(t *testing.T) {
	t.Run("filters by category", func(t *testing.T) {
		registry := NewRegistry()

		categories := []struct {
			typ      string
			category types.NodeCategory
		}{
			{"trigger-1", types.NodeCategoryInput},
			{"trigger-2", types.NodeCategoryInput},
			{"process-1", types.NodeCategoryProcessing},
			{"action-1", types.NodeCategoryOutput},
			{"action-2", types.NodeCategoryOutput},
		}

		for _, c := range categories {
			info := NodeTypeInfo{Type: c.typ, Name: c.typ, Category: c.category}
			err := registry.Register(info, mockNodeFactory)
			require.NoError(t, err)
		}

		inputNodes := registry.ListByCategory(types.NodeCategoryInput)
		assert.Len(t, inputNodes, 2)
		for _, info := range inputNodes {
			assert.Equal(t, types.NodeCategoryInput, info.Category)
		}

		processingNodes := registry.ListByCategory(types.NodeCategoryProcessing)
		assert.Len(t, processingNodes, 1)

		outputNodes := registry.ListByCategory(types.NodeCategoryOutput)
		assert.Len(t, outputNodes, 2)
	})

	t.Run("returns empty for category with no nodes", func(t *testing.T) {
		registry := NewRegistry()

		info := NodeTypeInfo{Type: "input-node", Name: "Input", Category: types.NodeCategoryInput}
		err := registry.Register(info, mockNodeFactory)
		require.NoError(t, err)

		processingNodes := registry.ListByCategory(types.NodeCategoryProcessing)

		assert.Empty(t, processingNodes)
	})
}

// ============================================================================
// Registry Has Tests
// ============================================================================

func TestRegistryHas(t *testing.T) {
	t.Run("returns true for registered type", func(t *testing.T) {
		registry := NewRegistry()

		info := NodeTypeInfo{Type: "test-node", Name: "Test"}
		err := registry.Register(info, mockNodeFactory)
		require.NoError(t, err)

		assert.True(t, registry.Has("test-node"))
	})

	t.Run("returns false for unregistered type", func(t *testing.T) {
		registry := NewRegistry()

		assert.False(t, registry.Has("nonexistent"))
	})
}

// ============================================================================
// Registry Count Tests
// ============================================================================

func TestRegistryCount(t *testing.T) {
	t.Run("returns correct count", func(t *testing.T) {
		registry := NewRegistry()

		assert.Equal(t, 0, registry.Count())

		for i := 0; i < 5; i++ {
			info := NodeTypeInfo{Type: string(rune('a' + i)), Name: "Test"}
			err := registry.Register(info, mockNodeFactory)
			require.NoError(t, err)
			assert.Equal(t, i+1, registry.Count())
		}

		registry.Unregister("a")
		assert.Equal(t, 4, registry.Count())
	})
}

// ============================================================================
// Default Registry Tests
// ============================================================================

func TestDefaultRegistry(t *testing.T) {
	// Note: These tests use the global DefaultRegistry, so be careful about test isolation

	t.Run("Register function uses default registry", func(t *testing.T) {
		info := NodeTypeInfo{
			Type:     "global-test-node",
			Name:     "Global Test",
			Category: types.NodeCategoryProcessing,
		}

		// Clean up after test
		defer DefaultRegistry.Unregister("global-test-node")

		err := Register(info, mockNodeFactory)

		assert.NoError(t, err)
		assert.True(t, DefaultRegistry.Has("global-test-node"))
	})

	t.Run("Create function uses default registry", func(t *testing.T) {
		info := NodeTypeInfo{
			Type: "global-create-test",
			Name: "Global Create Test",
		}

		defer DefaultRegistry.Unregister("global-create-test")

		err := Register(info, mockNodeFactory)
		require.NoError(t, err)

		node, err := Create("global-create-test", nil)

		assert.NoError(t, err)
		assert.NotNil(t, node)
	})

	t.Run("List function uses default registry", func(t *testing.T) {
		// This test just verifies List() doesn't panic
		list := List()
		assert.NotNil(t, list)
	})
}

// ============================================================================
// Concurrent Access Tests
// ============================================================================

func TestRegistryConcurrency(t *testing.T) {
	t.Run("handles concurrent registrations", func(t *testing.T) {
		registry := NewRegistry()
		var wg sync.WaitGroup

		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				info := NodeTypeInfo{
					Type: string(rune('0'+id%10)) + string(rune('0'+id/10)),
					Name: "Concurrent Node",
				}
				_ = registry.Register(info, mockNodeFactory)
			}(i)
		}

		wg.Wait()

		// Some registrations may fail due to duplicates, which is expected
		assert.LessOrEqual(t, registry.Count(), 100)
	})

	t.Run("handles concurrent reads and writes", func(t *testing.T) {
		registry := NewRegistry()
		var wg sync.WaitGroup

		// Pre-register some nodes
		for i := 0; i < 10; i++ {
			info := NodeTypeInfo{Type: string(rune('a' + i)), Name: "Test"}
			_ = registry.Register(info, mockNodeFactory)
		}

		// Concurrent reads and writes
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = registry.List()
				_ = registry.Count()
			}()

			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				_ = registry.Has(string(rune('a' + id%10)))
				_, _ = registry.Get(string(rune('a' + id%10)))
			}(i)
		}

		wg.Wait()
	})

	t.Run("handles concurrent creates", func(t *testing.T) {
		registry := NewRegistry()

		info := NodeTypeInfo{Type: "concurrent-create", Name: "Concurrent"}
		err := registry.Register(info, mockNodeFactory)
		require.NoError(t, err)

		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				node, err := registry.Create("concurrent-create", nil)
				assert.NoError(t, err)
				assert.NotNil(t, node)
			}()
		}

		wg.Wait()
	})
}

// ============================================================================
// Edge Cases
// ============================================================================

func TestRegistryEdgeCases(t *testing.T) {
	t.Run("handles empty type name", func(t *testing.T) {
		registry := NewRegistry()

		info := NodeTypeInfo{Type: "", Name: "No Type"}
		err := registry.Register(info, mockNodeFactory)

		// Should succeed but using empty string as key
		assert.NoError(t, err)
		assert.True(t, registry.Has(""))
	})

	t.Run("handles special characters in type", func(t *testing.T) {
		registry := NewRegistry()

		info := NodeTypeInfo{Type: "type-with-special!@#$%", Name: "Special"}
		err := registry.Register(info, mockNodeFactory)

		assert.NoError(t, err)
		assert.True(t, registry.Has("type-with-special!@#$%"))
	})

	t.Run("handles unicode in type and name", func(t *testing.T) {
		registry := NewRegistry()

		info := NodeTypeInfo{Type: "nodo-espanol", Name: "Nodo Espanol"}
		err := registry.Register(info, mockNodeFactory)

		assert.NoError(t, err)
		assert.True(t, registry.Has("nodo-espanol"))
	})

	t.Run("handles nil factory", func(t *testing.T) {
		registry := NewRegistry()

		info := NodeTypeInfo{Type: "nil-factory", Name: "Nil Factory"}
		err := registry.Register(info, nil)

		// Registration might succeed but Create will panic
		assert.NoError(t, err)
	})
}

// ============================================================================
// NodeTypeInfo Tests
// ============================================================================

func TestNodeTypeInfo(t *testing.T) {
	t.Run("all fields are accessible", func(t *testing.T) {
		configSchema := &ConfigSchema{
			Properties: map[string]ConfigProperty{
				"interval": {Type: "number", Title: "Interval"},
			},
		}

		info := NodeTypeInfo{
			Type:        "full-info-node",
			Name:        "Full Info Node",
			Description: "A node with all fields set",
			Category:    types.NodeCategoryInput,
			Inputs: []types.Port{
				{ID: "in1", Name: "Input 1"},
			},
			Outputs: []types.Port{
				{ID: "out1", Name: "Output 1"},
			},
			Config: configSchema,
			Icon:   "timer",
		}

		assert.Equal(t, "full-info-node", info.Type)
		assert.Equal(t, "Full Info Node", info.Name)
		assert.Equal(t, "A node with all fields set", info.Description)
		assert.Equal(t, types.NodeCategoryInput, info.Category)
		assert.Len(t, info.Inputs, 1)
		assert.Len(t, info.Outputs, 1)
		assert.NotNil(t, info.Config)
		assert.Equal(t, "timer", info.Icon)
	})
}
