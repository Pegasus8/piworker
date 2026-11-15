package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"sort"
	"strings"
)

// openAIClient talks to the OpenAI API or a server compatible with it
// (Ollama, LM Studio, OpenRouter, LocalAI, vLLM, …) with plain JSON over
// HTTP.
type openAIClient struct {
	conn Connection
	key  string
	http *http.Client
}

// usesDefaultEndpoint reports whether c talks to OpenAI itself, which wants
// max_completion_tokens (other servers know max_tokens).
func (c *openAIClient) usesDefaultEndpoint() bool {
	return strings.TrimRight(c.conn.Endpoint(), "/") == DefaultOpenAIURL
}

type oaToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaChatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   *string      `json:"content"`
			Refusal   *string      `json:"refusal"`
			ToolCalls []oaToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
}

// chatBody builds the chat completions request.
func (c *openAIClient) chatBody(req Request) (map[string]any, error) {
	var msgs []any
	if req.System != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": req.System})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleAssistant:
			msg := map[string]any{"role": "assistant"}
			if m.Text != "" || len(m.ToolCalls) == 0 {
				msg["content"] = m.Text
			} else {
				msg["content"] = nil
			}
			if len(m.ToolCalls) > 0 {
				calls := make([]any, 0, len(m.ToolCalls))
				for _, tc := range m.ToolCalls {
					args := string(tc.Input)
					if args == "" {
						args = "{}"
					}
					calls = append(calls, map[string]any{
						"id": tc.ID, "type": "function",
						"function": map[string]any{"name": tc.Name, "arguments": args},
					})
				}
				msg["tool_calls"] = calls
			}
			msgs = append(msgs, msg)
		case RoleUser:
			for _, r := range m.ToolResults {
				content := r.Content
				if r.IsError {
					content = "Error: " + content
				}
				msgs = append(msgs, map[string]any{"role": "tool", "tool_call_id": r.CallID, "content": content})
			}
			if m.Text == "" && len(m.Images) == 0 {
				if len(m.ToolResults) == 0 {
					msgs = append(msgs, map[string]any{"role": "user", "content": ""})
				}
				continue
			}
			if len(m.Images) == 0 {
				msgs = append(msgs, map[string]any{"role": "user", "content": m.Text})
				continue
			}
			parts := []any{map[string]any{"type": "text", "text": m.Text}}
			for _, img := range m.Images {
				url := img.URL
				if url == "" {
					url = "data:" + img.MediaType + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
				}
				parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
			}
			msgs = append(msgs, map[string]any{"role": "user", "content": parts})
		default:
			return nil, fmt.Errorf("invalid message role %q", m.Role)
		}
	}
	body := map[string]any{"model": req.Model, "messages": msgs}
	if c.usesDefaultEndpoint() {
		body["max_completion_tokens"] = req.MaxTokens
	} else {
		body["max_tokens"] = req.MaxTokens
	}
	switch req.Effort {
	case "":
	case "xhigh", "max":
		body["reasoning_effort"] = "high"
	default:
		body["reasoning_effort"] = req.Effort
	}
	if len(req.Tools) > 0 {
		tools := make([]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			params := json.RawMessage(t.InputSchema)
			if len(params) == 0 {
				params = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			fn := map[string]any{"name": t.Name, "parameters": params}
			if t.Description != "" {
				fn["description"] = t.Description
			}
			tools = append(tools, map[string]any{"type": "function", "function": fn})
		}
		body["tools"] = tools
	}
	if req.Schema != nil {
		body["response_format"] = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name": "answer", "schema": req.Schema, "strict": StrictCompatible(req.Schema),
			},
		}
	} else if req.JSON {
		body["response_format"] = map[string]any{"type": "json_object"}
	}
	return body, nil
}

func (c *openAIClient) chat(ctx context.Context, req Request) (*Response, Usage, error) {
	body, err := c.chatBody(req)
	if err != nil {
		return nil, Usage{}, err
	}
	var out oaChatResponse
	if err := c.postJSON(ctx, "/chat/completions", body, maxChatBody, &out); err != nil {
		return nil, Usage{}, err
	}
	usage := Usage{InputTokens: out.Usage.PromptTokens, OutputTokens: out.Usage.CompletionTokens, Requests: 1}
	if len(out.Choices) == 0 {
		return nil, usage, errors.New("the LLM provider answered no choice")
	}
	choice := out.Choices[0]
	if choice.Message.Refusal != nil && *choice.Message.Refusal != "" {
		return nil, usage, fmt.Errorf("%w: %s", ErrRefused, quote(*choice.Message.Refusal))
	}
	if choice.FinishReason == "content_filter" {
		return nil, usage, fmt.Errorf("%w (content filter)", ErrRefused)
	}
	resp := &Response{Model: out.Model, Usage: usage}
	if resp.Model == "" {
		resp.Model = req.Model
	}
	if choice.Message.Content != nil {
		resp.Text = *choice.Message.Content
	}
	for _, tc := range choice.Message.ToolCalls {
		resp.ToolCalls = append(resp.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Input: toolArguments(tc.Function.Arguments)})
	}
	switch choice.FinishReason {
	case "stop":
		resp.Stop = StopEnd
	case "length":
		resp.Stop = StopMaxTokens
	case "tool_calls", "function_call":
		resp.Stop = StopToolUse
	default:
		resp.Stop = StopOther
	}
	if len(resp.ToolCalls) > 0 && resp.Stop != StopMaxTokens {
		resp.Stop = StopToolUse // some servers answer "stop" with tool calls
	}
	resp.Message = Message{Role: RoleAssistant, Text: resp.Text, ToolCalls: resp.ToolCalls}
	return resp, usage, nil
}

// toolArguments turns a tool call's arguments into its Input: the JSON
// object as sent, {} for none, and anything else as a JSON string (which
// the tool's schema then refuses, so the model learns its call was bad).
func toolArguments(args string) json.RawMessage {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" {
		return json.RawMessage(`{}`)
	}
	if json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed)
	}
	quoted, _ := json.Marshal(args)
	return quoted
}

func (c *openAIClient) models(ctx context.Context) ([]ModelInfo, error) {
	var out struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/models", nil, "", maxModelsBody, &out); err != nil {
		return nil, err
	}
	models := make([]ModelInfo, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID != "" {
			models = append(models, ModelInfo{ID: m.ID, Name: m.Name})
		}
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func (c *openAIClient) embed(ctx context.Context, model string, inputs []string) ([]Embedding, Usage, error) {
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Usage struct {
			PromptTokens int64 `json:"prompt_tokens"`
		} `json:"usage"`
	}
	body := map[string]any{"model": model, "input": inputs}
	if err := c.postJSON(ctx, "/embeddings", body, maxEmbeddingsBody, &out); err != nil {
		return nil, Usage{}, err
	}
	usage := Usage{InputTokens: out.Usage.PromptTokens, Requests: 1}
	embs := make([]Embedding, len(inputs))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(inputs) || embs[d.Index] != nil {
			return nil, usage, fmt.Errorf("the LLM provider answered an embedding with an invalid index %d", d.Index)
		}
		if len(d.Embedding) == 0 {
			return nil, usage, fmt.Errorf("the LLM provider answered an empty embedding")
		}
		embs[d.Index] = d.Embedding
	}
	for i, e := range embs {
		if e == nil {
			return nil, usage, fmt.Errorf("the LLM provider answered no embedding for input %d", i+1)
		}
	}
	return embs, usage, nil
}

// audioExtensions name the file of a transcription: OpenAI tells the format
// by the file name's extension.
var audioExtensions = map[string]string{
	"audio/mpeg": ".mp3", "audio/mp3": ".mp3", "audio/wav": ".wav", "audio/wave": ".wav", "audio/x-wav": ".wav",
	"audio/ogg": ".ogg", "application/ogg": ".ogg", "audio/webm": ".webm", "video/webm": ".webm",
	"audio/mp4": ".m4a", "audio/x-m4a": ".m4a", "audio/m4a": ".m4a", "video/mp4": ".mp4",
	"audio/flac": ".flac", "audio/x-flac": ".flac", "audio/opus": ".opus", "audio/aac": ".aac",
}

// audioFileName names the file of audio of mediaType. Unknown content
// (application/octet-stream) is never named by the system's MIME table,
// which calls it ".bin".
func audioFileName(mediaType string) string {
	if ext, ok := audioExtensions[mediaType]; ok {
		return "audio" + ext
	}
	if mediaType == "" || mediaType == "application/octet-stream" {
		return "audio"
	}
	if exts, _ := mime.ExtensionsByType(mediaType); len(exts) > 0 {
		return "audio" + exts[0]
	}
	return "audio"
}

// sniffAudio returns the media type of audio whose signature
// http.DetectContentType does not know ("" when it is none of them): FLAC,
// AAC in ADTS frames and MPEG audio frames (an MP3 without an ID3 tag).
func sniffAudio(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("fLaC")):
		return "audio/flac"
	case len(data) < 2 || data[0] != 0xFF:
		return ""
	case data[1]&0xF6 == 0xF0:
		// ADTS: a 12-bit sync word, then the layer, always 00.
		return "audio/aac"
	case data[1]&0xE0 == 0xE0 && data[1]&0x06 != 0 && data[1]&0x18 != 0x08:
		// An MPEG audio frame: an 11-bit sync word, a version that is not
		// the reserved one and a layer.
		return "audio/mpeg"
	}
	return ""
}

func (c *openAIClient) transcribe(ctx context.Context, model string, audio Media, language string) (string, Usage, error) {
	if len(audio.Data) == 0 {
		return "", Usage{}, errors.New("transcription needs the audio's content (fetch a URL first)")
	}
	if len(audio.Data) > MaxAudioBytes {
		return "", Usage{}, fmt.Errorf("the audio is too large (at most %d MiB)", MaxAudioBytes>>20)
	}
	// OpenAI tells the format by the file name: content of no known type
	// is named by its own signature where it has one.
	mediaType := audio.MediaType
	if mediaType == "" || mediaType == "application/octet-stream" {
		mediaType = "application/octet-stream"
		if sniffed := sniffAudio(audio.Data); sniffed != "" {
			mediaType = sniffed
		}
	}
	var buf bytes.Buffer
	// Room for the audio and the multipart framing around it, so a large
	// file is copied once rather than through repeated growth.
	buf.Grow(len(audio.Data) + len(model) + len(language) + 4096)
	w := multipart.NewWriter(&buf)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, audioFileName(mediaType)))
	header.Set("Content-Type", mediaType)
	part, err := w.CreatePart(header)
	if err == nil {
		_, err = part.Write(audio.Data)
	}
	fields := [][2]string{{"model", model}, {"response_format", "json"}}
	if language != "" {
		fields = append(fields, [2]string{"language", language})
	}
	for _, f := range fields {
		if err == nil {
			err = w.WriteField(f[0], f[1])
		}
	}
	if err == nil {
		err = w.Close()
	}
	if err != nil {
		return "", Usage{}, fmt.Errorf("cannot build the transcription request: %w", err)
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := c.do(ctx, http.MethodPost, "/audio/transcriptions", &buf, w.FormDataContentType(), maxChatBody, &out); err != nil {
		return "", Usage{}, err
	}
	// Transcription spends no tokens: it counts one request.
	return out.Text, Usage{Requests: 1}, nil
}

func (c *openAIClient) postJSON(ctx context.Context, path string, body any, limit int64, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("cannot encode the LLM request: %w", err)
	}
	return c.do(ctx, http.MethodPost, path, bytes.NewReader(raw), "application/json", limit, out)
}

// do sends one request and decodes a successful answer into out. Error
// statuses become *APIError (see statusError); the key is only ever in the
// Authorization header, and is redacted from every error.
func (c *openAIClient) do(ctx context.Context, method, path string, body io.Reader, contentType string, limit int64, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.conn.Endpoint()+path, body)
	if err != nil {
		return fmt.Errorf("cannot build the LLM request: %s", redact(err.Error(), c.key))
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return requestError(ctx, err, c.key)
	}
	defer resp.Body.Close()
	capBody(resp, limit)
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return requestError(ctx, err, c.key)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return statusError(resp.StatusCode, openAIErrorMessage(data), c.key)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("the LLM provider's answer is not valid JSON: %s", quote(redact(err.Error(), c.key)))
	}
	return nil
}

// openAIErrorMessage extracts the message of an error body: OpenAI's
// {"error": {"message": …}}, the {"error": "…"} of some servers, a top-level
// "message", or the body itself. It does not cut the message: statusError
// removes the key first, so a cut never leaves part of a key behind.
func openAIErrorMessage(data []byte) string {
	var body struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if json.Unmarshal(data, &body) == nil {
		var nested struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body.Error, &nested) == nil && nested.Message != "" {
			return nested.Message
		}
		var s string
		if json.Unmarshal(body.Error, &s) == nil && s != "" {
			return s
		}
		if body.Message != "" {
			return body.Message
		}
	}
	return string(data)
}
