package service

import (
	"context"
	"fmt"
	"strings"
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

// statusNodeType outputs {"status": <its "value" setting>} and nothing of
// its input: a tool that never repeats its arguments.
const statusNodeType = "test-service-status"

type statusNode struct{ value string }

func (n *statusNode) Process(_ context.Context, msg *types.Message) ([]*types.Message, error) {
	msg.Payload = map[string]any{"status": n.value}
	return []*types.Message{msg}, nil
}
func (n *statusNode) Ports() ([]types.Port, []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny}}
}
func (n *statusNode) Validate() error { return nil }

var registerStatusNode sync.Once

func init() {
	registerStatusNode.Do(func() {
		err := node.Register(node.NodeTypeInfo{
			Type: statusNodeType, Name: "Status", Category: types.NodeCategoryProcessing, Risk: node.RiskSafe,
			Inputs:  []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}},
			Outputs: []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny}},
			Config: &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
				"value": {Type: "string", Title: "Value", Description: "The status.", AgentVisible: true},
			}},
		}, func(cfg map[string]any) (node.Node, error) {
			v, _ := cfg["value"].(string)
			return &statusNode{value: v}, nil
		})
		if err != nil {
			panic(err)
		}
	})
}

// guessConfig is a tool taking arguments the flow never repeats: a note, a
// number and a list.
func guessConfig(over map[string]any) map[string]any {
	cfg := toolConfig(map[string]any{"inputSchema": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"note": map[string]any{"type": "string", "maxLength": float64(4096)},
			"n":    map[string]any{"type": "integer"},
			"list": map[string]any{"type": "array", "maxItems": float64(100),
				"items": map[string]any{"type": "string", "maxLength": float64(64)}},
		},
	}})
	for k, v := range over {
		cfg[k] = v
	}
	return cfg
}

// pinDictionary is every four-digit candidate from first to last, separated
// by spaces, as one argument.
func pinDictionary(first, last int) string {
	var b strings.Builder
	for i := first; i <= last; i++ {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%04d", i)
	}
	return b.String()
}

// pinGuesses are arguments without and with the right PIN, alone or among
// many candidates, in every kind of argument.
func pinGuesses() []map[string]any {
	list := make([]any, 0, 100)
	for i := 4770; i < 4870; i++ {
		list = append(list, fmt.Sprintf("%04d", i))
	}
	return []map[string]any{
		{},
		{"note": "hello"},
		{"n": float64(4820)},
		{"n": float64(4821)},
		{"note": "4820"},
		{"note": doorPin},
		{"note": "4818 4819 4820 4821 4822"},
		{"note": pinDictionary(4500, 5299)},
		{"list": list},
	}
}

// A tool that never repeats its arguments but outputs a secret's value of
// its own (here a constant equal to it) answers an agent byte for byte the
// same whatever the arguments hold: the right guess, a wrong one, or a
// dictionary of candidates in one argument. Its result never depends on
// comparing the agent's text with the secret (ADR-0021).
func TestToolResultsNeverDependOnTheArgumentsSent(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.secrets.Set("door_pin", doorPin))
	info := e.deploy(chain(guessConfig(nil),
		step{id: "status", typ: statusNodeType, config: map[string]any{"value": "door code is " + doorPin}}, echo()))
	failing := e.deploy(fan(guessConfig(nil),
		step{id: "fail", typ: visibleFailNodeType, config: map[string]any{"message": "pin " + doorPin + " rejected"}}))
	agent := e.agent(storage.NewAPIToken{Grants: append(grant(info), grant(failing)...)})

	for _, args := range pinGuesses() {
		res, err := e.call(agent, info, args)
		require.NoError(t, err)
		e.waitIdle(info)
		require.Equal(t, flow.CallStatusCompleted, res.Status, "%+v", res)
		assert.Equal(t, `{"status":"door code is <redacted>"}`, string(res.Result), "%.80v", args)

		got, err := e.tools.Result(context.Background(), agent, res.RunID)
		require.NoError(t, err)
		assert.Equal(t, `{"status":"door code is <redacted>"}`, string(got.Result), "%.80v", args)

		res, err = e.call(agent, failing, args)
		require.NoError(t, err)
		e.waitIdle(failing)
		require.NotNil(t, res.Error, "%+v", res)
		assert.Equal(t, "pin <redacted> rejected", res.Error.Message, "%.80v", args)
	}
}

// The same holds for the result stored on an approved confirm:admin call.
func TestApprovedToolResultsNeverDependOnTheArgumentsSent(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.secrets.Set("door_pin", doorPin))
	info := e.deploy(chain(guessConfig(map[string]any{"confirm": "admin"}),
		step{id: "status", typ: statusNodeType, config: map[string]any{"value": "door code is " + doorPin}}, echo()))
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})

	for _, args := range []map[string]any{{"note": "4820"}, {"note": pinDictionary(4500, 5299)}} {
		res, err := e.call(agent, info, args)
		require.NoError(t, err)
		require.Equal(t, flow.CallStatusApprovalRequired, res.Status)
		approved, err := e.store.DecideAgentApproval(res.RunID, true, "admin", time.Now())
		require.NoError(t, err)
		require.NoError(t, e.tools.ExecuteApproval(context.Background(), *approved))
		e.waitIdle(info)

		got, err := e.tools.Result(context.Background(), agent, res.RunID)
		require.NoError(t, err)
		assert.Equal(t, `{"status":"door code is <redacted>"}`, string(got.Result), "%.80v", args)
	}
}
