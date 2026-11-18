package storage

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Test Helpers
// ============================================================================

func createTestStore(t *testing.T) *SQLiteStore {
	// Use temp file for tests to avoid shared cache pollution between tests
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewSQLiteStore(dbPath)
	require.NoError(t, err)
	return store
}

func createTestFlow(name string) *flow.Flow {
	f := flow.NewFlow(name)
	f.Description = "Test description"

	trigger := flow.NewNode("trigger-interval", flow.NodeCategoryInput)
	trigger.Outputs = []flow.Port{{ID: "output", Name: "Output", DataType: flow.DataTypeAny}}
	f.AddNode(*trigger)

	return f
}

// ============================================================================
// Store Creation Tests
// ============================================================================

func TestNewSQLiteStore(t *testing.T) {
	t.Run("creates in-memory store", func(t *testing.T) {
		store, err := NewSQLiteStore(":memory:")
		require.NoError(t, err)
		defer store.Close()

		assert.NotNil(t, store)
		assert.False(t, store.IsClosed())
	})

	t.Run("creates file-based store", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		store, err := NewSQLiteStore(dbPath)
		require.NoError(t, err)
		defer store.Close()

		assert.NotNil(t, store)

		// Check file exists
		_, err = os.Stat(dbPath)
		assert.NoError(t, err)
	})

	t.Run("fails with invalid path", func(t *testing.T) {
		_, err := NewSQLiteStore("/nonexistent/directory/test.db")

		assert.Error(t, err)
	})
}

// ============================================================================
// CreateFlow Tests
// ============================================================================

func TestSQLiteStoreCreateFlow(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	t.Run("creates flow successfully", func(t *testing.T) {
		f := createTestFlow("Test Flow")

		err := store.CreateFlow(f)

		assert.NoError(t, err)
	})

	t.Run("fails for nil flow", func(t *testing.T) {
		err := store.CreateFlow(nil)

		assert.ErrorIs(t, err, ErrInvalidFlowData)
	})

	t.Run("fails for flow without ID", func(t *testing.T) {
		f := createTestFlow("No ID")
		f.ID = ""

		err := store.CreateFlow(f)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty flow ID")
	})

	t.Run("fails for duplicate flow", func(t *testing.T) {
		f := createTestFlow("Duplicate")

		err := store.CreateFlow(f)
		require.NoError(t, err)

		err = store.CreateFlow(f)

		assert.ErrorIs(t, err, ErrFlowAlreadyExists)
	})

	t.Run("fails on closed store", func(t *testing.T) {
		store := createTestStore(t)
		store.Close()

		f := createTestFlow("Closed Store")

		err := store.CreateFlow(f)

		assert.ErrorIs(t, err, ErrDatabaseClosed)
	})
}

// ============================================================================
// GetFlow Tests
// ============================================================================

func TestSQLiteStoreGetFlow(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	t.Run("retrieves existing flow", func(t *testing.T) {
		f := createTestFlow("Get Flow")
		err := store.CreateFlow(f)
		require.NoError(t, err)

		retrieved, err := store.GetFlow(f.ID)

		assert.NoError(t, err)
		assert.Equal(t, f.ID, retrieved.ID)
		assert.Equal(t, f.Name, retrieved.Name)
		assert.Equal(t, f.Description, retrieved.Description)
		assert.Len(t, retrieved.Nodes, len(f.Nodes))
	})

	t.Run("returns error for non-existent flow", func(t *testing.T) {
		_, err := store.GetFlow("nonexistent-id")

		assert.ErrorIs(t, err, ErrFlowNotFound)
	})

	t.Run("returns error for empty ID", func(t *testing.T) {
		_, err := store.GetFlow("")

		assert.Error(t, err)
	})

	t.Run("fails on closed store", func(t *testing.T) {
		store := createTestStore(t)
		f := createTestFlow("Closed Get")
		store.CreateFlow(f)
		store.Close()

		_, err := store.GetFlow(f.ID)

		assert.ErrorIs(t, err, ErrDatabaseClosed)
	})
}

// ============================================================================
// UpdateFlow Tests
// ============================================================================

func TestSQLiteStoreUpdateFlow(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	t.Run("updates existing flow", func(t *testing.T) {
		f := createTestFlow("Update Flow")
		err := store.CreateFlow(f)
		require.NoError(t, err)

		f.Name = "Updated Name"
		f.Description = "Updated Description"

		err = store.UpdateFlow(f)

		assert.NoError(t, err)

		retrieved, err := store.GetFlow(f.ID)
		require.NoError(t, err)
		assert.Equal(t, "Updated Name", retrieved.Name)
		assert.Equal(t, "Updated Description", retrieved.Description)
	})

	t.Run("fails for non-existent flow", func(t *testing.T) {
		f := createTestFlow("Non-existent")

		err := store.UpdateFlow(f)

		assert.ErrorIs(t, err, ErrFlowNotFound)
	})

	t.Run("fails for nil flow", func(t *testing.T) {
		err := store.UpdateFlow(nil)

		assert.ErrorIs(t, err, ErrInvalidFlowData)
	})

	t.Run("fails for flow without ID", func(t *testing.T) {
		f := createTestFlow("No ID")
		f.ID = ""

		err := store.UpdateFlow(f)

		assert.Error(t, err)
	})

	t.Run("preserves nodes and connections", func(t *testing.T) {
		f := createTestFlow("Preserve Data")

		action := flow.NewNode("action-log", flow.NodeCategoryOutput)
		action.Inputs = []flow.Port{{ID: "input", Name: "Input", DataType: flow.DataTypeAny}}
		f.AddNode(*action)

		trigger := f.Nodes[0]
		f.AddConnection(*flow.NewConnection(trigger.ID, "output", action.ID, "input"))

		err := store.CreateFlow(f)
		require.NoError(t, err)

		f.Name = "Updated"
		err = store.UpdateFlow(f)
		require.NoError(t, err)

		retrieved, err := store.GetFlow(f.ID)
		require.NoError(t, err)
		assert.Len(t, retrieved.Nodes, 2)
		assert.Len(t, retrieved.Connections, 1)
	})

	t.Run("fails on closed store", func(t *testing.T) {
		store := createTestStore(t)
		f := createTestFlow("Closed Update")
		store.CreateFlow(f)
		store.Close()

		f.Name = "Updated"
		err := store.UpdateFlow(f)

		assert.ErrorIs(t, err, ErrDatabaseClosed)
	})
}

// ============================================================================
// DeleteFlow Tests
// ============================================================================

func TestSQLiteStoreDeleteFlow(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	t.Run("deletes existing flow", func(t *testing.T) {
		f := createTestFlow("Delete Flow")
		err := store.CreateFlow(f)
		require.NoError(t, err)

		err = store.DeleteFlow(f.ID)

		assert.NoError(t, err)

		_, err = store.GetFlow(f.ID)
		assert.ErrorIs(t, err, ErrFlowNotFound)
	})

	t.Run("fails for non-existent flow", func(t *testing.T) {
		err := store.DeleteFlow("nonexistent-id")

		assert.ErrorIs(t, err, ErrFlowNotFound)
	})

	t.Run("fails for empty ID", func(t *testing.T) {
		err := store.DeleteFlow("")

		assert.Error(t, err)
	})

	t.Run("fails on closed store", func(t *testing.T) {
		store := createTestStore(t)
		f := createTestFlow("Closed Delete")
		store.CreateFlow(f)
		store.Close()

		err := store.DeleteFlow(f.ID)

		assert.ErrorIs(t, err, ErrDatabaseClosed)
	})
}

// ============================================================================
// ListFlows Tests
// ============================================================================

func TestSQLiteStoreListFlows(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	t.Run("returns empty list for empty store", func(t *testing.T) {
		flows, err := store.ListFlows(nil)

		assert.NoError(t, err)
		assert.Empty(t, flows)
	})

	t.Run("returns all flows", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			f := createTestFlow("Flow " + string(rune('A'+i)))
			err := store.CreateFlow(f)
			require.NoError(t, err)
		}

		flows, err := store.ListFlows(nil)

		assert.NoError(t, err)
		assert.Len(t, flows, 5)
	})

	t.Run("filters by state", func(t *testing.T) {
		store := createTestStore(t)
		defer store.Close()

		// Create flows with different states
		activeFlow := createTestFlow("Active Flow")
		activeFlow.State = flow.FlowStateActive
		store.CreateFlow(activeFlow)

		inactiveFlow := createTestFlow("Inactive Flow")
		inactiveFlow.State = flow.FlowStateInactive
		store.CreateFlow(inactiveFlow)

		runningFlow := createTestFlow("Running Flow")
		runningFlow.State = flow.FlowStateRunning
		store.CreateFlow(runningFlow)

		activeState := flow.FlowStateActive
		flows, err := store.ListFlows(&activeState)

		assert.NoError(t, err)
		assert.Len(t, flows, 1)
		assert.Equal(t, "Active Flow", flows[0].Name)
	})

	t.Run("returns flows in order", func(t *testing.T) {
		store := createTestStore(t)
		defer store.Close()

		names := []string{"First", "Second", "Third"}
		for _, name := range names {
			f := createTestFlow(name)
			err := store.CreateFlow(f)
			require.NoError(t, err)
			time.Sleep(10 * time.Millisecond) // Ensure different timestamps
		}

		flows, err := store.ListFlows(nil)

		assert.NoError(t, err)
		assert.Len(t, flows, 3)
		// Should be in reverse order (most recent first)
		assert.Equal(t, "Third", flows[0].Name)
	})

	t.Run("fails on closed store", func(t *testing.T) {
		store := createTestStore(t)
		store.Close()

		_, err := store.ListFlows(nil)

		assert.ErrorIs(t, err, ErrDatabaseClosed)
	})
}

// ============================================================================
// ListFlowsPaginated Tests
// ============================================================================

func TestSQLiteStoreListFlowsPaginated(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	// Create 10 flows
	for i := 0; i < 10; i++ {
		f := createTestFlow("Flow " + string(rune('0'+i)))
		err := store.CreateFlow(f)
		require.NoError(t, err)
		time.Sleep(5 * time.Millisecond)
	}

	t.Run("returns correct page size", func(t *testing.T) {
		flows, total, err := store.ListFlowsPaginated(0, 5)

		assert.NoError(t, err)
		assert.Len(t, flows, 5)
		assert.Equal(t, 10, total)
	})

	t.Run("returns correct offset", func(t *testing.T) {
		flows1, _, err := store.ListFlowsPaginated(0, 5)
		require.NoError(t, err)

		flows2, _, err := store.ListFlowsPaginated(5, 5)
		require.NoError(t, err)

		// Should be different flows
		assert.NotEqual(t, flows1[0].ID, flows2[0].ID)
		assert.Len(t, flows2, 5)
	})

	t.Run("handles offset beyond data", func(t *testing.T) {
		flows, total, err := store.ListFlowsPaginated(100, 5)

		assert.NoError(t, err)
		assert.Empty(t, flows)
		assert.Equal(t, 10, total)
	})

	t.Run("handles limit of zero", func(t *testing.T) {
		flows, total, err := store.ListFlowsPaginated(0, 0)

		assert.NoError(t, err)
		assert.Empty(t, flows)
		assert.Equal(t, 10, total)
	})
}

// ============================================================================
// SearchFlows Tests
// ============================================================================

func TestSQLiteStoreSearchFlows(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	// Create flows with different names
	names := []string{"Alpha Flow", "Beta Flow", "Alpha Beta", "Gamma"}
	for _, name := range names {
		f := createTestFlow(name)
		err := store.CreateFlow(f)
		require.NoError(t, err)
	}

	t.Run("finds flows by name pattern", func(t *testing.T) {
		flows, err := store.SearchFlows("Alpha")

		assert.NoError(t, err)
		assert.Len(t, flows, 2) // "Alpha Flow" and "Alpha Beta"
	})

	t.Run("finds flows with partial match", func(t *testing.T) {
		flows, err := store.SearchFlows("Flow")

		assert.NoError(t, err)
		assert.Len(t, flows, 2)
	})

	t.Run("returns empty for no match", func(t *testing.T) {
		flows, err := store.SearchFlows("Nonexistent")

		assert.NoError(t, err)
		assert.Empty(t, flows)
	})

	t.Run("handles empty pattern", func(t *testing.T) {
		flows, err := store.SearchFlows("")

		assert.NoError(t, err)
		assert.Len(t, flows, 4) // All flows
	})

	t.Run("case insensitive search", func(t *testing.T) {
		flows, err := store.SearchFlows("alpha")

		assert.NoError(t, err)
		// SQLite LIKE is case-insensitive for ASCII
		assert.GreaterOrEqual(t, len(flows), 1)
	})
}

// ============================================================================
// UpdateFlowState Tests
// ============================================================================

func TestSQLiteStoreUpdateFlowState(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	t.Run("updates state successfully", func(t *testing.T) {
		f := createTestFlow("State Flow")
		f.State = flow.FlowStateInactive
		err := store.CreateFlow(f)
		require.NoError(t, err)

		err = store.UpdateFlowState(f.ID, flow.FlowStateRunning)

		assert.NoError(t, err)

		retrieved, err := store.GetFlow(f.ID)
		require.NoError(t, err)
		assert.Equal(t, flow.FlowStateRunning, retrieved.State)
	})

	t.Run("fails for non-existent flow", func(t *testing.T) {
		err := store.UpdateFlowState("nonexistent", flow.FlowStateActive)

		assert.ErrorIs(t, err, ErrFlowNotFound)
	})
}

// ============================================================================
// Store Close Tests
// ============================================================================

func TestSQLiteStoreClose(t *testing.T) {
	t.Run("closes successfully", func(t *testing.T) {
		store := createTestStore(t)

		err := store.Close()

		assert.NoError(t, err)
		assert.True(t, store.IsClosed())
	})

	t.Run("double close is safe", func(t *testing.T) {
		store := createTestStore(t)

		err := store.Close()
		assert.NoError(t, err)

		err = store.Close()
		assert.NoError(t, err)
	})
}

// ============================================================================
// Concurrent Access Tests
// ============================================================================

func TestSQLiteStoreConcurrency(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	t.Run("handles concurrent creates", func(t *testing.T) {
		var wg sync.WaitGroup

		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				f := createTestFlow("Concurrent " + string(rune('0'+id%10)))
				_ = store.CreateFlow(f)
			}(i)
		}

		wg.Wait()
	})

	t.Run("handles concurrent reads", func(t *testing.T) {
		// Create some flows first
		for i := 0; i < 10; i++ {
			f := createTestFlow("Read " + string(rune('0'+i)))
			store.CreateFlow(f)
		}

		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = store.ListFlows(nil)
			}()
		}

		wg.Wait()
	})

	t.Run("handles concurrent reads and writes", func(t *testing.T) {
		store := createTestStore(t)
		defer store.Close()

		var wg sync.WaitGroup

		// Writers
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				f := createTestFlow("RW " + string(rune('0'+id%10)))
				_ = store.CreateFlow(f)
			}(i)
		}

		// Readers
		for i := 0; i < 30; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = store.ListFlows(nil)
			}()
		}

		wg.Wait()
	})
}

// ============================================================================
// Data Integrity Tests
// ============================================================================

func TestSQLiteStoreDataIntegrity(t *testing.T) {
	t.Run("preserves complex flow data", func(t *testing.T) {
		store := createTestStore(t)
		defer store.Close()

		f := createTestFlow("Complex Flow")
		f.Description = "A complex flow with multiple nodes"
		f.Variables = map[string]interface{}{
			"var1": "value1",
			"var2": 42,
		}

		// Add multiple nodes
		for i := 0; i < 5; i++ {
			n := flow.NewNode("action-log", flow.NodeCategoryOutput)
			n.Config["setting"] = i
			f.AddNode(*n)
		}

		// Add connections
		for i := 1; i < len(f.Nodes); i++ {
			conn := flow.NewConnection(f.Nodes[i-1].ID, "output", f.Nodes[i].ID, "input")
			f.AddConnection(*conn)
		}

		err := store.CreateFlow(f)
		require.NoError(t, err)

		retrieved, err := store.GetFlow(f.ID)
		require.NoError(t, err)

		assert.Equal(t, f.ID, retrieved.ID)
		assert.Equal(t, f.Name, retrieved.Name)
		assert.Equal(t, f.Description, retrieved.Description)
		assert.Len(t, retrieved.Nodes, len(f.Nodes))
		assert.Len(t, retrieved.Connections, len(f.Connections))
	})

	t.Run("handles special characters in name", func(t *testing.T) {
		store := createTestStore(t)
		defer store.Close()

		f := createTestFlow("Flow with 'quotes' and \"double quotes\" and <brackets>")

		err := store.CreateFlow(f)
		require.NoError(t, err)

		retrieved, err := store.GetFlow(f.ID)
		require.NoError(t, err)

		assert.Equal(t, f.Name, retrieved.Name)
	})

	t.Run("handles unicode characters", func(t *testing.T) {
		store := createTestStore(t)
		defer store.Close()

		f := createTestFlow("Flujo Automatizado")

		err := store.CreateFlow(f)
		require.NoError(t, err)

		retrieved, err := store.GetFlow(f.ID)
		require.NoError(t, err)

		assert.Equal(t, f.Name, retrieved.Name)
	})

	t.Run("handles empty description", func(t *testing.T) {
		store := createTestStore(t)
		defer store.Close()

		f := createTestFlow("No Description")
		f.Description = ""

		err := store.CreateFlow(f)
		require.NoError(t, err)

		retrieved, err := store.GetFlow(f.ID)
		require.NoError(t, err)

		assert.Empty(t, retrieved.Description)
	})
}

// ============================================================================
// Persistence Tests
// ============================================================================

func TestSQLiteStorePersistence(t *testing.T) {
	t.Run("data persists across store instances", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "persist.db")

		// Create store and add data
		store1, err := NewSQLiteStore(dbPath)
		require.NoError(t, err)

		f := createTestFlow("Persistent Flow")
		err = store1.CreateFlow(f)
		require.NoError(t, err)

		store1.Close()

		// Reopen store
		store2, err := NewSQLiteStore(dbPath)
		require.NoError(t, err)
		defer store2.Close()

		// Data should still be there
		retrieved, err := store2.GetFlow(f.ID)
		require.NoError(t, err)
		assert.Equal(t, f.Name, retrieved.Name)
	})
}

// ============================================================================
// Edge Cases
// ============================================================================

func TestSQLiteStoreEdgeCases(t *testing.T) {
	store := createTestStore(t)
	defer store.Close()

	t.Run("handles very long flow name", func(t *testing.T) {
		longName := ""
		for i := 0; i < 1000; i++ {
			longName += "a"
		}

		f := createTestFlow(longName)
		err := store.CreateFlow(f)
		require.NoError(t, err)

		retrieved, err := store.GetFlow(f.ID)
		require.NoError(t, err)
		assert.Equal(t, longName, retrieved.Name)
	})

	t.Run("handles flow with many nodes", func(t *testing.T) {
		f := createTestFlow("Many Nodes")

		for i := 0; i < 100; i++ {
			n := flow.NewNode("action-log", flow.NodeCategoryOutput)
			f.AddNode(*n)
		}

		err := store.CreateFlow(f)
		require.NoError(t, err)

		retrieved, err := store.GetFlow(f.ID)
		require.NoError(t, err)
		assert.Len(t, retrieved.Nodes, 101) // 100 + 1 trigger
	})

	t.Run("handles flow with nil variables", func(t *testing.T) {
		f := createTestFlow("Nil Variables")
		f.Variables = nil

		err := store.CreateFlow(f)
		require.NoError(t, err)

		retrieved, err := store.GetFlow(f.ID)
		require.NoError(t, err)
		// Variables might be nil or empty map
		assert.True(t, retrieved.Variables == nil || len(retrieved.Variables) == 0)
	})
}

// ============================================================================
// Error Handling Tests
// ============================================================================

func TestSQLiteStoreErrors(t *testing.T) {
	t.Run("ErrFlowNotFound on get", func(t *testing.T) {
		store := createTestStore(t)
		defer store.Close()

		_, err := store.GetFlow("nonexistent")

		assert.ErrorIs(t, err, ErrFlowNotFound)
	})

	t.Run("ErrFlowNotFound on delete", func(t *testing.T) {
		store := createTestStore(t)
		defer store.Close()

		err := store.DeleteFlow("nonexistent")

		assert.ErrorIs(t, err, ErrFlowNotFound)
	})

	t.Run("ErrFlowNotFound on update", func(t *testing.T) {
		store := createTestStore(t)
		defer store.Close()

		f := createTestFlow("Nonexistent")
		err := store.UpdateFlow(f)

		assert.ErrorIs(t, err, ErrFlowNotFound)
	})

	t.Run("ErrFlowAlreadyExists on duplicate create", func(t *testing.T) {
		store := createTestStore(t)
		defer store.Close()

		f := createTestFlow("Duplicate")
		store.CreateFlow(f)

		err := store.CreateFlow(f)

		assert.ErrorIs(t, err, ErrFlowAlreadyExists)
	})

	t.Run("ErrDatabaseClosed on operations after close", func(t *testing.T) {
		store := createTestStore(t)
		f := createTestFlow("Test")
		store.CreateFlow(f)
		store.Close()

		_, err := store.GetFlow(f.ID)
		assert.ErrorIs(t, err, ErrDatabaseClosed)

		err = store.CreateFlow(createTestFlow("New"))
		assert.ErrorIs(t, err, ErrDatabaseClosed)

		err = store.UpdateFlow(f)
		assert.ErrorIs(t, err, ErrDatabaseClosed)

		err = store.DeleteFlow(f.ID)
		assert.ErrorIs(t, err, ErrDatabaseClosed)

		_, err = store.ListFlows(nil)
		assert.ErrorIs(t, err, ErrDatabaseClosed)
	})
}
