package builtin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// LLMForgetAction clears a conversation the LLM nodes remember (their
// memoryKey) and passes the message on (docs/design/llm.md §3), e.g. on a
// chat's /reset command.
type LLMForgetAction struct {
	*node.BaseNode

	memoryKey string // template
}

// NewLLMForgetAction creates an LLMForgetAction from configuration.
func NewLLMForgetAction(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	key, err := textSetting(base.GetConfig(), "memoryKey")
	if err != nil {
		return nil, err
	}
	if key = strings.TrimSpace(key); key == "" {
		return nil, errors.New("memoryKey is required: the conversation to forget, e.g. telegram:{{payload.body.message.chat.id}}")
	}
	return &LLMForgetAction{BaseNode: base, memoryKey: key}, nil
}

// Process clears the conversation from the memory of its context
// (llm.MemoryFromContext) and passes the message on. A sandboxed run (a
// dry-run or a node test) has an empty memory of its own there, so it never
// changes the process-wide one.
func (f *LLMForgetAction) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	key, err := renderMemoryKey(ctx, f.memoryKey, msg)
	if err != nil {
		return nil, err
	}
	if err := llm.MemoryFromContext(ctx).Clear(key); err != nil {
		return nil, fmt.Errorf("cannot forget the conversation: %w", err)
	}
	out := msg.Clone()
	out.SourcePort = "output"
	return []*types.Message{out}, nil
}

// Ports returns the port definitions.
func (f *LLMForgetAction) Ports() (inputs []types.Port, outputs []types.Port) {
	return llmInputs, llmOutputs
}

// Validate checks the configuration (the constructor already did).
func (f *LLMForgetAction) Validate() error { return nil }

func llmForgetConfigSchema() node.ConfigSchema {
	schema := node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{"memoryKey": llmSchemaMemoryKey()},
		Required:   []string{"memoryKey"},
	}
	key := schema.Properties["memoryKey"]
	key.Description = "Conversation to forget: the memory key of the LLM node, e.g. telegram:{{payload.body.message.chat.id}} (supports {{templates}})"
	schema.Properties["memoryKey"] = key
	return schema
}

// GetConfigSchema returns the configuration schema for the UI.
func (f *LLMForgetAction) GetConfigSchema() node.ConfigSchema { return llmForgetConfigSchema() }

// LLMForgetActionInfo returns the node type info for registration.
func LLMForgetActionInfo() node.NodeTypeInfo {
	schema := llmForgetConfigSchema()
	return node.NodeTypeInfo{
		Type:        "action-llm-forget",
		Name:        "Forget Conversation",
		Description: "Clear a conversation the LLM nodes remember",
		Documentation: `## Forget Conversation

Clears a conversation that **LLM Prompt** or **AI Agent** nodes remember
(their **memoryKey**) and passes the message on. The next message of that
conversation starts afresh.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| memoryKey | string | - | Conversation to forget (templates; required) |

Use the same key template as the LLM node, so it renders to the same key.

## Example

A Telegram bot that starts over on /reset: a **Switch** on
` + "`payload.body.message.text == \"/reset\"`" + ` sends those messages here with

` + "```" + `
memoryKey: telegram:{{payload.body.message.chat.id}}
` + "```" + `

Conversations can also be viewed and cleared under **AI memory**.
`,
		Category: types.NodeCategoryOutput,
		Risk:     node.RiskSafe,
		Inputs:   llmInputs,
		Outputs:  llmOutputs,
		Config:   &schema,
		Icon:     "eraser",
	}
}

func init() {
	info := LLMForgetActionInfo()
	if err := node.Register(info, NewLLMForgetAction); err != nil {
		panic("failed to register llm forget action: " + err.Error())
	}
}
