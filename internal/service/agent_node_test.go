package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tool service is the agent nodes' tool caller (ADR-0039).
var _ node.ToolCaller = (*Tools)(nil)

// agentNode is the caller of an agent node of another flow, listing tools.
func agentNode(tools ...string) node.AgentCaller {
	return node.AgentCaller{FlowID: "agent-flow", NodeID: "agent", Tools: tools}
}

func (e *env) callAsNode(caller node.AgentCaller, name string, args map[string]any) (node.ToolCallOutcome, error) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return e.tools.CallAgentTool(ctx, caller, name, args)
}

func agentToolNames(tools []node.AgentTool) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}

// AgentTools lists the published tools the node names, in its order, with
// what the model needs (the advertised input schema, the description) and
// what bounds them (risk, confirm); a name nothing publishes is left out.
func TestAgentToolsListsTheNamedToolsInOrder(t *testing.T) {
	e := newEnv(t)
	a := e.deploy(fan(toolConfig(map[string]any{"description": "Opens the door the agent names."}), echo()))
	b := e.deploy(fan(toolConfig(nil), echo()))
	e.deploy(fan(toolConfig(nil), echo())) // published, not listed

	tools := e.tools.AgentTools(agentNode(b.ToolName, "nothing_publishes_this", a.ToolName, b.ToolName))
	require.Equal(t, []string{b.ToolName, a.ToolName}, agentToolNames(tools), "in order, once each")
	assert.Equal(t, "Opens the door the agent names.", tools[1].Description)
	assert.JSONEq(t, string(a.Schema.JSON()), string(tools[1].InputSchema))
	assert.Equal(t, a.Risk, tools[1].Risk)
	assert.Equal(t, node.ToolConfirmNone, tools[1].Confirm)

	assert.Empty(t, e.tools.AgentTools(agentNode()), "a node listing nothing has no tools")
}

// What a node may not call is not listed: its own flow's tools, privileged
// tools without allowPrivileged, and tools that need a person to confirm
// (confirm client or admin).
func TestAgentToolsOmitsWhatTheNodeMayNotCall(t *testing.T) {
	e := newEnv(t)
	ok := e.deploy(fan(toolConfig(nil), echo()))
	priv := e.deploy(fan(strictConfig(), command()))
	client := e.deploy(fan(toolConfig(map[string]any{"confirm": "client"}), echo()))
	admin := e.deploy(fan(toolConfig(map[string]any{"confirm": "admin"}), echo()))
	names := []string{ok.ToolName, priv.ToolName, client.ToolName, admin.ToolName}

	assert.Equal(t, []string{ok.ToolName}, agentToolNames(e.tools.AgentTools(agentNode(names...))))

	allowed := agentNode(names...)
	allowed.AllowPrivileged = true
	assert.Equal(t, []string{ok.ToolName, priv.ToolName}, agentToolNames(e.tools.AgentTools(allowed)))

	own := agentNode(names...)
	own.FlowID = ok.FlowID
	assert.Empty(t, agentToolNames(e.tools.AgentTools(own)), "a node never calls its own flow's tools")

	assert.Empty(t, e.tools.AgentTools(node.AgentCaller{Tools: names}), "a caller that names no node has none")
}

// actorRecorder keeps the run actor of every node execution.
type actorRecorder struct {
	mu     sync.Mutex
	actors map[string]string // run ID → actor
}

func (r *actorRecorder) observe(ev flow.NodeEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ev.CorrID != "" && ev.Actor != "" {
		r.actors[ev.CorrID] = ev.Actor
	}
}

func (r *actorRecorder) actor(runID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.actors[runID]
}

// A listed tool runs with the arguments; the run's actor names the agent
// node, and no MCP audit row is written (there is no token: the run is in
// the flow's activity).
func TestAgentNodeCallsAListedTool(t *testing.T) {
	rec := &actorRecorder{actors: map[string]string{}}
	e := newEnv(t, func(_ *Options, m *[]flow.ManagerOption) {
		*m = append(*m, flow.WithManagerObserver(rec.observe))
	})
	info := e.deploy(fan(toolConfig(nil), echo()))

	out, err := e.callAsNode(agentNode(info.ToolName), info.ToolName, map[string]any{"door": "left"})
	require.NoError(t, err)
	assert.Equal(t, node.ToolCallOK, out.Status)
	assert.JSONEq(t, `{"door":"left"}`, string(out.Result))
	assert.Empty(t, out.Error)

	e.waitIdle(info)
	runs := 0
	rec.mu.Lock()
	for _, actor := range rec.actors {
		runs++
		assert.Equal(t, "agent-node:agent-flow/agent", actor)
	}
	rec.mu.Unlock()
	assert.Equal(t, 1, runs)

	require.NoError(t, e.audit.Flush())
	rows, err := e.store.ListMCPAudit(storage.MCPAuditFilter{})
	require.NoError(t, err)
	assert.Empty(t, rows)
	_, err = e.callAsNode(agentNode(info.ToolName), info.ToolName, map[string]any{"door": "garden"})
	requireKind(t, err, ErrInvalidArguments)
	require.NoError(t, e.audit.Flush())
	rejections, err := e.store.ListMCPRejections(storage.MCPAuditFilter{})
	require.NoError(t, err)
	assert.Empty(t, rejections, "nor a rejection counter")
}

// Every rule of an agent node's call; a refusal starts no run and holds no
// slot.
func TestAgentNodeCallRules(t *testing.T) {
	e := newEnv(t)
	ok := e.deploy(fan(toolConfig(nil), echo()))
	priv := e.deploy(fan(strictConfig(), command()))
	client := e.deploy(fan(toolConfig(map[string]any{"confirm": "client"}), echo()))
	admin := e.deploy(fan(toolConfig(map[string]any{"confirm": "admin"}), echo()))
	all := []string{ok.ToolName, priv.ToolName, client.ToolName, admin.ToolName, "nothing_publishes_this"}

	allowed := agentNode(all...)
	allowed.AllowPrivileged = true
	own := agentNode(all...)
	own.FlowID = ok.FlowID

	cases := []struct {
		name    string
		caller  node.AgentCaller
		tool    string
		args    map[string]any
		want    error // nil = runs
		message string
	}{
		{"listed", agentNode(all...), ok.ToolName, map[string]any{"door": "left"}, nil, ""},
		{"not listed", agentNode(priv.ToolName), ok.ToolName, map[string]any{"door": "left"}, ErrToolNotFound, ok.ToolName},
		{"listed but unknown", agentNode(all...), "nothing_publishes_this", nil, ErrToolNotFound, "nothing_publishes_this"},
		{"own flow", own, ok.ToolName, map[string]any{"door": "left"}, ErrPermissionDenied, "own flow"},
		{"privileged", agentNode(all...), priv.ToolName, map[string]any{"door": "left"}, ErrPermissionDenied, "allowPrivileged"},
		{"privileged allowed", allowed, priv.ToolName, map[string]any{"door": "left"}, nil, ""},
		{"confirm client", agentNode(all...), client.ToolName, map[string]any{"door": "left"}, ErrPermissionDenied, "confirm"},
		{"confirm admin", agentNode(all...), admin.ToolName, map[string]any{"door": "left"}, ErrPermissionDenied, "approval"},
		{"invalid arguments", agentNode(all...), ok.ToolName, map[string]any{"door": "garden"}, ErrInvalidArguments, ""},
		{"no node", node.AgentCaller{Tools: all}, ok.ToolName, map[string]any{"door": "left"}, ErrPermissionDenied, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := e.callAsNode(tc.caller, tc.tool, tc.args)
			if tc.want == nil {
				require.NoError(t, err)
				assert.NotEqual(t, node.ToolCallError, out.Status, "%+v", out)
				for _, info := range []flow.ToolInfo{ok, priv} {
					e.waitIdle(info)
				}
				return
			}
			te := requireKind(t, err, tc.want)
			assert.Contains(t, te.Message, tc.message)
			assert.Empty(t, out.Status)
			for _, info := range []flow.ToolInfo{ok, priv, client, admin} {
				assert.Zero(t, e.tools.slots.inflight(toolKey(info)), "a refusal holds no slot")
			}
		})
	}
	approvals, err := e.store.ListAgentApprovals(storage.AgentApprovalFilter{})
	require.NoError(t, err)
	assert.Empty(t, approvals, "an agent node never queues an approval")
}

// Each agent node is one slot key limited like a token; another node has
// its own slots. The tool's maxConcurrent and cooldown apply as to any
// caller.
func TestAgentNodeLimits(t *testing.T) {
	e := newEnv(t)
	wide := e.deploy(fan(toolConfig(map[string]any{"maxConcurrent": float64(8)}), echo(), slowBranch(300)))
	first := agentNode(wide.ToolName)
	for range DefaultPerTokenInflight {
		_, err := e.callAsNode(first, wide.ToolName, map[string]any{"door": "left"})
		require.NoError(t, err)
	}
	_, err := e.callAsNode(first, wide.ToolName, map[string]any{"door": "left"})
	te := requireKind(t, err, ErrBusy)
	assert.Contains(t, te.Message, "agent node")
	second := agentNode(wide.ToolName)
	second.NodeID = "agent-2"
	_, err = e.callAsNode(second, wide.ToolName, map[string]any{"door": "left"})
	require.NoError(t, err, "another node has its own slots")
	e.waitIdle(wide)

	narrow := e.deploy(fan(toolConfig(map[string]any{"maxConcurrent": float64(1)}), echo(), slowBranch(300)))
	_, err = e.callAsNode(agentNode(narrow.ToolName), narrow.ToolName, map[string]any{"door": "left"})
	require.NoError(t, err)
	_, err = e.callAsNode(second, narrow.ToolName, map[string]any{"door": "left"})
	requireKind(t, err, ErrToolNotFound) // second does not list it
	other := agentNode(narrow.ToolName)
	other.NodeID = "agent-3"
	_, err = e.callAsNode(other, narrow.ToolName, map[string]any{"door": "left"})
	requireKind(t, err, ErrBusy)
	e.waitIdle(narrow)

	cool := e.deploy(fan(toolConfig(map[string]any{"cooldownMs": float64(60000)}), echo()))
	_, err = e.callAsNode(agentNode(cool.ToolName), cool.ToolName, map[string]any{"door": "left"})
	require.NoError(t, err)
	e.waitIdle(cool)
	_, err = e.callAsNode(agentNode(cool.ToolName), cool.ToolName, map[string]any{"door": "left"})
	requireKind(t, err, ErrCooldown)
}

// Results and errors are an MCP agent's: secrets removed, the error of a
// node holding hidden settings withheld, a visible one redacted.
func TestAgentNodeResultsAreRedactedAsAnAgents(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.secrets.Set("api_key", canary))
	info := e.deploy(fan(toolConfig(nil), echo()))
	out, err := e.callAsNode(agentNode(info.ToolName), info.ToolName, map[string]any{"text": "value: " + canary})
	require.NoError(t, err)
	assert.NotContains(t, string(out.Result), canary)
	assert.Contains(t, string(out.Result), redactedMark)
	assert.True(t, json.Valid(out.Result))
	e.waitIdle(info)

	hidden := e.deploy(chain(toolConfig(nil), hiddenCalc(), echo()))
	out, err = e.callAsNode(agentNode(hidden.ToolName), hidden.ToolName, map[string]any{"text": "not a number"})
	require.NoError(t, err)
	assert.Equal(t, node.ToolCallError, out.Status)
	assert.NotContains(t, out.Error, hiddenConstant)
	assert.Contains(t, out.Error, "not shown")
	assert.Contains(t, out.Error, "calc", "the error names the node")
	e.waitIdle(hidden)

	visible := e.deploy(fan(toolConfig(nil), step{id: "fail", typ: visibleFailNodeType, config: map[string]any{"message": "no luck with " + canary}}))
	out, err = e.callAsNode(agentNode(visible.ToolName), visible.ToolName, map[string]any{"door": "left"})
	require.NoError(t, err)
	assert.Equal(t, node.ToolCallError, out.Status)
	assert.Contains(t, out.Error, "no luck with "+redactedMark)
	assert.NotContains(t, out.Error, canary)
}

// A run that does not reply within the tool's timeout is "running": the
// node goes on and the run finishes on its own.
func TestAgentNodeRunningCall(t *testing.T) {
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

	out, err := e.callAsNode(agentNode(info.ToolName), info.ToolName, map[string]any{"door": "left"})
	require.NoError(t, err)
	assert.Equal(t, node.ToolCallRunning, out.Status)
	assert.Empty(t, out.Result)
	e.waitIdle(info)
}

// An agent node's results are not kept for Result: nothing reads them again,
// and they would take the room MCP agents' results need. Its runs are no
// one else's to read either.
func TestAgentNodeResultsAreNotKept(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(nil), echo()))
	_, err := e.callAsNode(agentNode(info.ToolName), info.ToolName, map[string]any{"door": "left"})
	require.NoError(t, err)
	e.waitIdle(info)
	e.tools.results.mu.Lock()
	kept := len(e.tools.results.runs)
	e.tools.results.mu.Unlock()
	assert.Zero(t, kept)

	res, err := e.call(EditorActor(), info, map[string]any{"door": "left"})
	require.NoError(t, err)
	_, err = e.tools.Result(context.Background(), AgentNodeActor(agentNode(info.ToolName)), res.RunID)
	requireKind(t, err, ErrRunNotFound)
}

// Invalid arguments are refused with the problems in the error's message,
// so the model can fix its call (each issue echoes at most what the model
// sent, ADR-0021).
func TestAgentNodeInvalidArgumentsListTheIssues(t *testing.T) {
	e := newEnv(t)
	info := e.deploy(fan(toolConfig(nil), echo()))
	_, err := e.callAsNode(agentNode(info.ToolName), info.ToolName, map[string]any{"door": "garden", "extra": 1})
	te := requireKind(t, err, ErrInvalidArguments)
	require.Len(t, te.Issues, 2)
	assert.Contains(t, err.Error(), "the arguments do not match the tool's input schema")
	assert.Contains(t, err.Error(), "/door: must be one of")
	assert.Contains(t, err.Error(), "/extra: unknown property")
}
