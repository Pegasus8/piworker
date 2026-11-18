package flow

import (
	"context"
	"sync"
	"testing"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// llmSeen is what an llmSniff found on its context.
type llmSeen struct {
	conns     *llm.Store
	memory    *llm.Memory
	knowledge *llm.Knowledge
}

// llmSniff is a safe processing node that records the LLM stores of its
// context.
type llmSniff struct {
	*node.BaseNode
	mu   *sync.Mutex
	seen *[]llmSeen
}

func (n *llmSniff) Process(ctx context.Context, msg *Message) ([]*Message, error) {
	n.mu.Lock()
	*n.seen = append(*n.seen, llmSeen{llm.FromContext(ctx), llm.MemoryFromContext(ctx), llm.KnowledgeFromContext(ctx)})
	n.mu.Unlock()
	out := msg.Clone()
	out.SourcePort = "output"
	return []*Message{out}, nil
}

func (n *llmSniff) Ports() (inputs, outputs []Port) {
	return []Port{{ID: "input", DataType: DataTypeAny}}, []Port{{ID: "output", DataType: DataTypeAny}}
}

type llmSniffs struct {
	mu   sync.Mutex
	seen []llmSeen
}

func (s *llmSniffs) register(t *testing.T, reg *node.Registry) {
	t.Helper()
	require.NoError(t, reg.Register(node.NodeTypeInfo{Type: "llmsniff", Category: NodeCategoryProcessing, Risk: node.RiskSafe,
		Inputs: []Port{{ID: "input", DataType: DataTypeAny}}, Outputs: []Port{{ID: "output", DataType: DataTypeAny}}},
		func(cfg map[string]any) (node.Node, error) {
			return &llmSniff{BaseNode: node.NewBaseNode(cfg), mu: &s.mu, seen: &s.seen}, nil
		}))
}

// realLLMContext is a context carrying LLM stores with content, as a
// caller's could.
func realLLMContext(t *testing.T) (context.Context, llmSeen) {
	t.Helper()
	real := llmSeen{llm.NewStore(), llm.NewMemory(), llm.NewKnowledge()}
	require.NoError(t, real.conns.Set(llm.Connection{Name: "claude", Provider: llm.ProviderOpenAI}))
	require.NoError(t, real.memory.Append("chat:1", 10, llm.Turn{Role: llm.RoleUser, Text: "hi"}))
	require.NoError(t, real.knowledge.Add("manuals", llm.Document{ID: "a", Text: "x", Model: "m", Embedding: llm.Embedding{1}}))
	ctx := llm.WithKnowledge(llm.WithMemory(llm.WithStore(context.Background(), real.conns), real.memory), real.knowledge)
	return ctx, real
}

// assertIsolatedLLM checks that seen are empty stores, neither the
// process-wide ones nor real's.
func assertIsolatedLLM(t *testing.T, seen, real llmSeen) {
	t.Helper()
	assert.NotSame(t, llm.DefaultStore, seen.conns)
	assert.NotSame(t, real.conns, seen.conns)
	assert.Empty(t, seen.conns.Names())
	assert.NotSame(t, llm.DefaultMemory, seen.memory)
	assert.NotSame(t, real.memory, seen.memory)
	assert.Empty(t, seen.memory.List())
	assert.NotSame(t, llm.DefaultKnowledge, seen.knowledge)
	assert.NotSame(t, real.knowledge, seen.knowledge)
	assert.Empty(t, seen.knowledge.Collections())
}

// A dry-run's nodes get empty LLM stores: they never read or change the
// connections, conversations or knowledge base, whatever the caller's
// context carries (the process-wide ones otherwise).
func TestDryRunHasEmptyLLMStores(t *testing.T) {
	p := newSandboxProbe()
	reg := probeRegistry(t, p)
	sniffs := &llmSniffs{}
	sniffs.register(t, reg)
	ctx, real := realLLMContext(t)

	_, err := DryRun(ctx, probeFlow(nil, "llmsniff"), reg, DryRunInput{TriggerNodeID: "trig", Payload: 1})
	require.NoError(t, err)
	sniffs.mu.Lock()
	defer sniffs.mu.Unlock()
	require.Len(t, sniffs.seen, 1, "the node ran")
	assertIsolatedLLM(t, sniffs.seen[0], real)
}

// So does a node test.
func TestTestNodeHasEmptyLLMStores(t *testing.T) {
	reg := node.NewRegistry()
	sniffs := &llmSniffs{}
	sniffs.register(t, reg)
	ctx, real := realLLMContext(t)

	res, err := TestNode(ctx, reg, "llmsniff", nil, 1)
	require.NoError(t, err)
	require.Empty(t, res.Error)
	sniffs.mu.Lock()
	defer sniffs.mu.Unlock()
	require.Len(t, sniffs.seen, 1)
	assertIsolatedLLM(t, sniffs.seen[0], real)
}
