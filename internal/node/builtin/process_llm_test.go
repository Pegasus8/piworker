package builtin

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLLMProcessInfo(t *testing.T) {
	info := LLMProcessInfo()
	assert.Equal(t, "process-llm", info.Type)
	assert.Equal(t, types.NodeCategoryProcessing, info.Category)
	assert.Equal(t, node.RiskExternal, info.Risk)
	assert.Equal(t, "sparkles", info.Icon)
	require.NotNil(t, info.Config)
	assert.ElementsMatch(t, []string{"connection", "prompt"}, info.Config.Required)
	props := info.Config.Properties
	assert.Equal(t, node.OptionsLLMConnections, props["connection"].Options)
	assert.True(t, props["connection"].Destination)
	assert.True(t, props["connection"].AgentVisible)
	assert.Equal(t, node.OptionsLLMModels, props["model"].Options)
	assert.Equal(t, "{{payload}}", props["prompt"].Default)
	assert.Equal(t, []string{"text", "json"}, props["output"].Enum)
	assert.Equal(t, "json", props["schema"].Type)
	assert.Equal(t, 1024, props["maxTokens"].Default)
	assert.Equal(t, "tokens", props["maxTokens"].Unit)
	assert.Equal(t, []string{"", "low", "medium", "high", "xhigh", "max"}, props["effort"].Enum)
	assert.Equal(t, 20, props["memoryTurns"].Default)
	assert.Equal(t, "messages", props["memoryTurns"].Unit)
	assert.Equal(t, 120, props["timeout"].Default)
	assert.Equal(t, "seconds", props["timeout"].Unit)
	for _, visible := range []string{"images", "output", "effort", "memoryKey", "resultField", "model"} {
		assert.Truef(t, props[visible].AgentVisible, "%s is AgentVisible", visible)
	}
	for _, hidden := range []string{"system", "prompt", "schema"} {
		assert.Falsef(t, props[hidden].AgentVisible, "%s is not AgentVisible", hidden)
	}
	assert.Contains(t, info.Documentation, "## Configuration")
	in, out := mustLLMProcess(t, map[string]any{"connection": "c"}).Ports()
	assert.Equal(t, info.Inputs, in)
	assert.Equal(t, info.Outputs, out)
}

func mustLLMProcess(t *testing.T, cfg map[string]any) node.Node {
	t.Helper()
	n, err := NewLLMProcess(cfg)
	require.NoError(t, err)
	require.NoError(t, n.Validate())
	return n
}

func TestLLMProcessRefusesBadSettings(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"empty prompt", map[string]any{"prompt": "  "}, "prompt is required"},
		{"output", map[string]any{"output": "xml"}, "output must be text or json"},
		{"schema without json", map[string]any{"schema": map[string]any{"type": "object"}}, "schema needs output json"},
		{"schema not an object", map[string]any{"output": "json", "schema": map[string]any{"type": "string"}}, "schema"},
		{"schema not JSON", map[string]any{"output": "json", "schema": "{nope"}, "schema"},
		{"maxTokens", map[string]any{"maxTokens": 64001}, "maxTokens must be between 1 and 64000 tokens"},
		{"effort", map[string]any{"effort": "extreme"}, "effort"},
		{"memoryTurns low", map[string]any{"memoryKey": "k", "memoryTurns": 1}, "memoryTurns must be between 2 and 200 messages"},
		{"memoryTurns high", map[string]any{"memoryKey": "k", "memoryTurns": 201}, "memoryTurns must be between 2 and 200 messages"},
		{"images path", map[string]any{"images": "image"}, "images"},
		{"resultField", map[string]any{"resultField": "a..b"}, "resultField"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := map[string]any{"connection": "c"}
			for k, v := range tc.cfg {
				cfg[k] = v
			}
			_, err := NewLLMProcess(cfg)
			assert.ErrorContains(t, err, tc.want)
		})
	}
	// A schema may also arrive as JSON text (older editors, hand-written flows).
	mustLLMProcess(t, map[string]any{"connection": "c", "output": "json", "schema": `{"type":"object","properties":{"a":{"type":"string"}}}`})
}

func TestLLMProcessAnswersText(t *testing.T) {
	f := newFakeLLM(t, func(int, string, map[string]any) (int, any) { return 200, chatAnswer("It is 21 °C.", 12, 7) })
	env := f.env(t)
	n := mustLLMProcess(t, map[string]any{
		"connection": fakeLLMConnection,
		"system":     "You answer about {{payload.room}}.",
		"prompt":     "How warm is it? {{payload.temp}}",
		"effort":     "low",
	})
	in := types.NewMessage(map[string]any{"room": "kitchen", "temp": 21}, types.DataTypeObject)
	in.SourcePort = "true"
	outs, err := n.Process(env.ctx, in)
	require.NoError(t, err)
	assert.Equal(t, "It is 21 °C.", llmAnswerText(t, outs))
	assert.Equal(t, "output", outs[0].SourcePort)
	assert.Equal(t, types.DataTypeString, outs[0].PayloadType)
	assert.Equal(t, "true", in.SourcePort, "the input is not changed")
	assert.Equal(t, map[string]any{
		"connection": fakeLLMConnection, "model": fakeLLMServed, "inputTokens": int64(12), "outputTokens": int64(7),
		"requests": int64(1), "stop": "end",
	}, outs[0].Meta["llm"])

	reqs := f.Requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, []string{"/v1/chat/completions"}, f.Paths())
	body := reqs[0]
	assert.Equal(t, fakeLLMModel, body["model"])
	assert.EqualValues(t, 1024, body["max_tokens"])
	assert.Equal(t, "low", body["reasoning_effort"])
	assert.Nil(t, body["response_format"])
	msgs := requestMessages(t, body)
	require.Len(t, msgs, 2)
	assert.Equal(t, map[string]any{"role": "system", "content": "You answer about kitchen."}, msgs[0])
	assert.Equal(t, map[string]any{"role": "user", "content": "How warm is it? 21"}, msgs[1])
	assert.Equal(t, int64(19), env.store.UsageToday(fakeLLMConnection).Total(), "the usage is recorded")
}

func TestLLMProcessResultField(t *testing.T) {
	f := newFakeLLM(t, func(int, string, map[string]any) (int, any) { return 200, chatAnswer("summary", 1, 1) })
	env := f.env(t)
	n := mustLLMProcess(t, map[string]any{"connection": fakeLLMConnection, "resultField": "ai.summary"})
	in := types.NewMessage(map[string]any{"text": "long"}, types.DataTypeObject)
	outs, err := n.Process(env.ctx, in)
	require.NoError(t, err)
	require.Len(t, outs, 1)
	assert.Equal(t, map[string]any{"text": "long", "ai": map[string]any{"summary": "summary"}}, outs[0].Payload)
	assert.Equal(t, map[string]any{"text": "long"}, in.Payload, "the input is not changed")
	assert.Equal(t, "{\"text\":\"long\"}", requestMessages(t, f.Requests()[0])[0]["content"], "the default prompt is {{payload}}, without a system message")
}

func TestLLMProcessAnswersJSON(t *testing.T) {
	f := newFakeLLM(t, func(int, string, map[string]any) (int, any) {
		return 200, chatAnswer("```json\n{\"mood\":\"happy\",\"score\":0.9}\n```", 1, 1)
	})
	env := f.env(t)

	t.Run("without a schema", func(t *testing.T) {
		n := mustLLMProcess(t, map[string]any{"connection": fakeLLMConnection, "output": "json"})
		outs, err := n.Process(env.ctx, types.NewMessage("I love it", types.DataTypeString))
		require.NoError(t, err)
		require.Len(t, outs, 1)
		assert.Equal(t, map[string]any{"mood": "happy", "score": 0.9}, outs[0].Payload)
		assert.Equal(t, types.DataTypeObject, outs[0].PayloadType)
		reqs := f.Requests()
		assert.Equal(t, map[string]any{"type": "json_object"}, reqs[len(reqs)-1]["response_format"])
	})

	t.Run("with a schema", func(t *testing.T) {
		schema := map[string]any{"type": "object", "properties": map[string]any{
			"mood": map[string]any{"type": "string"}, "score": map[string]any{"type": "number"},
		}, "required": []any{"mood", "score"}}
		n := mustLLMProcess(t, map[string]any{"connection": fakeLLMConnection, "output": "json", "schema": schema})
		outs, err := n.Process(env.ctx, types.NewMessage("I love it", types.DataTypeString))
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"mood": "happy", "score": 0.9}, outs[0].Payload)
		reqs := f.Requests()
		format, ok := reqs[len(reqs)-1]["response_format"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "json_schema", format["type"])
	})

	t.Run("an answer that does not match the schema", func(t *testing.T) {
		schema := map[string]any{"type": "object", "properties": map[string]any{"other": map[string]any{"type": "string"}}, "required": []any{"other"}}
		n := mustLLMProcess(t, map[string]any{"connection": fakeLLMConnection, "output": "json", "schema": schema})
		_, err := n.Process(env.ctx, types.NewMessage("I love it", types.DataTypeString))
		assert.ErrorIs(t, err, llm.ErrInvalidAnswer)
		assert.False(t, node.IsRetryable(err))
	})
}

func TestLLMProcessRefusal(t *testing.T) {
	f := newFakeLLM(t, func(int, string, map[string]any) (int, any) {
		return 200, map[string]any{"model": "m", "choices": []any{map[string]any{
			"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": nil, "refusal": "I can't help with that."},
		}}}
	})
	env := f.env(t)
	n := mustLLMProcess(t, map[string]any{"connection": fakeLLMConnection})
	_, err := n.Process(env.ctx, types.NewMessage("x", types.DataTypeString))
	assert.ErrorIs(t, err, llm.ErrRefused)
	assert.False(t, node.IsRetryable(err))
}

// A 1×1 PNG.
var tinyPNG, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")

func TestLLMProcessSendsImages(t *testing.T) {
	f := newFakeLLM(t, func(int, string, map[string]any) (int, any) { return 200, chatAnswer("a cat", 1, 1) })
	env := f.env(t)
	n := mustLLMProcess(t, map[string]any{"connection": fakeLLMConnection, "prompt": "What is this?", "images": "payload.photos"})
	photo := base64.StdEncoding.EncodeToString(tinyPNG)
	in := types.NewMessage(map[string]any{"photos": []any{photo, "https://example.com/cat.jpg"}}, types.DataTypeObject)
	_, err := n.Process(env.ctx, in)
	require.NoError(t, err)
	msgs := requestMessages(t, f.Requests()[0])
	require.Len(t, msgs, 1)
	parts, ok := msgs[0]["content"].([]any)
	require.True(t, ok, "content with images is a list of parts: %v", msgs[0]["content"])
	require.Len(t, parts, 3)
	assert.Equal(t, map[string]any{"type": "text", "text": "What is this?"}, parts[0])
	assert.Equal(t, "data:image/png;base64,"+photo, parts[1].(map[string]any)["image_url"].(map[string]any)["url"])
	assert.Equal(t, "https://example.com/cat.jpg", parts[2].(map[string]any)["image_url"].(map[string]any)["url"])

	// A message without the path sends no image.
	_, err = n.Process(env.ctx, types.NewMessage(map[string]any{"text": "hi"}, types.DataTypeObject))
	require.NoError(t, err)
	assert.Equal(t, "What is this?", requestMessages(t, f.Requests()[1])[0]["content"])

	// What is not an image fails before anything is sent.
	_, err = n.Process(env.ctx, types.NewMessage(map[string]any{"photos": base64.StdEncoding.EncodeToString([]byte("plain text, not an image"))}, types.DataTypeObject))
	assert.ErrorContains(t, err, "image")
	assert.Len(t, f.Requests(), 2)
}

func TestLLMProcessMemory(t *testing.T) {
	answers := []string{"Hi Ana!", "Your name is Ana.", "Third.", "Fourth."}
	f := newFakeLLM(t, func(n int, _ string, _ map[string]any) (int, any) {
		if n >= len(answers) {
			return 400, apiError("no more answers")
		}
		return 200, chatAnswer(answers[n], 1, 1)
	})
	env := f.env(t)
	n := mustLLMProcess(t, map[string]any{
		"connection": fakeLLMConnection, "prompt": "{{payload.text}}",
		"memoryKey": "chat:{{payload.chat}}", "memoryTurns": 3,
	})
	send := func(text string) {
		t.Helper()
		_, err := n.Process(env.ctx, types.NewMessage(map[string]any{"chat": 42, "text": text}, types.DataTypeObject))
		require.NoError(t, err)
	}

	send("I am Ana")
	turns := env.memory.Turns("chat:42")
	require.Len(t, turns, 2)
	assert.Equal(t, llm.RoleUser, turns[0].Role)
	assert.Equal(t, "I am Ana", turns[0].Text)
	assert.Equal(t, llm.RoleAssistant, turns[1].Role)
	assert.Equal(t, "Hi Ana!", turns[1].Text)

	send("What is my name?")
	msgs := requestMessages(t, f.Requests()[1])
	require.Len(t, msgs, 3, "the stored turns precede the prompt")
	assert.Equal(t, map[string]any{"role": "user", "content": "I am Ana"}, msgs[0])
	assert.Equal(t, map[string]any{"role": "assistant", "content": "Hi Ana!"}, msgs[1])
	assert.Equal(t, map[string]any{"role": "user", "content": "What is my name?"}, msgs[2])

	// memoryTurns keeps the last 3 turns; a history never starts with the
	// assistant.
	send("third")
	assert.Len(t, env.memory.Turns("chat:42"), 3)
	send("fourth")
	msgs = requestMessages(t, f.Requests()[3])
	assert.Equal(t, "user", msgs[0]["role"])
	assert.Equal(t, "fourth", msgs[len(msgs)-1]["content"])

	// Another key is another conversation.
	_, err := n.Process(env.ctx, types.NewMessage(map[string]any{"chat": 7, "text": "new"}, types.DataTypeObject))
	require.Error(t, err, "no answer is left in the script")
	assert.Len(t, requestMessages(t, f.Requests()[4]), 1)
	assert.Empty(t, env.memory.Turns("chat:7"), "a failed call stores nothing")
}

func TestLLMProcessMemoryKeyMustRender(t *testing.T) {
	f := newFakeLLM(t, func(int, string, map[string]any) (int, any) { return 200, chatAnswer("x", 1, 1) })
	env := f.env(t)
	n := mustLLMProcess(t, map[string]any{"connection": fakeLLMConnection, "memoryKey": "{{payload.chat}}"})
	_, err := n.Process(env.ctx, types.NewMessage(map[string]any{"text": "no chat"}, types.DataTypeObject))
	assert.ErrorContains(t, err, "memoryKey")
	_, err = n.Process(env.ctx, types.NewMessage(map[string]any{"chat": strings.Repeat("k", 201)}, types.DataTypeObject))
	assert.ErrorContains(t, err, "memoryKey")
	assert.Empty(t, f.Requests())
}

// With memory, Anthropic is asked to cache the prefix (the conversation is
// sent again next time); OpenAI-compatible servers ignore it.
func TestLLMProcessRequestCaching(t *testing.T) {
	with := mustLLMProcess(t, map[string]any{"connection": "c", "memoryKey": "k"}).(*LLMProcess)
	without := mustLLMProcess(t, map[string]any{"connection": "c"}).(*LLMProcess)
	msg := types.NewMessage("hi", types.DataTypeString)
	reqWith, err := with.request(context.Background(), msg, "m", nil)
	require.NoError(t, err)
	reqWithout, err := without.request(context.Background(), msg, "m", nil)
	require.NoError(t, err)
	assert.True(t, reqWith.Cache)
	assert.False(t, reqWithout.Cache)
}

// A Forget while the model answers (a chat's /reset sent during a long
// answer) wins: the exchange that was in flight is not written back into
// the forgotten conversation.
func TestLLMProcessForgetDuringTheCallWins(t *testing.T) {
	asked := make(chan struct{})
	release := make(chan struct{})
	f := newFakeLLM(t, func(n int, _ string, _ map[string]any) (int, any) {
		close(asked)
		<-release
		return 200, chatAnswer("Here is your long answer.", 1, 1)
	})
	env := f.env(t)
	require.NoError(t, env.memory.Append("chat:42", 0,
		llm.Turn{Role: llm.RoleUser, Text: "my secret"}, llm.Turn{Role: llm.RoleAssistant, Text: "noted"}))
	ask := mustLLMProcess(t, map[string]any{"connection": fakeLLMConnection, "prompt": "{{payload.text}}", "memoryKey": "chat:{{payload.chat}}"})
	forget, err := NewLLMForgetAction(map[string]any{"memoryKey": "chat:{{payload.chat}}"})
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		_, err := ask.Process(env.ctx, types.NewMessage(map[string]any{"chat": 42, "text": "a long question"}, types.DataTypeObject))
		done <- err
	}()
	<-asked
	_, err = forget.Process(env.ctx, types.NewMessage(map[string]any{"chat": 42, "text": "/reset"}, types.DataTypeObject))
	require.NoError(t, err)
	close(release)
	require.NoError(t, <-done, "the answer is still delivered")
	assert.Nil(t, env.memory.Turns("chat:42"), "the forgotten conversation stays forgotten")
}
