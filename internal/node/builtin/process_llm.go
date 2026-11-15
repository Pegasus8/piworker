package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// LLMProcess sends a prompt built from the message to a model and passes on
// its answer: text, or JSON (optionally matching a schema). It can send
// images from the message and remember a conversation per key
// (docs/design/llm.md §3).
type LLMProcess struct {
	*node.BaseNode
	llmSettings

	system      string // template
	prompt      string // template
	images      string // message path ("" = none)
	output      string // "text" or "json"
	schema      map[string]any
	maxTokens   int
	effort      string
	memory      llmMemory
	resultField string
}

const (
	llmProcessDefaultMaxTokens = 1024
	// maxLLMImageBytes bounds the images of one request together (decoded).
	maxLLMImageBytes = 20 << 20
)

// NewLLMProcess creates an LLMProcess from configuration.
func NewLLMProcess(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	cfg := base.GetConfig()
	settings, err := parseLLMSettings(base, llmDefaultTimeout)
	if err != nil {
		return nil, err
	}
	p := &LLMProcess{BaseNode: base, llmSettings: settings}
	if p.system, err = textSetting(cfg, "system"); err != nil {
		return nil, err
	}
	if p.prompt, err = templateSetting(cfg, "prompt", "{{payload}}"); err != nil {
		return nil, err
	}
	if p.images, err = textSetting(cfg, "images"); err != nil {
		return nil, err
	}
	if p.images = strings.TrimSpace(p.images); p.images != "" {
		if err := checkMessagePath("images", p.images); err != nil {
			return nil, err
		}
	}
	if p.output, err = textSetting(cfg, "output"); err != nil {
		return nil, err
	}
	switch p.output {
	case "":
		p.output = "text"
	case "text", "json":
	default:
		return nil, errors.New("output must be text or json")
	}
	raw, err := jsonObjectSetting(cfg, "schema")
	if err != nil {
		return nil, err
	}
	if raw != nil {
		if err := json.Unmarshal(raw, &p.schema); err != nil {
			return nil, errors.New("schema must be a JSON object (a JSON Schema)")
		}
	}
	if len(p.schema) == 0 {
		p.schema = nil // {} is what a cleared editor field may hold
	} else {
		if p.output != "json" {
			return nil, errors.New("schema needs output json (the schema describes a JSON answer)")
		}
		if _, err := llm.PrepareSchema(p.schema); err != nil {
			return nil, fmt.Errorf("schema: %w", err)
		}
	}
	if p.maxTokens, err = parseLLMMaxTokens(base, llmProcessDefaultMaxTokens); err != nil {
		return nil, err
	}
	if p.effort, err = parseLLMEffort(base); err != nil {
		return nil, err
	}
	if p.memory, err = parseLLMMemory(base); err != nil {
		return nil, err
	}
	if p.resultField, err = parseResultField(base); err != nil {
		return nil, err
	}
	return p, nil
}

// Process asks the model and passes the message on with its answer.
func (p *LLMProcess) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	var out *types.Message
	err := p.run(ctx, func(ctx context.Context) error {
		s, err := p.open(ctx, "")
		if err != nil {
			return err
		}
		conv, history, err := p.memory.conversation(ctx, msg)
		if err != nil {
			return err
		}
		req, err := p.request(ctx, msg, s.model, history)
		if err != nil {
			return err
		}
		resp, err := s.client.Chat(ctx, req)
		if err != nil {
			return err
		}
		p.memory.remember(ctx, conv, req.Messages[len(req.Messages)-1].Text, resp.Text)

		out = msg.Clone()
		var result any = resp.Text
		if p.output == "json" {
			result = resp.JSON
		}
		llm.SetResult(out, p.resultField, result)
		out.SetMeta("llm", llmMeta(s.conn.Name, resp.Model, resp.Usage, resp.Stop))
		out.SourcePort = "output"
		return nil
	})
	if err != nil {
		return nil, err
	}
	return []*types.Message{out}, nil
}

// request builds the chat request for msg: the rendered system prompt, the
// conversation's history, then the rendered prompt with the message's
// images.
func (p *LLMProcess) request(ctx context.Context, msg *types.Message, model string, history []llm.Message) (llm.Request, error) {
	images, err := p.loadImages(ctx, msg)
	if err != nil {
		return llm.Request{}, err
	}
	prompt := renderTemplate(p.prompt, msg, ctx)
	if strings.TrimSpace(prompt) == "" && len(images) == 0 {
		return llm.Request{}, errors.New("the prompt is empty for this message")
	}
	messages := append(append([]llm.Message(nil), history...), llm.Message{Role: llm.RoleUser, Text: prompt, Images: images})
	req := llm.Request{
		Model:     model,
		System:    strings.TrimSpace(renderTemplate(p.system, msg, ctx)),
		Messages:  messages,
		MaxTokens: p.maxTokens,
		Effort:    p.effort,
		// A conversation sends the same prefix again next time.
		Cache: p.memory.enabled(),
	}
	if p.output == "json" {
		if p.schema != nil {
			req.Schema = p.schema
		} else {
			req.JSON = true
		}
	}
	return req, nil
}

// loadImages reads the images at the node's message path: none when the
// path is not set or names nothing in this message.
func (p *LLMProcess) loadImages(ctx context.Context, msg *types.Message) ([]llm.Media, error) {
	if p.images == "" {
		return nil, nil
	}
	v, ok := llm.MessageValue(msg, p.images)
	if !ok || v == nil {
		return nil, nil
	}
	media, err := llm.LoadMedia(ctx, v, maxLLMImageBytes, true)
	if err != nil {
		return nil, fmt.Errorf("images (%s): %w", p.images, err)
	}
	for i, m := range media {
		if m.URL == "" && !strings.HasPrefix(m.MediaType, "image/") {
			return nil, fmt.Errorf("images (%s): item %d is not an image (%s)", p.images, i+1, m.MediaType)
		}
	}
	return media, nil
}

// Ports returns the port definitions.
func (p *LLMProcess) Ports() (inputs []types.Port, outputs []types.Port) {
	return llmInputs, llmOutputs
}

// Validate checks the configuration (the constructor already did).
func (p *LLMProcess) Validate() error { return nil }

func llmProcessConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"connection": llmSchemaConnection(),
			"model":      llmSchemaModel("Model to ask (empty: the connection's default model)"),
			"system": {Type: "string", Title: "System prompt",
				Description: "Instructions for the model: its role, tone and rules (supports {{templates}})"},
			"prompt": {Type: "string", Title: "Prompt",
				Description: "What to ask, built from the message (supports {{templates}})", Default: "{{payload}}"},
			"images": {Type: "string", Title: "Images",
				Description:  "Message path of the images to send, e.g. payload.image: base64, a data URL, an http(s) URL or a list of them (empty: none)",
				AgentVisible: true},
			"output": {Type: "string", Title: "Output",
				Description: "text: the answer as text; json: the answer parsed as a JSON object", Default: "text",
				Enum: []string{"text", "json"}, AgentVisible: true},
			"schema": {Type: "json", Title: "JSON schema",
				Description: "Optional JSON Schema the answer must match (output json), e.g. {\"type\": \"object\", \"properties\": {...}}"},
			"maxTokens":   llmSchemaMaxTokens(llmProcessDefaultMaxTokens),
			"effort":      llmSchemaEffort(),
			"memoryKey":   llmSchemaMemoryKey(),
			"memoryTurns": llmSchemaMemoryTurns(),
			"resultField": llmSchemaResultField(),
			"timeout":     llmSchemaTimeout(llmDefaultTimeout),
		},
		Required: []string{"connection", "prompt"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (p *LLMProcess) GetConfigSchema() node.ConfigSchema { return llmProcessConfigSchema() }

// LLMProcessInfo returns the node type info for registration.
func LLMProcessInfo() node.NodeTypeInfo {
	schema := llmProcessConfigSchema()
	return node.NodeTypeInfo{
		Type:        "process-llm",
		Name:        "LLM Prompt",
		Description: "Ask a language model and pass on its answer (text or JSON)",
		Documentation: `## LLM Prompt

Sends a prompt built from the message to a language model (Claude, OpenAI or
any OpenAI-compatible server such as Ollama or LM Studio) and passes the
message on with the answer. Define the model's provider, endpoint and API key
once under **LLM connections** and pick the connection here.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| connection | string | - | LLM connection (required) |
| model | string | connection's | Model to ask |
| system | string | - | System prompt: the model's role and rules (templates) |
| prompt | string | {{payload}} | What to ask (templates) |
| images | string | - | Message path of images to send (e.g. ` + "`payload.image`" + `) |
| output | enum | text | ` + "`text`" + ` or ` + "`json`" + ` |
| schema | json | - | JSON Schema the answer must match (output json) |
| maxTokens | integer | 1024 | Longest answer, in tokens (1-64000) |
| effort | enum | - | low, medium, high, xhigh or max, only for models that support it (others may reject the request) |
| memoryKey | string | - | Conversation to remember (templates; empty: none) |
| memoryTurns | integer | 20 | Past messages kept and sent, in messages (2-200) |
| resultField | string | - | Payload field for the answer (empty: replace the payload) |
| timeout | integer | 120 | Longest time for the call, in seconds (1-600) |

## Output

The message with the answer: the text, or the parsed JSON (` + "`output: json`" + `),
as the payload or in ` + "`resultField`" + `. **meta.llm** holds the connection, the
model that answered, ` + "`inputTokens`" + `, ` + "`outputTokens`" + `, ` + "`requests`" + ` and ` + "`stop`" + `
(` + "`end`" + `, or ` + "`max_tokens`" + ` when the answer was cut).

## Images

Set **images** to a message path holding a base64 image, a data URL, an
http(s) URL (the provider fetches it) or a list of them: PNG, JPEG, GIF or
WebP, at most 20 MiB together. A message without the path sends no image.

## Memory

With a **memoryKey** such as ` + "`telegram:{{payload.message.chat.id}}`" + `, the
node remembers each conversation: the last **memoryTurns** messages are sent
before the prompt, and the prompt and answer are added after a successful
call. Clear one with **Forget Conversation**; see and clear them all under
**AI memory**.

## Examples

Summarize an e-mail into a field:

` + "```" + `
prompt: Summarize in one sentence: {{payload.body}}
resultField: summary
` + "```" + `

Extract data as JSON:

` + "```" + `
output: json
prompt: Extract the amount and due date from: {{payload.text}}
schema: {"type":"object","properties":{"amount":{"type":"number"},"due":{"type":"string"}},"required":["amount","due"]}
` + "```" + `

## Errors

A refused request, a spent daily token limit, an answer that is not the JSON
asked for or does not match the schema, and a timeout fail the message.
Rate limits, overloaded providers and network errors are transient: the
flow's retries apply to them.

## Privacy

The prompt, the images and the remembered conversation are sent to the
connection's provider. With a cloud provider they leave the device; a local
model (Ollama, LM Studio) keeps them on your network. Text that comes from
outside (webhooks, chats, e-mail) may try to instruct the model: treat the
answer as data.
`,
		Category: types.NodeCategoryProcessing,
		Risk:     node.RiskExternal,
		Inputs:   llmInputs,
		Outputs:  llmOutputs,
		Config:   &schema,
		Icon:     "sparkles",
	}
}

func init() {
	info := LLMProcessInfo()
	if err := node.Register(info, NewLLMProcess); err != nil {
		panic("failed to register llm process: " + err.Error())
	}
}
