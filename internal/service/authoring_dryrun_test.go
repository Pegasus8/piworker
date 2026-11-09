package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sensitiveEchoType is a safe processing node with a Sensitive setting: it
// emits its input unchanged (node tests refuse literal credentials in it).
const sensitiveEchoType = "test-service-sensitive-echo"

type echoNode struct{}

func (echoNode) Process(_ context.Context, msg *types.Message) ([]*types.Message, error) {
	return []*types.Message{msg}, nil
}
func (echoNode) Ports() ([]types.Port, []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny}}
}
func (echoNode) Validate() error { return nil }

var registerEchoNode sync.Once

func init() {
	registerEchoNode.Do(func() {
		err := node.Register(node.NodeTypeInfo{
			Type: sensitiveEchoType, Name: "Echo", Category: types.NodeCategoryProcessing, Risk: node.RiskSafe,
			Inputs:  []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}},
			Outputs: []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny}},
			Config: &node.ConfigSchema{Properties: map[string]node.ConfigProperty{
				"apiKey": {Type: "string", Title: "API key", Description: "A credential.", Sensitive: true},
			}},
		}, func(map[string]any) (node.Node, error) { return echoNode{}, nil })
		if err != nil {
			panic(err)
		}
	})
}

// templateFlow is a manual trigger and two templates (whose text is hidden
// from agents: not AgentVisible).
func templateFlow() *flow.Flow {
	f := flow.NewFlow("templates")
	addNode(f, "start", "trigger-manual", nil)
	addNode(f, "t1", "process-template", map[string]any{"template": "BASEHIDDENWORDS {{payload}}"})
	addNode(f, "t2", "process-template", map[string]any{"template": "second {{payload}}"})
	connect(f, "start", "t1")
	connect(f, "t1", "t2")
	return f
}

func stepOf(res flow.DryRunResult, nodeID string) *flow.TraceStep {
	for i := range res.Trace {
		if res.Trace[i].NodeID == nodeID {
			return &res.Trace[i]
		}
	}
	return nil
}

// outputOf is the first output preview of nodeID's step, decoded when it is
// a JSON string (previews are JSON, "<" escaped).
func outputOf(t *testing.T, res flow.DryRunResult, nodeID string) string {
	t.Helper()
	s := stepOf(res, nodeID)
	require.NotNil(t, s, "no step of %s in %+v", nodeID, res)
	require.NotEmpty(t, s.Outputs, "%+v", s)
	return decodedPreview(s.Outputs[0].Preview)
}

func decodedPreview(p string) string {
	var s string
	if err := json.Unmarshal([]byte(p), &s); err == nil {
		return s
	}
	return p
}

// An agent's inline flow is its own: nothing is hidden from the run. What it
// returns has URLs cut past their host; what the agent sent comes back as
// it sent it, a secret's value or not (hiding it would confirm a guess).
func TestDryRunInlineFlowForAnAgent(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.token()
	res, err := e.a.DryRun(t.Context(), agent, DryRunRequest{
		Flow:    templateFlow(),
		Payload: "see https://hooks.example.com/services/T0/B0/pathsecret with " + apiKeyCanary,
	})
	require.NoError(t, err)
	require.Empty(t, flow.BlockingIssues(res.Issues), "%v", res.Issues)
	out := outputOf(t, res, "t1")
	assert.Contains(t, out, "BASEHIDDENWORDS", "the agent wrote it")
	assert.Contains(t, out, "https://hooks.example.com/<redacted>")
	assert.NotContains(t, mustJSON(t, res), "pathsecret")
	assert.Contains(t, out, apiKeyCanary, "the agent's own text")

	rows := e.auditRows()
	require.Len(t, rows, 1)
	assert.Equal(t, AuthorToolDryRun, rows[0].Tool)
	assert.Equal(t, "completed", rows[0].Outcome)
	assert.NotContains(t, rows[0].ArgsPreview, "hooks.example.com", "outcome only, no payloads")
}

// By flowId, every setting agents may not read is hidden from the run itself
// (H4): the templates render the sentinel, not the admin's text.
func TestDryRunStoredFlowHidesSettingsFromAgents(t *testing.T) {
	e := newAuthEnv(t)
	stored := e.create(templateFlow())
	agent := e.token()
	res, err := e.a.DryRun(t.Context(), agent, DryRunRequest{FlowID: stored.ID, Payload: "x"})
	require.NoError(t, err)
	out := outputOf(t, res, "t2")
	assert.NotContains(t, mustJSON(t, res), "BASEHIDDENWORDS")
	assert.NotContains(t, out, "second", "the agent did not write it")
	assert.Contains(t, out, RedactedValue)

	// The editor sees its own flow run as it is.
	res, err = e.a.DryRun(t.Context(), EditorActor(), DryRunRequest{FlowID: stored.ID, Payload: "x"})
	require.NoError(t, err)
	assert.Contains(t, outputOf(t, res, "t1"), "BASEHIDDENWORDS x")
	assert.Contains(t, outputOf(t, res, "t2"), "second BASEHIDDENWORDS x")

	_, err = e.a.DryRun(t.Context(), agent, DryRunRequest{FlowID: "nope"})
	requireKind(t, err, ErrFlowNotFound)
}

// By draftId, what the agent wrote runs; what it kept of the base version
// is hidden (H4).
func TestDryRunDraftHidesOnlyTheBaseValues(t *testing.T) {
	e := newAuthEnv(t)
	stored := e.create(templateFlow())
	agent := e.token()
	saved, err := e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: stored.ID, BaseVersion: 1, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "t2", Config: map[string]any{"template": "AGENTTEXT {{payload}}"}},
	}})
	require.NoError(t, err)
	res, err := e.a.DryRun(t.Context(), agent, DryRunRequest{DraftID: saved.DraftID, Payload: "x"})
	require.NoError(t, err)
	assert.NotContains(t, mustJSON(t, res), "BASEHIDDENWORDS")
	assert.Contains(t, outputOf(t, res, "t2"), "AGENTTEXT "+RedactedValue)

	// Another token's draft is unknown; the editor may dry-run any.
	_, err = e.a.DryRun(t.Context(), e.token(), DryRunRequest{DraftID: saved.DraftID})
	requireKind(t, err, ErrToolNotFound)
	res, err = e.a.DryRun(t.Context(), EditorActor(), DryRunRequest{DraftID: saved.DraftID, Payload: "x"})
	require.NoError(t, err)
	assert.Contains(t, outputOf(t, res, "t2"), "AGENTTEXT BASEHIDDENWORDS x")

	// A new flow's draft is all the agent's: nothing is hidden from it.
	fresh := e.draft(agent, DraftInput{Flow: templateFlow(), Name: "fresh"})
	res, err = e.a.DryRun(t.Context(), agent, DryRunRequest{DraftID: fresh.DraftID, Payload: "x"})
	require.NoError(t, err)
	assert.Contains(t, outputOf(t, res, "t2"), "second BASEHIDDENWORDS x")
}

func TestDryRunRefusals(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.token()
	stored := e.create(safeFlow())
	for name, req := range map[string]DryRunRequest{
		"nothing":        {},
		"flow and id":    {Flow: safeFlow(), FlowID: stored.ID},
		"draft and flow": {DraftID: "d", FlowID: stored.ID},
	} {
		_, err := e.a.DryRun(t.Context(), agent, req)
		requireKind(t, err, ErrInvalidArguments)
		_ = name
	}
	_, err := e.a.DryRun(t.Context(), agent, DryRunRequest{Flow: safeFlow(), TriggerNodeID: "nope"})
	requireKind(t, err, ErrInvalidArguments)

	// A stored flow whose hidden settings leave a node invalid once hidden
	// still runs: that node is a stub with a hidden_settings warning (the
	// transform's expression is hidden from agents).
	res, err := e.a.DryRun(t.Context(), agent, DryRunRequest{FlowID: stored.ID})
	require.NoError(t, err)
	assert.Empty(t, flow.BlockingIssues(res.Issues))
	require.Len(t, res.Issues, 1)
	assert.Equal(t, flow.IssueHiddenSettings, res.Issues[0].Code)
	require.Len(t, res.WouldExecute, 1)
	assert.Equal(t, "calc", res.WouldExecute[0].NodeID)

	_, err = e.a.DryRun(t.Context(), e.token(storage.ScopeRead), DryRunRequest{FlowID: stored.ID})
	requireKind(t, err, ErrPermissionDenied)

	// One sandboxed run at a time: busy.
	e.a.dryRun = func(context.Context, *flow.Flow, *node.Registry, flow.DryRunInput) (flow.DryRunResult, error) {
		return flow.DryRunResult{}, fmt.Errorf("dry run: %w", flow.ErrSandboxBusy)
	}
	_, err = e.a.DryRun(t.Context(), agent, DryRunRequest{FlowID: stored.ID})
	requireKind(t, err, ErrBusy)
}

func TestTestNode(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.token()
	res, err := e.a.TestNode(t.Context(), agent, TestNodeRequest{
		Type: "process-transform", Config: map[string]any{"expression": "payload + ' ok'"},
		Input: "go to https://api.example.com/private/path " + apiKeyCanary,
	})
	require.NoError(t, err)
	require.Empty(t, res.Issues)
	require.NotEmpty(t, res.Outputs)
	assert.Contains(t, res.Outputs[0].Preview, "ok")
	assert.Contains(t, res.Outputs[0].Preview, "https://api.example.com/<redacted>")
	assert.Contains(t, res.Outputs[0].Preview, apiKeyCanary, "the agent's own input, as sent")
	assert.NotContains(t, mustJSON(t, res), "private/path")

	// The editor sees the URL (secret values are still redacted).
	res, err = e.a.TestNode(t.Context(), EditorActor(), TestNodeRequest{
		Type: "process-transform", Config: map[string]any{"expression": "payload"}, Input: "https://api.example.com/private/path",
	})
	require.NoError(t, err)
	assert.Contains(t, res.Outputs[0].Preview, "private/path")

	// Config problems are the result's issues.
	res, err = e.a.TestNode(t.Context(), agent, TestNodeRequest{Type: "process-transform", Config: map[string]any{"expression": float64(1)}})
	require.NoError(t, err)
	assert.Contains(t, issueCodes(res.Issues), flow.IssueConfigType)

	rows := e.auditRows()
	require.Len(t, rows, 2, "the agent's two tests")
	assert.Equal(t, AuthorToolTestNode, rows[0].Tool)
	assert.NotContains(t, rows[0].ArgsPreview, "payload")
}

func TestTestNodeRefusals(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.token()
	_, err := e.a.TestNode(t.Context(), agent, TestNodeRequest{Type: sensitiveEchoType, Config: map[string]any{"apiKey": "literal-key"}})
	te := requireKind(t, err, ErrInvalidArguments)
	issue := findIssue(te.FlowIssues, IssueSensitiveLiteral)
	require.NotNil(t, issue)
	assert.Equal(t, "/config/apiKey", issue.Path)
	assert.NotContains(t, mustJSON(t, te)+te.Message, "literal-key")
	_, err = e.a.TestNode(t.Context(), agent, TestNodeRequest{Type: sensitiveEchoType, Config: map[string]any{"apiKey": "{{secret.api_key}}"}, Input: 1})
	require.NoError(t, err)

	_, err = e.a.TestNode(t.Context(), agent, TestNodeRequest{Type: "action-http", Config: map[string]any{"url": "https://x.example"}})
	requireKind(t, err, ErrPermissionDenied)
	_, err = e.a.TestNode(t.Context(), agent, TestNodeRequest{Type: "no-such-type"})
	requireKind(t, err, ErrInvalidArguments)
	_, err = e.a.TestNode(t.Context(), agent, TestNodeRequest{Type: "process-transform", Config: map[string]any{"expression": "payload"},
		Input: strings.Repeat("x", flow.DryRunMaxInputBytes+1)})
	requireKind(t, err, ErrInvalidArguments)
	_, err = e.a.TestNode(t.Context(), e.token(storage.ScopeRead), TestNodeRequest{Type: "process-transform"})
	requireKind(t, err, ErrPermissionDenied)

	e.a.testNode = func(context.Context, *node.Registry, string, map[string]any, any) (flow.TestNodeResult, error) {
		return flow.TestNodeResult{}, flow.ErrSandboxBusy
	}
	_, err = e.a.TestNode(t.Context(), agent, TestNodeRequest{Type: "process-transform", Config: map[string]any{"expression": "payload"}})
	requireKind(t, err, ErrBusy)
}
