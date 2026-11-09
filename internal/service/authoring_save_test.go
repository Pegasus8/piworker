package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SaveDraft (docs/design/mcp.md §10): an agent's proposal is resolved
// against the current version, checked and stored; nothing runs.

func storedDraft(t *testing.T, e *authEnv, id string) (*storage.FlowDraft, *flow.Flow) {
	t.Helper()
	d, err := e.store.GetFlowDraft(id)
	require.NoError(t, err)
	f, err := flow.FromJSON(d.Data)
	require.NoError(t, err)
	return d, f
}

func TestSaveDraftWithOpsOnTheCurrentVersion(t *testing.T) {
	e := newAuthEnv(t)
	base := e.create(lightsFlow())
	agent := e.token()
	res, err := e.a.SaveDraft(t.Context(), agent, DraftInput{
		FlowID: base.ID, BaseVersion: 1, Note: "log the result",
		Ops: []flow.Op{
			{Op: flow.OpAddNode, Node: &flow.OpNode{ID: "log", Type: "action-log", Config: map[string]any{"message": "done"}}},
			{Op: flow.OpConnect, SourceNode: "call", SourcePort: "output", TargetNode: "log", TargetPort: "input"},
			{Op: flow.OpUpdateNode, NodeID: "calc", Config: map[string]any{"expression": "payload + 1"}},
		},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, res.DraftID)
	assert.Equal(t, storage.DraftPending, res.Status)
	assert.Equal(t, base.ID, res.FlowID)
	assert.Equal(t, 1, res.BaseVersion)
	assert.Equal(t, node.RiskExternal, res.Risk)
	assert.NotNil(t, res.Issues)
	assert.NotNil(t, res.Alerts)

	d, proposed := storedDraft(t, e, res.DraftID)
	assert.Equal(t, agent.TokenID, d.AuthorTokenID)
	assert.Equal(t, agent.TokenName, d.AuthorTokenName)
	assert.Equal(t, "log the result", d.Note)
	assert.Equal(t, "external", d.Risk)
	assert.Equal(t, "lights", proposed.Name, "an existing flow keeps its name")
	assert.Equal(t, "payload + 1", nodeOf(proposed, "calc").Config["expression"])
	logNode := nodeOf(proposed, "log")
	require.NotNil(t, logNode)
	assert.NotEqual(t, flow.Position{}, logNode.Position, "a new node is laid out")
	assert.Len(t, proposed.Connections, 3)
	assert.Equal(t, "https://api.example.com/hook/very-private-path", nodeOf(proposed, "call").Config["url"])

	// The stored flow is untouched: a draft changes nothing that runs.
	current, err := e.store.GetFlow(base.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, current.Version)
	assert.Len(t, current.Nodes, 3)

	rows := e.auditRows()
	require.Len(t, rows, 1)
	assert.Equal(t, AuthorToolSaveDraft, rows[0].Tool)
	assert.Equal(t, "completed", rows[0].Outcome)
	assert.Equal(t, base.ID, rows[0].FlowID)
	assert.NotContains(t, rows[0].ArgsPreview, "payload + 1", "outcome only, no payloads")
}

func TestSaveDraftOfANewFlow(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.token()
	f := safeFlow()
	f.ID = "agent-chosen"
	f.Name = ""
	f.Nodes[1].Position = flow.Position{}
	res, err := e.a.SaveDraft(t.Context(), agent, DraftInput{Flow: f, Name: "Morning routine", Note: "new"})
	require.NoError(t, err)
	assert.Empty(t, res.FlowID)
	assert.Zero(t, res.BaseVersion)
	assert.Equal(t, node.RiskSafe, res.Risk)
	_, proposed := storedDraft(t, e, res.DraftID)
	assert.Equal(t, "Morning routine", proposed.Name)
	assert.Empty(t, proposed.ID, "the ID of a new flow is the server's")
	assert.NotEqual(t, flow.Position{}, nodeOf(proposed, "calc").Position, "new nodes are laid out")

	// A new flow needs a whole flow and a name; ops need an existing flow.
	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{Flow: safeFlow(), Name: strings.Repeat("n", MaxDraftNameChars+1)})
	requireKind(t, err, ErrInvalidArguments)
	unnamed := safeFlow()
	unnamed.Name = ""
	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{Flow: unnamed})
	requireKind(t, err, ErrInvalidArguments)
	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{Ops: []flow.Op{{Op: flow.OpRemoveNode, NodeID: "x"}}})
	requireKind(t, err, ErrInvalidArguments)
	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{Name: "nothing"})
	requireKind(t, err, ErrInvalidArguments)
	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{Flow: safeFlow(), BaseVersion: 3})
	requireKind(t, err, ErrInvalidArguments)
}

func TestSaveDraftBaseVersion(t *testing.T) {
	e := newAuthEnv(t)
	base := e.create(lightsFlow())
	require.NoError(t, e.store.UpdateFlow(base)) // version 2
	agent := e.token()
	ops := []flow.Op{{Op: flow.OpUpdateNode, NodeID: "calc", Config: map[string]any{"expression": "1"}}}

	_, err := e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: base.ID, BaseVersion: 1, Ops: ops})
	te := requireKind(t, err, ErrConflict)
	assert.Equal(t, 2, te.CurrentVersion, "a stale base names the current version")

	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: base.ID, Ops: ops})
	te = requireKind(t, err, ErrInvalidArguments)
	assert.Contains(t, te.Message, "baseVersion")

	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: "nope", BaseVersion: 1, Ops: ops})
	requireKind(t, err, ErrFlowNotFound)

	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: base.ID, BaseVersion: 2, Ops: ops, Flow: lightsFlow()})
	requireKind(t, err, ErrInvalidArguments)

	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: base.ID, BaseVersion: 2, Ops: ops})
	require.NoError(t, err)
}

// "<redacted>" — what the read views show of a hidden value — keeps the
// base value of the same node and type; the stored draft holds it.
func TestSaveDraftResolvesTheRedactionSentinel(t *testing.T) {
	e := newAuthEnv(t)
	base := e.create(lightsFlow())
	agent := e.token()

	whole := lightsFlow()
	whole.ID = base.ID
	call := nodeOf(whole, "call")
	call.Config["url"] = RedactedValue
	call.Config["headers"] = RedactedValue
	call.Config["method"] = "PUT"
	res, err := e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: base.ID, BaseVersion: 1, Flow: whole})
	require.NoError(t, err)
	_, proposed := storedDraft(t, e, res.DraftID)
	assert.Equal(t, "https://api.example.com/hook/very-private-path", nodeOf(proposed, "call").Config["url"])
	assert.Equal(t, map[string]any{"Authorization": "{{secret.api_key}}"}, nodeOf(proposed, "call").Config["headers"])
	assert.Equal(t, "PUT", nodeOf(proposed, "call").Config["method"])

	// Through ops too.
	res, err = e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: base.ID, BaseVersion: 1, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "call", Config: map[string]any{"url": RedactedValue, "method": "GET"}},
	}})
	require.NoError(t, err)
	_, proposed = storedDraft(t, e, res.DraftID)
	assert.Equal(t, "https://api.example.com/hook/very-private-path", nodeOf(proposed, "call").Config["url"])

	// Nothing to keep: a node the base does not have, a node whose type
	// changed, a property the base node does not have.
	cases := map[string]func(f *flow.Flow){
		"missing node": func(f *flow.Flow) {
			addNode(f, "extra", "action-http", map[string]any{"url": RedactedValue})
			connect(f, "calc", "extra")
		},
		"changed type": func(f *flow.Flow) {
			n := nodeOf(f, "calc")
			n.Type, n.Config = "process-template", map[string]any{"template": RedactedValue}
		},
		"missing property": func(f *flow.Flow) {
			nodeOf(f, "call").Config["body"] = RedactedValue
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := lightsFlow()
			f.ID = base.ID
			change(f)
			_, err := e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: base.ID, BaseVersion: 1, Flow: f})
			te := requireKind(t, err, ErrInvalidArguments)
			issue := findIssue(te.FlowIssues, IssueUnresolvedRedaction)
			require.NotNil(t, issue, "%v", te.FlowIssues)
			assert.Equal(t, flow.IssueError, issue.Severity)
			assert.True(t, strings.HasPrefix(issue.Path, "/nodes/"), issue.Path)
		})
	}
}

// A Sensitive property holds only {{secret.NAME}} references (or the
// sentinel): a literal credential is refused without being echoed.
func TestSaveDraftRefusesSensitiveLiterals(t *testing.T) {
	e := newAuthEnv(t)
	base := e.create(lightsFlow())
	agent := e.token()
	_, err := e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: base.ID, BaseVersion: 1, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "call", Config: map[string]any{"headers": map[string]any{"Authorization": "Bearer hunter2-literal"}}},
	}})
	te := requireKind(t, err, ErrInvalidArguments)
	issue := findIssue(te.FlowIssues, IssueSensitiveLiteral)
	require.NotNil(t, issue, "%v", te.FlowIssues)
	assert.Equal(t, "/nodes/call/config/headers", issue.Path)
	data, _ := json.Marshal(te)
	assert.NotContains(t, string(data)+te.Message, "hunter2-literal")

	// A reference is fine, even to a secret not defined yet (a warning).
	res, err := e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: base.ID, BaseVersion: 1, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "call", Config: map[string]any{"headers": map[string]any{"Authorization": "{{secret.later}}"}}},
	}})
	require.NoError(t, err)
	warning := findIssue(res.Issues, IssueUnknownSecret)
	require.NotNil(t, warning, "%v", res.Issues)
	assert.Equal(t, flow.IssueWarning, warning.Severity)
	assert.Contains(t, warning.Message, "later")

	// A literal the admin typed into the base stays when the agent leaves
	// the node alone.
	literal := lightsFlow()
	nodeOf(literal, "call").Config["headers"] = map[string]any{"Authorization": "Bearer admin-typed"}
	stored := e.create(literal)
	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: stored.ID, BaseVersion: 1, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "calc", Config: map[string]any{"expression": "2"}},
	}})
	require.NoError(t, err)
}

// What the author scope may not change is forbidden, naming the path.
func TestSaveDraftAuthorRestrictions(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.token()

	tooled := safeFlow()
	addNode(tooled, "tool", "trigger-tool", map[string]any{
		"toolName": "kitchen_lights", "description": "Turns the lights on in the kitchen.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
	})
	addNode(tooled, "reply", "action-reply", nil)
	connect(tooled, "tool", "reply")
	addNode(tooled, "hook", "trigger-webhook", map[string]any{"path": "lights", "token": "{{secret.api_key}}"})
	connect(tooled, "hook", "calc")
	addNode(tooled, "http", "action-http", map[string]any{"url": "https://api.example.com/x"})
	connect(tooled, "calc", "http")
	tooled.Variables = map[string]any{"legacy": "x"}
	base := e.create(tooled)

	cases := map[string]struct {
		ops  []flow.Op
		path string
	}{
		"change a tool":  {[]flow.Op{{Op: flow.OpUpdateNode, NodeID: "tool", Config: map[string]any{"title": "Lights"}}}, "/nodes/tool"},
		"remove a reply": {[]flow.Op{{Op: flow.OpRemoveNode, NodeID: "reply"}}, "/nodes/reply"},
		"add a tool": {[]flow.Op{{Op: flow.OpAddNode, Node: &flow.OpNode{ID: "tool2", Type: "trigger-tool", Config: map[string]any{
			"toolName": "other_tool", "description": "Another tool that agents could call.", "inputSchema": map[string]any{"type": "object"},
		}}}}, "/nodes/tool2"},
		"add a reply":        {[]flow.Op{{Op: flow.OpAddNode, Node: &flow.OpNode{ID: "reply2", Type: "action-reply"}}}, "/nodes/reply2"},
		"webhook path":       {[]flow.Op{{Op: flow.OpUpdateNode, NodeID: "hook", Config: map[string]any{"path": "other"}}}, "/nodes/hook/config/path"},
		"webhook token":      {[]flow.Op{{Op: flow.OpUpdateNode, NodeID: "hook", Config: map[string]any{"token": nil}}}, "/nodes/hook/config/token"},
		"new webhook":        {[]flow.Op{{Op: flow.OpAddNode, Node: &flow.OpNode{ID: "hook2", Type: "trigger-webhook", Config: map[string]any{"path": "new"}}}}, "/nodes/hook2/config/path"},
		"risk switch turned": {[]flow.Op{{Op: flow.OpUpdateNode, NodeID: "http", Config: map[string]any{"allowPrivate": true}}}, "/nodes/http/config/allowPrivate"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: base.ID, BaseVersion: 1, Ops: tc.ops})
			te := requireKind(t, err, ErrPermissionDenied)
			assert.Contains(t, te.Message, tc.path)
		})
	}

	// The legacy variables: a whole flow without them keeps them; another
	// value is forbidden.
	whole := tooled
	whole.Variables = nil
	_, err := e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: base.ID, BaseVersion: 1, Flow: whole})
	require.NoError(t, err, "absent variables keep the base's")
	whole.Variables = map[string]any{"legacy": "changed"}
	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: base.ID, BaseVersion: 1, Flow: whole})
	te := requireKind(t, err, ErrPermissionDenied)
	assert.Contains(t, te.Message, "/variables")

	// Moving a tool node on the canvas changes nothing it does.
	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: base.ID, BaseVersion: 1, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "tool", Position: &flow.Position{X: 900, Y: 900}},
	}})
	require.NoError(t, err)
}

// A proposal deploy would refuse (Lint errors other than undefined secrets)
// is refused with its issues (H2); undefined secrets are stored with it.
func TestSaveDraftRefusesBlockingLintErrors(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.token()
	dup := safeFlow()
	addNode(dup, "calc", "process-transform", map[string]any{"expression": "payload"})
	unknown := safeFlow()
	addNode(unknown, "mystery", "no-such-type", nil)
	badConfig := safeFlow()
	nodeOf(badConfig, "calc").Config["expression"] = float64(3)
	for name, tc := range map[string]struct {
		f    *flow.Flow
		code string
	}{
		"duplicate node id": {dup, flow.IssueDuplicateNodeID},
		"unknown type":      {unknown, flow.IssueUnknownType},
		"config typing":     {badConfig, flow.IssueConfigType},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := e.a.SaveDraft(t.Context(), agent, DraftInput{Flow: tc.f, Name: "bad"})
			te := requireKind(t, err, ErrInvalidArguments)
			assert.Contains(t, issueCodes(te.FlowIssues), tc.code)
		})
	}

	strict := safeFlow()
	addNode(strict, "tg", "action-telegram", map[string]any{"botToken": "{{secret.bot_token}}", "chatId": "42"})
	connect(strict, "calc", "tg")
	res, err := e.a.SaveDraft(t.Context(), agent, DraftInput{Flow: strict, Name: "telegram"})
	require.NoError(t, err)
	assert.Contains(t, issueCodes(res.Issues), flow.IssueUndefinedSecret)
}

func TestSaveDraftLimits(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.token()
	_, err := e.a.SaveDraft(t.Context(), agent, DraftInput{Flow: safeFlow(), Note: strings.Repeat("é", MaxDraftNoteChars+1)})
	requireKind(t, err, ErrInvalidArguments)
	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{Flow: safeFlow(), Note: strings.Repeat("é", MaxDraftNoteChars)})
	require.NoError(t, err, "the note is counted in characters")

	big := safeFlow()
	for i := range MaxDraftNodes {
		addNode(big, fmt.Sprintf("n%d", i), "process-delay", map[string]any{"delay": float64(1)})
	}
	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{Flow: big, Name: "big"})
	te := requireKind(t, err, ErrInvalidArguments)
	assert.Contains(t, te.Message, "nodes")

	wired := safeFlow()
	for range MaxDraftConnections {
		connect(wired, "start", "calc")
	}
	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{Flow: wired, Name: "wired"})
	te = requireKind(t, err, ErrInvalidArguments)
	assert.Contains(t, te.Message, "connections")

	heavy := safeFlow()
	nodeOf(heavy, "calc").Config["expression"] = `"` + strings.Repeat("x", MaxDraftFlowBytes) + `"`
	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{Flow: heavy, Name: "heavy"})
	te = requireKind(t, err, ErrInvalidArguments)
	assert.Contains(t, te.Message, "bytes")
}

// The 21st pending draft of a token is forbidden: the admin decides first.
func TestSaveDraftPendingLimit(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.token()
	for range storage.MaxPendingDraftsPerToken {
		_, err := e.a.SaveDraft(t.Context(), agent, DraftInput{Flow: safeFlow(), Name: "one more"})
		require.NoError(t, err)
	}
	_, err := e.a.SaveDraft(t.Context(), agent, DraftInput{Flow: safeFlow(), Name: "too many"})
	te := requireKind(t, err, ErrPermissionDenied)
	assert.Contains(t, te.Message, "pending drafts")
	_, err = e.a.SaveDraft(t.Context(), e.token(), DraftInput{Flow: safeFlow(), Name: "another token"})
	require.NoError(t, err, "the limit is per token")
}

func TestSaveDraftNeedsTheAuthorScope(t *testing.T) {
	e := newAuthEnv(t)
	_, err := e.a.SaveDraft(t.Context(), e.token(storage.ScopeRead), DraftInput{Flow: safeFlow(), Name: "x"})
	requireKind(t, err, ErrPermissionDenied)
	_, err = e.a.SaveDraft(t.Context(), EditorActor(), DraftInput{Flow: safeFlow(), Name: "x"})
	requireKind(t, err, ErrPermissionDenied)

	off := newAuthEnv(t, func(o *AuthoringOptions) { o.AuthorEnabled = false })
	_, err = off.a.SaveDraft(t.Context(), off.token(), DraftInput{Flow: safeFlow(), Name: "x"})
	requireKind(t, err, ErrPermissionDenied)

	// A revoked token cannot store a draft (storage re-checks it).
	agent := e.token()
	require.NoError(t, e.store.RevokeAPIToken(agent.TokenID, time.Now()))
	_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{Flow: safeFlow(), Name: "x"})
	requireKind(t, err, ErrPermissionDenied)
}

// The alerts stored with a draft are flow.Diff's against the current
// version, agent-safe; the issues and alerts never carry secret values.
func TestSaveDraftStoresAgentSafeAlerts(t *testing.T) {
	e := newAuthEnv(t)
	base := e.create(lightsFlow())
	agent := e.token()
	res, err := e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: base.ID, BaseVersion: 1, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "call", Config: map[string]any{"url": "https://evil.example.net/collect"}},
	}})
	require.NoError(t, err)
	kinds := []string{}
	for _, a := range res.Alerts {
		kinds = append(kinds, a.Kind)
	}
	assert.Contains(t, kinds, flow.AlertDestinationChanged)
	assert.Contains(t, kinds, flow.AlertNewHost)
	d, _ := storedDraft(t, e, res.DraftID)
	assert.JSONEq(t, mustJSON(t, res.Alerts), string(d.AlertsJSON))
	assert.NotContains(t, string(d.AlertsJSON), "very-private-path", "the base's hidden URL is not an alert value")
	assert.NotContains(t, string(d.AlertsJSON)+string(d.IssuesJSON), apiKeyCanary)
}
