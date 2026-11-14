package service

import (
	"context"
	"encoding/json"
	"errors"
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

// visibleFailNodeType is a test node whose error message is its "message"
// setting, which agents may read (AgentVisible): its errors quote nothing
// hidden.
const visibleFailNodeType = "test-service-fail-visible"

var registerVisibleFailNode sync.Once

func init() {
	registerVisibleFailNode.Do(func() {
		err := node.Register(node.NodeTypeInfo{
			Type: visibleFailNodeType, Name: "Fail (visible)", Category: types.NodeCategoryOutput, Risk: node.RiskSafe,
			Inputs: []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}},
			Config: &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
				"message": {Type: "string", Title: "Message", Description: "The error.", AgentVisible: true},
			}},
		}, func(cfg map[string]any) (node.Node, error) {
			msg, _ := cfg["message"].(string)
			return &failNode{msg: msg}, nil
		})
		if err != nil {
			panic(err)
		}
	})
}

// hiddenConstant is a constant the admin wrote into a hidden expression.
const hiddenConstant = "garage-pin-4821"

// hiddenExpression fails on any text that is not a number, quoting itself
// in expr-lang's error.
const hiddenExpression = `{"ok": int(payload.text) > 0, "door": "` + hiddenConstant + `"}`

// chain connects trigger-tool "tool" (cfg) through steps in order.
func chain(cfg map[string]any, steps ...step) *flow.Flow {
	f := flow.NewFlow("svc-chain")
	tool := flow.NewNode("trigger-tool", flow.NodeCategoryInput)
	tool.ID, tool.Config = "tool", cfg
	f.AddNode(*tool)
	prev := "tool"
	for _, s := range steps {
		n := flow.NewNode(s.typ, flow.NodeCategoryProcessing)
		n.ID, n.Config = s.id, s.config
		if n.Config == nil {
			n.Config = map[string]any{}
		}
		f.AddNode(*n)
		f.AddConnection(*flow.NewConnection(prev, "output", s.id, "input"))
		prev = s.id
	}
	return f
}

func hiddenCalc() step {
	return step{id: "calc", typ: "process-transform", config: map[string]any{"expression": hiddenExpression}}
}

// assertWithheld checks that a call error names the node but quotes none of
// its hidden settings.
func assertWithheld(t *testing.T, res flow.CallResult) {
	t.Helper()
	require.Equal(t, flow.CallStatusError, res.Status, "%+v", res)
	require.NotNil(t, res.Error)
	assert.Equal(t, "calc", res.Error.NodeID)
	assert.Equal(t, "process-transform", res.Error.NodeType)
	assert.NotContains(t, res.Error.Message, hiddenConstant)
	assert.NotContains(t, res.Error.Message, "int(payload.text)")
	assert.Contains(t, res.Error.Message, "not shown")
}

// An agent never reads a node's hidden settings through the node's error:
// expr-lang quotes the whole expression, so the error of a node holding
// settings agents may not read (here a process-transform expression, which
// pw_get_flow shows as "<redacted>") is withheld whole, keeping the node's
// ID and type (ADR-0021). The admin, in the editor, sees it.
func TestAgentCallErrorWithholdsHiddenSettings(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(chain(toolConfig(nil), hiddenCalc(), echo()))
	require.Equal(t, node.RiskSafe, info.Risk)
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})

	for _, args := range []map[string]any{
		{"text": "abc"},  // int() of a word
		{"door": "left"}, // text left out
	} {
		res, err := e.call(agent, info, args)
		require.NoError(t, err)
		e.waitIdle(info)
		assertWithheld(t, res)

		got, err := e.tools.Result(context.Background(), agent, res.RunID)
		require.NoError(t, err)
		assertWithheld(t, got)
	}

	res, err := e.call(EditorActor(), info, map[string]any{"text": "abc"})
	require.NoError(t, err)
	require.NotNil(t, res.Error)
	assert.Contains(t, res.Error.Message, hiddenConstant, "the admin sees the node's error")
}

// The approved call of a confirm:admin tool stores the same withheld error
// on the approval, and pw_get_result returns it.
func TestApprovedCallErrorWithholdsHiddenSettings(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(chain(toolConfig(map[string]any{"confirm": "admin"}), hiddenCalc(), echo()))
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})
	res, err := e.call(agent, info, map[string]any{"text": "abc"})
	require.NoError(t, err)
	require.Equal(t, flow.CallStatusApprovalRequired, res.Status)

	approved, err := e.store.DecideAgentApproval(res.RunID, true, "admin", time.Now())
	require.NoError(t, err)
	require.NoError(t, e.tools.ExecuteApproval(context.Background(), *approved))
	e.waitIdle(info)

	stored, err := e.store.GetAgentApproval(res.RunID)
	require.NoError(t, err)
	assert.NotContains(t, string(stored.Result), hiddenConstant)
	got, err := e.tools.Result(context.Background(), agent, res.RunID)
	require.NoError(t, err)
	assertWithheld(t, got)
}

// A call that reports running and fails later is withheld the same way when
// its final result is collected or fetched.
func TestRunningCallErrorWithholdsHiddenSettings(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(chain(toolConfig(map[string]any{"timeoutMs": float64(1000)}),
		step{id: "wait", typ: "process-delay", config: map[string]any{"delay": float64(1400)}}, hiddenCalc(), echo()))
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})

	res, err := e.call(agent, info, map[string]any{"text": "abc"})
	require.NoError(t, err)
	require.Equal(t, flow.CallStatusRunning, res.Status)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := e.tools.Result(ctx, agent, res.RunID)
	require.NoError(t, err)
	assertWithheld(t, got)

	e.waitIdle(info)
	again, err := e.tools.Result(context.Background(), agent, res.RunID)
	require.NoError(t, err)
	assertWithheld(t, again)
}

// The error of a node whose settings agents may all read keeps its text
// (secrets and URL paths still hidden).
func TestAgentCallErrorOfAVisibleNodeIsShown(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(nil), step{id: "fail", typ: visibleFailNodeType,
		config: map[string]any{"message": "the door is jammed, see https://example.com/doors/42"}}))
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})

	res, err := e.call(agent, info, map[string]any{"door": "left"})
	require.NoError(t, err)
	require.Equal(t, flow.CallStatusError, res.Status)
	require.NotNil(t, res.Error)
	assert.Equal(t, "fail", res.Error.NodeID)
	assert.Equal(t, "the door is jammed, see https://example.com/<redacted>", res.Error.Message)
}

// A node of a type without a config schema holds only settings agents may
// not read: its errors are withheld.
func TestAgentCallErrorOfAnUndeclaredSettingIsWithheld(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(nil), step{id: "fail", typ: failNodeType,
		config: map[string]any{"message": "boom " + hiddenConstant}}))
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})

	res, err := e.call(agent, info, map[string]any{"door": "left"})
	require.NoError(t, err)
	require.NotNil(t, res.Error)
	assert.Equal(t, "fail", res.Error.NodeID)
	assert.Equal(t, failNodeType, res.Error.NodeType)
	assert.NotContains(t, res.Error.Message, hiddenConstant)
	var te *ToolError
	assert.False(t, errors.As(err, &te))
	data, mErr := json.Marshal(res)
	require.NoError(t, mErr)
	assert.NotContains(t, string(data), hiddenConstant)
}
