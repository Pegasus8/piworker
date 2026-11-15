package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLLMForgetInfo(t *testing.T) {
	info := LLMForgetActionInfo()
	assert.Equal(t, "action-llm-forget", info.Type)
	assert.Equal(t, types.NodeCategoryOutput, info.Category)
	assert.Equal(t, node.RiskSafe, info.Risk)
	assert.Equal(t, "eraser", info.Icon)
	require.NotNil(t, info.Config)
	assert.Equal(t, []string{"memoryKey"}, info.Config.Required)
	assert.True(t, info.Config.Properties["memoryKey"].AgentVisible)
	n, err := NewLLMForgetAction(map[string]any{"memoryKey": "k"})
	require.NoError(t, err)
	in, out := n.Ports()
	assert.Equal(t, info.Inputs, in)
	assert.Equal(t, info.Outputs, out)
}

func TestLLMForgetRefusesBadSettings(t *testing.T) {
	_, err := NewLLMForgetAction(nil)
	assert.ErrorContains(t, err, "memoryKey is required")
	_, err = NewLLMForgetAction(map[string]any{"memoryKey": "  "})
	assert.ErrorContains(t, err, "memoryKey is required")
	_, err = NewLLMForgetAction(map[string]any{"memoryKey": 3})
	assert.ErrorContains(t, err, "memoryKey")
}

func TestLLMForgetClearsTheConversation(t *testing.T) {
	mem := llm.NewMemory()
	require.NoError(t, mem.Append("chat:42", 0, llm.Turn{Role: llm.RoleUser, Text: "hi"}, llm.Turn{Role: llm.RoleAssistant, Text: "hello"}))
	require.NoError(t, mem.Append("chat:7", 0, llm.Turn{Role: llm.RoleUser, Text: "other"}))
	ctx := llm.WithMemory(context.Background(), mem)

	n, err := NewLLMForgetAction(map[string]any{"memoryKey": "chat:{{payload.chat}}"})
	require.NoError(t, err)
	require.NoError(t, n.Validate())
	in := types.NewMessage(map[string]any{"chat": 42}, types.DataTypeObject)
	in.SourcePort = "true"
	outs, err := n.Process(ctx, in)
	require.NoError(t, err)
	require.Len(t, outs, 1)
	assert.Equal(t, "output", outs[0].SourcePort)
	assert.Equal(t, in.Payload, outs[0].Payload, "the message is passed on")
	assert.Empty(t, mem.Turns("chat:42"))
	assert.Len(t, mem.Turns("chat:7"), 1, "other conversations stay")

	// Forgetting a conversation that does not exist is fine.
	_, err = n.Process(ctx, types.NewMessage(map[string]any{"chat": 1}, types.DataTypeObject))
	assert.NoError(t, err)

	// A key that renders empty (or too long) fails.
	_, err = n.Process(ctx, types.NewMessage(map[string]any{}, types.DataTypeObject))
	require.NoError(t, err, "chat: is a key")
	n2, err := NewLLMForgetAction(map[string]any{"memoryKey": "{{payload.chat}}"})
	require.NoError(t, err)
	_, err = n2.Process(ctx, types.NewMessage(map[string]any{}, types.DataTypeObject))
	assert.ErrorContains(t, err, "memoryKey")
	_, err = n2.Process(ctx, types.NewMessage(map[string]any{"chat": strings.Repeat("x", 201)}, types.DataTypeObject))
	assert.ErrorContains(t, err, "memoryKey")
}
