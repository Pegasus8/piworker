package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/toolschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shapeSchema is a schema outside toolschema's closed subset: a free-form
// object nested in an array of objects.
const shapeSchema = `{"type":"object","properties":{"shapes":{"type":"array","maxItems":3,` +
	`"items":{"type":"object","properties":{"kind":{"type":"string"},"props":{"type":"object"}},"required":["kind"]}}},` +
	`"required":["shapes"],"additionalProperties":false}`

// shapeTool is a built-in tool of the read scope with a raw input schema
// and a validator; it records the arguments its handler got.
func shapeTool(got *[]map[string]any, mu *sync.Mutex) StaticTool {
	return StaticTool{
		Name:        "pw_shapes",
		Title:       "Shapes",
		Description: "Counts the shapes it is given.",
		RawInput: &RawInput{
			Schema:   json.RawMessage(shapeSchema),
			MaxBytes: 200,
			Validate: func(args map[string]any) []toolschema.Issue {
				var issues []toolschema.Issue
				for name := range args {
					if name != "shapes" {
						issues = append(issues, toolschema.Issue{Pointer: "/" + name, Message: "unknown property"})
					}
				}
				shapes, ok := args["shapes"].([]any)
				if !ok {
					return append(issues, toolschema.Issue{Pointer: "/shapes", Message: "must be an array"})
				}
				for i, s := range shapes {
					if m, ok := s.(map[string]any); !ok || m["kind"] == nil {
						issues = append(issues, toolschema.Issue{Pointer: "/shapes/" + string(rune('0'+i)), Message: "needs a kind"})
					}
				}
				return issues
			},
		},
		Annotations: flow.Annotations{ReadOnlyHint: true, IdempotentHint: true},
		Scope:       storage.ScopeRead,
		Handler: func(_ context.Context, _ service.Actor, args map[string]any) (*mcp.CallToolResult, error) {
			mu.Lock()
			*got = append(*got, args)
			mu.Unlock()
			return StructuredResult(map[string]any{"count": len(args["shapes"].([]any))})
		},
	}
}

// A built-in tool whose arguments do not fit the closed subset advertises
// its raw schema exactly as written; its arguments are bounded in size and
// checked by its Go validator before the handler runs.
func TestRawInputTools(t *testing.T) {
	var mu sync.Mutex
	var got []map[string]any
	rejections := &fakeRejections{}
	e := newTestEnv(t, func(o *Options) {
		o.StaticTools = []StaticTool{shapeTool(&got, &mu), echoTool()}
		o.Rejections = rejections
	})

	tools := listed(t, e, readToken)
	schema, err := json.Marshal(tools["pw_shapes"]["inputSchema"])
	require.NoError(t, err)
	assert.JSONEq(t, shapeSchema, string(schema), "advertised as written")
	echo, err := json.Marshal(tools["pw_echo"]["inputSchema"])
	require.NoError(t, err)
	compiled, err := toolschema.Compile(echoTool().InputSchema)
	require.NoError(t, err)
	assert.JSONEq(t, string(compiled.JSON()), string(echo), "closed-subset tools are unchanged")

	res, body := called(t, e, readToken, "pw_shapes", `{"shapes":[{"kind":"circle","props":{"r":{"deep":[1,2]}}}]}`)
	assert.NotEqual(t, true, res["isError"])
	assert.Equal(t, map[string]any{"count": float64(1)}, body)
	require.Len(t, got, 1)
	assert.Equal(t, map[string]any{"r": map[string]any{"deep": []any{float64(1), float64(2)}}},
		got[0]["shapes"].([]any)[0].(map[string]any)["props"])

	for _, tc := range []struct {
		args, pointer string
	}{
		{`{"shapes":[{"props":{}}]}`, "/shapes/0"},
		{`{"shapes":[],"extra":1}`, "/extra"},
		{`{"shapes":"x"}`, "/shapes"},
		{`[1]`, ""},
		{`{"shapes":[{"kind":"` + strings.Repeat("x", 200) + `"}]}`, ""},
	} {
		res, body := called(t, e, readToken, "pw_shapes", tc.args)
		assert.Equal(t, true, res["isError"], tc.args)
		assert.Equal(t, "invalid_arguments", errorCode(body), tc.args)
		issues := body["error"].(map[string]any)["issues"].([]any)
		require.NotEmpty(t, issues, tc.args)
		assert.Equal(t, tc.pointer, issues[0].(map[string]any)["pointer"], tc.args)
	}
	assert.Len(t, got, 1, "refused arguments never reach the handler")
	want := "readreadreadread invalid_arguments " + t0.Format(time.RFC3339)
	assert.Equal(t, []string{want, want, want, want, want}, rejections.seen, "every refusal is counted")

	// The size bound is reported without echoing the arguments.
	res, body = called(t, e, readToken, "pw_shapes", `{"shapes":[{"kind":"`+strings.Repeat("y", 300)+`"}]}`)
	assert.Equal(t, true, res["isError"])
	issue := body["error"].(map[string]any)["issues"].([]any)[0].(map[string]any)
	assert.Contains(t, issue["message"], "200 bytes")
	assert.NotContains(t, issue["message"], "yyy")
}

// A raw input schema must be a JSON object schema of type object, within
// bounds, with a size bound and a validator; a tool has exactly one input
// contract.
func TestRawInputRegistrationIsChecked(t *testing.T) {
	var mu sync.Mutex
	var got []map[string]any
	for name, mutate := range map[string]func(*StaticTool){
		"both contracts": func(st *StaticTool) { st.InputSchema = json.RawMessage(noArguments) },
		"not JSON":       func(st *StaticTool) { st.RawInput.Schema = json.RawMessage(`{"type":`) },
		"not an object":  func(st *StaticTool) { st.RawInput.Schema = json.RawMessage(`{"type":"array"}`) },
		"not a schema": func(st *StaticTool) {
			st.RawInput.Schema = json.RawMessage(`{"type":"object","properties":{"x":{"type":7}}}`)
		},
		"no size bound":   func(st *StaticTool) { st.RawInput.MaxBytes = 0 },
		"too large bound": func(st *StaticTool) { st.RawInput.MaxBytes = DefaultMaxRequestBodyBytes + 1 },
		"no validator":    func(st *StaticTool) { st.RawInput.Validate = nil },
		"huge schema": func(st *StaticTool) {
			st.RawInput.Schema = json.RawMessage(`{"type":"object","description":"` + strings.Repeat("d", MaxRawInputSchemaBytes) + `"}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			st := shapeTool(&got, &mu)
			mutate(&st)
			_, err := New(Options{Tools: &fakeTools{}, Tokens: newFakeVerifier(), StaticTools: []StaticTool{st}})
			assert.Error(t, err)
		})
	}
	_, err := New(Options{Tools: &fakeTools{}, Tokens: newFakeVerifier(), StaticTools: []StaticTool{shapeTool(&got, &mu)}})
	assert.NoError(t, err)
}

// The structured content of a built-in tool's successful result is checked
// against its output schema before it is sent: clients refuse a result
// that does not match (a protocol error the agent cannot read), so a
// mismatch is logged and answered as a failure.
func TestStaticToolOutputIsCheckedAgainstItsSchema(t *testing.T) {
	st := echoTool()
	st.OutputSchema = json.RawMessage(`{"type":"object","required":["word"],"properties":{"word":{"type":"string"},"token":{"type":"string"}}}`)
	st.Handler = func(_ context.Context, _ service.Actor, args map[string]any) (*mcp.CallToolResult, error) {
		if args["word"] == "ho" {
			return StructuredResult(map[string]any{"word": 7})
		}
		return StructuredResult(map[string]any{"word": args["word"]})
	}
	e := newTestEnv(t, func(o *Options) { o.StaticTools = []StaticTool{st} })
	res, body := called(t, e, readToken, "pw_echo", `{"word":"hi"}`)
	assert.NotEqual(t, true, res["isError"])
	assert.Equal(t, map[string]any{"word": "hi"}, body)

	res, body = called(t, e, readToken, "pw_echo", `{"word":"ho"}`)
	assert.Equal(t, true, res["isError"])
	assert.Equal(t, "failed", errorCode(body))
	assert.Contains(t, e.logs.String(), "does not match its output schema")
}
