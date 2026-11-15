package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeToolCaller is a node.ToolCaller: it lists tools and answers calls
// from a script, recording what it was asked.
type fakeToolCaller struct {
	tools []node.AgentTool
	// call answers a call of the tool name.
	call func(name string, args map[string]any) (node.ToolCallOutcome, error)

	mu      sync.Mutex
	listed  []node.AgentCaller
	callers []node.AgentCaller
	calls   []string
	args    []map[string]any
}

func (f *fakeToolCaller) AgentTools(caller node.AgentCaller) []node.AgentTool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listed = append(f.listed, caller)
	return f.tools
}

func (f *fakeToolCaller) CallAgentTool(_ context.Context, caller node.AgentCaller, name string, args map[string]any) (node.ToolCallOutcome, error) {
	f.mu.Lock()
	f.callers = append(f.callers, caller)
	f.calls = append(f.calls, name)
	f.args = append(f.args, args)
	f.mu.Unlock()
	return f.call(name, args)
}

func (f *fakeToolCaller) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func agentTool(name, description string) node.AgentTool {
	return node.AgentTool{
		Name: name, Description: description, Risk: node.RiskExternal, Confirm: node.ToolConfirmNone,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"room":{"type":"string"}},"required":["room"]}`),
	}
}

// agentEnv is a context with the test LLM stores, a tool caller and the
// identity of the node (flow f1, node agent1).
func agentEnv(t *testing.T, f *fakeLLM, tc node.ToolCaller) llmTestEnv {
	t.Helper()
	env := f.env(t)
	env.ctx = node.WithNodeInfo(node.WithToolCaller(env.ctx, tc), node.NodeInfo{FlowID: "f1", NodeID: "agent1"})
	return env
}

func mustAgent(t *testing.T, cfg map[string]any) node.Node {
	t.Helper()
	n, err := NewLLMAgentProcess(cfg)
	require.NoError(t, err)
	require.NoError(t, n.Validate())
	return n
}

func TestLLMAgentInfo(t *testing.T) {
	info := LLMAgentProcessInfo()
	assert.Equal(t, "process-llm-agent", info.Type)
	assert.Equal(t, types.NodeCategoryProcessing, info.Category)
	assert.Equal(t, node.RiskExternal, info.Risk)
	assert.Equal(t, "bot", info.Icon)
	require.NotNil(t, info.Config)
	props := info.Config.Properties
	tools := props["tools"]
	assert.Equal(t, "array", tools.Type)
	assert.True(t, tools.AgentVisible)
	assert.True(t, tools.Destination)
	assert.Equal(t, node.OptionsTools, tools.Options)
	assert.True(t, props["allowPrivileged"].RiskSwitch)
	assert.Equal(t, "boolean", props["allowPrivileged"].Type)
	assert.Equal(t, 8, props["maxSteps"].Default)
	assert.Equal(t, "steps", props["maxSteps"].Unit)
	assert.Equal(t, 4096, props["maxTokens"].Default)
	assert.Equal(t, 300, props["timeout"].Default)
	assert.Contains(t, info.Documentation, "prompt injection")
}

func TestLLMAgentRefusesBadSettings(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"tools not a list", map[string]any{"tools": "a,b"}, "tools"},
		{"tool not a name", map[string]any{"tools": []any{"lights on"}}, "tools"},
		{"tool twice", map[string]any{"tools": []any{"a", "a"}}, `"a" twice`},
		{"too many tools", map[string]any{"tools": manyNames(65)}, "at most 64 tools"},
		{"maxSteps", map[string]any{"maxSteps": 33}, "maxSteps must be between 1 and 32 steps"},
		{"allowPrivileged", map[string]any{"allowPrivileged": "yes"}, "allowPrivileged"},
		{"empty prompt", map[string]any{"prompt": ""}, "prompt is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := map[string]any{"connection": "c"}
			for k, v := range tc.cfg {
				cfg[k] = v
			}
			_, err := NewLLMAgentProcess(cfg)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func manyNames(n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = fmt.Sprintf("tool%d", i)
	}
	return out
}

func TestLLMAgentCallsToolsUntilItAnswers(t *testing.T) {
	f := newFakeLLM(t, func(n int, _ string, _ map[string]any) (int, any) {
		switch n {
		case 0:
			return 200, toolCallsAnswer(fakeToolCall{"call_1", "lights_on", `{"room":"kitchen"}`})
		case 1:
			return 200, toolCallsAnswer(
				fakeToolCall{"call_2", "lights_status", `{"room":"kitchen"}`},
				fakeToolCall{"call_3", "lights_status", `{"room":"hall"}`},
			)
		default:
			return 200, chatAnswer("The kitchen light is on.", 40, 9)
		}
	})
	tc := &fakeToolCaller{
		tools: []node.AgentTool{agentTool("lights.on", "Turns a light on"), agentTool("lights_status", "")},
		call: func(name string, args map[string]any) (node.ToolCallOutcome, error) {
			if name == "lights.on" {
				return node.ToolCallOutcome{Status: node.ToolCallOK, Result: json.RawMessage(`{"on":true}`)}, nil
			}
			if args["room"] == "hall" {
				return node.ToolCallOutcome{Status: node.ToolCallError, Error: "node n2 (action-http): 404"}, nil
			}
			return node.ToolCallOutcome{Status: node.ToolCallRunning}, nil
		},
	}
	env := agentEnv(t, f, tc)
	n := mustAgent(t, map[string]any{
		"connection": fakeLLMConnection, "system": "You run a house.", "prompt": "{{payload.text}}",
		"tools": []any{"lights.on", "lights_status"}, "allowPrivileged": true,
	})
	outs, err := n.Process(env.ctx, types.NewMessage(map[string]any{"text": "Turn on the kitchen light"}, types.DataTypeObject))
	require.NoError(t, err)
	assert.Equal(t, "The kitchen light is on.", llmAnswerText(t, outs))
	assert.Equal(t, "output", outs[0].SourcePort)

	// The caller is the node, with its settings.
	want := node.AgentCaller{FlowID: "f1", NodeID: "agent1", Tools: []string{"lights.on", "lights_status"}, AllowPrivileged: true}
	require.NotEmpty(t, tc.listed)
	assert.Equal(t, want, tc.listed[0])
	for _, c := range tc.callers {
		assert.Equal(t, want, c)
	}
	// The real names are called, in order, with the model's arguments.
	assert.Equal(t, []string{"lights.on", "lights_status", "lights_status"}, tc.Calls())
	assert.Equal(t, map[string]any{"room": "kitchen"}, tc.args[0])

	reqs := f.Requests()
	require.Len(t, reqs, 3)
	// The tools are offered under names every provider accepts.
	tools := reqs[0]["tools"].([]any)
	require.Len(t, tools, 2)
	fn0 := tools[0].(map[string]any)["function"].(map[string]any)
	assert.Equal(t, "lights_on", fn0["name"])
	assert.Equal(t, "Turns a light on", fn0["description"])
	assert.Equal(t, "object", fn0["parameters"].(map[string]any)["type"])
	assert.EqualValues(t, 4096, reqs[0]["max_tokens"])

	// The results go back as tool messages, after the assistant's calls.
	msgs := requestMessages(t, reqs[1])
	require.Len(t, msgs, 4)
	assert.Equal(t, "system", msgs[0]["role"])
	assert.Equal(t, map[string]any{"role": "user", "content": "Turn on the kitchen light"}, msgs[1])
	assert.Equal(t, "assistant", msgs[2]["role"])
	assert.Equal(t, map[string]any{"role": "tool", "tool_call_id": "call_1", "content": `{"on":true}`}, msgs[3])
	msgs = requestMessages(t, reqs[2])
	require.Len(t, msgs, 7)
	assert.Equal(t, "call_2", msgs[5]["tool_call_id"])
	assert.Contains(t, msgs[5]["content"], "still running")
	assert.Equal(t, "call_3", msgs[6]["tool_call_id"])
	assert.Equal(t, "Error: node n2 (action-http): 404", msgs[6]["content"], "a failed run is an error result")

	meta := outs[0].Meta["llm"].(map[string]any)
	assert.Equal(t, int64(3), meta["requests"])
	assert.Equal(t, int64(10+10+40), meta["inputTokens"])
	assert.Equal(t, int64(5+5+9), meta["outputTokens"])
	assert.Equal(t, "end", meta["stop"])
	steps, ok := meta["steps"].([]any)
	require.True(t, ok, "steps: %T", meta["steps"])
	require.Len(t, steps, 3)
	var statuses []string
	for _, s := range steps {
		step := s.(map[string]any)
		statuses = append(statuses, step["tool"].(string)+":"+step["status"].(string))
		assert.Contains(t, step, "durationMs")
		assert.Len(t, step, 3, "a step holds no arguments or results")
	}
	assert.Equal(t, []string{"lights.on:ok", "lights_status:running", "lights_status:error"}, statuses)
}

// A refused call is an error result the model can react to; a name the
// model made up is never called.
func TestLLMAgentReportsRefusalsToTheModel(t *testing.T) {
	f := newFakeLLM(t, func(n int, _ string, _ map[string]any) (int, any) {
		if n == 0 {
			return 200, toolCallsAnswer(
				fakeToolCall{"c1", "deploy", `{"room":"x"}`},
				fakeToolCall{"c2", "made_up", `{}`},
				fakeToolCall{"c3", "deploy", `"not an object"`},
			)
		}
		return 200, chatAnswer("I could not do it.", 1, 1)
	})
	tc := &fakeToolCaller{
		tools: []node.AgentTool{agentTool("deploy", "")},
		call: func(string, map[string]any) (node.ToolCallOutcome, error) {
			return node.ToolCallOutcome{}, errors.New(`tool "deploy" needs the admin's approval for each call`)
		},
	}
	env := agentEnv(t, f, tc)
	n := mustAgent(t, map[string]any{"connection": fakeLLMConnection, "tools": []any{"deploy"}})
	outs, err := n.Process(env.ctx, types.NewMessage("go", types.DataTypeString))
	require.NoError(t, err)
	assert.Equal(t, "I could not do it.", llmAnswerText(t, outs))
	assert.Equal(t, []string{"deploy"}, tc.Calls(), "only the listed tool, with an object of arguments, is called")

	msgs := requestMessages(t, f.Requests()[1])
	results := msgs[len(msgs)-3:]
	assert.Equal(t, "Error: tool \"deploy\" needs the admin's approval for each call", results[0]["content"])
	assert.Contains(t, results[1]["content"], `unknown tool "made_up"`)
	assert.Contains(t, results[2]["content"], "arguments must be a JSON object")

	var statuses []string
	for _, s := range outs[0].Meta["llm"].(map[string]any)["steps"].([]any) {
		step := s.(map[string]any)
		statuses = append(statuses, step["tool"].(string)+":"+step["status"].(string))
	}
	assert.Equal(t, []string{"deploy:refused", "made_up:refused", "deploy:refused"}, statuses)
}

func TestLLMAgentStopsAtMaxSteps(t *testing.T) {
	f := newFakeLLM(t, func(n int, _ string, _ map[string]any) (int, any) {
		return 200, toolCallsAnswer(fakeToolCall{fmt.Sprintf("c%d", n), "ping", `{}`})
	})
	tc := &fakeToolCaller{
		tools: []node.AgentTool{agentTool("ping", "")},
		call: func(string, map[string]any) (node.ToolCallOutcome, error) {
			return node.ToolCallOutcome{Status: node.ToolCallOK}, nil
		},
	}
	env := agentEnv(t, f, tc)
	n := mustAgent(t, map[string]any{"connection": fakeLLMConnection, "tools": []any{"ping"}, "maxSteps": 3})
	_, err := n.Process(env.ctx, types.NewMessage("loop", types.DataTypeString))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the agent did not finish within 3 steps")
	assert.False(t, node.IsRetryable(err))
	assert.Len(t, f.Requests(), 3, "maxSteps requests")
	assert.Len(t, tc.Calls(), 2, "the calls of the last answer are not run: nothing could read their results")
	// A result with no reply still answers the call.
	msgs := requestMessages(t, f.Requests()[1])
	assert.Contains(t, msgs[len(msgs)-1]["content"], "without a reply")
}

// Outside a runtime with a tool caller (and in a node test) the agent offers
// no tools and answers by itself.
func TestLLMAgentWithoutAToolCaller(t *testing.T) {
	f := newFakeLLM(t, func(int, string, map[string]any) (int, any) { return 200, chatAnswer("hello", 1, 1) })
	env := f.env(t)
	n := mustAgent(t, map[string]any{"connection": fakeLLMConnection, "tools": []any{"ping"}})
	outs, err := n.Process(env.ctx, types.NewMessage("hi", types.DataTypeString))
	require.NoError(t, err)
	assert.Equal(t, "hello", llmAnswerText(t, outs))
	assert.Nil(t, f.Requests()[0]["tools"])
	assert.Empty(t, outs[0].Meta["llm"].(map[string]any)["steps"])
}

func TestLLMAgentMemoryAndResultField(t *testing.T) {
	answers := []string{"Noted.", "You like tea."}
	f := newFakeLLM(t, func(n int, _ string, _ map[string]any) (int, any) { return 200, chatAnswer(answers[n%2], 1, 1) })
	env := agentEnv(t, f, &fakeToolCaller{})
	n := mustAgent(t, map[string]any{
		"connection": fakeLLMConnection, "prompt": "{{payload.text}}", "memoryKey": "user:{{payload.user}}",
		"resultField": "reply",
	})
	_, err := n.Process(env.ctx, types.NewMessage(map[string]any{"user": "ana", "text": "I like tea"}, types.DataTypeObject))
	require.NoError(t, err)
	outs, err := n.Process(env.ctx, types.NewMessage(map[string]any{"user": "ana", "text": "What do I like?"}, types.DataTypeObject))
	require.NoError(t, err)
	assert.Equal(t, "You like tea.", outs[0].Payload.(map[string]any)["reply"])
	msgs := requestMessages(t, f.Requests()[1])
	require.Len(t, msgs, 3)
	assert.Equal(t, "I like tea", msgs[0]["content"])
	assert.Equal(t, "Noted.", msgs[1]["content"])
	assert.Len(t, env.memory.Turns("user:ana"), 4)
}

func TestProviderToolNames(t *testing.T) {
	long := strings.Repeat("a", 100)
	names := []string{"lights.on", "lights_on", "ok-name", long, long + "b", "ü", "lights.on"}
	toProvider, fromProvider := providerToolNames(names)
	assert.Equal(t, "lights_on", toProvider["lights.on"], "the first name keeps its sanitized form")
	assert.NotEqual(t, "lights_on", toProvider["lights_on"], "a sanitized name never collides")
	assert.Equal(t, "ok-name", toProvider["ok-name"])
	assert.Len(t, toProvider, 6, "each name once")
	unique := map[string]bool{}
	for name, p := range toProvider {
		assert.Regexp(t, `^[A-Za-z0-9_-]{1,64}$`, p)
		assert.Equal(t, name, fromProvider[p], "%q maps back", p)
		unique[p] = true
	}
	assert.Len(t, unique, 6, "the provider names are unique")
}

// The node's timeout bounds the tools too: once it runs out the agent
// stops, whatever the tool says.
func TestLLMAgentStopsWhenItsContextEnds(t *testing.T) {
	f := newFakeLLM(t, func(int, string, map[string]any) (int, any) {
		return 200, toolCallsAnswer(fakeToolCall{"c1", "slow", `{}`})
	})
	var cancel context.CancelFunc
	tc := &fakeToolCaller{
		tools: []node.AgentTool{agentTool("slow", "")},
		call: func(string, map[string]any) (node.ToolCallOutcome, error) {
			cancel()
			return node.ToolCallOutcome{Status: node.ToolCallRunning}, context.Canceled
		},
	}
	env := agentEnv(t, f, tc)
	ctx, c := context.WithCancel(env.ctx)
	cancel = c
	defer cancel()
	n := mustAgent(t, map[string]any{"connection": fakeLLMConnection, "tools": []any{"slow"}})
	_, err := n.Process(ctx, types.NewMessage("x", types.DataTypeString))
	assert.ErrorIs(t, err, context.Canceled)
	assert.Len(t, f.Requests(), 1, "nothing is sent after the context ended")
}

// Once a tool ran, a failure of the agent is permanent: a retry would run
// the whole loop again, and the model would call the tools it already
// called (each a real run of another flow) a second time. Before any tool
// ran, a transient error stays transient.
func TestLLMAgentIsNotRetriedAfterAToolRan(t *testing.T) {
	for _, status := range []int{529, 503, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newFakeLLM(t, func(n int, _ string, _ map[string]any) (int, any) {
				if n == 0 {
					return 200, toolCallsAnswer(fakeToolCall{"call_1", "garage_open", `{"room":"garage"}`})
				}
				return status, apiError("overloaded")
			})
			tc := &fakeToolCaller{
				tools: []node.AgentTool{agentTool("garage_open", "Opens the garage door")},
				call: func(string, map[string]any) (node.ToolCallOutcome, error) {
					return node.ToolCallOutcome{Status: node.ToolCallOK, Result: json.RawMessage(`{"open":true}`)}, nil
				},
			}
			env := agentEnv(t, f, tc)
			n := mustAgent(t, map[string]any{"connection": fakeLLMConnection, "tools": []any{"garage_open"}})
			_, err := n.Process(env.ctx, types.NewMessage("open the garage", types.DataTypeString))
			require.Error(t, err)
			assert.False(t, node.IsRetryable(err), "a retry would open the garage again: %v", err)
			var apiErr *llm.APIError
			require.ErrorAs(t, err, &apiErr, "the provider's error is kept")
			assert.Equal(t, status, apiErr.Status)
			assert.Equal(t, []string{"garage_open"}, tc.Calls())
		})
	}

	f := newFakeLLM(t, func(int, string, map[string]any) (int, any) { return 529, apiError("overloaded") })
	tc := &fakeToolCaller{tools: []node.AgentTool{agentTool("garage_open", "")}}
	env := agentEnv(t, f, tc)
	_, err := mustAgent(t, map[string]any{"connection": fakeLLMConnection, "tools": []any{"garage_open"}}).
		Process(env.ctx, types.NewMessage("open the garage", types.DataTypeString))
	assert.True(t, node.IsRetryable(err), "no tool ran yet: %v", err)
}

// waitingReplier is the Replier of a runtime whose called runs have
// flows waiting for them (node.WaitingFlows).
type waitingReplier map[string][]string

func (waitingReplier) Reply(string, any, bool) bool { return false }

func (w waitingReplier) WaitingFlows(correlationID string) []string { return w[correlationID] }

// The agent tells the tool caller which flows wait for the run its message
// belongs to (a tool call of another flow's agent led here), so a tool of
// one of them is refused instead of waiting for this node until it times
// out.
func TestLLMAgentTellsTheToolCallerWhoWaits(t *testing.T) {
	f := newFakeLLM(t, func(n int, _ string, _ map[string]any) (int, any) {
		if n == 0 {
			return 200, toolCallsAnswer(fakeToolCall{"call_1", "garage_open", `{"room":"garage"}`})
		}
		return 200, chatAnswer("Done.", 1, 1)
	})
	tc := &fakeToolCaller{
		tools: []node.AgentTool{agentTool("garage_open", "")},
		call: func(string, map[string]any) (node.ToolCallOutcome, error) {
			return node.ToolCallOutcome{Status: node.ToolCallOK, Result: json.RawMessage(`{}`)}, nil
		},
	}
	env := agentEnv(t, f, tc)
	ctx := node.WithReplier(env.ctx, waitingReplier{"run-1": {"flow-a"}})
	n := mustAgent(t, map[string]any{"connection": fakeLLMConnection, "tools": []any{"garage_open"}})
	msg := types.NewMessage("open the garage", types.DataTypeString)
	msg.SetMeta("correlationID", "run-1")
	_, err := n.Process(ctx, msg)
	require.NoError(t, err)

	tc.mu.Lock()
	defer tc.mu.Unlock()
	require.Len(t, tc.listed, 1)
	require.Len(t, tc.callers, 1)
	assert.Equal(t, []string{"flow-a"}, tc.listed[0].Waiting)
	assert.Equal(t, []string{"flow-a"}, tc.callers[0].Waiting)
}
