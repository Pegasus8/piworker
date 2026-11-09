package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Pegasus8/piworker/internal/agentauth"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	_ "github.com/Pegasus8/piworker/internal/node/builtin"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failNodeType is a test node whose error message carries whatever its
// "message" setting says (a node error leaking a secret).
const failNodeType = "test-service-fail"

type failNode struct{ msg string }

func (n *failNode) Process(context.Context, *types.Message) ([]*types.Message, error) {
	return nil, errors.New(n.msg)
}
func (n *failNode) Ports() ([]types.Port, []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}}, nil
}
func (n *failNode) Validate() error { return nil }

var registerFailNode sync.Once

func init() {
	registerFailNode.Do(func() {
		err := node.Register(node.NodeTypeInfo{
			Type: failNodeType, Name: "Fail", Category: types.NodeCategoryOutput, Risk: node.RiskSafe,
			Inputs: []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}},
		}, func(cfg map[string]any) (node.Node, error) {
			msg, _ := cfg["message"].(string)
			return &failNode{msg: msg}, nil
		})
		if err != nil {
			panic(err)
		}
	})
}

type env struct {
	t       *testing.T
	manager *flow.RuntimeManager
	store   *storage.SQLiteStore
	secrets *secrets.Store
	audit   *AuditWriter
	tools   *Tools
}

func newEnv(t *testing.T, customize ...func(*Options, *[]flow.ManagerOption)) *env {
	t.Helper()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "svc.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	e := &env{t: t, store: store, secrets: secrets.NewStore()}
	e.audit = NewAuditWriter(store, AuditOptions{FlushInterval: time.Hour})
	opts := Options{Store: store, Secrets: e.secrets, Audit: e.audit, Logger: zerolog.Nop()}
	var mopts []flow.ManagerOption
	for _, c := range customize {
		c(&opts, &mopts)
	}
	e.manager = flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop(), mopts...)
	t.Cleanup(e.manager.StopAll)
	opts.Runtimes = e.manager
	e.tools = NewTools(opts)
	return e
}

type step struct {
	id     string
	typ    string
	config map[string]any
}

func toolConfig(over map[string]any) map[string]any {
	cfg := map[string]any{
		"toolName":    "svc_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		"description": "Echoes the arguments back so tests can look at the result.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"door": map[string]any{"type": "string", "enum": []any{"left", "right"}},
				"text": map[string]any{"type": "string", "maxLength": float64(4096)},
			},
		},
	}
	for k, v := range over {
		cfg[k] = v
	}
	return cfg
}

// fan connects trigger-tool "tool" to every step directly.
func fan(cfg map[string]any, steps ...step) *flow.Flow {
	f := flow.NewFlow("svc")
	tool := flow.NewNode("trigger-tool", flow.NodeCategoryInput)
	tool.ID, tool.Config = "tool", cfg
	f.AddNode(*tool)
	for _, s := range steps {
		n := flow.NewNode(s.typ, flow.NodeCategoryOutput)
		n.ID, n.Config = s.id, s.config
		if n.Config == nil {
			n.Config = map[string]any{}
		}
		f.AddNode(*n)
		f.AddConnection(*flow.NewConnection("tool", "output", s.id, "input"))
	}
	return f
}

func echo() step { return step{id: "reply", typ: "action-reply"} }

func slowBranch(ms int) step {
	return step{id: "slow", typ: "process-delay", config: map[string]any{"delay": float64(ms)}}
}

// strictConfig is a tool config whose arguments may reach a privileged node.
func strictConfig() map[string]any {
	return toolConfig(map[string]any{"inputSchema": map[string]any{
		"type":       "object",
		"properties": map[string]any{"door": map[string]any{"type": "string", "enum": []any{"left", "right"}}},
	}})
}

func command() step {
	return step{id: "cmd", typ: "action-command", config: map[string]any{"command": "true"}}
}

func (e *env) deploy(f *flow.Flow) flow.ToolInfo {
	e.t.Helper()
	require.NoError(e.t, e.manager.Deploy(context.Background(), f))
	rt, ok := e.manager.GetRuntime(f.ID)
	require.True(e.t, ok)
	tools := rt.Tools()
	require.Len(e.t, tools, 1)
	return tools[0]
}

func (e *env) agent(req storage.NewAPIToken) Actor {
	e.t.Helper()
	if req.Name == "" {
		req.Name = "agent-" + uuid.NewString()[:8]
	}
	if req.Scopes == nil {
		req.Scopes = []string{storage.ScopeCall}
	}
	tok, _, err := e.store.CreateAPIToken(req, time.Now())
	require.NoError(e.t, err)
	return AgentActor(agentauth.TokenInfo{ID: tok.ID, Name: tok.Name, Scopes: tok.Scopes, Privileged: tok.Privileged, Grants: tok.Grants, AllTools: tok.AllTools, ExpiresAt: tok.ExpiresAt})
}

func grant(info flow.ToolInfo) []storage.TokenGrant {
	return []storage.TokenGrant{{FlowID: info.FlowID, TriggerNodeID: info.TriggerNodeID, Fingerprint: info.Fingerprint}}
}

func (e *env) call(actor Actor, info flow.ToolInfo, args map[string]any) (flow.CallResult, error) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return e.tools.Call(ctx, actor, ToolByNode(info.FlowID, info.TriggerNodeID), args)
}

func (e *env) waitIdle(info flow.ToolInfo) {
	e.t.Helper()
	require.Eventually(e.t, func() bool { return e.tools.slots.inflight(toolKey(info)) == 0 }, 5*time.Second, 5*time.Millisecond)
}

func TestEditorCallsAnyTool(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(nil), echo()))
	res, err := e.call(EditorActor(), info, map[string]any{"door": "left"})
	require.NoError(t, err)
	assert.Equal(t, flow.CallStatusCompleted, res.Status, "%+v", res)
	assert.NotEmpty(t, res.RunID)
	assert.JSONEq(t, `{"door":"left"}`, string(res.Result))
}

func TestCallByToolName(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(nil), echo()))
	agent := e.agent(storage.NewAPIToken{AllTools: true})
	res, err := e.tools.Call(context.Background(), agent, ToolByName(info.ToolName), map[string]any{"door": "right"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"door":"right"}`, string(res.Result))

	_, err = e.tools.Call(context.Background(), agent, ToolByName("missing_tool"), nil)
	requireKind(t, err, ErrToolNotFound)
	_, err = e.tools.Call(context.Background(), EditorActor(), ToolByNode(info.FlowID, "nope"), nil)
	requireKind(t, err, ErrToolNotFound)
}

func TestCallPermissionMatrix(t *testing.T) {
	e := newEnv(t)
	safe := e.deploy(fan(toolConfig(nil), echo()))
	priv := e.deploy(fan(strictConfig(), command()))
	require.Equal(t, node.RiskPrivileged, priv.Risk)

	cases := []struct {
		name string
		tok  storage.NewAPIToken
		tool flow.ToolInfo
		want error // nil = allowed
	}{
		{"granted", storage.NewAPIToken{Grants: grant(safe)}, safe, nil},
		{"all tools", storage.NewAPIToken{AllTools: true}, safe, nil},
		{"not granted is hidden", storage.NewAPIToken{}, safe, ErrToolNotFound},
		{"grant for another tool", storage.NewAPIToken{Grants: grant(priv)}, safe, ErrToolNotFound},
		{"no call scope", storage.NewAPIToken{Scopes: []string{storage.ScopeRead}, Grants: grant(safe)}, safe, ErrPermissionDenied},
		{"privileged granted with the flag", storage.NewAPIToken{Privileged: true, Grants: grant(priv)}, priv, nil},
		{"privileged granted without the flag", storage.NewAPIToken{Grants: grant(priv)}, priv, ErrPermissionDenied},
		{"all tools never includes privileged tools", storage.NewAPIToken{AllTools: true, Privileged: true}, priv, ErrToolNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent := e.agent(tc.tok)
			args := map[string]any{"door": "left"}
			res, err := e.call(agent, tc.tool, args)
			if tc.want == nil {
				require.NoError(t, err)
				assert.NotEqual(t, flow.CallStatusApprovalRequired, res.Status)
				e.waitIdle(tc.tool)
				return
			}
			requireKind(t, err, tc.want)
			assert.Empty(t, res.RunID, "no run started")
		})
	}
}

func TestFingerprintMismatchSuspendsTheGrant(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(nil), echo()))
	stale := info
	stale.Fingerprint = "sha256:" + strings.Repeat("0", 64)
	agent := e.agent(storage.NewAPIToken{Grants: grant(stale)})
	evicted := &recordingEvictor{}
	e.tools.tokens = evicted

	_, err := e.call(agent, info, map[string]any{"door": "left"})
	te := requireKind(t, err, ErrReconfirmRequired)
	assert.Contains(t, te.Message, "re-confirm")

	tok, err := e.store.GetAPIToken(agent.TokenID)
	require.NoError(t, err)
	require.Len(t, tok.Grants, 1)
	assert.True(t, tok.Grants[0].Suspended, "the grant waits for the admin")
	assert.Equal(t, stale.Fingerprint, tok.Grants[0].Fingerprint, "kept for the re-confirmation")
	assert.Equal(t, []string{agent.TokenID}, evicted.ids(), "cached token info is dropped")

	// A suspended grant stays refused even with the right fingerprint.
	agent.Grants = []storage.TokenGrant{{FlowID: info.FlowID, TriggerNodeID: info.TriggerNodeID, Fingerprint: info.Fingerprint, Suspended: true}}
	_, err = e.call(agent, info, map[string]any{"door": "left"})
	requireKind(t, err, ErrReconfirmRequired)
}

type recordingEvictor struct {
	mu  sync.Mutex
	got []string
}

func (r *recordingEvictor) Evict(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, id)
}

func (r *recordingEvictor) ids() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.got...)
}

func TestInvalidArgumentsStartNoRun(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(nil), echo()))
	for _, actor := range []Actor{EditorActor(), e.agent(storage.NewAPIToken{AllTools: true})} {
		res, err := e.call(actor, info, map[string]any{"door": "garden", "extra": 1})
		te := requireKind(t, err, ErrInvalidArguments)
		assert.Empty(t, res.RunID)
		require.Len(t, te.Issues, 2)
		assert.Equal(t, "/door", te.Issues[0].Pointer)
		assert.Equal(t, "/extra", te.Issues[1].Pointer)
		assert.Equal(t, 0, e.tools.slots.inflight(toolKey(info)))
	}
}

func TestConfirmAdminCreatesAnApproval(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(map[string]any{"confirm": "admin"}), echo()))
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})

	res, err := e.call(agent, info, map[string]any{"door": "left"})
	require.NoError(t, err)
	assert.Equal(t, flow.CallStatusApprovalRequired, res.Status)
	require.NotEmpty(t, res.RunID)

	a, err := e.store.GetAgentApproval(res.RunID)
	require.NoError(t, err)
	assert.Equal(t, storage.ApprovalKindToolCall, a.Kind)
	assert.Equal(t, agent.TokenID, a.TokenID)
	assert.Equal(t, info.FlowID, a.FlowID)
	assert.Equal(t, info.TriggerNodeID, a.Ref)
	assert.JSONEq(t, `{"door":"left"}`, string(a.Args))
	assert.Contains(t, a.Summary, info.ToolName)
	assert.Equal(t, info.ToolName, a.ToolName)
	assert.Equal(t, info.Fingerprint, a.ToolFingerprint)
	assert.Equal(t, storage.ApprovalPending, a.Status)

	got, err := e.tools.Result(context.Background(), agent, res.RunID)
	require.NoError(t, err)
	assert.Equal(t, flow.CallStatusApprovalRequired, got.Status)

	// The editor's test call is not gated.
	res, err = e.call(EditorActor(), info, map[string]any{"door": "left"})
	require.NoError(t, err)
	assert.Equal(t, flow.CallStatusCompleted, res.Status)
}

func TestApprovalQueueFullIsBusy(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(map[string]any{"confirm": "admin"}), echo()))
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})
	for range storage.MaxPendingApprovalsPerToken {
		_, err := e.call(agent, info, map[string]any{"door": "left"})
		require.NoError(t, err)
	}
	_, err := e.call(agent, info, map[string]any{"door": "left"})
	requireKind(t, err, ErrBusy)
}

func TestExecuteApprovalRunsTheCallAndStoresTheResult(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(map[string]any{"confirm": "admin"}), echo()))
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})
	res, err := e.call(agent, info, map[string]any{"door": "right"})
	require.NoError(t, err)

	approved, err := e.store.DecideAgentApproval(res.RunID, true, "admin", time.Now())
	require.NoError(t, err)
	require.NoError(t, e.tools.ExecuteApproval(context.Background(), *approved))

	stored, err := e.store.GetAgentApproval(res.RunID)
	require.NoError(t, err)
	var final flow.CallResult
	require.NoError(t, json.Unmarshal(stored.Result, &final))
	assert.Equal(t, flow.CallStatusCompleted, final.Status)
	assert.JSONEq(t, `{"door":"right"}`, string(final.Result))

	got, err := e.tools.Result(context.Background(), agent, res.RunID)
	require.NoError(t, err)
	assert.Equal(t, flow.CallStatusCompleted, got.Status)
	assert.JSONEq(t, `{"door":"right"}`, string(got.Result))

	other := e.agent(storage.NewAPIToken{Grants: grant(info)})
	_, err = e.tools.Result(context.Background(), other, res.RunID)
	requireKind(t, err, ErrRunNotFound)
}

func TestRejectedApprovalResult(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(map[string]any{"confirm": "admin"}), echo()))
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})
	res, err := e.call(agent, info, map[string]any{"door": "right"})
	require.NoError(t, err)
	_, err = e.store.DecideAgentApproval(res.RunID, false, "admin", time.Now())
	require.NoError(t, err)
	got, err := e.tools.Result(context.Background(), agent, res.RunID)
	require.NoError(t, err)
	assert.Equal(t, flow.CallStatusRejected, got.Status)
	assert.Equal(t, res.RunID, got.RunID)
}

func TestExecuteApprovalRespectsRevokedTokens(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(map[string]any{"confirm": "admin"}), echo()))
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})
	res, err := e.call(agent, info, map[string]any{"door": "right"})
	require.NoError(t, err)
	approved, err := e.store.DecideAgentApproval(res.RunID, true, "admin", time.Now())
	require.NoError(t, err)
	require.NoError(t, e.store.RevokeAPIToken(agent.TokenID, time.Now()))

	err = e.tools.ExecuteApproval(context.Background(), *approved)
	requireKind(t, err, ErrPermissionDenied)
	stored, err := e.store.GetAgentApproval(res.RunID)
	require.NoError(t, err)
	var final flow.CallResult
	require.NoError(t, json.Unmarshal(stored.Result, &final))
	assert.Equal(t, flow.CallStatusRejected, final.Status)
	assert.Equal(t, 0, e.tools.slots.inflight(toolKey(info)), "nothing ran")
}

// An approval runs the tool the admin was shown. When the flow is redeployed
// with the tool renamed or changed (risk, annotations, schema, confirm)
// before the approval is executed, nothing runs, even for an all_tools token,
// which no grant fingerprint binds.
func TestExecuteApprovalRefusesAChangedTool(t *testing.T) {
	cases := []struct {
		name    string
		over    map[string]any
		changed bool
	}{
		{"unchanged", nil, false},
		{"schema", map[string]any{"inputSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{"door": map[string]any{"type": "string", "maxLength": float64(64)}},
		}}, true},
		{"name", map[string]any{"toolName": "svc_renamed_" + strings.ReplaceAll(uuid.NewString(), "-", "")}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			cfg := toolConfig(map[string]any{"confirm": "admin"})
			f := fan(cfg, echo())
			info := e.deploy(f)
			agent := e.agent(storage.NewAPIToken{AllTools: true})
			res, err := e.call(agent, info, map[string]any{"door": "left"})
			require.NoError(t, err)
			require.Equal(t, flow.CallStatusApprovalRequired, res.Status)

			require.NoError(t, e.manager.Undeploy(f.ID))
			for k, v := range tc.over {
				cfg[k] = v
			}
			redeployed := e.deploy(f)
			require.Equal(t, tc.changed, redeployed.Fingerprint != info.Fingerprint || redeployed.ToolName != info.ToolName)

			approved, err := e.store.DecideAgentApproval(res.RunID, true, "admin", time.Now())
			require.NoError(t, err)
			err = e.tools.ExecuteApproval(context.Background(), *approved)
			stored, getErr := e.store.GetAgentApproval(res.RunID)
			require.NoError(t, getErr)
			var final flow.CallResult
			require.NoError(t, json.Unmarshal(stored.Result, &final))
			if !tc.changed {
				require.NoError(t, err)
				assert.Equal(t, flow.CallStatusCompleted, final.Status)
				return
			}
			te := requireKind(t, err, ErrUnavailable)
			assert.Contains(t, te.Message, "changed since")
			assert.Equal(t, flow.CallStatusError, final.Status)
			assert.Equal(t, res.RunID, final.RunID)
			require.NotNil(t, final.Error)
			assert.Contains(t, final.Error.Message, "the tool changed since the request")
			assert.Empty(t, e.tools.results.runs, "nothing ran")
		})
	}
}

// An approval recorded without the tool's identity (made before it was
// recorded) cannot prove which tool the admin saw, so it runs nothing.
func TestExecuteApprovalNeedsTheToolIdentity(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(map[string]any{"confirm": "admin"}), echo()))
	agent := e.agent(storage.NewAPIToken{AllTools: true})
	a, err := e.store.CreateAgentApproval(storage.AgentApproval{
		Kind: storage.ApprovalKindToolCall, TokenID: agent.TokenID, FlowID: info.FlowID, Ref: info.TriggerNodeID,
		Args: json.RawMessage(`{"door":"left"}`), Summary: "legacy",
	}, time.Now())
	require.NoError(t, err)
	approved, err := e.store.DecideAgentApproval(a.ID, true, "admin", time.Now())
	require.NoError(t, err)
	err = e.tools.ExecuteApproval(context.Background(), *approved)
	requireKind(t, err, ErrUnavailable)
	assert.Empty(t, e.tools.results.runs, "nothing ran")
}

// approvedApproval stores an approved tool-call approval of a new token.
func (e *env) approvedApproval() string {
	e.t.Helper()
	agent := e.agent(storage.NewAPIToken{AllTools: true})
	a, err := e.store.CreateAgentApproval(storage.AgentApproval{
		Kind: storage.ApprovalKindToolCall, TokenID: agent.TokenID, FlowID: "f", Ref: "tool",
	}, time.Now())
	require.NoError(e.t, err)
	_, err = e.store.DecideAgentApproval(a.ID, true, "admin", time.Now())
	require.NoError(e.t, err)
	return a.ID
}

func (e *env) storedApprovalResult(id string) flow.CallResult {
	e.t.Helper()
	a, err := e.store.GetAgentApproval(id)
	require.NoError(e.t, err)
	var res flow.CallResult
	require.NoError(e.t, json.Unmarshal(a.Result, &res))
	return res
}

// The stored result is not HTML-escaped (each <, > and & would take six
// bytes), and a result that cannot be stored is replaced by an error, so
// the agent never reads "running" forever.
func TestApprovalResultStorage(t *testing.T) {
	e := newEnv(t)
	html := `"` + strings.Repeat("<b>&", (1<<20)/4) + `"` // 1 MiB, 6 MiB escaped
	id := e.approvedApproval()
	e.tools.storeApprovalResult(id, flow.CallResult{Status: flow.CallStatusCompleted, RunID: "run-1", Result: json.RawMessage(html)})
	got := e.storedApprovalResult(id)
	assert.Equal(t, flow.CallStatusCompleted, got.Status)
	assert.JSONEq(t, html, string(got.Result))

	id = e.approvedApproval()
	e.tools.storeApprovalResult(id, flow.CallResult{Status: flow.CallStatusRunning, RunID: "run-2"})
	tooBig := `"` + strings.Repeat("x", storage.MaxApprovalResultBytes) + `"`
	e.tools.storeApprovalResult(id, flow.CallResult{Status: flow.CallStatusCompleted, RunID: "run-2", Result: json.RawMessage(tooBig)})
	got = e.storedApprovalResult(id)
	assert.Equal(t, flow.CallStatusError, got.Status)
	assert.Equal(t, "run-2", got.RunID)
	require.NotNil(t, got.Error)
	assert.Contains(t, got.Error.Message, "could not be stored")
}

// Arguments too large to queue for the admin are the agent's problem
// (invalid arguments), not an internal failure.
func TestApprovalArgumentsTooLargeToQueue(t *testing.T) {
	e := newEnv(t)
	props := map[string]any{}
	args := map[string]any{}
	for i := range 20 {
		name := fmt.Sprintf("p%02d", i)
		props[name] = map[string]any{"type": "string", "maxLength": float64(4096)}
		args[name] = strings.Repeat("a", 4000)
	}
	info := e.deploy(fan(toolConfig(map[string]any{"confirm": "admin", "inputSchema": map[string]any{
		"type": "object", "properties": props,
	}}), echo()))
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})
	_, err := e.call(agent, info, args)
	te := requireKind(t, err, ErrInvalidArguments)
	assert.Contains(t, te.Message, "too large")
}

func TestExecuteApprovalRefusesOtherKinds(t *testing.T) {
	e := newEnv(t)
	err := e.tools.ExecuteApproval(context.Background(), storage.AgentApproval{Kind: storage.ApprovalKindDraftDeploy})
	requireKind(t, err, ErrUnavailable)
}

// The slot of a call is held until its run ends, not until the reply.
func TestSlotsAreHeldUntilTheRunEnds(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(map[string]any{"maxConcurrent": float64(1)}), echo(), slowBranch(300)))

	start := time.Now()
	res, err := e.call(EditorActor(), info, map[string]any{"door": "left"})
	require.NoError(t, err)
	require.Equal(t, flow.CallStatusCompleted, res.Status)
	require.Less(t, time.Since(start), 250*time.Millisecond, "the reply does not wait for the side branch")

	_, err = e.call(EditorActor(), info, map[string]any{"door": "left"})
	requireKind(t, err, ErrBusy)

	e.waitIdle(info)
	assert.GreaterOrEqual(t, time.Since(start), 300*time.Millisecond)
	_, err = e.call(EditorActor(), info, map[string]any{"door": "left"})
	require.NoError(t, err)
}

func TestPerTokenLimit(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(map[string]any{"maxConcurrent": float64(8)}), echo(), slowBranch(300)))
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})
	for range DefaultPerTokenInflight {
		_, err := e.call(agent, info, map[string]any{"door": "left"})
		require.NoError(t, err)
	}
	_, err := e.call(agent, info, map[string]any{"door": "left"})
	requireKind(t, err, ErrBusy)
	_, err = e.call(e.agent(storage.NewAPIToken{Grants: grant(info)}), info, map[string]any{"door": "left"})
	require.NoError(t, err, "another token has its own slots")
}

func TestGlobalLimit(t *testing.T) {
	e := newEnv(t, func(o *Options, _ *[]flow.ManagerOption) { o.MaxInflight = 2 })
	info := e.deploy(fan(toolConfig(map[string]any{"maxConcurrent": float64(8)}), echo(), slowBranch(300)))
	for range 2 {
		_, err := e.call(EditorActor(), info, map[string]any{"door": "left"})
		require.NoError(t, err)
	}
	_, err := e.call(EditorActor(), info, map[string]any{"door": "left"})
	requireKind(t, err, ErrBusy)
}

func TestCooldownFromRunEnd(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(map[string]any{"cooldownMs": float64(300)}), echo()))
	_, err := e.call(EditorActor(), info, map[string]any{"door": "left"})
	require.NoError(t, err)
	e.waitIdle(info)

	_, err = e.call(EditorActor(), info, map[string]any{"door": "left"})
	te := requireKind(t, err, ErrCooldown)
	assert.Positive(t, te.RetryAfter)
	assert.LessOrEqual(t, te.RetryAfter, 300*time.Millisecond)

	time.Sleep(te.RetryAfter + 20*time.Millisecond)
	_, err = e.call(EditorActor(), info, map[string]any{"door": "left"})
	require.NoError(t, err)
}

// The runtime's own cap on tracked calls is reported as busy.
func TestTooManyRuntimeCallsIsBusy(t *testing.T) {
	e := newEnv(t, func(_ *Options, m *[]flow.ManagerOption) {
		*m = append(*m, flow.WithRuntimeOptions(flow.WithMaxCallWaiters(1)))
	})
	info := e.deploy(fan(toolConfig(map[string]any{"maxConcurrent": float64(4)}), echo(), slowBranch(300)))
	_, err := e.call(EditorActor(), info, map[string]any{"door": "left"})
	require.NoError(t, err)
	_, err = e.call(EditorActor(), info, map[string]any{"door": "left"})
	requireKind(t, err, ErrBusy)
	assert.Equal(t, 1, e.tools.slots.inflight(toolKey(info)), "the refused call holds no slot")
}

// A call the runtime refuses to start (its waiter cap is full, here filled
// by another tool of the same flow) runs nothing, so it starts no cooldown.
func TestRefusedStartDoesNotStartTheCooldown(t *testing.T) {
	e := newEnv(t, func(_ *Options, m *[]flow.ManagerOption) {
		*m = append(*m, flow.WithRuntimeOptions(flow.WithMaxCallWaiters(1)))
	})
	f := fan(toolConfig(map[string]any{"cooldownMs": float64(60000)}), echo())
	other := flow.NewNode("trigger-tool", flow.NodeCategoryInput)
	other.ID, other.Config = "other", toolConfig(nil)
	f.AddNode(*other)
	slow := flow.NewNode("process-delay", flow.NodeCategoryProcessing)
	slow.ID, slow.Config = "slow", map[string]any{"delay": float64(300)}
	f.AddNode(*slow)
	f.AddConnection(*flow.NewConnection("other", "output", "reply", "input"))
	f.AddConnection(*flow.NewConnection("other", "output", "slow", "input"))
	require.NoError(t, e.manager.Deploy(context.Background(), f))
	rt, ok := e.manager.GetRuntime(f.ID)
	require.True(t, ok)
	var cooled, busy flow.ToolInfo
	for _, ti := range rt.Tools() {
		if ti.TriggerNodeID == "tool" {
			cooled = ti
		} else {
			busy = ti
		}
	}

	_, err := e.call(EditorActor(), busy, map[string]any{"door": "left"})
	require.NoError(t, err, "holds the flow's only waiter until its slow branch ends")
	_, err = e.call(EditorActor(), cooled, map[string]any{"door": "left"})
	requireKind(t, err, ErrBusy)

	e.waitIdle(busy)
	res, err := e.call(EditorActor(), cooled, map[string]any{"door": "left"})
	require.NoError(t, err, "the refused call did not start the cooldown")
	assert.Equal(t, flow.CallStatusCompleted, res.Status)
}

// Results are redacted of every known secret, in every form, for the editor
// and agents alike (TestToolResultsNeverDependOnTheArgumentsSent).
func TestRedactionCanaries(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.secrets.Set("api_key", canary))
	info := e.deploy(fan(toolConfig(nil), echo()))
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})

	for name, leak := range map[string]string{
		"literal":     canary,
		"url-encoded": url.QueryEscape(canary),
		"base64":      base64.StdEncoding.EncodeToString([]byte(canary)),
	} {
		res, err := e.call(EditorActor(), info, map[string]any{"text": "value: " + leak})
		require.NoError(t, err, name)
		e.waitIdle(info)
		assert.NotContains(t, string(res.Result), leak, name)
		assert.Contains(t, string(res.Result), redactedMark, name)
		assert.True(t, json.Valid(res.Result), name)

		res, err = e.call(agent, info, map[string]any{"text": "value: " + leak})
		require.NoError(t, err, name)
		e.waitIdle(info)
		assert.NotContains(t, string(res.Result), leak, "agent: %s", name)
		assert.Contains(t, string(res.Result), redactedMark, "agent: %s", name)
	}

	// A node error carrying the secret (and a credential-bearing URL).
	b64 := base64.StdEncoding.EncodeToString([]byte("user:" + canary))
	msg := "request failed for " + canary + " (auth " + b64 + ") via https://bob:" + url.QueryEscape("hunter2!") + "@api.example.com/x?key=" + url.QueryEscape(canary)
	failing := e.deploy(fan(toolConfig(nil), step{id: "fail", typ: failNodeType, config: map[string]any{"message": msg}}))
	res, err := e.call(EditorActor(), failing, map[string]any{"door": "left"})
	require.NoError(t, err)
	require.Equal(t, flow.CallStatusError, res.Status)
	require.NotNil(t, res.Error)
	assert.NotContains(t, res.Error.Message, canary)
	assert.NotContains(t, res.Error.Message, "hunter2")
	assert.NotContains(t, res.Error.Message, url.QueryEscape(canary))
	assert.NotContains(t, res.Error.Message, b64)
	assert.Contains(t, res.Error.Message, "https://api.example.com/<redacted>")
	assert.Equal(t, "fail", res.Error.NodeID)
}

// A tool error hides URL paths too: Telegram bot, Discord and Slack webhook
// tokens sit in the path, not in the userinfo or the query (the read views'
// rule, hideURLsInText).
func TestToolErrorHidesURLPaths(t *testing.T) {
	e := newEnv(t)
	msg := "POST https://api.telegram.org/bot123456:AAH-path-token/sendMessage failed; " +
		"see https://hooks.slack.com/services/T000/B000/XXXXsecretXXXX. Plain https://example.com/ stays."
	failing := e.deploy(fan(toolConfig(nil), step{id: "fail", typ: failNodeType, config: map[string]any{"message": msg}}))
	res, err := e.call(EditorActor(), failing, map[string]any{"door": "left"})
	require.NoError(t, err)
	require.NotNil(t, res.Error)
	assert.NotContains(t, res.Error.Message, "AAH-path-token")
	assert.NotContains(t, res.Error.Message, "XXXXsecretXXXX")
	assert.Contains(t, res.Error.Message, "https://api.telegram.org/<redacted>")
	assert.Contains(t, res.Error.Message, "https://hooks.slack.com/<redacted>.")
	assert.Contains(t, res.Error.Message, "https://example.com/ stays")
}

func TestLargeResultsAreTruncatedToValidJSON(t *testing.T) {
	e := newEnv(t, func(o *Options, _ *[]flow.ManagerOption) { o.MaxResultBytes = 1024 })
	info := e.deploy(fan(toolConfig(nil), echo()))
	res, err := e.call(EditorActor(), info, map[string]any{"text": strings.Repeat("x\"é", 1300)})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(res.Result), 1024)
	require.True(t, json.Valid(res.Result))
	var v map[string]any
	require.NoError(t, json.Unmarshal(res.Result, &v))
	assert.Equal(t, true, v["truncated"])
}

func TestOnlyTheCallerReadsAResult(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(nil), echo()))
	owner := e.agent(storage.NewAPIToken{AllTools: true})
	res, err := e.call(owner, info, map[string]any{"door": "left"})
	require.NoError(t, err)

	got, err := e.tools.Result(context.Background(), owner, res.RunID)
	require.NoError(t, err)
	assert.Equal(t, res, got, "the caller can read it again")

	_, err = e.tools.Result(context.Background(), e.agent(storage.NewAPIToken{AllTools: true}), res.RunID)
	requireKind(t, err, ErrRunNotFound)
	_, err = e.tools.Result(context.Background(), EditorActor(), res.RunID)
	requireKind(t, err, ErrRunNotFound)

	editorRun, err := e.call(EditorActor(), info, map[string]any{"door": "left"})
	require.NoError(t, err)
	_, err = e.tools.Result(context.Background(), owner, editorRun.RunID)
	requireKind(t, err, ErrRunNotFound)
	_, err = e.tools.Result(context.Background(), owner, "no-such-run")
	requireKind(t, err, ErrRunNotFound)
}

// A call that outlives its timeout reports running; the result is fetched
// later.
func TestResultOfARunningCall(t *testing.T) {
	e := newEnv(t)
	f := flow.NewFlow("slow")
	tool := flow.NewNode("trigger-tool", flow.NodeCategoryInput)
	tool.ID, tool.Config = "tool", toolConfig(map[string]any{"timeoutMs": float64(1000)})
	wait := flow.NewNode("process-delay", flow.NodeCategoryProcessing)
	wait.ID, wait.Config = "wait", map[string]any{"delay": float64(1400)}
	rep := flow.NewNode("action-reply", flow.NodeCategoryOutput)
	rep.ID = "reply"
	f.AddNode(*tool)
	f.AddNode(*wait)
	f.AddNode(*rep)
	f.AddConnection(*flow.NewConnection("tool", "output", "wait", "input"))
	f.AddConnection(*flow.NewConnection("wait", "output", "reply", "input"))
	info := e.deploy(f)
	agent := e.agent(storage.NewAPIToken{AllTools: true})

	res, err := e.call(agent, info, map[string]any{"door": "left"})
	require.NoError(t, err)
	require.Equal(t, flow.CallStatusRunning, res.Status)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := e.tools.Result(ctx, agent, res.RunID)
	require.NoError(t, err)
	assert.Equal(t, flow.CallStatusCompleted, got.Status)
	assert.Equal(t, res.RunID, got.RunID)
	assert.JSONEq(t, `{"door":"left"}`, string(got.Result))
}

func TestAgentCallsAreAudited(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(nil), echo()))
	priv := e.deploy(fan(strictConfig(), command()))
	agent := e.agent(storage.NewAPIToken{Privileged: true, Grants: append(grant(info), grant(priv)...)})

	res, err := e.call(agent, info, map[string]any{"door": "left"})
	require.NoError(t, err)
	e.waitIdle(info)
	_, err = e.call(agent, priv, map[string]any{"door": "left"})
	require.NoError(t, err)
	_, err = e.call(agent, info, map[string]any{"door": "nope"}) // invalid: a counter, not a row
	require.Error(t, err)
	_, err = e.call(EditorActor(), info, map[string]any{"door": "left"}) // editor: not an MCP call
	require.NoError(t, err)
	require.NoError(t, e.audit.Flush())

	rows, err := e.store.ListMCPAudit(storage.MCPAuditFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	byTool := map[string]storage.MCPAuditEntry{}
	for _, r := range rows {
		byTool[r.Tool] = r
	}
	first := byTool[info.ToolName]
	assert.Equal(t, agent.TokenID, first.TokenID)
	assert.Equal(t, agent.TokenName, first.TokenName)
	assert.Equal(t, info.FlowID, first.FlowID)
	assert.Equal(t, res.RunID, first.RunID)
	assert.Equal(t, "completed", first.Outcome)
	assert.Len(t, first.ArgsSHA256, 64)
	assert.JSONEq(t, `{"door":"left"}`, first.ArgsPreview)
	assert.False(t, first.Privileged)
	assert.True(t, byTool[priv.ToolName].Privileged)

	rejections, err := e.store.ListMCPRejections(storage.MCPAuditFilter{})
	require.NoError(t, err)
	require.Len(t, rejections, 1)
	assert.Equal(t, agent.TokenID, rejections[0].TokenID)
	assert.Equal(t, "invalid_arguments", rejections[0].Reason)
	assert.Equal(t, 1, rejections[0].Count)
}

// A call of a privileged tool is a privileged audit row (never evicted
// early) only when it reached the runtime or queued an approval. A refusal
// before that (a suspended or stale grant, a token without the privileged
// flag) is an ordinary row the row cap may evict, so an agent retrying a
// refused call cannot fill the audit with rows that are kept 30 days.
func TestRefusedPrivilegedCallsAreNotPrivilegedRows(t *testing.T) {
	e := newEnv(t)
	priv := e.deploy(fan(strictConfig(), command()))
	confirmCfg := strictConfig()
	confirmCfg["confirm"] = "admin"
	privConfirm := e.deploy(fan(confirmCfg, step{id: "cmd", typ: "action-command", config: map[string]any{"command": "true"}}))
	require.Equal(t, node.RiskPrivileged, priv.Risk)
	require.Equal(t, node.RiskPrivileged, privConfirm.Risk)
	args := map[string]any{"door": "left"}

	unflagged := e.agent(storage.NewAPIToken{Grants: grant(priv)})
	_, err := e.call(unflagged, priv, args)
	requireKind(t, err, ErrPermissionDenied)

	stale := priv
	stale.Fingerprint = "sha256:" + strings.Repeat("0", 64)
	changed := e.agent(storage.NewAPIToken{Privileged: true, Grants: grant(stale)})
	_, err = e.call(changed, priv, args) // suspends the grant
	requireKind(t, err, ErrReconfirmRequired)
	tok, err := e.store.GetAPIToken(changed.TokenID)
	require.NoError(t, err)
	require.True(t, tok.Grants[0].Suspended)
	suspended := AgentActor(agentauth.TokenInfo{ID: tok.ID, Name: tok.Name, Scopes: tok.Scopes, Privileged: true, Grants: tok.Grants})
	_, err = e.call(suspended, priv, args)
	requireKind(t, err, ErrReconfirmRequired)

	require.NoError(t, e.audit.Flush())
	rows, err := e.store.ListMCPAudit(storage.MCPAuditFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 3)
	for _, r := range rows {
		assert.Contains(t, []string{"forbidden", "reconfirm_required"}, r.Outcome)
		assert.Equal(t, priv.ToolName, r.Tool)
		assert.False(t, r.Privileged, "%s: refused before the runtime", r.Outcome)
	}

	// A run and a queued approval of a privileged tool are privileged rows.
	allowed := e.agent(storage.NewAPIToken{Privileged: true, Grants: append(grant(priv), grant(privConfirm)...)})
	res, err := e.call(allowed, priv, args)
	require.NoError(t, err)
	require.NotEmpty(t, res.RunID)
	e.waitIdle(priv)
	res, err = e.call(allowed, privConfirm, args)
	require.NoError(t, err)
	require.Equal(t, flow.CallStatusApprovalRequired, res.Status)
	require.NoError(t, e.audit.Flush())
	rows, err = e.store.ListMCPAudit(storage.MCPAuditFilter{TokenID: allowed.TokenID})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, r := range rows {
		assert.True(t, r.Privileged, "%s: %s", r.Tool, r.Outcome)
	}
}

func TestAuditPreviewIsRedacted(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.secrets.Set("api_key", canary))
	info := e.deploy(fan(toolConfig(nil), echo()))
	agent := e.agent(storage.NewAPIToken{AllTools: true})
	_, err := e.call(agent, info, map[string]any{"text": canary})
	require.NoError(t, err)
	require.NoError(t, e.audit.Flush())
	rows, err := e.store.ListMCPAudit(storage.MCPAuditFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.NotContains(t, rows[0].ArgsPreview, canary)
	escaped, err := json.Marshal(canary) // "&" becomes \u0026
	require.NoError(t, err)
	assert.NotContains(t, rows[0].ArgsPreview, strings.Trim(string(escaped), `"`), "JSON escaping must not hide the secret")
	assert.JSONEq(t, `{"text":"<redacted>"}`, rows[0].ArgsPreview)
}

// The preview is cut to what storage keeps before it waits in the queue,
// so up to DefaultAuditMaxQueue entries never pin full-size arguments.
func TestAuditPreviewIsCappedBeforeQueueing(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(nil), echo()))
	agent := e.agent(storage.NewAPIToken{AllTools: true})
	_, err := e.call(agent, info, map[string]any{"text": strings.Repeat("é", 2000)})
	require.NoError(t, err)
	e.audit.mu.Lock()
	defer e.audit.mu.Unlock()
	require.Len(t, e.audit.queue, 1)
	preview := e.audit.queue[0].ArgsPreview
	assert.LessOrEqual(t, len(preview), storage.MaxMCPAuditPreview)
	assert.True(t, utf8.ValidString(preview))
	assert.True(t, strings.HasPrefix(preview, `{"text":"éé`), preview)
}

// An approved call that outlives its timeout first stores "running", then
// its final result once the run ends.
func TestApprovedRunningCallStoresTheFinalResult(t *testing.T) {
	e := newEnv(t)
	f := flow.NewFlow("slow approval")
	tool := flow.NewNode("trigger-tool", flow.NodeCategoryInput)
	tool.ID, tool.Config = "tool", toolConfig(map[string]any{"timeoutMs": float64(1000), "confirm": "admin"})
	wait := flow.NewNode("process-delay", flow.NodeCategoryProcessing)
	wait.ID, wait.Config = "wait", map[string]any{"delay": float64(1300)}
	rep := flow.NewNode("action-reply", flow.NodeCategoryOutput)
	rep.ID = "reply"
	f.AddNode(*tool)
	f.AddNode(*wait)
	f.AddNode(*rep)
	f.AddConnection(*flow.NewConnection("tool", "output", "wait", "input"))
	f.AddConnection(*flow.NewConnection("wait", "output", "reply", "input"))
	info := e.deploy(f)
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})

	res, err := e.call(agent, info, map[string]any{"door": "left"})
	require.NoError(t, err)
	approved, err := e.store.DecideAgentApproval(res.RunID, true, "admin", time.Now())
	require.NoError(t, err)
	require.NoError(t, e.tools.ExecuteApproval(context.Background(), *approved))

	stored := func() flow.CallResult {
		a, err := e.store.GetAgentApproval(res.RunID)
		require.NoError(t, err)
		var r flow.CallResult
		require.NoError(t, json.Unmarshal(a.Result, &r))
		return r
	}
	assert.Equal(t, flow.CallStatusRunning, stored().Status)
	require.Eventually(t, func() bool { return stored().Status == flow.CallStatusCompleted }, 5*time.Second, 20*time.Millisecond)
	assert.JSONEq(t, `{"door":"left"}`, string(stored().Result))

	got, err := e.tools.Result(context.Background(), agent, res.RunID)
	require.NoError(t, err)
	assert.Equal(t, flow.CallStatusCompleted, got.Status)
}

func TestStoppedFlowIsUnknown(t *testing.T) {
	e := newEnv(t)
	f := fan(toolConfig(nil), echo())
	info := e.deploy(f)
	require.NoError(t, e.manager.Undeploy(f.ID))
	_, err := e.call(EditorActor(), info, map[string]any{"door": "left"})
	requireKind(t, err, ErrToolNotFound)
}

func TestConcurrentCalls(t *testing.T) {
	e := newEnv(t, func(o *Options, _ *[]flow.ManagerOption) { o.MaxInflight = 64 })
	info := e.deploy(fan(toolConfig(map[string]any{"maxConcurrent": float64(16)}), echo()))
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			res, err := e.call(EditorActor(), info, map[string]any{"door": "left"})
			if err != nil {
				assert.True(t, errors.Is(err, ErrBusy), "%v", err)
				return
			}
			assert.Equal(t, flow.CallStatusCompleted, res.Status)
		})
	}
	wg.Wait()
	e.waitIdle(info)
}

// slowTool is a flow whose tool replies after delayMs, past its 1 s timeout.
func slowTool(delayMs int, over map[string]any) *flow.Flow {
	cfg := toolConfig(map[string]any{"timeoutMs": float64(1000)})
	for k, v := range over {
		cfg[k] = v
	}
	f := flow.NewFlow("slow")
	tool := flow.NewNode("trigger-tool", flow.NodeCategoryInput)
	tool.ID, tool.Config = "tool", cfg
	wait := flow.NewNode("process-delay", flow.NodeCategoryProcessing)
	wait.ID, wait.Config = "wait", map[string]any{"delay": float64(delayMs)}
	rep := flow.NewNode("action-reply", flow.NodeCategoryOutput)
	rep.ID = "reply"
	f.AddNode(*tool)
	f.AddNode(*wait)
	f.AddNode(*rep)
	f.AddConnection(*flow.NewConnection("tool", "output", "wait", "input"))
	f.AddConnection(*flow.NewConnection("wait", "output", "reply", "input"))
	return f
}

// Final results filling the result store never push out the record of a
// call still running: its caller still gets the result, and an approved
// call's final result still reaches its approval.
func TestRunningCallsSurviveAFullResultStore(t *testing.T) {
	e := newEnv(t)
	e.tools.results.maxEntries = 4
	slow := e.deploy(slowTool(1500, map[string]any{"confirm": "admin", "maxConcurrent": float64(2)}))
	quick := e.deploy(fan(toolConfig(nil), echo()))
	agent := e.agent(storage.NewAPIToken{Grants: grant(slow)})
	req, err := e.call(agent, slow, map[string]any{"door": "left"})
	require.NoError(t, err)
	approved, err := e.store.DecideAgentApproval(req.RunID, true, "admin", time.Now())
	require.NoError(t, err)

	var wg sync.WaitGroup
	var editorRun flow.CallResult
	wg.Go(func() {
		res, err := e.call(EditorActor(), slow, map[string]any{"door": "right"})
		assert.NoError(t, err)
		editorRun = res
	})
	wg.Go(func() { assert.NoError(t, e.tools.ExecuteApproval(context.Background(), *approved)) })
	wg.Wait()
	require.Equal(t, flow.CallStatusRunning, editorRun.Status)

	for range 3 * e.tools.results.maxEntries {
		res, err := e.call(EditorActor(), quick, map[string]any{"door": "left"})
		require.NoError(t, err)
		require.Equal(t, flow.CallStatusCompleted, res.Status)
		e.waitIdle(quick)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := e.tools.Result(ctx, EditorActor(), editorRun.RunID)
	require.NoError(t, err)
	assert.Equal(t, flow.CallStatusCompleted, got.Status)
	assert.JSONEq(t, `{"door":"right"}`, string(got.Result))

	got, err = e.tools.Result(ctx, agent, req.RunID)
	require.NoError(t, err)
	assert.Equal(t, flow.CallStatusCompleted, got.Status)
	assert.JSONEq(t, `{"door":"left"}`, string(got.Result))

	var final flow.CallResult
	require.Eventually(t, func() bool {
		a, err := e.store.GetAgentApproval(req.RunID)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(a.Result, &final))
		return final.Status != flow.CallStatusRunning
	}, 5*time.Second, 20*time.Millisecond)
	assert.Equal(t, flow.CallStatusCompleted, final.Status)
	assert.JSONEq(t, `{"door":"left"}`, string(final.Result))
}

// The handle of a confirm:admin call is its approval's id (§8): every answer
// for it carries that id as RunID, never the id of the flow run the approved
// call started, so an agent that polls with the runId it was last given
// keeps reaching the call, also after the in-memory record of the run is
// gone and only the approval's stored copy answers.
func TestApprovedCallAnswersWithTheApprovalID(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(slowTool(1500, map[string]any{"confirm": "admin"}))
	agent := e.agent(storage.NewAPIToken{Grants: grant(info)})
	req, err := e.call(agent, info, map[string]any{"door": "left"})
	require.NoError(t, err)
	require.Equal(t, flow.CallStatusApprovalRequired, req.Status)
	approvalID := req.RunID
	approved, err := e.store.DecideAgentApproval(approvalID, true, "admin", time.Now())
	require.NoError(t, err)
	require.NoError(t, e.tools.ExecuteApproval(context.Background(), *approved))

	poll := func() flow.CallResult {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		got, err := e.tools.Result(ctx, agent, approvalID)
		require.NoError(t, err)
		return got
	}
	forget := func() {
		e.tools.results.mu.Lock()
		defer e.tools.results.mu.Unlock()
		for id := range e.tools.results.runs {
			e.tools.results.dropLocked(id)
		}
	}

	got := poll() // the live record of the run answers
	assert.Equal(t, flow.CallStatusRunning, got.Status)
	assert.Equal(t, approvalID, got.RunID)

	forget() // the stored copy answers
	got = poll()
	assert.Equal(t, flow.CallStatusRunning, got.Status)
	assert.Equal(t, approvalID, got.RunID)

	require.Eventually(t, func() bool {
		return e.storedApprovalResult(approvalID).Status == flow.CallStatusCompleted
	}, 5*time.Second, 20*time.Millisecond)
	forget()
	got = poll()
	assert.Equal(t, flow.CallStatusCompleted, got.Status)
	assert.Equal(t, approvalID, got.RunID)
	assert.JSONEq(t, `{"door":"left"}`, string(got.Result))
}
