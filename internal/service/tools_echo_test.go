package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test node types of the echo tests.
const (
	// echoFailNodeType fails with its "message" setting (AgentVisible)
	// followed by the payload's text: an error that quotes the agent's
	// argument.
	echoFailNodeType = "test-service-fail-echo"
	// emitNodeType adds its "value" setting to the payload as "emitted": a
	// flow that outputs a value of its own.
	emitNodeType = "test-service-emit"
)

type echoFailNode struct{ msg string }

func (n *echoFailNode) Process(_ context.Context, msg *types.Message) ([]*types.Message, error) {
	text := ""
	if p, ok := msg.Payload.(map[string]any); ok {
		text, _ = p["text"].(string)
	}
	return nil, errors.New(n.msg + text)
}
func (n *echoFailNode) Ports() ([]types.Port, []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}}, nil
}
func (n *echoFailNode) Validate() error { return nil }

type emitNode struct{ value string }

func (n *emitNode) Process(_ context.Context, msg *types.Message) ([]*types.Message, error) {
	out := map[string]any{"emitted": n.value}
	if p, ok := msg.Payload.(map[string]any); ok {
		for k, v := range p {
			out[k] = v
		}
	}
	msg.Payload = out
	return []*types.Message{msg}, nil
}
func (n *emitNode) Ports() ([]types.Port, []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny}}
}
func (n *emitNode) Validate() error { return nil }

var registerEchoTestNodes sync.Once

func init() {
	registerEchoTestNodes.Do(func() {
		visible := &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
			"message": {Type: "string", Title: "Message", Description: "The error.", AgentVisible: true},
			"value":   {Type: "string", Title: "Value", Description: "What is emitted.", AgentVisible: true},
		}}
		for _, reg := range []struct {
			info    node.NodeTypeInfo
			factory node.NodeFactory
		}{
			{node.NodeTypeInfo{
				Type: echoFailNodeType, Name: "Fail (echo)", Category: types.NodeCategoryOutput, Risk: node.RiskSafe,
				Inputs: []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}}, Config: visible,
			}, func(cfg map[string]any) (node.Node, error) {
				msg, _ := cfg["message"].(string)
				return &echoFailNode{msg: msg}, nil
			}},
			{node.NodeTypeInfo{
				Type: emitNodeType, Name: "Emit", Category: types.NodeCategoryProcessing, Risk: node.RiskSafe,
				Inputs:  []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}},
				Outputs: []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny}},
				Config:  visible,
			}, func(cfg map[string]any) (node.Node, error) {
				v, _ := cfg["value"].(string)
				return &emitNode{value: v}, nil
			}},
		} {
			if err := node.Register(reg.info, reg.factory); err != nil {
				panic(err)
			}
		}
	})
}

// doorPin is a low-entropy secret an agent could guess.
const doorPin = "4821"

// echoConfig is a tool taking a text and a number.
func echoConfig(over map[string]any) map[string]any {
	cfg := toolConfig(map[string]any{"inputSchema": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{"type": "string", "maxLength": float64(4096)},
			"n":    map[string]any{"type": "integer"},
		},
	}})
	for k, v := range over {
		cfg[k] = v
	}
	return cfg
}

// echoCase is an argument and whether it carries a secret's value.
type echoCase struct {
	args   map[string]any
	secret bool
}

// echoCases carry a secret's value (or not) in every form the redactor
// matches.
func echoCases() []echoCase {
	return []echoCase{
		{map[string]any{"text": "1111"}, false},
		{map[string]any{"text": "4820"}, false},
		{map[string]any{"text": doorPin}, true},
		{map[string]any{"text": "4819 4820 " + doorPin + " 4822"}, true},
		{map[string]any{"n": float64(4820)}, false},
		{map[string]any{"n": float64(4821)}, true},
		{map[string]any{"text": "value: " + canary}, true},
		{map[string]any{"text": url.QueryEscape(canary)}, true},
		{map[string]any{"text": url.PathEscape(canary)}, true},
		{map[string]any{"text": base64.StdEncoding.EncodeToString([]byte(canary))}, true},
		{map[string]any{"text": base64.URLEncoding.EncodeToString([]byte("user:" + canary))}, true},
	}
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return string(data)
}

// A tool result is redacted of every known secret whoever sent it, the
// agent included: leaving out what an agent sent would show a secret the
// flow outputs of its own to an agent that sends candidates
// (TestToolResultsNeverDependOnTheArgumentsSent). What this leaves open, and
// what this test pins so the docs stay true: a granted tool that repeats its
// arguments shows an agent with only the call scope whether a guess comes
// back as "<redacted>", many guesses per call (ADR-0021, action item 3;
// docs/design/mcp.md §6). The Secrets dialog marks the short values this
// makes guessable.
func TestEchoingToolRedactsWhatTheAgentSent(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.secrets.Set("door_pin", doorPin))
	require.NoError(t, e.secrets.Set("api_key", canary))
	info := e.deploy(fan(echoConfig(nil), echo()))
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})

	for _, c := range echoCases() {
		res, err := e.call(agent, info, c.args)
		require.NoError(t, err)
		e.waitIdle(info)
		require.Equal(t, flow.CallStatusCompleted, res.Status, "%+v", res)
		if c.secret {
			assert.Contains(t, string(res.Result), redactedMark, "%v", c.args)
			assert.NotContains(t, string(res.Result), doorPin, "%v", c.args)
			assert.NotContains(t, string(res.Result), canary, "%v", c.args)
		} else {
			assert.JSONEq(t, jsonOf(t, c.args), string(res.Result), "%v", c.args)
		}

		got, err := e.tools.Result(context.Background(), agent, res.RunID)
		require.NoError(t, err)
		assert.Equal(t, string(res.Result), string(got.Result), "%v", c.args)
	}
}

// An error that quotes the agent's argument is redacted the same way.
func TestErrorEchoRedactsWhatTheAgentSent(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.secrets.Set("door_pin", doorPin))
	info := e.deploy(fan(echoConfig(nil), step{id: "fail", typ: echoFailNodeType, config: map[string]any{"message": "jammed: "}}))
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})

	for guess, want := range map[string]string{"4820": "jammed: 4820", doorPin: "jammed: <redacted>"} {
		res, err := e.call(agent, info, map[string]any{"text": guess})
		require.NoError(t, err)
		e.waitIdle(info)
		require.NotNil(t, res.Error, "%+v", res)
		assert.Equal(t, want, res.Error.Message)
	}
}

// So is the result stored on an approved confirm:admin call.
func TestApprovedEchoRedactsWhatTheAgentSent(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.secrets.Set("door_pin", doorPin))
	info := e.deploy(fan(echoConfig(map[string]any{"confirm": "admin"}), echo()))
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})
	res, err := e.call(agent, info, map[string]any{"text": doorPin})
	require.NoError(t, err)
	require.Equal(t, flow.CallStatusApprovalRequired, res.Status)

	approved, err := e.store.DecideAgentApproval(res.RunID, true, "admin", time.Now())
	require.NoError(t, err)
	require.NoError(t, e.tools.ExecuteApproval(context.Background(), *approved))
	e.waitIdle(info)

	got, err := e.tools.Result(context.Background(), agent, res.RunID)
	require.NoError(t, err)
	assert.JSONEq(t, `{"text":"<redacted>"}`, string(got.Result))
}

// What the flow outputs of its own (here a value equal to a secret) is
// redacted for agents, in results and in errors, also when the agent sent
// that secret's value itself, and for the editor.
func TestAgentResultsRedactWhatTheFlowOutputs(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.secrets.Set("door_pin", doorPin))
	f := chain(echoConfig(nil), step{id: "emit", typ: emitNodeType, config: map[string]any{"value": "pin " + doorPin}}, echo())
	info := e.deploy(f)
	failing := e.deploy(fan(echoConfig(nil), step{id: "fail", typ: echoFailNodeType,
		config: map[string]any{"message": fmt.Sprintf("pin %s rejected for ", doorPin)}}))
	agent := e.agent(storage.NewAPIToken{Grants: append(grant(info), grant(failing)...)})

	res, err := e.call(agent, info, map[string]any{"text": "hello"})
	require.NoError(t, err)
	e.waitIdle(info)
	assert.JSONEq(t, `{"text":"hello","emitted":"pin <redacted>"}`, string(res.Result))

	res, err = e.call(agent, failing, map[string]any{"text": "hello"})
	require.NoError(t, err)
	require.NotNil(t, res.Error)
	assert.Equal(t, "pin <redacted> rejected for hello", res.Error.Message)

	res, err = e.call(agent, info, map[string]any{"text": doorPin})
	require.NoError(t, err)
	e.waitIdle(info)
	assert.JSONEq(t, `{"text":"<redacted>","emitted":"pin <redacted>"}`, string(res.Result))

	res, err = e.call(EditorActor(), info, map[string]any{"text": doorPin})
	require.NoError(t, err)
	assert.JSONEq(t, `{"text":"<redacted>","emitted":"pin <redacted>"}`, string(res.Result))
}
