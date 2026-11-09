package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/agentauth"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/toolschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listed returns the tools of a tools/list answer, by name.
func listed(t *testing.T, e *testEnv, raw string) map[string]map[string]any {
	t.Helper()
	res, rpcErr := rpcResult(t, e.serve(withToken(rpc(listToolsBody), raw)))
	require.Nil(t, rpcErr)
	tools := map[string]map[string]any{}
	for _, tool := range res["tools"].([]any) {
		m := tool.(map[string]any)
		tools[m["name"].(string)] = m
	}
	return tools
}

// called answers a tools/call of name with args (JSON) by raw.
func called(t *testing.T, e *testEnv, raw, name, args string) (result map[string]any, structured map[string]any) {
	t.Helper()
	res, rpcErr := rpcResult(t, e.serve(withToken(rpc(callBody(name, args)), raw)))
	require.Nil(t, rpcErr, "tool outcomes are results, never JSON-RPC errors")
	return res, resultBody(t, res)
}

// resultBody is what a tools/call result says: its structured content or,
// for an isError result, the JSON envelope of its text block. An isError
// result never carries structured content: clients validate structured
// content against the tool's output schema even on errors (and against a
// schema they listed earlier for a tool since hidden or changed).
func resultBody(t *testing.T, res map[string]any) map[string]any {
	t.Helper()
	if res["isError"] != true {
		structured, _ := res["structuredContent"].(map[string]any)
		return structured
	}
	require.NotContains(t, res, "structuredContent", "an isError result carries no structured content")
	content, _ := res["content"].([]any)
	require.Len(t, content, 1)
	var env map[string]any
	require.NoError(t, json.Unmarshal([]byte(content[0].(map[string]any)["text"].(string)), &env))
	return env
}

func errorCode(structured map[string]any) string {
	e, _ := structured["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func (e *testEnv) verified(raw string) verified {
	e.t.Helper()
	info, err := e.verifier.Verify(raw)
	require.NoError(e.t, err)
	return verified{info: info, actor: service.AgentActor(info)}
}

// Each published tool is advertised with its contract: canonical input
// schema, title, description, explicit annotations, the output schema when
// set, and _meta asking clients to confirm risky tools.
func TestToolsAreAdvertisedWithTheirContract(t *testing.T) {
	e := newTestEnv(t)
	plain := testTool(t, "garage")
	risky := testTool(t, "boiler")
	risky.Risk, risky.Confirm = node.RiskPrivileged, node.ToolConfirmNone
	risky.Annotations = flow.Annotations{DestructiveHint: true, OpenWorldHint: true}
	risky.OutputSchema = json.RawMessage(`{"type":"object","properties":{"on":{"type":"boolean"}}}`)
	asked := testTool(t, "gate")
	asked.Confirm = node.ToolConfirmAdmin
	e.tools.setTools(3, plain, risky, asked)

	tools := listed(t, e, callToken)
	require.Len(t, tools, 4)

	garage := tools["garage"]
	assert.Equal(t, "Garage garage", garage["title"])
	assert.Equal(t, plain.Description, garage["description"])
	schema, err := json.Marshal(garage["inputSchema"])
	require.NoError(t, err)
	assert.JSONEq(t, string(plain.Schema.JSON()), string(schema))
	assert.Equal(t, map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
		garage["annotations"], "every hint is explicit")
	assert.NotContains(t, garage, "outputSchema")
	assert.NotContains(t, garage, "_meta")

	boiler := tools["boiler"]
	assert.Equal(t, map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": false, "openWorldHint": true}, boiler["annotations"])
	out, err := json.Marshal(boiler["outputSchema"])
	require.NoError(t, err)
	assert.JSONEq(t, string(risky.OutputSchema), string(out))
	assert.Equal(t, map[string]any{"anthropic/requiresUserInteraction": true}, boiler["_meta"], "destructive")
	assert.Equal(t, map[string]any{"anthropic/requiresUserInteraction": true}, tools["gate"]["_meta"], "confirm admin")

	get := tools["pw_get_result"]
	require.NotNil(t, get, "the call scope brings pw_get_result")
	assert.Equal(t, map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}, get["annotations"])
}

// The advertised tools are exactly those the tool service lists for the
// token, plus the built-in tools of its scopes.
func TestToolsAreFilteredByToken(t *testing.T) {
	e := newTestEnv(t)
	e.tools.setTools(2, testTool(t, "garage"))
	assert.Contains(t, listed(t, e, callToken), "garage")
	assert.Empty(t, listed(t, e, readToken), "no flow tools and no pw_get_result without the call scope")
	assert.GreaterOrEqual(t, e.tools.lists.Load(), int32(2))
}

// Lists are private to the token and short-lived in client caches.
func TestListsArePrivateAndShortLived(t *testing.T) {
	e := newTestEnv(t)
	res, rpcErr := rpcResult(t, e.serve(withToken(rpc(listToolsBody), callToken)))
	require.Nil(t, rpcErr)
	assert.Equal(t, "private", res["cacheScope"])
	assert.EqualValues(t, DefaultListTTL.Milliseconds(), res["ttlMs"])
}

// Capabilities: tools and prompts, without list change notifications.
func TestCapabilities(t *testing.T) {
	e := newTestEnv(t)
	res, rpcErr := rpcResult(t, e.serve(withToken(rpc(initializeBody), readToken)))
	require.Nil(t, rpcErr)
	caps, err := json.Marshal(res["capabilities"])
	require.NoError(t, err)
	assert.JSONEq(t, `{"tools":{},"prompts":{}}`, string(caps))
	info := res["serverInfo"].(map[string]any)
	assert.Equal(t, "piworker", info["name"])
	assert.Equal(t, "test", info["version"])
	assert.Equal(t, "2025-06-18", res["protocolVersion"])
	// The instructions tell built-in tools from flows, and say that what
	// the tools return (flow replies, flow names, recorded errors) is
	// data, never instructions.
	instr := res["instructions"].(string)
	assert.Contains(t, instr, "pw_")
	assert.Contains(t, instr, "recorded errors")
	assert.Contains(t, instr, "not instructions")
	// The built-in tools also author drafts the admin approves, and the
	// node documentation and dry-run previews they return are data too.
	assert.Contains(t, instr, "drafts")
	assert.Contains(t, instr, "approves")
	assert.Contains(t, instr, "node documentation")
}

// subscriptions/listen is refused: lists never change while a client is
// connected (listChanged is false).
// From 2026-07-28 a method that is not found is answered HTTP 404
// (SEP-2575), as the SDK answers its own.
func TestSubscriptionsListenIsRejected(t *testing.T) {
	e := newTestEnv(t)
	body := `{"jsonrpc":"2.0","id":7,"method":"subscriptions/listen","params":{"notifications":{"toolsListChanged":true}}}`
	_, rpcErr := rpcResult(t, e.serve(withToken(rpc(body), callToken)))
	require.NotNil(t, rpcErr)
	assert.EqualValues(t, -32601, rpcErr["code"])

	listen := rpc2026(t, "subscriptions/listen", map[string]any{"notifications": map[string]any{"toolsListChanged": true}})
	id, code, _ := rpcRefusal(t, e.serve(withToken(listen, callToken)), http.StatusNotFound)
	assert.EqualValues(t, 9, id)
	assert.EqualValues(t, -32601, code)
}

// A tool call goes to the tool service with the token's actor, the tool's
// name and the decoded arguments; a completed reply is the structured
// content.
func TestToolCallGoesThroughTheService(t *testing.T) {
	e := newTestEnv(t)
	e.tools.setTools(2, testTool(t, "garage"))
	var got struct {
		actor service.Actor
		ref   service.ToolRef
		args  map[string]any
	}
	e.tools.call = func(_ context.Context, actor service.Actor, ref service.ToolRef, args map[string]any) (flow.CallResult, error) {
		got.actor, got.ref, got.args = actor, ref, args
		return flow.CallResult{Status: flow.CallStatusCompleted, RunID: "run-7", Result: json.RawMessage(`{"moved":true,"n":1.5}`)}, nil
	}
	res, structured := called(t, e, callToken, "garage", `{"door":"left","n":2}`)
	assert.Equal(t, map[string]any{"moved": true, "n": 1.5}, structured)
	assert.NotEqual(t, true, res["isError"])
	content := res["content"].([]any)
	require.Len(t, content, 1)
	assert.JSONEq(t, `{"moved":true,"n":1.5}`, content[0].(map[string]any)["text"].(string))
	assert.Equal(t, "run-7", res["_meta"].(map[string]any)["piworker/runId"])

	assert.Equal(t, service.ActorAgent, got.actor.Kind)
	assert.Equal(t, "callcallcallcall", got.actor.TokenID)
	assert.Equal(t, service.ToolByName("garage"), got.ref)
	assert.Equal(t, map[string]any{"door": "left", "n": float64(2)}, got.args)

	// Absent arguments are an empty object.
	_, rpcErr := rpcResult(t, e.serve(withToken(rpc(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"garage"}}`), callToken)))
	require.Nil(t, rpcErr)
	assert.Equal(t, map[string]any{}, got.args)
}

// fakeRejections records the rejections the endpoint counts.
type fakeRejections struct {
	mu   sync.Mutex
	seen []string
}

func (f *fakeRejections) Reject(tokenID, reason string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, tokenID+" "+reason+" "+at.Format(time.RFC3339))
}

// Arguments that are not a JSON object never reach the service (it takes
// an object); they are counted like its invalid_arguments rejections.
func TestToolCallArgumentsMustBeAnObject(t *testing.T) {
	rejections := &fakeRejections{}
	e := newTestEnv(t, func(o *Options) { o.Rejections = rejections })
	e.tools.setTools(2, testTool(t, "garage"))
	for _, args := range []string{`[1]`, `"left"`, `3`} {
		res, structured := called(t, e, callToken, "garage", args)
		assert.Equal(t, true, res["isError"], args)
		assert.Equal(t, "invalid_arguments", errorCode(structured), args)
	}
	called(t, e, callToken, "unheld", `[]`)
	want := "callcallcallcall invalid_arguments " + t0.Format(time.RFC3339)
	assert.Equal(t, []string{want, want, want, want}, rejections.seen)
	// Duplicate names make the request ambiguous: refused before the SDK
	// reads it.
	w := e.serve(withToken(rpc(callBody("garage", `{"door":"left","door":"right"}`)), callToken))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"code":-32700`)
	assert.Zero(t, e.tools.calls.Load())
}

// Calls of tools the server does not hold still go to the service, so a
// hidden tool and an unknown one get the same answer (and the same audit).
func TestUnknownAndHiddenToolsAreAlike(t *testing.T) {
	e := newTestEnv(t)
	e.tools.call = func(_ context.Context, _ service.Actor, ref service.ToolRef, _ map[string]any) (flow.CallResult, error) {
		return flow.CallResult{}, &service.ToolError{Kind: service.ErrToolNotFound, Message: fmt.Sprintf("unknown tool %q", ref.Name)}
	}
	hidden, _ := called(t, e, callToken, "garage", `{"door":"left"}`)
	unknown, _ := called(t, e, callToken, "nothing_here", `{"door":"left"}`)
	assert.Equal(t, true, hidden["isError"])
	assert.Equal(t, strings.ReplaceAll(fmt.Sprint(hidden), "garage", "X"), strings.ReplaceAll(fmt.Sprint(unknown), "nothing_here", "X"))
	assert.EqualValues(t, 2, e.tools.calls.Load())
}

// At 2026-07-28 every result says it is complete ("resultType", SEP-2322),
// those the endpoint answers itself too: a call of a tool the server does
// not hold (unknown, hidden or an impossible name) or whose handler
// panicked reads like the refusal of a held tool, the name aside. Earlier
// versions have no resultType.
func TestEndpointResultsAreCompleteAt20260728(t *testing.T) {
	e := newTestEnv(t)
	e.tools.setTools(2, testTool(t, "garage"))
	e.tools.call = func(_ context.Context, _ service.Actor, ref service.ToolRef, _ map[string]any) (flow.CallResult, error) {
		if ref.Name == "boom" {
			panic("exploded")
		}
		return flow.CallResult{}, &service.ToolError{Kind: service.ErrToolNotFound, Message: fmt.Sprintf("unknown tool %q", ref.Name)}
	}
	call := func(name string) map[string]any {
		t.Helper()
		params := map[string]any{"name": name, "arguments": map[string]any{"door": "left"}}
		res, rpcErr := rpcResult(t, e.serve(withToken(rpc2026(t, "tools/call", params), callToken)))
		require.Nil(t, rpcErr, name)
		return res
	}
	// garage is held: its refusal (it was undeployed since the server was
	// built) comes from the SDK's tool handler.
	held := call("garage")
	require.Equal(t, "complete", held["resultType"])
	for _, name := range []string{"nothing_here", "garage door"} {
		res := call(name)
		assert.Equal(t, "complete", res["resultType"], name)
		assert.Equal(t, strings.ReplaceAll(fmt.Sprint(held), "garage", "X"), strings.ReplaceAll(fmt.Sprint(res), name, "X"))
	}
	panicked := call("boom")
	assert.Equal(t, "complete", panicked["resultType"])
	assert.Equal(t, "failed", errorCode(resultBody(t, panicked)))

	for _, name := range []string{"garage", "nothing_here", "boom"} {
		res, _ := called(t, e, callToken, name, `{"door":"left"}`)
		assert.NotContains(t, res, "resultType", "%s at 2025-06-18", name)
	}
}

// Internal failures are logged; the agent sees no details.
func TestInternalFailuresAreLoggedNotShown(t *testing.T) {
	e := newTestEnv(t)
	e.tools.setTools(2, testTool(t, "garage"))
	e.tools.call = func(context.Context, service.Actor, service.ToolRef, map[string]any) (flow.CallResult, error) {
		return flow.CallResult{}, errors.New("disk I/O error at /var/lib/piworker")
	}
	res, structured := called(t, e, callToken, "garage", `{"door":"left"}`)
	assert.Equal(t, true, res["isError"])
	assert.Equal(t, "failed", errorCode(structured))
	assert.NotContains(t, fmt.Sprint(res), "/var/lib")
	assert.Contains(t, e.logs.String(), "disk I/O error")
}

// pw_get_result reads the result of the token's own run through the service,
// waiting a bounded time for a running call.
func TestGetResult(t *testing.T) {
	e := newTestEnv(t, func(o *Options) { o.ResultWait = 2 * time.Second })
	var mu sync.Mutex
	var gotActor service.Actor
	var gotRun string
	var deadline time.Duration
	e.tools.result = func(ctx context.Context, actor service.Actor, runID string) (flow.CallResult, error) {
		mu.Lock()
		defer mu.Unlock()
		gotActor, gotRun = actor, runID
		if d, ok := ctx.Deadline(); ok {
			deadline = time.Until(d)
		}
		if runID == "foreign" {
			return flow.CallResult{}, &service.ToolError{Kind: service.ErrRunNotFound, Message: "unknown run"}
		}
		return flow.CallResult{Status: flow.CallStatusCompleted, RunID: runID, Result: json.RawMessage(`{"ok":true}`)}, nil
	}
	res, structured := called(t, e, callToken, "pw_get_result", `{"runId":"run-1"}`)
	assert.NotEqual(t, true, res["isError"])
	assert.Equal(t, "completed", structured["status"])
	assert.Equal(t, map[string]any{"ok": true}, structured["result"])
	assert.Equal(t, "callcallcallcall", gotActor.TokenID)
	assert.Equal(t, "run-1", gotRun)
	assert.Greater(t, deadline, time.Duration(0))
	assert.LessOrEqual(t, deadline, 2*time.Second, "a running call is waited for a bounded time")

	res, structured = called(t, e, callToken, "pw_get_result", `{"runId":"foreign"}`)
	assert.Equal(t, true, res["isError"])
	assert.Equal(t, "run_not_found", errorCode(structured))

	res, structured = called(t, e, callToken, "pw_get_result", `{}`)
	assert.Equal(t, true, res["isError"])
	assert.Equal(t, "invalid_arguments", errorCode(structured))
	issues := structured["error"].(map[string]any)["issues"].([]any)
	assert.Equal(t, "/runId", issues[0].(map[string]any)["pointer"])
}

// echoTool is a built-in tool of the read scope for the registration tests.
func echoTool() StaticTool {
	return StaticTool{
		Name:        "pw_echo",
		Title:       "Echo",
		Description: "Answers its arguments and the calling token.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"word":{"type":"string","enum":["hi","ho"]}},"required":["word"]}`),
		Annotations: flow.Annotations{ReadOnlyHint: true, IdempotentHint: true},
		Scope:       storage.ScopeRead,
		Handler: func(_ context.Context, actor service.Actor, args map[string]any) (*mcp.CallToolResult, error) {
			if args["word"] == "ho" {
				return nil, &service.ToolError{Kind: service.ErrUnavailable, Message: "no ho today"}
			}
			return StructuredResult(map[string]any{"word": args["word"], "token": actor.TokenName})
		},
	}
}

// Built-in tools are registered with a scope and a handler taking the
// actor and the validated arguments.
func TestStaticTools(t *testing.T) {
	e := newTestEnv(t, func(o *Options) { o.StaticTools = []StaticTool{echoTool()} })
	tools := listed(t, e, readToken)
	require.Contains(t, tools, "pw_echo")
	assert.Equal(t, "Echo", tools["pw_echo"]["title"])
	assert.NotContains(t, listed(t, e, callToken), "pw_echo", "the call scope does not bring read tools")
	assert.Contains(t, listed(t, e, otherToken), "pw_echo")

	res, structured := called(t, e, readToken, "pw_echo", `{"word":"hi"}`)
	assert.NotEqual(t, true, res["isError"])
	assert.Equal(t, map[string]any{"word": "hi", "token": "reader"}, structured)

	res, structured = called(t, e, readToken, "pw_echo", `{"word":"ha"}`)
	assert.Equal(t, true, res["isError"])
	assert.Equal(t, "invalid_arguments", errorCode(structured))

	res, structured = called(t, e, readToken, "pw_echo", `{"word":"ho"}`)
	assert.Equal(t, true, res["isError"])
	assert.Equal(t, "unavailable", errorCode(structured))

	// A built-in tool of a scope the token lacks is unknown to it: called,
	// it gets exactly the answer (and the audit) of a name that does not
	// exist, from the tool service.
	e.tools.call = func(_ context.Context, _ service.Actor, ref service.ToolRef, _ map[string]any) (flow.CallResult, error) {
		return flow.CallResult{}, &service.ToolError{Kind: service.ErrToolNotFound, Message: fmt.Sprintf("unknown tool %q", ref.Name)}
	}
	hidden, _ := called(t, e, callToken, "pw_echo", `{"word":"hi"}`)
	unknown, _ := called(t, e, callToken, "pw_nothing", `{"word":"hi"}`)
	assert.Equal(t, true, hidden["isError"])
	assert.Equal(t, strings.ReplaceAll(fmt.Sprint(hidden), "pw_echo", "X"), strings.ReplaceAll(fmt.Sprint(unknown), "pw_nothing", "X"))
	assert.EqualValues(t, 2, e.tools.calls.Load())
}

// A built-in tool's arguments outside its schema are refused before its
// handler runs, and counted like the tool service's invalid_arguments
// rejections (the audit's per-token counters).
func TestStaticToolRefusalsAreCounted(t *testing.T) {
	rejections := &fakeRejections{}
	e := newTestEnv(t, func(o *Options) {
		o.StaticTools = []StaticTool{echoTool()}
		o.Rejections = rejections
	})
	for _, args := range []string{`{"word":"ha"}`, `[1]`, `{}`} {
		res, body := called(t, e, readToken, "pw_echo", args)
		assert.Equal(t, true, res["isError"], args)
		assert.Equal(t, "invalid_arguments", errorCode(body), args)
	}
	called(t, e, readToken, "pw_echo", `{"word":"hi"}`)
	want := "readreadreadread invalid_arguments " + t0.Format(time.RFC3339)
	assert.Equal(t, []string{want, want, want}, rejections.seen)
}

// An isError result a built-in tool's handler builds itself also reaches
// the client without structured content (the tool's output schema would
// not describe it).
func TestStaticToolErrorResultsCarryNoStructuredContent(t *testing.T) {
	st := echoTool()
	st.OutputSchema = json.RawMessage(`{"type":"object","required":["word"],"properties":{"word":{"type":"string"}}}`)
	st.Handler = func(context.Context, service.Actor, map[string]any) (*mcp.CallToolResult, error) {
		res, err := StructuredResult(map[string]any{"status": "error", "error": map[string]any{"code": "x"}})
		res.IsError = true
		return res, err
	}
	e := newTestEnv(t, func(o *Options) { o.StaticTools = []StaticTool{st} })
	res, body := called(t, e, readToken, "pw_echo", `{"word":"hi"}`)
	assert.Equal(t, true, res["isError"])
	assert.Equal(t, "x", errorCode(body), "the text block keeps the envelope")
}

func TestStaticToolRegistrationIsChecked(t *testing.T) {
	base := func() Options {
		return Options{Tools: &fakeTools{}, Tokens: newFakeVerifier()}
	}
	for name, mutate := range map[string]func(*StaticTool){
		"no pw_ prefix": func(st *StaticTool) { st.Name = "echo" },
		"no scope":      func(st *StaticTool) { st.Scope = "" },
		"no handler":    func(st *StaticTool) { st.Handler = nil },
		"bad schema": func(st *StaticTool) {
			st.InputSchema = json.RawMessage(`{"type":"object","properties":{"x":{"anyOf":[]}}}`)
		},
		"no description":  func(st *StaticTool) { st.Description = "" },
		"bad output":      func(st *StaticTool) { st.OutputSchema = json.RawMessage(`{"type":`) },
		"taken name":      func(st *StaticTool) { st.Name = "pw_get_result" },
		"unknown scope x": func(st *StaticTool) { st.Scope = "admin" },
	} {
		t.Run(name, func(t *testing.T) {
			st := echoTool()
			mutate(&st)
			opts := base()
			opts.StaticTools = []StaticTool{st}
			_, err := New(opts)
			assert.Error(t, err)
		})
	}
	opts := base()
	opts.StaticTools = []StaticTool{echoTool(), echoTool()}
	_, err := New(opts)
	assert.Error(t, err, "registered twice")
}

// Prompts are registered with a scope too, and get the actor.
func TestStaticPrompts(t *testing.T) {
	prompt := StaticPrompt{
		Prompt: &mcp.Prompt{Name: "diagnose", Description: "Diagnose a flow", Arguments: []*mcp.PromptArgument{{Name: "flowId", Required: true}}},
		Scope:  storage.ScopeRead,
		Handler: func(_ context.Context, actor service.Actor, args map[string]string) (*mcp.GetPromptResult, error) {
			return &mcp.GetPromptResult{Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: actor.TokenName + " asks about " + args["flowId"]}}}}, nil
		},
	}
	e := newTestEnv(t, func(o *Options) { o.Prompts = []StaticPrompt{prompt} })
	list := `{"jsonrpc":"2.0","id":5,"method":"prompts/list"}`
	res, rpcErr := rpcResult(t, e.serve(withToken(rpc(list), readToken)))
	require.Nil(t, rpcErr)
	require.Len(t, res["prompts"], 1)
	assert.Equal(t, "private", res["cacheScope"])
	res, rpcErr = rpcResult(t, e.serve(withToken(rpc(list), callToken)))
	require.Nil(t, rpcErr)
	assert.Empty(t, res["prompts"])

	get := `{"jsonrpc":"2.0","id":6,"method":"prompts/get","params":{"name":"diagnose","arguments":{"flowId":"f1"}}}`
	res, rpcErr = rpcResult(t, e.serve(withToken(rpc(get), readToken)))
	require.Nil(t, rpcErr)
	msg := res["messages"].([]any)[0].(map[string]any)
	assert.Equal(t, "reader asks about f1", msg["content"].(map[string]any)["text"])
}

// A prompt's refusal (a *service.ToolError) is a JSON-RPC invalid-params
// error with its message; any other failure is logged and answered as an
// internal error without details.
func TestStaticPromptErrors(t *testing.T) {
	prompt := StaticPrompt{
		Prompt: &mcp.Prompt{Name: "diagnose", Description: "Diagnose a flow"},
		Scope:  storage.ScopeRead,
		Handler: func(_ context.Context, _ service.Actor, args map[string]string) (*mcp.GetPromptResult, error) {
			if args["flowId"] == "gone" {
				return nil, &service.ToolError{Kind: service.ErrFlowNotFound, Message: `unknown flow "gone"`}
			}
			return nil, errors.New("database is locked at /var/lib/piworker/piworker.db")
		},
	}
	e := newTestEnv(t, func(o *Options) { o.Prompts = []StaticPrompt{prompt} })
	get := func(flowID string) map[string]any {
		_, rpcErr := rpcResult(t, e.serve(withToken(rpc(`{"jsonrpc":"2.0","id":6,"method":"prompts/get","params":{"name":"diagnose","arguments":{"flowId":"`+flowID+`"}}}`), readToken)))
		require.NotNil(t, rpcErr)
		return rpcErr
	}
	refused := get("gone")
	assert.EqualValues(t, -32602, refused["code"])
	assert.Equal(t, `unknown flow "gone"`, refused["message"])

	failed := get("f1")
	assert.EqualValues(t, -32603, failed["code"])
	assert.NotContains(t, fmt.Sprint(failed), "database")
	assert.Contains(t, e.logs.String(), "database is locked")
}

// One tool that cannot be built is skipped and logged; the others are
// served.
func TestABadToolIsSkipped(t *testing.T) {
	e := newTestEnv(t)
	broken := testTool(t, "broken")
	broken.Schema = nil // panics while building
	badOutput := testTool(t, "bad_output")
	badOutput.OutputSchema = json.RawMessage(`{"type":"object","properties":{"x":{"type":7}}}`)
	e.tools.setTools(2, broken, testTool(t, "garage"), badOutput)
	tools := listed(t, e, callToken)
	assert.Contains(t, tools, "garage")
	assert.NotContains(t, tools, "broken")
	assert.NotContains(t, tools, "bad_output")
	assert.Contains(t, e.logs.String(), "broken")
	assert.Contains(t, e.logs.String(), "bad_output")

	// A skipped tool is not callable either: it is answered as unknown
	// without reaching the service.
	for _, name := range []string{"broken", "bad_output"} {
		res, structured := called(t, e, callToken, name, `{"door":"left"}`)
		assert.Equal(t, true, res["isError"], name)
		assert.Equal(t, "not_found", errorCode(structured), name)
	}
	assert.Zero(t, e.tools.calls.Load())
}

// Servers are cached per token and rebuilt when what the token may see
// changes: a redeploy (tools version), a grant change (token info) or a
// tokens epoch change (e.g. the agent deploy level).
func TestServerCache(t *testing.T) {
	e := newTestEnv(t)
	e.tools.setTools(2, testTool(t, "garage"))
	first := e.h.serverFor(e.verified(callToken))
	require.NotNil(t, first)
	assert.Same(t, first, e.h.serverFor(e.verified(callToken)), "cached")
	assert.NotSame(t, first, e.h.serverFor(e.verified(otherToken)), "per token")

	e.tools.setTools(3, testTool(t, "garage"), testTool(t, "gate"))
	redeployed := e.h.serverFor(e.verified(callToken))
	assert.NotSame(t, first, redeployed, "a redeploy rebuilds")
	assert.Contains(t, listed(t, e, callToken), "gate")

	e.verifier.set(callToken, func(info *agentauth.TokenInfo) {
		info.Grants = []agentauth.Grant{{FlowID: "flow-gate", TriggerNodeID: "tool", Fingerprint: "sha256:x"}}
	})
	regranted := e.h.serverFor(e.verified(callToken))
	assert.NotSame(t, redeployed, regranted, "a grant change rebuilds")

	e.verifier.bumpEpoch()
	rebuilt := e.h.serverFor(e.verified(callToken))
	assert.NotSame(t, regranted, rebuilt, "an epoch change rebuilds")

	// The server's handlers act as the actor it was built for, so any
	// change of the actor rebuilds it (here its name, shown in run history).
	e.verifier.set(callToken, func(info *agentauth.TokenInfo) { info.Name = "renamed" })
	assert.NotSame(t, rebuilt, e.h.serverFor(e.verified(callToken)), "a renamed token rebuilds")
	assert.Equal(t, 2, e.h.servers.len(), "one server per token")
}

// The cache holds at most MaxServers servers and drops the least recently
// used one first.
func TestServerCacheIsBounded(t *testing.T) {
	e := newTestEnv(t, func(o *Options) { o.MaxServers = 2 })
	token := func(name string) verified { return e.verified(e.verifier.add(fmt.Sprintf("%-16s", name))) }
	a, b, c := token("a"), token("b"), token("c")
	firstA, firstB := e.h.serverFor(a), e.h.serverFor(b)
	require.Same(t, firstA, e.h.serverFor(a), "a is now the most recently used")
	e.h.serverFor(c) // evicts b, the least recently used
	assert.Equal(t, 2, e.h.servers.len())
	assert.Same(t, firstA, e.h.serverFor(a), "a stayed")
	assert.NotSame(t, firstB, e.h.serverFor(b), "b was dropped and is rebuilt")
	assert.Equal(t, 2, e.h.servers.len())

	for i := range 10 {
		e.h.serverFor(token(fmt.Sprint(i)))
		require.LessOrEqual(t, e.h.servers.len(), 2)
	}
}

// Concurrent requests of many tokens are safe.
func TestServerCacheConcurrency(t *testing.T) {
	e := newTestEnv(t, func(o *Options) { o.MaxServers = 3; o.RequestBurst = 1000 })
	e.tools.setTools(2, testTool(t, "garage"))
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			raw := []string{callToken, otherToken, readToken}[i%3]
			for range 10 {
				w := e.serve(withToken(rpc(listToolsBody), raw))
				assert.Equal(t, http.StatusOK, w.Code)
			}
			if i%4 == 0 {
				e.tools.setTools(uint64(10+i), testTool(t, "garage"))
			}
		})
	}
	wg.Wait()
}

// Tool schemas are advertised as compiled (toolschema), never as written.
func TestStaticToolSchemaIsCanonical(t *testing.T) {
	e := newTestEnv(t, func(o *Options) { o.StaticTools = []StaticTool{echoTool()} })
	schema, err := toolschema.Compile(echoTool().InputSchema)
	require.NoError(t, err)
	got, err := json.Marshal(listed(t, e, readToken)["pw_echo"]["inputSchema"])
	require.NoError(t, err)
	assert.JSONEq(t, string(schema.JSON()), string(got))
}

// Names no tool can have (trigger-tool names are 1 to 128 letters, digits,
// '_', '.' or '-') are answered here as unknown, echoed shortened, without
// reaching the service: an agent cannot have a huge name audited or echoed
// back. Valid names still go to the service, hidden or unknown alike.
func TestImpossibleToolNamesAreUnknown(t *testing.T) {
	e := newTestEnv(t)
	for _, name := range []string{strings.Repeat("x", 200_000), strings.Repeat("a", 129), "garage door", "ga/rage", "é"} {
		w := e.serve(withToken(rpc(callBody(name, `{}`)), callToken))
		res, rpcErr := rpcResult(t, w)
		require.Nil(t, rpcErr)
		assert.Equal(t, true, res["isError"])
		assert.Equal(t, "not_found", errorCode(resultBody(t, res)))
		assert.Less(t, w.Body.Len(), 2048, "the name is echoed shortened")
	}
	assert.Zero(t, e.tools.calls.Load())

	e.tools.call = func(_ context.Context, _ service.Actor, ref service.ToolRef, _ map[string]any) (flow.CallResult, error) {
		return flow.CallResult{}, &service.ToolError{Kind: service.ErrToolNotFound, Message: fmt.Sprintf("unknown tool %q", ref.Name)}
	}
	longest := strings.Repeat("a", 128)
	unknown, _ := called(t, e, callToken, longest, `{}`)
	assert.EqualValues(t, 1, e.tools.calls.Load(), "a possible name is the service's to answer")
	impossible, _ := called(t, e, callToken, longest+"a", `{}`)
	assert.Equal(t, strings.ReplaceAll(fmt.Sprint(unknown), longest, "X"), strings.ReplaceAll(fmt.Sprint(impossible), longest+"…", "X"),
		"an impossible name looks like any unknown one")
}

// The SDK runs every handler on a goroutine of its own, out of reach of
// the router's recovery: a panic in a tool call (flow tool, unheld name or
// built-in tool) or a prompt is recovered and logged, the agent gets an
// error without details, and the process keeps serving.
func TestPanicsAreRecovered(t *testing.T) {
	boom := func() { var m map[string]int; m["x"] = 1 }
	prompt := StaticPrompt{
		Prompt: &mcp.Prompt{Name: "boom", Description: "Panics"},
		Scope:  storage.ScopeRead,
		Handler: func(context.Context, service.Actor, map[string]string) (*mcp.GetPromptResult, error) {
			boom()
			return nil, nil
		},
	}
	static := echoTool()
	static.Name, static.Scope = "pw_boom", storage.ScopeCall
	static.Handler = func(context.Context, service.Actor, map[string]any) (*mcp.CallToolResult, error) {
		boom()
		return nil, nil
	}
	e := newTestEnv(t, func(o *Options) {
		o.Prompts = []StaticPrompt{prompt}
		o.StaticTools = []StaticTool{static}
	})
	e.tools.setTools(2, testTool(t, "garage"))
	e.tools.call = func(context.Context, service.Actor, service.ToolRef, map[string]any) (flow.CallResult, error) {
		panic("service exploded at /var/lib/piworker")
	}

	for _, tc := range []struct{ name, args string }{{"garage", `{"door":"left"}`}, {"no_such", `{}`}, {"pw_boom", `{"word":"hi"}`}} {
		res, structured := called(t, e, callToken, tc.name, tc.args)
		assert.Equal(t, true, res["isError"], tc.name)
		assert.Equal(t, "failed", errorCode(structured), tc.name)
		assert.NotContains(t, fmt.Sprint(res), "/var/lib", tc.name)
	}

	get := `{"jsonrpc":"2.0","id":6,"method":"prompts/get","params":{"name":"boom"}}`
	_, rpcErr := rpcResult(t, e.serve(withToken(rpc(get), readToken)))
	require.NotNil(t, rpcErr)
	assert.EqualValues(t, -32603, rpcErr["code"])
	assert.NotContains(t, fmt.Sprint(rpcErr), "nil map")

	logs := e.logs.String()
	assert.Contains(t, logs, "service exploded")
	assert.Contains(t, logs, "nil map")
	assert.Contains(t, logs, `"tool":"garage"`)
	assert.Contains(t, logs, `"prompt":"boom"`)
	assert.Contains(t, logs, `"tokenId":"callcallcallcall"`)

	_, rpcErr = rpcResult(t, e.serve(withToken(rpc(listToolsBody), callToken)))
	assert.Nil(t, rpcErr, "still serving")
}
