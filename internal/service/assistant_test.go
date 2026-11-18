package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The flow assistant (docs/design/llm.md §7, ADR-0041) against a fake
// OpenAI-compatible server.

// fakeChat is an OpenAI-compatible chat server: it records each request
// body and answers the next scripted content (the last one again once the
// script runs out). block, when set, holds every request until it closes.
type fakeChat struct {
	mu       sync.Mutex
	requests []map[string]any
	answers  []string
	arrived  chan struct{}
	block    chan struct{}
}

func newFakeChat(t *testing.T, answers ...string) (*fakeChat, *httptest.Server) {
	t.Helper()
	f := &fakeChat{answers: answers, arrived: make(chan struct{}, 16)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		data, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		f.mu.Lock()
		f.requests = append(f.requests, body)
		n := len(f.requests)
		content := f.answers[min(n, len(f.answers))-1]
		block := f.block
		f.mu.Unlock()
		f.arrived <- struct{}{}
		if block != nil {
			select {
			case <-block:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "m-answered",
			"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
			"usage":   map[string]any{"prompt_tokens": 100, "completion_tokens": 20},
		})
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeChat) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeChat) request(i int) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[i]
}

// messages returns the role and content of request i's messages.
func (f *fakeChat) messages(t *testing.T, i int) [][2]string {
	t.Helper()
	var out [][2]string
	for _, m := range f.request(i)["messages"].([]any) {
		msg := m.(map[string]any)
		content, _ := msg["content"].(string)
		out = append(out, [2]string{msg["role"].(string), content})
	}
	return out
}

type assistantNode struct{ outputs []types.Port }

func (n assistantNode) Process(context.Context, *types.Message) ([]*types.Message, error) {
	return nil, nil
}
func (n assistantNode) Ports() ([]types.Port, []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}}, n.outputs
}
func (assistantNode) Validate() error { return nil }

type assistantTrigger struct{ assistantNode }

func (assistantTrigger) Ports() ([]types.Port, []types.Port) {
	return nil, []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny}}
}
func (assistantTrigger) Start(context.Context, chan<- *types.Message) error { return nil }
func (assistantTrigger) Stop() error                                        { return nil }

type assistantLLMNode struct {
	assistantNode
	connection string
}

func (n assistantLLMNode) RequiredLLMConnections() map[string]string {
	return map[string]string{"connection": n.connection}
}

func assistantRegistry(t *testing.T) *node.Registry {
	t.Helper()
	reg := node.NewRegistry()
	out := []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny}}
	in := []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}}
	ptr := func(v float64) *float64 { return &v }
	require.NoError(t, reg.Register(node.NodeTypeInfo{
		Type: "trigger-test", Name: "Test trigger", Description: "Fires now and then.", Category: types.NodeCategoryInput,
		Risk: node.RiskSafe, Outputs: out,
		Documentation: "TRIGGER-DOC-MARKER: long markdown the assistant leaves out.",
		Config: &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
			"interval": {Type: "integer", Title: "Interval", Description: "Seconds between runs.", Default: 5,
				Minimum: ptr(1), Maximum: ptr(3600), Unit: "seconds"},
		}},
	}, func(map[string]any) (node.Node, error) { return assistantTrigger{}, nil }))
	require.NoError(t, reg.Register(node.NodeTypeInfo{
		Type: "process-test", Name: "Test process", Description: "A processing test node.", Category: types.NodeCategoryProcessing,
		Risk: node.RiskSafe, Inputs: in, Outputs: out,
		Config: &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
			"text": {Type: "string", Title: "Text", Description: "A template."},
			"mode": {Type: "string", Title: "Mode", Enum: []string{"upper", "lower"}, Default: "upper"},
		}, Required: []string{"text"}},
	}, func(map[string]any) (node.Node, error) { return assistantNode{outputs: out}, nil }))
	require.NoError(t, reg.Register(node.NodeTypeInfo{
		Type: "action-test", Name: "Test action", Description: "Sends somewhere.", Category: types.NodeCategoryOutput,
		Risk: node.RiskExternal, Inputs: in, Outputs: out,
		Config: &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
			"url":      {Type: "string", Title: "URL", Destination: true},
			"password": {Type: "string", Title: "Password", Sensitive: true},
		}},
	}, func(map[string]any) (node.Node, error) { return assistantNode{outputs: out}, nil }))
	require.NoError(t, reg.Register(node.NodeTypeInfo{
		Type: "process-sort", Name: "Sort", Description: "One port per label.", Category: types.NodeCategoryProcessing,
		Risk: node.RiskSafe, Inputs: in, Outputs: []types.Port{{ID: "other", Name: "Other", DataType: types.DataTypeAny}},
		OutputsFrom: "labels",
		Config: &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
			"labels": {Type: "array", Title: "Labels"},
		}},
	}, func(cfg map[string]any) (node.Node, error) {
		info, _ := reg.Get("process-sort")
		return assistantNode{outputs: node.ConfigOutputs(info, cfg)}, nil
	}))
	require.NoError(t, reg.Register(node.NodeTypeInfo{
		Type: "process-llm-test", Name: "LLM test", Description: "Names an LLM connection.", Category: types.NodeCategoryProcessing,
		Risk: node.RiskExternal, Inputs: in, Outputs: out,
		Config: &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
			"connection": {Type: "string", Title: "Connection", Options: node.OptionsLLMConnections},
		}},
	}, func(cfg map[string]any) (node.Node, error) {
		name, _ := cfg["connection"].(string)
		return assistantLLMNode{assistantNode: assistantNode{outputs: out}, connection: name}, nil
	}))
	return reg
}

// assistantEnv is an assistant over a fake server: the LLM connection
// "local" (its default model m-default) points at it; "cloud" and the broker
// connection "home" are only listed by name.
func assistantEnv(t *testing.T, answers ...string) (*Assistant, *fakeChat, *llm.Store) {
	t.Helper()
	chat, srv := newFakeChat(t, answers...)
	conns := llm.NewStore()
	require.NoError(t, conns.Set(llm.Connection{Name: "local", Provider: llm.ProviderOpenAI, BaseURL: srv.URL + "/v1", Model: "m-default"}))
	require.NoError(t, conns.Set(llm.Connection{Name: "cloud", Provider: llm.ProviderOpenAI,
		BaseURL: "https://cloud.example/v1", APIKey: "cloud-secret-key", Model: "gpt-x"}))
	bks := brokers.NewStore()
	require.NoError(t, bks.Set(brokers.Connection{Name: "home", Broker: "tcp://10.0.0.7:1883", Password: "broker-pass"}))
	a := NewAssistant(AssistantOptions{
		Registry: assistantRegistry(t), LLM: conns, Brokers: bks, Secrets: secrets.NewStore(), Logger: zerolog.Nop(),
	})
	return a, chat, conns
}

const validProposal = `{"name":"Hourly hello","description":"Says hello every hour.","notes":"Define the secret HOOK_PASS.",
"nodes":[{"id":"t","type":"trigger-test","config":{"interval":60}},
{"id":"p","type":"process-test","config":{"text":"hello {{payload}}"}},
{"id":"a","type":"action-test","config":{"url":"http://example.com","password":"{{secret.HOOK_PASS}}"}}],
"connections":[{"sourceNode":"t","sourcePort":"output","targetNode":"p","targetPort":"input"},
{"sourceNode":"p","sourcePort":"output","targetNode":"a","targetPort":"input"}]}`

// brokenProposal connects p to a port action-test does not have.
const brokenProposal = `{"name":"Hourly hello","description":"","notes":"",
"nodes":[{"id":"t","type":"trigger-test","config":{}},
{"id":"p","type":"process-test","config":{"text":"x"}},
{"id":"a","type":"action-test","config":{}}],
"connections":[{"sourceNode":"t","sourcePort":"output","targetNode":"p","targetPort":"input"},
{"sourceNode":"p","sourcePort":"output","targetNode":"a","targetPort":"nope"}]}`

func TestAssistantProposesAFlow(t *testing.T) {
	a, chat, conns := assistantEnv(t, validProposal)
	res, err := a.ProposeFlow(context.Background(), AssistantFlowRequest{Connection: "local", Description: "Say hello every hour"})
	require.NoError(t, err)
	require.Equal(t, 1, chat.count())

	assert.Equal(t, "Hourly hello", res.Flow.Name)
	assert.Equal(t, "Says hello every hour.", res.Flow.Description)
	assert.Equal(t, "Define the secret HOOK_PASS.", res.Notes)
	assert.Equal(t, []flow.Issue{}, res.Issues)
	assert.Equal(t, llm.Usage{InputTokens: 100, OutputTokens: 20, Requests: 1}, res.Usage)
	assert.Equal(t, llm.Usage{InputTokens: 100, OutputTokens: 20, Requests: 1}, conns.UsageToday("local"), "spent from the connection's budget")

	require.Len(t, res.Flow.Nodes, 3)
	byID := map[string]flow.Node{}
	for _, n := range res.Flow.Nodes {
		byID[n.ID] = n
		assert.True(t, n.Enabled)
		assert.NotNil(t, n.Config)
		assert.NotNil(t, n.Inputs)
		assert.NotNil(t, n.Outputs)
		assert.NotEqual(t, flow.Position{}, n.Position, "laid out")
	}
	assert.Equal(t, types.NodeCategoryInput, byID["t"].Category)
	assert.Equal(t, types.NodeCategoryProcessing, byID["p"].Category)
	assert.Equal(t, types.NodeCategoryOutput, byID["a"].Category)
	assert.Equal(t, "output", byID["p"].Outputs[0].ID)
	assert.Equal(t, "input", byID["p"].Inputs[0].ID)
	assert.Less(t, byID["t"].Position.X, byID["p"].Position.X, "left to right")
	assert.Less(t, byID["p"].Position.X, byID["a"].Position.X, "left to right")
	require.Len(t, res.Flow.Connections, 2)
	for _, c := range res.Flow.Connections {
		assert.NotEmpty(t, c.ID)
	}

	req := chat.request(0)
	assert.Equal(t, "m-default", req["model"], "the connection's model")
	assert.Equal(t, float64(AssistantMaxTokens), req["max_tokens"])
	assert.Equal(t, map[string]any{"type": "json_object"}, req["response_format"])
	msgs := chat.messages(t, 0)
	require.Len(t, msgs, 2)
	system, user := msgs[0][1], msgs[1][1]
	assert.Equal(t, "system", msgs[0][0])
	for _, want := range []string{"process-test", "A processing test node.", "trigger-test", "Seconds between runs.",
		`"upper"`, `"outputsFrom":"labels"`, `"local"`, `"cloud"`, `"home"`, "{{secret.NAME}}", `"error"`} {
		assert.Contains(t, system, want)
	}
	for _, never := range []string{"TRIGGER-DOC-MARKER", "cloud-secret-key", "cloud.example", "10.0.0.7", "broker-pass", "gpt-x", "m-default"} {
		assert.NotContains(t, system, never, "documentation and connection settings are left out")
		assert.NotContains(t, user, never)
	}
	assert.Equal(t, "user", msgs[1][0])
	assert.Contains(t, user, "Say hello every hour")
}

// The request's model overrides the connection's.
func TestAssistantUsesTheRequestedModel(t *testing.T) {
	a, chat, _ := assistantEnv(t, validProposal)
	_, err := a.ProposeFlow(context.Background(), AssistantFlowRequest{Connection: "local", Model: "bigger", Description: "x"})
	require.NoError(t, err)
	assert.Equal(t, "bigger", chat.request(0)["model"])
}

// Blocking issues go back to the model, which answers a fixed flow; the
// usage of both requests is counted.
func TestAssistantRepairsBlockingIssues(t *testing.T) {
	a, chat, _ := assistantEnv(t, brokenProposal, validProposal)
	res, err := a.ProposeFlow(context.Background(), AssistantFlowRequest{Connection: "local", Description: "Say hello"})
	require.NoError(t, err)
	require.Equal(t, 2, chat.count())
	assert.Equal(t, []flow.Issue{}, res.Issues)
	assert.Equal(t, llm.Usage{InputTokens: 200, OutputTokens: 40, Requests: 2}, res.Usage)

	msgs := chat.messages(t, 1)
	require.Len(t, msgs, 4, "system, the description, the first answer, the issues")
	assert.Equal(t, chat.messages(t, 0)[0], msgs[0], "the same system prompt")
	assert.Equal(t, "assistant", msgs[2][0])
	assert.Contains(t, msgs[2][1], `"targetPort":"nope"`)
	assert.Equal(t, "user", msgs[3][0])
	assert.Contains(t, msgs[3][1], flow.IssueConnectionUnknownPort)
	assert.Contains(t, msgs[3][1], "p.output -> a.nope", "the connection by its endpoints: the model gave it no ID")
}

// At most AssistantRepairRounds repairs: then the proposal is answered with
// the issues that remain.
func TestAssistantStopsAfterTwoRepairRounds(t *testing.T) {
	a, chat, _ := assistantEnv(t, brokenProposal)
	res, err := a.ProposeFlow(context.Background(), AssistantFlowRequest{Connection: "local", Description: "Say hello"})
	require.NoError(t, err)
	assert.Equal(t, 1+AssistantRepairRounds, chat.count())
	require.NotEmpty(t, flow.BlockingIssues(res.Issues))
	assert.Equal(t, flow.IssueConnectionUnknownPort, res.Issues[0].Code)
	assert.Equal(t, int64(3), res.Usage.Requests)
}

// Issues that depend on the environment (an LLM connection the admin has
// not defined yet) are reported, not repaired; the connection names Lint
// checks are the assistant's.
func TestAssistantReportsEnvironmentIssuesWithoutRepairing(t *testing.T) {
	answer := `{"name":"n","nodes":[{"id":"t","type":"trigger-test"},
{"id":"l","type":"process-llm-test","config":{"connection":"local"}},
{"id":"g","type":"process-llm-test","config":{"connection":"ghost"}}],
"connections":[{"sourceNode":"t","sourcePort":"output","targetNode":"l","targetPort":"input"},
{"sourceNode":"l","sourcePort":"output","targetNode":"g","targetPort":"input"}]}`
	a, chat, _ := assistantEnv(t, answer)
	res, err := a.ProposeFlow(context.Background(), AssistantFlowRequest{Connection: "local", Description: "x"})
	require.NoError(t, err)
	assert.Equal(t, 1, chat.count())
	require.Len(t, res.Issues, 1)
	assert.Equal(t, flow.IssueUndefinedLLMConnection, res.Issues[0].Code)
	assert.Equal(t, "/nodes/g/config/connection", res.Issues[0].Path)
}

// An answer that is not JSON fails the request (the conversation cannot go
// on without it); its tokens were spent all the same.
func TestAssistantRefusesAnAnswerThatIsNotJSON(t *testing.T) {
	a, chat, conns := assistantEnv(t, "Sure! Here is your flow: a trigger, then a node.")
	_, err := a.ProposeFlow(context.Background(), AssistantFlowRequest{Connection: "local", Description: "x"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAssistantModel)
	assert.ErrorIs(t, err, llm.ErrInvalidAnswer)
	assert.Equal(t, 1, chat.count())
	assert.Equal(t, int64(1), conns.UsageToday("local").Requests)
}

// JSON that is not a flow goes back to the model like a blocking issue;
// when it never is one the request fails.
func TestAssistantRepairsAnAnswerThatIsNotAFlow(t *testing.T) {
	a, chat, _ := assistantEnv(t, `{"nodes":"a trigger"}`, validProposal)
	res, err := a.ProposeFlow(context.Background(), AssistantFlowRequest{Connection: "local", Description: "x"})
	require.NoError(t, err)
	assert.Equal(t, 2, chat.count())
	assert.Len(t, res.Flow.Nodes, 3)
	assert.Contains(t, chat.messages(t, 1)[3][1], "nodes")

	a, chat, _ = assistantEnv(t, `[1, 2]`)
	_, err = a.ProposeFlow(context.Background(), AssistantFlowRequest{Connection: "local", Description: "x"})
	assert.ErrorIs(t, err, ErrAssistantModel)
	assert.ErrorIs(t, err, llm.ErrInvalidAnswer)
	assert.Equal(t, 1+AssistantRepairRounds, chat.count())
}

// One request at a time: a second one while the first runs is refused
// (busy) without reaching the model.
func TestAssistantRunsOneRequestAtATime(t *testing.T) {
	a, chat, _ := assistantEnv(t, validProposal)
	chat.block = make(chan struct{})
	first := make(chan error, 1)
	go func() {
		_, err := a.ProposeFlow(context.Background(), AssistantFlowRequest{Connection: "local", Description: "x"})
		first <- err
	}()
	<-chat.arrived

	// Bounded, so a missing lock fails the test instead of hanging it.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := a.ProposeFlow(ctx, AssistantFlowRequest{Connection: "local", Description: "y"})
	var te *ToolError
	require.ErrorAs(t, err, &te)
	assert.ErrorIs(t, err, ErrBusy)
	assert.Equal(t, 1, chat.count())

	close(chat.block)
	require.NoError(t, <-first)
	_, err = a.ProposeFlow(context.Background(), AssistantFlowRequest{Connection: "local", Description: "z"})
	assert.NoError(t, err, "free again")
}

// A request is bounded (3 minutes; shorter here).
func TestAssistantRequestIsBounded(t *testing.T) {
	a, chat, _ := assistantEnv(t, validProposal)
	a.timeout = 50 * time.Millisecond
	chat.block = make(chan struct{})
	defer close(chat.block)
	_, err := a.ProposeFlow(context.Background(), AssistantFlowRequest{Connection: "local", Description: "x"})
	assert.ErrorIs(t, err, ErrAssistantModel)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// The connection's daily token limit applies to the assistant too.
func TestAssistantSpendsFromTheConnectionsBudget(t *testing.T) {
	a, chat, conns := assistantEnv(t, validProposal)
	c, _ := conns.Get("local")
	c.DailyTokenLimit = 50
	require.NoError(t, conns.Set(c))
	require.NoError(t, conns.RecordUsage("local", llm.Usage{InputTokens: 50, Requests: 1}))
	_, err := a.ProposeFlow(context.Background(), AssistantFlowRequest{Connection: "local", Description: "x"})
	assert.ErrorIs(t, err, ErrAssistantModel)
	assert.ErrorIs(t, err, llm.ErrBudgetExhausted)
	assert.Equal(t, 0, chat.count())
}

func TestAssistantRefusesInvalidRequests(t *testing.T) {
	a, chat, conns := assistantEnv(t, validProposal)
	require.NoError(t, conns.Set(llm.Connection{Name: "nomodel", Provider: llm.ProviderOpenAI, BaseURL: "http://127.0.0.1:9/v1"}))
	require.NoError(t, conns.Set(llm.Connection{Name: "keyless", Provider: llm.ProviderOpenAI, BaseURL: "http://127.0.0.1:9/v1",
		APIKey: "{{secret.MISSING_KEY}}", Model: "m"}))
	many := &AssistantFlow{}
	for range MaxDraftNodes + 1 {
		many.Nodes = append(many.Nodes, flow.Node{ID: "n", Type: "process-test"})
	}
	for _, c := range []struct {
		req  AssistantFlowRequest
		want string
	}{
		{AssistantFlowRequest{Connection: "local", Description: "  "}, "description"},
		{AssistantFlowRequest{Connection: "local", Description: strings.Repeat("é", MaxAssistantDescriptionChars+1)}, "description"},
		{AssistantFlowRequest{Connection: "", Description: "x"}, "connection"},
		{AssistantFlowRequest{Connection: "ghost", Description: "x"}, `"ghost"`},
		{AssistantFlowRequest{Connection: "nomodel", Description: "x"}, "model"},
		{AssistantFlowRequest{Connection: "keyless", Description: "x"}, `"MISSING_KEY"`},
		{AssistantFlowRequest{Connection: "local", Description: "x", Flow: many}, "nodes"},
	} {
		_, err := a.ProposeFlow(context.Background(), c.req)
		var te *ToolError
		require.ErrorAs(t, err, &te, c.want)
		assert.ErrorIs(t, err, ErrInvalidArguments, c.want)
		assert.Contains(t, te.Message, c.want)
	}
	assert.Equal(t, 0, chat.count())
}

// "Change this flow": the current flow goes to the model with its
// credentials withheld; the answer gets them back on the nodes it kept (same
// ID and type), and those nodes keep their positions.
func TestAssistantChangesTheCurrentFlowWithoutSendingCredentials(t *testing.T) {
	answer := `{"name":"Changed","description":"","notes":"Added a step.",
"nodes":[{"id":"t","type":"trigger-test","config":{"interval":5}},
{"id":"p","type":"process-test","config":{"text":"{{payload}}"}},
{"id":"a","type":"action-test","config":{"url":"http://x","password":"<redacted>"}},
{"id":"b","type":"action-test","config":{"password":"{{secret.HOOK}}"}}],
"connections":[{"sourceNode":"t","sourcePort":"output","targetNode":"p","targetPort":"input"},
{"sourceNode":"p","sourcePort":"output","targetNode":"a","targetPort":"input"},
{"sourceNode":"p","sourcePort":"output","targetNode":"b","targetPort":"input"}]}`
	a, chat, _ := assistantEnv(t, answer)
	current := &AssistantFlow{
		Name: "Mine",
		Nodes: []flow.Node{
			{ID: "t", Type: "trigger-test", Position: flow.Position{X: 40, Y: 60}, Config: map[string]any{"interval": 5}, Enabled: true},
			{ID: "a", Type: "action-test", Position: flow.Position{X: 700, Y: 60}, Enabled: true,
				Config: map[string]any{"url": "http://x", "password": "hunter2-literal"}},
			{ID: "b", Type: "action-test", Position: flow.Position{X: 700, Y: 300}, Enabled: true,
				Config: map[string]any{"password": "{{secret.HOOK}}"}},
		},
		Connections: []flow.Connection{{ID: "c1", SourceNode: "t", SourcePort: "output", TargetNode: "a", TargetPort: "input"}},
	}
	res, err := a.ProposeFlow(context.Background(), AssistantFlowRequest{Connection: "local", Description: "Add a step", Flow: current})
	require.NoError(t, err)

	user := chat.messages(t, 0)[1][1]
	assert.NotContains(t, user, "hunter2-literal", "a literal credential never reaches the model")
	assert.Contains(t, user, RedactedValue)
	assert.Contains(t, user, "{{secret.HOOK}}", "a secret reference is not a credential")
	assert.Contains(t, user, `"id":"a"`)
	assert.Contains(t, user, "Add a step")

	byID := map[string]flow.Node{}
	for _, n := range res.Flow.Nodes {
		byID[n.ID] = n
	}
	assert.Equal(t, "hunter2-literal", byID["a"].Config["password"], "restored on the node it kept")
	assert.Equal(t, flow.Position{X: 40, Y: 60}, byID["t"].Position, "kept nodes keep their positions")
	assert.Equal(t, flow.Position{X: 700, Y: 60}, byID["a"].Position)
	assert.NotEqual(t, flow.Position{}, byID["p"].Position, "the new node is laid out")
	assert.Equal(t, "Changed", res.Flow.Name)
	assert.Equal(t, "hunter2-literal", current.Nodes[1].Config["password"], "the request is not modified")
}

// A "<redacted>" the model puts where nothing was withheld stays as it is
// (no value of another node or setting leaks into it).
func TestAssistantRestoresOnlyWithheldValues(t *testing.T) {
	answer := `{"name":"n","nodes":[{"id":"t","type":"trigger-test"},
{"id":"a","type":"process-test","config":{"text":"<redacted>"}},
{"id":"x","type":"action-test","config":{"password":"<redacted>"}}],
"connections":[{"sourceNode":"t","sourcePort":"output","targetNode":"a","targetPort":"input"},
{"sourceNode":"a","sourcePort":"output","targetNode":"x","targetPort":"input"}]}`
	a, _, _ := assistantEnv(t, answer)
	current := &AssistantFlow{Nodes: []flow.Node{
		{ID: "a", Type: "action-test", Enabled: true, Config: map[string]any{"password": "hunter2-literal"}},
	}}
	res, err := a.ProposeFlow(context.Background(), AssistantFlowRequest{Connection: "local", Description: "x", Flow: current})
	require.NoError(t, err)
	for _, n := range res.Flow.Nodes {
		for _, v := range n.Config {
			assert.NotEqual(t, "hunter2-literal", v, "node %s changed type or was never withheld", n.ID)
		}
	}
}

// The model is told to leave "enabled" out: a node it keeps (same ID and
// type) keeps the state it has in the current flow, so a node the admin
// disabled is never turned back on by a proposal. New nodes are enabled,
// and an explicit "enabled" in the answer still wins.
func TestAssistantKeepsTheEnabledStateOfKeptNodes(t *testing.T) {
	answer := `{"name":"Changed","nodes":[{"id":"t","type":"trigger-test","config":{"interval":5}},
{"id":"mail","type":"action-test","config":{"url":"http://x"}},
{"id":"log","type":"action-test","config":{"url":"http://log"}},
{"id":"p","type":"process-test","config":{"text":"x"}},
{"id":"on","type":"action-test","enabled":true,"config":{"url":"http://on"}},
{"id":"off","type":"action-test","enabled":false,"config":{"url":"http://off"}}],
"connections":[{"sourceNode":"t","sourcePort":"output","targetNode":"mail","targetPort":"input"},
{"sourceNode":"t","sourcePort":"output","targetNode":"log","targetPort":"input"},
{"sourceNode":"t","sourcePort":"output","targetNode":"p","targetPort":"input"},
{"sourceNode":"t","sourcePort":"output","targetNode":"on","targetPort":"input"},
{"sourceNode":"t","sourcePort":"output","targetNode":"off","targetPort":"input"}]}`
	a, chat, _ := assistantEnv(t, answer)
	current := &AssistantFlow{Name: "Mine", Nodes: []flow.Node{
		{ID: "t", Type: "trigger-test", Config: map[string]any{"interval": 5}, Enabled: true},
		{ID: "mail", Type: "action-test", Config: map[string]any{"url": "http://x"}, Enabled: false},
		{ID: "p", Type: "action-test", Config: map[string]any{"url": "http://p"}, Enabled: false},
		{ID: "on", Type: "action-test", Config: map[string]any{"url": "http://on"}, Enabled: false},
		{ID: "off", Type: "action-test", Config: map[string]any{"url": "http://off"}, Enabled: true},
	}}
	res, err := a.ProposeFlow(context.Background(), AssistantFlowRequest{Connection: "local", Description: "also log every message", Flow: current})
	require.NoError(t, err)
	assert.Contains(t, chat.messages(t, 0)[1][1], `"enabled":false`, "the model sees which nodes are disabled")

	enabled := map[string]bool{}
	for _, n := range res.Flow.Nodes {
		enabled[n.ID] = n.Enabled
	}
	assert.Equal(t, map[string]bool{
		"t":    true,
		"mail": false, // kept: stays disabled
		"log":  true,  // new
		"p":    true,  // another type under the ID: a new node
		"on":   true,  // the answer says so
		"off":  false, // the answer says so
	}, enabled)
}
