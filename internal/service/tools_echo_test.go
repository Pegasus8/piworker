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

// guesses are arguments carrying a secret's value (or not) in every form
// the redactor matches.
func guesses() []map[string]any {
	return []map[string]any{
		{"text": "1111"},
		{"text": "4820"},
		{"text": doorPin},
		{"text": "4819 4820 " + doorPin + " 4822"},
		{"n": float64(4820)},
		{"n": float64(4821)},
		{"text": "value: " + canary},
		{"text": url.QueryEscape(canary)},
		{"text": url.PathEscape(canary)},
		{"text": base64.StdEncoding.EncodeToString([]byte(canary))},
		{"text": base64.URLEncoding.EncodeToString([]byte("user:" + canary))},
	}
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return string(data)
}

// What an agent sends comes back as it sent it, even when it equals a
// secret's value in any form the redactor matches: otherwise an echoing
// tool would confirm guesses of a PIN, one call (or many guesses per call)
// at a time (ADR-0021).
func TestAgentEchoNeverConfirmsASecret(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.secrets.Set("door_pin", doorPin))
	require.NoError(t, e.secrets.Set("api_key", canary))
	info := e.deploy(fan(echoConfig(nil), echo()))
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})

	for _, args := range guesses() {
		res, err := e.call(agent, info, args)
		require.NoError(t, err)
		e.waitIdle(info)
		require.Equal(t, flow.CallStatusCompleted, res.Status, "%+v", res)
		assert.JSONEq(t, jsonOf(t, args), string(res.Result), "%v", args)

		got, err := e.tools.Result(context.Background(), agent, res.RunID)
		require.NoError(t, err)
		assert.JSONEq(t, jsonOf(t, args), string(got.Result), "%v", args)
	}
}

// An error that quotes the agent's argument is not an oracle either.
func TestAgentErrorEchoNeverConfirmsASecret(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.secrets.Set("door_pin", doorPin))
	info := e.deploy(fan(echoConfig(nil), step{id: "fail", typ: echoFailNodeType, config: map[string]any{"message": "jammed: "}}))
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})

	for _, guess := range []string{"4820", doorPin} {
		res, err := e.call(agent, info, map[string]any{"text": guess})
		require.NoError(t, err)
		e.waitIdle(info)
		require.NotNil(t, res.Error, "%+v", res)
		assert.Equal(t, "jammed: "+guess, res.Error.Message)
	}
}

// An approved confirm:admin call follows the same rule.
func TestApprovedEchoNeverConfirmsASecret(t *testing.T) {
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
	assert.JSONEq(t, `{"text":"`+doorPin+`"}`, string(got.Result))
}

// What the flow outputs of its own (here a value equal to a secret) is still
// redacted for agents, in results and in errors, unless the agent sent that
// secret's value itself: then it already holds it. That residual (a flow
// that both echoes the argument and outputs the same secret, or derives a
// secret's value from the arguments) is documented in docs/design/mcp.md §6.
func TestAgentResultsStillRedactWhatTheFlowOutputs(t *testing.T) {
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

	// The residual: the agent sent the value, so it is not redacted.
	res, err = e.call(agent, info, map[string]any{"text": doorPin})
	require.NoError(t, err)
	e.waitIdle(info)
	assert.JSONEq(t, `{"text":"`+doorPin+`","emitted":"pin `+doorPin+`"}`, string(res.Result))

	// The editor (the admin) still gets every secret redacted.
	res, err = e.call(EditorActor(), info, map[string]any{"text": doorPin})
	require.NoError(t, err)
	assert.JSONEq(t, `{"text":"<redacted>","emitted":"pin <redacted>"}`, string(res.Result))
}
