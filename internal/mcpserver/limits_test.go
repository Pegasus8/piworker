package mcpserver

import (
	"encoding/json"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flood repeats item (separated by commas) to fill about size bytes.
func flood(item string, size int) string {
	n := size / (len(item) + 1)
	return strings.TrimSuffix(strings.Repeat(item+",", n), ",")
}

// floodBody builds a message of about DefaultMaxRequestBodyBytes: prefix,
// then a flood of item, then suffix.
func floodBody(prefix, item, suffix string) string {
	return prefix + flood(item, DefaultMaxRequestBodyBytes-len(prefix)-len(suffix)-64) + suffix
}

// The body limit bounds a message's bytes, not what decoding it costs: 1 MiB
// of tiny values ("{}" becomes a Go map) costs hundreds of MiB once the SDK
// and the endpoint have decoded it. Such messages are refused before
// anything decodes them, whatever the method, the token's scopes or the
// tool, and counted as the token's rejections.
func TestMessagesOfTooManyValuesAreRefusedBeforeDecoding(t *testing.T) {
	rejections := &fakeRejections{}
	e := newTestEnv(t, func(o *Options) { o.Rejections = rejections; o.RequestBurst = 100 })
	e.tools.setTools(2, testTool(t, "garage"))
	metaFlood := func(item string) string {
		return floodBody(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"_meta":{"x":[`, item, `]}}}`)
	}
	argsFlood := func(name, item string) string {
		return floodBody(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"`+name+`","arguments":{"x":[`, item, `]}}}`)
	}
	for name, tc := range map[string]struct{ token, body, why string }{
		// _meta is also bounded in bytes (TestLargeMetaIsRefused).
		"tools/list, _meta of objects":    {readToken, metaFlood("{}"), "_meta"},
		"tools/list, _meta of numbers":    {readToken, metaFlood("0"), "_meta"},
		"tools/list, _meta of lists":      {readToken, metaFlood("[]"), "_meta"},
		"flow tool, arguments of objects": {callToken, argsFlood("garage", "{}"), "values"},
		"flow tool, arguments of names":   {callToken, argsFlood("garage", `{"a":0,"b":1}`), "values"},
		"unknown tool":                    {callToken, argsFlood("nope", "{}"), "values"},
		"initialize":                      {callToken, floodBody(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{"x":[`, "{}", `]},"clientInfo":{"name":"t","version":"1"}}}`), "values"},
	} {
		t.Run(name, func(t *testing.T) {
			require.Less(t, len(tc.body), DefaultMaxRequestBodyBytes)
			id, code, msg := rpcRefusal(t, e.serve(withToken(rpc(tc.body), tc.token)), http.StatusRequestEntityTooLarge)
			assert.Nil(t, id)
			assert.EqualValues(t, -32600, code)
			assert.Contains(t, msg, "Request too large")
			assert.Contains(t, msg, tc.why)
		})
	}
	// A flood of values outside _meta and the arguments too.
	body := floodBody(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"x":[`, "{}", `]}}`)
	_, _, msg := rpcRefusal(t, e.serve(withToken(rpc(body), readToken)), http.StatusRequestEntityTooLarge)
	assert.Contains(t, msg, "values")
	assert.Zero(t, e.tools.calls.Load(), "no call ran")
	rejections.mu.Lock()
	defer rejections.mu.Unlock()
	require.Len(t, rejections.seen, 8)
	assert.Contains(t, rejections.seen[0], " request_too_large ")
}

// Nesting is bounded too.
func TestDeeplyNestedMessagesAreRefused(t *testing.T) {
	e := newTestEnv(t)
	deep := strings.Repeat("[", MaxMessageDepth) + strings.Repeat("]", MaxMessageDepth)
	body := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"_meta":{"x":` + deep + `}}}`
	_, code, msg := rpcRefusal(t, e.serve(withToken(rpc(body), readToken)), http.StatusRequestEntityTooLarge)
	assert.EqualValues(t, -32600, code)
	assert.Contains(t, msg, "nest")

	// Within the bound it is served.
	ok := strings.Repeat("[", MaxMessageDepth-4) + strings.Repeat("]", MaxMessageDepth-4)
	body = `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"_meta":{"x":` + ok + `}}}`
	_, rpcErr := rpcResult(t, e.serve(withToken(rpc(body), readToken)))
	assert.Nil(t, rpcErr)
}

// params._meta (progress tokens, client hints), which the SDK decodes on
// every request, is bounded in bytes.
func TestLargeMetaIsRefused(t *testing.T) {
	e := newTestEnv(t)
	for name, meta := range map[string]string{
		"object": `{"x":"` + strings.Repeat("a", MaxMetaBytes) + `"}`,
		"string": `"` + strings.Repeat("a", MaxMetaBytes) + `"`,
	} {
		t.Run(name, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"_meta":` + meta + `}}`
			_, code, msg := rpcRefusal(t, e.serve(withToken(rpc(body), readToken)), http.StatusRequestEntityTooLarge)
			assert.EqualValues(t, -32600, code)
			assert.Contains(t, msg, "_meta")
		})
	}
	body := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"_meta":{"progressToken":"p1","x":"` + strings.Repeat("a", MaxMetaBytes-64) + `"}}}`
	_, rpcErr := rpcResult(t, e.serve(withToken(rpc(body), readToken)))
	assert.Nil(t, rpcErr)
}

// Messages within the bounds are served as before, large ones included: a
// long string costs about its size, and a flow-sized document of values
// stays under the value budget.
func TestMessagesWithinTheBoundsAreServed(t *testing.T) {
	e := newTestEnv(t)
	e.tools.setTools(2, testTool(t, "garage"))
	items := make([]string, 0, MaxMessageValues/2)
	for range MaxMessageValues/2 - 64 {
		items = append(items, "1")
	}
	args := `{"door":"left","x":[` + strings.Join(items, ",") + `],"s":"` + strings.Repeat("y", 512<<10) + `"}`
	res, rpcErr := rpcResult(t, e.serve(withToken(rpc(callBody("garage", args)), callToken)))
	require.Nil(t, rpcErr)
	assert.NotNil(t, res)
	assert.Equal(t, int32(1), e.tools.calls.Load())

	// Duplicate names and syntax errors are still parse errors.
	w := e.serve(withToken(rpc(callBody("garage", `{"door":"left","door":"right"}`)), callToken))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"code":-32700`)
	w = e.serve(withToken(rpc(`{"jsonrpc":"2.0","id":2,"method":"tools/list"`), callToken))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"code":-32700`)
}

// A token's whole burst of floods (10 concurrent requests of 1 MiB of tiny
// values, in _meta and in a flow tool's arguments) allocates little more
// than the bodies themselves: nothing decodes them. Before the bound, such a
// burst allocated over 600 MiB.
func TestAFloodBurstAllocatesLittle(t *testing.T) {
	e := newTestEnv(t)
	e.tools.setTools(2, testTool(t, "garage"))
	bodies := []string{
		floodBody(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"_meta":{"x":[`, "{}", `]}}}`),
		floodBody(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"garage","arguments":{"x":[`, "{}", `]}}}`),
	}
	reqs := make([]*http.Request, 0, DefaultRequestBurst)
	for i := range DefaultRequestBurst {
		token := readToken
		if i%2 == 1 {
			token = callToken
		}
		reqs = append(reqs, withToken(rpc(bodies[i%2]), token))
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var wg sync.WaitGroup
	codes := make([]int, len(reqs))
	for i, r := range reqs {
		wg.Go(func() { codes[i] = e.serve(r).Code })
	}
	wg.Wait()
	runtime.ReadMemStats(&after)
	for _, c := range codes {
		assert.Equal(t, http.StatusRequestEntityTooLarge, c)
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("allocated %d MiB for %d flood requests", allocated>>20, len(reqs))
	assert.Less(t, allocated, uint64(64<<20), "the floods were decoded")
	assert.Zero(t, e.tools.calls.Load())
}

// The largest flow pw_save_draft takes (200 nodes and 400 connections, each
// node with a few settings, a nested one and a tool's input schema among
// them) fits the value budget twice over.
func TestTheLargestFlowsFitTheValueBudget(t *testing.T) {
	nodes := make([]any, 0, 200)
	for i := range 200 {
		cfg := map[string]any{
			"url":     "https://api.example.com/v1/items/{{payload.id}}",
			"method":  "POST",
			"headers": map[string]any{"Content-Type": "application/json", "Authorization": "Bearer {{secret.api}}"},
			"retries": 3,
		}
		if i%20 == 0 {
			props := map[string]any{}
			for p := range 20 {
				props[strings.Repeat("p", p+1)] = map[string]any{"type": "string", "maxLength": 256, "description": "an argument"}
			}
			cfg = map[string]any{"toolName": "tool", "description": "a tool of the flow", "inputSchema": map[string]any{"type": "object", "properties": props}}
		}
		nodes = append(nodes, map[string]any{
			"id": "node_1731000000000_" + strings.Repeat("7", i%5+1), "type": "action-http", "enabled": true,
			"position": map[string]any{"x": 120.5, "y": 340}, "config": cfg,
		})
	}
	conns := make([]any, 0, 400)
	for range 400 {
		conns = append(conns, map[string]any{"id": "edge_a_b_1731000000000", "sourceNode": "node_a", "sourcePort": "output",
			"targetNode": "node_b", "targetPort": "input"})
	}
	flowJSON, err := json.Marshal(map[string]any{"name": "big", "nodes": nodes, "connections": conns})
	require.NoError(t, err)
	body := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"pw_save_draft","arguments":{"note":"n","flow":` +
		string(flowJSON) + `,"twice":` + string(flowJSON) + `}}}`
	assert.Empty(t, scanMessage([]byte(body)))
}

// The refusal is a JSON-RPC error any client can read.
func TestTooLargeRefusalIsJSONRPC(t *testing.T) {
	e := newTestEnv(t)
	body := floodBody(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"_meta":{"x":[`, "{}", `]}}}`)
	start := time.Now()
	w := e.serve(withToken(rpc(body), readToken))
	assert.Less(t, time.Since(start), 5*time.Second)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "2.0", resp["jsonrpc"])
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}
