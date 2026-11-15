package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"github.com/Pegasus8/piworker/internal/node"
)

// anthropicClient talks to the Claude API through the official Go SDK.
type anthropicClient struct {
	key string
	api anthropic.Client
}

func newAnthropicClient(c Connection, key string, hc *http.Client) *anthropicClient {
	api := anthropic.NewClient(
		// The connection is the only source of credentials and endpoint:
		// never ANTHROPIC_API_KEY, ANTHROPIC_BASE_URL, profiles or custom
		// headers from PiWorker's own environment.
		option.WithoutEnvironmentDefaults(),
		option.WithAPIKey(key),
		option.WithBaseURL(c.Endpoint()),
		// Retries are the flow runtime's, opt-in (node.Transient).
		option.WithMaxRetries(0),
		option.WithHTTPClient(hc),
	)
	return &anthropicClient{key: key, api: api}
}

// limitBody caps the answers' bodies (the SDK reads them itself).
func limitBody(n int64) option.RequestOption {
	return option.WithMiddleware(func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		resp, err := next(req)
		capBody(resp, n)
		return resp, err
	})
}

// params builds the Messages API request.
func (c *anthropicClient) params(req Request) (anthropic.MessageNewParams, error) {
	p := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: int64(req.MaxTokens),
	}
	if req.System != "" {
		p.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	for _, m := range req.Messages {
		param, ok, err := messageParam(m)
		if err != nil {
			return p, err
		}
		if ok {
			p.Messages = append(p.Messages, param)
		}
	}
	for _, t := range req.Tools {
		tool, err := toolParam(t)
		if err != nil {
			return p, err
		}
		p.Tools = append(p.Tools, anthropic.ToolUnionParam{OfTool: &tool})
	}
	if req.Schema != nil {
		p.OutputConfig.Format = anthropic.JSONOutputFormatParam{Schema: req.Schema}
	}
	if req.Effort != "" {
		p.OutputConfig.Effort = anthropic.OutputConfigEffort(req.Effort)
	}
	if req.Cache {
		p.CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
	return p, nil
}

// messageParam maps one message. An assistant message with Native (the
// SDK's own form of an answer) is replayed unchanged, so the thinking blocks
// a tool loop must send back survive. A message with no content is left out.
func messageParam(m Message) (anthropic.MessageParam, bool, error) {
	switch m.Role {
	case RoleAssistant:
		if native, ok := m.Native.(anthropic.MessageParam); ok {
			return native, true, nil
		}
		var blocks []anthropic.ContentBlockParamUnion
		if m.Text != "" {
			blocks = append(blocks, anthropic.NewTextBlock(m.Text))
		}
		for _, tc := range m.ToolCalls {
			var input any = map[string]any{}
			if len(tc.Input) > 0 {
				input = json.RawMessage(tc.Input)
			}
			blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, input, tc.Name))
		}
		if len(blocks) == 0 {
			return anthropic.MessageParam{}, false, nil
		}
		return anthropic.NewAssistantMessage(blocks...), true, nil
	case RoleUser:
		var blocks []anthropic.ContentBlockParamUnion
		for _, r := range m.ToolResults {
			blocks = append(blocks, anthropic.NewToolResultBlock(r.CallID, r.Content, r.IsError))
		}
		// Images go before their text, as Claude reads them best.
		for _, img := range m.Images {
			if img.URL != "" {
				blocks = append(blocks, anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: img.URL}))
				continue
			}
			blocks = append(blocks, anthropic.NewImageBlockBase64(img.MediaType, base64.StdEncoding.EncodeToString(img.Data)))
		}
		if m.Text != "" {
			blocks = append(blocks, anthropic.NewTextBlock(m.Text))
		}
		if len(blocks) == 0 {
			return anthropic.MessageParam{}, false, nil
		}
		return anthropic.NewUserMessage(blocks...), true, nil
	}
	return anthropic.MessageParam{}, false, fmt.Errorf("invalid message role %q", m.Role)
}

// toolParam maps a tool: its schema's properties and required go to their
// fields, every other keyword (additionalProperties, $defs, …) as is.
func toolParam(t Tool) (anthropic.ToolParam, error) {
	schema := map[string]any{}
	if len(t.InputSchema) > 0 {
		if err := json.Unmarshal(t.InputSchema, &schema); err != nil {
			return anthropic.ToolParam{}, fmt.Errorf("tool %q: the input schema is not a JSON object", t.Name)
		}
	}
	in := anthropic.ToolInputSchemaParam{ExtraFields: map[string]any{}}
	for k, v := range schema {
		switch k {
		case "type":
		case "properties":
			in.Properties = v
		case "required":
			list, _ := v.([]any)
			for _, item := range list {
				if s, ok := item.(string); ok {
					in.Required = append(in.Required, s)
				}
			}
		default:
			in.ExtraFields[k] = v
		}
	}
	tool := anthropic.ToolParam{Name: t.Name, InputSchema: in}
	if t.Description != "" {
		tool.Description = anthropic.String(t.Description)
	}
	return tool, nil
}

func (c *anthropicClient) chat(ctx context.Context, req Request) (*Response, Usage, error) {
	params, err := c.params(req)
	if err != nil {
		return nil, Usage{}, err
	}
	// Streaming: large max_tokens values would otherwise hit HTTP timeouts.
	stream := c.api.Messages.NewStreaming(ctx, params, limitBody(maxChatBody))
	defer func() { _ = stream.Close() }()
	var msg anthropic.Message
	started, stopped := false, false
	var streamErr error
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "message_start":
			started = true
		case "message_stop":
			stopped = true
		}
		if err := msg.Accumulate(event); err != nil {
			streamErr = fmt.Errorf("invalid answer stream: %s", quote(err.Error()))
			break
		}
	}
	// What the answer spent counts as soon as it started, also when it
	// then fails.
	usage := anthropicUsage(msg.Usage)
	if started {
		usage.Requests = 1
	}
	if streamErr != nil {
		return nil, usage, streamErr
	}
	if err := stream.Err(); err != nil {
		return nil, usage, c.classify(ctx, err)
	}
	if !started {
		// Not the Messages API (a wrong base URL): retrying cannot help.
		return nil, usage, errors.New("the LLM provider did not answer with a message stream (is the base URL an Anthropic API endpoint?)")
	}
	if !stopped {
		// The connection dropped mid-answer: the answer is incomplete.
		return nil, usage, node.Transient(errors.New("the LLM provider's answer stream ended early"))
	}
	if msg.StopReason == anthropic.StopReasonRefusal {
		return nil, usage, refusalError(msg.StopDetails)
	}

	resp := &Response{Model: string(msg.Model), Usage: usage}
	if resp.Model == "" {
		resp.Model = req.Model
	}
	var text strings.Builder
	for _, block := range msg.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			text.WriteString(b.Text)
		case anthropic.ToolUseBlock:
			input := json.RawMessage(b.Input)
			if len(strings.TrimSpace(string(input))) == 0 {
				input = json.RawMessage(`{}`)
			}
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{ID: b.ID, Name: b.Name, Input: input})
		}
	}
	resp.Text = text.String()
	switch msg.StopReason {
	case anthropic.StopReasonEndTurn, anthropic.StopReasonStopSequence:
		resp.Stop = StopEnd
	case anthropic.StopReasonMaxTokens, anthropic.StopReasonModelContextWindowExceeded:
		resp.Stop = StopMaxTokens
	case anthropic.StopReasonToolUse:
		resp.Stop = StopToolUse
	default:
		resp.Stop = StopOther
	}
	resp.Message = Message{Role: RoleAssistant, Text: resp.Text, ToolCalls: resp.ToolCalls, Native: msg.ToParam()}
	return resp, usage, nil
}

// anthropicUsage counts every input token the model read (cache writes and
// reads included) and the output tokens.
func anthropicUsage(u anthropic.Usage) Usage {
	return Usage{
		InputTokens:  u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens,
		OutputTokens: u.OutputTokens,
	}
}

// refusalError is the error of a refusal stop reason, with its category and
// explanation when the API gives them.
func refusalError(d anthropic.RefusalStopDetails) error {
	var parts []string
	if d.Category != "" {
		parts = append(parts, string(d.Category))
	}
	if d.Explanation != "" {
		parts = append(parts, quote(d.Explanation))
	}
	if len(parts) == 0 {
		return ErrRefused
	}
	return fmt.Errorf("%w (%s)", ErrRefused, strings.Join(parts, ": "))
}

// errorTypeStatus maps the error types of an error event sent in the middle
// of a stream (the HTTP status was 200) to the status they stand for.
var errorTypeStatus = map[string]int{
	"rate_limit_error": 429,
	"overloaded_error": 529,
	"api_error":        500,
	"timeout_error":    504,
}

// classify turns an SDK error into the package's errors: *anthropic.Error
// into *APIError (its message, never the SDK's text, which quotes the
// request), anything else through requestError.
func (c *anthropicClient) classify(ctx context.Context, err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		status := apiErr.StatusCode
		if status < 400 {
			if s, ok := errorTypeStatus[string(apiErr.Type())]; ok {
				status = s
			} else {
				status = 500
			}
		}
		message := apiErr.Message
		if message == "" {
			message = apiErr.RawJSON()
		}
		return statusError(status, message, c.key)
	}
	return requestError(ctx, err, c.key)
}

func (c *anthropicClient) models(ctx context.Context) ([]ModelInfo, error) {
	pager := c.api.Models.ListAutoPaging(ctx, anthropic.ModelListParams{Limit: anthropic.Int(100)}, limitBody(maxModelsBody))
	var out []ModelInfo
	for pager.Next() {
		m := pager.Current()
		out = append(out, ModelInfo{ID: m.ID, Name: m.DisplayName})
		if len(out) >= 1000 {
			break
		}
	}
	if err := pager.Err(); err != nil {
		return nil, c.classify(ctx, err)
	}
	return out, nil
}

// The answers of the features Anthropic does not offer, each naming servers
// that offer that feature (Ollama embeds text but transcribes no audio).
var (
	errNoEmbeddings    = fmt.Errorf("%w: Anthropic has no embeddings API; use an OpenAI-compatible connection such as OpenAI or Ollama", ErrUnsupported)
	errNoTranscription = fmt.Errorf("%w: Anthropic has no transcription API; use an OpenAI connection or a local Whisper server with an OpenAI-compatible API (LocalAI, Speaches)", ErrUnsupported)
)

func (c *anthropicClient) embed(context.Context, string, []string) ([]Embedding, Usage, error) {
	return nil, Usage{}, errNoEmbeddings
}

func (c *anthropicClient) transcribe(context.Context, string, Media, string) (string, Usage, error) {
	return "", Usage{}, errNoTranscription
}
