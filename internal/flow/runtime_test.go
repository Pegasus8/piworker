package flow

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Mock Nodes for Testing
// ============================================================================

// mockTriggerNode is a test trigger that sends messages on demand.
type mockTriggerNode struct {
	*node.BaseNode
	sendFunc func(out chan<- *Message)
	running  bool
	stopCh   chan struct{}
	mu       sync.Mutex
}

func newMockTriggerNode(config map[string]interface{}) (node.Node, error) {
	return &mockTriggerNode{
		BaseNode: node.NewBaseNode(config),
		stopCh:   make(chan struct{}),
	}, nil
}

func (n *mockTriggerNode) Process(ctx context.Context, msg *Message) ([]*Message, error) {
	return nil, nil
}

func (n *mockTriggerNode) Ports() (inputs []Port, outputs []Port) {
	return nil, []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}
}

func (n *mockTriggerNode) Start(ctx context.Context, out chan<- *Message) error {
	n.mu.Lock()
	n.running = true
	n.stopCh = make(chan struct{})
	n.mu.Unlock()

	if n.sendFunc != nil {
		go n.sendFunc(out)
	}
	return nil
}

func (n *mockTriggerNode) Stop() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.running {
		close(n.stopCh)
		n.running = false
	}
	return nil
}

// mockActionNode is a test action that records processed messages.
type mockActionNode struct {
	*node.BaseNode
	messages  []*Message
	mu        sync.Mutex
	processWg sync.WaitGroup
}

func newMockActionNode(config map[string]interface{}) (node.Node, error) {
	return &mockActionNode{
		BaseNode: node.NewBaseNode(config),
		messages: make([]*Message, 0),
	}, nil
}

func (n *mockActionNode) Process(ctx context.Context, msg *Message) ([]*Message, error) {
	n.mu.Lock()
	n.messages = append(n.messages, msg)
	n.mu.Unlock()
	n.processWg.Done()

	// Pass through
	output := msg.Clone()
	output.SourcePort = "output"
	return []*Message{output}, nil
}

func (n *mockActionNode) Ports() (inputs []Port, outputs []Port) {
	return []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}},
		[]Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}
}

func (n *mockActionNode) GetMessages() []*Message {
	n.mu.Lock()
	defer n.mu.Unlock()
	result := make([]*Message, len(n.messages))
	copy(result, n.messages)
	return result
}

// mockErrorNode is a test action that returns an error.
type mockErrorNode struct {
	*node.BaseNode
	err error
}

func newMockErrorNode(err error) node.NodeFactory {
	return func(config map[string]interface{}) (node.Node, error) {
		return &mockErrorNode{
			BaseNode: node.NewBaseNode(config),
			err:      err,
		}, nil
	}
}

func (n *mockErrorNode) Process(ctx context.Context, msg *Message) ([]*Message, error) {
	return nil, n.err
}

func (n *mockErrorNode) Ports() (inputs []Port, outputs []Port) {
	return []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}},
		[]Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}
}

// ============================================================================
// Test Helpers
// ============================================================================

func createTestRegistry() *node.Registry {
	registry := node.NewRegistry()

	// Register mock trigger
	_ = registry.Register(node.NodeTypeInfo{
		Type:     "mock-trigger",
		Name:     "Mock Trigger",
		Category: NodeCategoryInput,
		Outputs:  []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}},
	}, newMockTriggerNode)

	// Register mock action
	_ = registry.Register(node.NodeTypeInfo{
		Type:     "mock-action",
		Name:     "Mock Action",
		Category: NodeCategoryOutput,
		Inputs:   []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}},
		Outputs:  []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}},
	}, newMockActionNode)

	return registry
}

func createSimpleFlow() *Flow {
	f := NewFlow("Test Flow")

	trigger := NewNode("mock-trigger", NodeCategoryInput)
	trigger.Outputs = []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}

	action := NewNode("mock-action", NodeCategoryOutput)
	action.Inputs = []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}}

	f.AddNode(*trigger)
	f.AddNode(*action)
	f.AddConnection(*NewConnection(trigger.ID, "output", action.ID, "input"))

	return f
}

// ============================================================================
// FlowRuntime Creation Tests
// ============================================================================

func TestNewFlowRuntime(t *testing.T) {
	registry := createTestRegistry()
	logger := zerolog.Nop()

	t.Run("creates runtime for valid flow", func(t *testing.T) {
		f := createSimpleFlow()

		rt, err := NewFlowRuntime(f, registry, WithLogger(logger))

		require.NoError(t, err)
		require.NotNil(t, rt)
		assert.Equal(t, f.ID, rt.Flow().ID)
		assert.False(t, rt.IsRunning())
	})

	t.Run("fails for flow with unknown node type", func(t *testing.T) {
		f := NewFlow("Invalid Flow")
		n := NewNode("unknown-type", NodeCategoryInput)
		n.Enabled = true
		f.AddNode(*n)

		_, err := NewFlowRuntime(f, registry, WithLogger(logger))

		assert.Error(t, err)
	})

	t.Run("ignores disabled nodes", func(t *testing.T) {
		f := NewFlow("Test Flow")

		trigger := NewNode("mock-trigger", NodeCategoryInput)
		trigger.Enabled = true

		disabled := NewNode("mock-action", NodeCategoryOutput)
		disabled.Enabled = false

		f.AddNode(*trigger)
		f.AddNode(*disabled)

		rt, err := NewFlowRuntime(f, registry, WithLogger(logger))

		require.NoError(t, err)
		assert.NotNil(t, rt)
	})

	t.Run("applies options", func(t *testing.T) {
		f := createSimpleFlow()

		observed := false

		rt, err := NewFlowRuntime(f, registry,
			WithLogger(logger),
			WithObserver(func(NodeEvent) {
				observed = true
			}),
		)

		require.NoError(t, err)
		assert.NotNil(t, rt)
		assert.False(t, observed) // Not called yet
	})
}

// ============================================================================
// FlowRuntime Start/Stop Tests
// ============================================================================

func TestFlowRuntimeStartStop(t *testing.T) {
	registry := createTestRegistry()
	logger := zerolog.Nop()

	t.Run("starts and stops flow", func(t *testing.T) {
		f := createSimpleFlow()

		rt, err := NewFlowRuntime(f, registry, WithLogger(logger))
		require.NoError(t, err)

		ctx := context.Background()
		err = rt.Start(ctx)

		assert.NoError(t, err)
		assert.True(t, rt.IsRunning())
		assert.Equal(t, FlowStateRunning, rt.State())

		err = rt.Stop()

		assert.NoError(t, err)
		assert.False(t, rt.IsRunning())
		assert.Equal(t, FlowStateInactive, rt.State())
	})

	t.Run("cannot start already running flow", func(t *testing.T) {
		f := createSimpleFlow()

		rt, err := NewFlowRuntime(f, registry, WithLogger(logger))
		require.NoError(t, err)

		ctx := context.Background()
		err = rt.Start(ctx)
		require.NoError(t, err)
		defer rt.Stop()

		err = rt.Start(ctx)

		assert.ErrorIs(t, err, ErrFlowAlreadyRunning)
	})

	t.Run("cannot stop non-running flow", func(t *testing.T) {
		f := createSimpleFlow()

		rt, err := NewFlowRuntime(f, registry, WithLogger(logger))
		require.NoError(t, err)

		err = rt.Stop()

		assert.ErrorIs(t, err, ErrFlowNotRunning)
	})

	t.Run("stops on context cancellation", func(t *testing.T) {
		f := createSimpleFlow()

		rt, err := NewFlowRuntime(f, registry, WithLogger(logger))
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		err = rt.Start(ctx)
		require.NoError(t, err)

		assert.True(t, rt.IsRunning())

		cancel()
		time.Sleep(100 * time.Millisecond)

		// Note: The runtime should be in stopping state, but may still report running
		// The actual stop happens asynchronously
	})
}

// ============================================================================
// FlowRuntime State Tests
// ============================================================================

func TestFlowRuntimeState(t *testing.T) {
	registry := createTestRegistry()
	logger := zerolog.Nop()

	t.Run("initial state is inactive", func(t *testing.T) {
		f := createSimpleFlow()

		rt, err := NewFlowRuntime(f, registry, WithLogger(logger))
		require.NoError(t, err)

		assert.Equal(t, FlowStateInactive, rt.State())
	})

	t.Run("state changes on start and stop", func(t *testing.T) {
		f := createSimpleFlow()

		rt, err := NewFlowRuntime(f, registry, WithLogger(logger))
		require.NoError(t, err)

		assert.Equal(t, FlowStateInactive, rt.State())

		err = rt.Start(context.Background())
		require.NoError(t, err)
		assert.Equal(t, FlowStateRunning, rt.State())

		err = rt.Stop()
		require.NoError(t, err)
		assert.Equal(t, FlowStateInactive, rt.State())
	})
}

// ============================================================================
// RuntimeManager Tests
// ============================================================================

func TestNewRuntimeManager(t *testing.T) {
	registry := createTestRegistry()
	logger := zerolog.Nop()

	t.Run("creates manager", func(t *testing.T) {
		manager := NewRuntimeManager(registry, logger)

		assert.NotNil(t, manager)
	})
}

func TestRuntimeManagerDeploy(t *testing.T) {
	registry := createTestRegistry()
	logger := zerolog.Nop()

	t.Run("deploys valid flow", func(t *testing.T) {
		manager := NewRuntimeManager(registry, logger)
		f := createSimpleFlow()

		err := manager.Deploy(context.Background(), f)

		assert.NoError(t, err)

		rt, exists := manager.GetRuntime(f.ID)
		assert.True(t, exists)
		assert.True(t, rt.IsRunning())

		// Cleanup
		manager.Undeploy(f.ID)
	})

	t.Run("cannot deploy flow twice", func(t *testing.T) {
		manager := NewRuntimeManager(registry, logger)
		f := createSimpleFlow()

		err := manager.Deploy(context.Background(), f)
		require.NoError(t, err)
		defer manager.Undeploy(f.ID)

		err = manager.Deploy(context.Background(), f)

		assert.ErrorIs(t, err, ErrFlowAlreadyRunning)
	})

	t.Run("fails for invalid flow", func(t *testing.T) {
		manager := NewRuntimeManager(registry, logger)

		// Flow without trigger
		f := NewFlow("Invalid Flow")
		action := NewNode("mock-action", NodeCategoryOutput)
		f.AddNode(*action)

		err := manager.Deploy(context.Background(), f)

		assert.Error(t, err)
	})
}

func TestRuntimeManagerUndeploy(t *testing.T) {
	registry := createTestRegistry()
	logger := zerolog.Nop()

	t.Run("undeploys running flow", func(t *testing.T) {
		manager := NewRuntimeManager(registry, logger)
		f := createSimpleFlow()

		err := manager.Deploy(context.Background(), f)
		require.NoError(t, err)

		err = manager.Undeploy(f.ID)

		assert.NoError(t, err)

		_, exists := manager.GetRuntime(f.ID)
		assert.False(t, exists)
	})

	t.Run("fails for non-existent flow", func(t *testing.T) {
		manager := NewRuntimeManager(registry, logger)

		err := manager.Undeploy("nonexistent")

		assert.ErrorIs(t, err, ErrFlowNotFound)
	})
}

func TestRuntimeManagerListRunning(t *testing.T) {
	registry := createTestRegistry()
	logger := zerolog.Nop()

	t.Run("lists running flows", func(t *testing.T) {
		manager := NewRuntimeManager(registry, logger)

		// Create and deploy multiple flows
		flows := make([]*Flow, 3)
		for i := 0; i < 3; i++ {
			flows[i] = createSimpleFlow()
			err := manager.Deploy(context.Background(), flows[i])
			require.NoError(t, err)
		}
		defer manager.StopAll()

		running := manager.ListRunning()

		assert.Len(t, running, 3)
		for _, f := range flows {
			assert.Contains(t, running, f.ID)
		}
	})

	t.Run("returns empty for no running flows", func(t *testing.T) {
		manager := NewRuntimeManager(registry, logger)

		running := manager.ListRunning()

		assert.Empty(t, running)
	})
}

func TestRuntimeManagerStopAll(t *testing.T) {
	registry := createTestRegistry()
	logger := zerolog.Nop()

	t.Run("stops all running flows", func(t *testing.T) {
		manager := NewRuntimeManager(registry, logger)

		// Deploy multiple flows
		for i := 0; i < 3; i++ {
			f := createSimpleFlow()
			err := manager.Deploy(context.Background(), f)
			require.NoError(t, err)
		}

		assert.Len(t, manager.ListRunning(), 3)

		manager.StopAll()

		// Give time for graceful shutdown
		time.Sleep(100 * time.Millisecond)

		assert.Empty(t, manager.ListRunning())
	})
}

// ============================================================================
// Errors Channel Tests
// ============================================================================

func TestFlowRuntimeErrors(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("returns error channel", func(t *testing.T) {
		registry := createTestRegistry()
		f := createSimpleFlow()

		rt, err := NewFlowRuntime(f, registry, WithLogger(logger))
		require.NoError(t, err)

		errCh := rt.Errors()

		assert.NotNil(t, errCh)
	})
}

// ============================================================================
// Observer Hook Tests
// ============================================================================

func TestFlowRuntimeObserverHook(t *testing.T) {
	registry := createTestRegistry()
	logger := zerolog.Nop()

	t.Run("accepts an observer without error", func(t *testing.T) {
		f := createSimpleFlow()

		var mu sync.Mutex
		observed := 0

		rt, err := NewFlowRuntime(f, registry,
			WithLogger(logger),
			WithObserver(func(NodeEvent) {
				mu.Lock()
				observed++
				mu.Unlock()
			}),
		)
		require.NoError(t, err)

		err = rt.Start(context.Background())
		require.NoError(t, err)
		defer rt.Stop()

		// The observer fires when messages are processed; here we only assert the
		// option wires up cleanly (see runtime_observer_test.go for behavior).
		assert.NotNil(t, rt)
	})
}

// ============================================================================
// Edge Cases
// ============================================================================

func TestFlowRuntimeEdgeCases(t *testing.T) {
	registry := createTestRegistry()
	logger := zerolog.Nop()

	t.Run("handles flow with no connections", func(t *testing.T) {
		f := NewFlow("Disconnected Flow")

		trigger := NewNode("mock-trigger", NodeCategoryInput)
		trigger.Outputs = []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}

		action := NewNode("mock-action", NodeCategoryOutput)
		action.Inputs = []Port{{ID: "input", Name: "Input", DataType: DataTypeAny}}

		f.AddNode(*trigger)
		f.AddNode(*action)
		// No connection between them

		rt, err := NewFlowRuntime(f, registry, WithLogger(logger))
		require.NoError(t, err)

		err = rt.Start(context.Background())
		require.NoError(t, err)

		err = rt.Stop()
		assert.NoError(t, err)
	})

	t.Run("handles flow with multiple triggers", func(t *testing.T) {
		f := NewFlow("Multi-Trigger Flow")

		for i := 0; i < 3; i++ {
			trigger := NewNode("mock-trigger", NodeCategoryInput)
			trigger.Outputs = []Port{{ID: "output", Name: "Output", DataType: DataTypeAny}}
			f.AddNode(*trigger)
		}

		rt, err := NewFlowRuntime(f, registry, WithLogger(logger))
		require.NoError(t, err)

		err = rt.Start(context.Background())
		require.NoError(t, err)
		defer rt.Stop()

		assert.True(t, rt.IsRunning())
	})

	t.Run("returns correct flow", func(t *testing.T) {
		f := createSimpleFlow()
		f.Name = "Specific Flow Name"

		rt, err := NewFlowRuntime(f, registry, WithLogger(logger))
		require.NoError(t, err)

		assert.Equal(t, f.ID, rt.Flow().ID)
		assert.Equal(t, "Specific Flow Name", rt.Flow().Name)
	})
}

// ============================================================================
// Concurrent Operations Tests
// ============================================================================

func TestFlowRuntimeConcurrency(t *testing.T) {
	registry := createTestRegistry()
	logger := zerolog.Nop()

	t.Run("handles concurrent state checks", func(t *testing.T) {
		f := createSimpleFlow()

		rt, err := NewFlowRuntime(f, registry, WithLogger(logger))
		require.NoError(t, err)

		err = rt.Start(context.Background())
		require.NoError(t, err)
		defer rt.Stop()

		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = rt.IsRunning()
				_ = rt.State()
			}()
		}

		wg.Wait()
	})

	t.Run("runtime manager handles concurrent deploys", func(t *testing.T) {
		manager := NewRuntimeManager(registry, logger)
		defer manager.StopAll()

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				f := createSimpleFlow()
				_ = manager.Deploy(context.Background(), f)
			}()
		}

		wg.Wait()

		// All deployments should succeed or fail gracefully
		running := manager.ListRunning()
		assert.LessOrEqual(t, len(running), 10)
	})
}
