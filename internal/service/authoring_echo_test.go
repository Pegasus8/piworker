package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// No guessing oracle by redaction (docs/design/mcp.md §10): what an agent
// sent comes back as it sent it, whether or not it equals a secret's value
// or a credential the admin typed. Only content the agent did not write (the
// admin's, kept in a draft or in the stored flow) is hidden, and how it is
// hidden never depends on what the agent sent.

// pinSecret is a short secret value (a door PIN).
const pinSecret = "48213"

// echoGuesses are texts an agent may send that equal a secret's value or a
// credential the admin typed into the stored flow (echoFlow).
var echoGuesses = []string{pinSecret, apiKeyCanary, adminLiteral}

// echoFlow passes its payload through a node holding a credential the admin
// typed (adminLiteral, Sensitive) to a delay that previews it.
func echoFlow() *flow.Flow {
	f := flow.NewFlow("echo")
	addNode(f, "start", "trigger-manual", nil)
	addNode(f, "key", sensitiveEchoType, map[string]any{"apiKey": adminLiteral})
	addNode(f, "wait", "process-delay", map[string]any{"delay": float64(1)})
	connect(f, "start", "key")
	connect(f, "key", "wait")
	return f
}

func newEchoEnv(t *testing.T) (*authEnv, *flow.Flow, Actor) {
	t.Helper()
	e := newAuthEnv(t)
	require.NoError(t, e.secrets.Set("door_pin", pinSecret))
	return e, e.create(echoFlow()), e.token()
}

func TestNodeTestEchoesComeBackVerbatim(t *testing.T) {
	e, _, agent := newEchoEnv(t)
	for _, guess := range echoGuesses {
		res, err := e.a.TestNode(t.Context(), agent, TestNodeRequest{
			Type: "process-transform", Config: map[string]any{"expression": "payload"}, Input: guess,
		})
		require.NoError(t, err)
		require.NotEmpty(t, res.Outputs)
		assert.Equal(t, guess, decodedPreview(res.Outputs[0].Preview))

		// Config problems quote the agent's own config as it sent it.
		res, err = e.a.TestNode(t.Context(), agent, TestNodeRequest{
			Type: "trigger-cron", Config: map[string]any{"expression": guess},
		})
		if err == nil {
			assert.Contains(t, mustJSON(t, res.Issues)+res.Error, guess)
		}
	}
}

func TestDryRunEchoesComeBackVerbatim(t *testing.T) {
	e, stored, agent := newEchoEnv(t)
	draft := e.draft(agent, DraftInput{FlowID: stored.ID, BaseVersion: 1, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "wait", Config: map[string]any{"delay": float64(2)}},
	}})
	// The agent's own flow, without a literal of its own (what it typed
	// into a Sensitive setting is hidden from it as its own credential).
	inline := echoFlow()
	nodeOf(inline, "key").Config["apiKey"] = "{{secret.api_key}}"
	for _, guess := range echoGuesses {
		for name, req := range map[string]DryRunRequest{
			"inline":  {Flow: inline},
			"flowId":  {FlowID: stored.ID},
			"draftId": {DraftID: draft.DraftID},
		} {
			t.Run(name+"/"+guess, func(t *testing.T) {
				req.Payload = guess
				res, err := e.a.DryRun(t.Context(), agent, req)
				require.NoError(t, err)
				assert.Equal(t, guess, outputOf(t, res, "wait"))
				assert.Equal(t, guess, decodedPreview(stepOf(res, "wait").Input))
			})
		}
	}
}

// The issues of a proposal quote what the agent wrote as it wrote it: in a
// message, and in a path (a node ID it chose).
func TestProposalIssueEchoesComeBackVerbatim(t *testing.T) {
	e, stored, agent := newEchoEnv(t)
	for _, guess := range echoGuesses {
		t.Run(guess, func(t *testing.T) {
			ops := []flow.Op{{Op: flow.OpAddNode, Node: &flow.OpNode{ID: "cron", Type: "trigger-cron",
				Config: map[string]any{"expression": guess}}}}
			res, err := e.a.Validate(t.Context(), agent, ValidateRequest{FlowID: stored.ID, Ops: ops})
			require.NoError(t, err)
			msg := issueAt(t, res.Issues, "/nodes/cron")
			assert.Contains(t, msg, `"`+guess+`"`)

			_, err = e.a.SaveDraft(t.Context(), agent, DraftInput{FlowID: stored.ID, BaseVersion: 1, Ops: ops})
			te := requireKind(t, err, ErrInvalidArguments)
			assert.Contains(t, issueAt(t, te.FlowIssues, "/nodes/cron"), `"`+guess+`"`)

			whole := echoFlow()
			addNode(whole, "cron", "trigger-cron", map[string]any{"expression": guess})
			res, err = e.a.Validate(t.Context(), agent, ValidateRequest{Flow: whole})
			require.NoError(t, err)
			assert.Contains(t, issueAt(t, res.Issues, "/nodes/cron"), `"`+guess+`"`)
		})
	}
	// A node ID equal to a secret's value is a path as the agent wrote it.
	for _, id := range []string{pinSecret, apiKeyCanary} {
		ops := []flow.Op{{Op: flow.OpAddNode, Node: &flow.OpNode{ID: id, Type: "trigger-cron",
			Config: map[string]any{"expression": "nope"}}}}
		res, err := e.a.Validate(t.Context(), agent, ValidateRequest{FlowID: stored.ID, Ops: ops})
		require.NoError(t, err)
		issueAt(t, res.Issues, "/nodes/"+id)
	}
}

// issueAt returns the message of the first issue whose path starts with
// prefix.
func issueAt(t *testing.T, issues []flow.Issue, prefix string) string {
	t.Helper()
	for _, is := range issues {
		if strings.HasPrefix(is.Path, prefix) {
			return is.Message
		}
	}
	require.Failf(t, "no issue", "no issue at %s in %+v", prefix, issues)
	return ""
}

// A draft's view and its alerts show what the agent wrote as it wrote it.
func TestDraftEchoesComeBackVerbatim(t *testing.T) {
	e, stored, agent := newEchoEnv(t)
	for _, guess := range []string{pinSecret, apiKeyCanary} {
		t.Run(guess, func(t *testing.T) {
			host := "h" + guess + ".example"
			saved := e.draft(agent, DraftInput{FlowID: stored.ID, BaseVersion: 1, Name: "flow " + guess, Ops: []flow.Op{
				{Op: flow.OpAddNode, Node: &flow.OpNode{ID: "dbg", Type: "process-debug", Config: map[string]any{"name": guess}}},
				{Op: flow.OpAddNode, Node: &flow.OpNode{ID: "send", Type: "action-http",
					Config: map[string]any{"url": "https://" + host + "/x", "method": "GET"}}},
				{Op: flow.OpConnect, SourceNode: "wait", SourcePort: "output", TargetNode: "dbg", TargetPort: "input"},
				{Op: flow.OpConnect, SourceNode: "dbg", SourcePort: "output", TargetNode: "send", TargetPort: "input"},
			}})
			var newHost *flow.Alert
			for i := range saved.Alerts {
				if saved.Alerts[i].Kind == flow.AlertNewHost {
					newHost = &saved.Alerts[i]
				}
			}
			require.NotNil(t, newHost, "%+v", saved.Alerts)
			assert.Equal(t, host, newHost.After)
			assert.Contains(t, newHost.Message, host)

			view, err := e.a.GetDraft(t.Context(), agent, saved.DraftID)
			require.NoError(t, err)
			assert.Equal(t, "flow "+guess, view.Flow.Name)
			for _, n := range view.Flow.Nodes {
				if n.ID == "dbg" {
					assert.Equal(t, guess, n.Config["name"])
				}
			}
			assert.Equal(t, saved.Alerts, view.Alerts)
		})
	}
}

// The admin's content an agent may not read stays hidden, the same way
// whatever the agent sends. A value Lint quotes from a hidden setting of
// the admin's is not shown, and a guess of it the agent writes into the
// same node is answered like any other guess.
func TestProposalIssuesHideTheAdminsHiddenValues(t *testing.T) {
	e := newAuthEnv(t)
	stored := e.create(lightsFlow())
	agent := e.token()
	const baseURL = "https://api.example.com/hook/very-private-path"
	answers := map[string]string{}
	for _, guess := range []string{baseURL, "https://api.example.com/hook/other-path"} {
		res, err := e.a.Validate(t.Context(), agent, ValidateRequest{FlowID: stored.ID, Ops: []flow.Op{
			{Op: flow.OpUpdateNode, NodeID: "call", Config: map[string]any{"method": guess}},
		}})
		require.NoError(t, err)
		msg := issueAt(t, res.Issues, "/nodes/call/config/method")
		assert.NotContains(t, msg, "very-private-path")
		answers[guess] = mustJSON(t, res.Issues)
	}
	assert.Equal(t, answers[baseURL], answers["https://api.example.com/hook/other-path"], "a right guess reads like a wrong one")

	// A hidden value Lint quotes (an invalid cron expression the admin hid
	// is not possible: a factory error of a node with hidden settings) is
	// not shown.
	f := safeFlow()
	nodeOf(f, "calc").Config["expression"] = "zzHIDDENzz((("
	broken := e.create(f)
	res, err := e.a.Validate(t.Context(), agent, ValidateRequest{FlowID: broken.ID})
	require.NoError(t, err)
	msg := issueAt(t, res.Issues, "/nodes/calc")
	assert.NotContains(t, msg, "zzHIDDENzz")
}

// A setting the agent can read but whose stored value holds a secret's value
// or a credential the admin typed is hidden from the run (as it is from the
// read views) whatever the agent sends, by flowId and in a draft that keeps
// it; what the agent writes is never hidden for what it holds.
func TestDryRunHidesKeptValuesHoldingKnownValues(t *testing.T) {
	e := newAuthEnv(t)
	require.NoError(t, e.secrets.Set("door_pin", pinSecret))
	f := flow.NewFlow("dbg")
	addNode(f, "start", "trigger-manual", nil)
	addNode(f, "leaky", "process-debug", map[string]any{"name": "pin " + pinSecret})
	addNode(f, "plain", "process-debug", map[string]any{"name": "plain"})
	connect(f, "start", "leaky")
	connect(f, "leaky", "plain")
	stored := e.create(f)
	agent := e.token()

	var redact func(*flow.Node, string) bool
	var ran *flow.Flow
	e.a.dryRun = func(_ context.Context, f *flow.Flow, _ *node.Registry, in flow.DryRunInput) (flow.DryRunResult, error) {
		redact, ran = in.Redact, f
		return flow.DryRunResult{}, nil
	}
	_, err := e.a.DryRun(t.Context(), agent, DryRunRequest{FlowID: stored.ID})
	require.NoError(t, err)
	require.NotNil(t, redact)
	assert.True(t, redact(nodeOf(ran, "leaky"), "name"))
	assert.False(t, redact(nodeOf(ran, "plain"), "name"))

	saved := e.draft(agent, DraftInput{FlowID: stored.ID, BaseVersion: 1, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "plain", Config: map[string]any{"name": "mine " + pinSecret}},
	}})
	_, err = e.a.DryRun(t.Context(), agent, DryRunRequest{DraftID: saved.DraftID})
	require.NoError(t, err)
	require.NotNil(t, redact)
	assert.True(t, redact(nodeOf(ran, "leaky"), "name"), "kept: the admin's")
	assert.False(t, redact(nodeOf(ran, "plain"), "name"), "written: the agent's")

	// The draft's view hides the kept one the same way.
	view, err := e.a.GetDraft(t.Context(), agent, saved.DraftID)
	require.NoError(t, err)
	for _, n := range view.Flow.Nodes {
		switch n.ID {
		case "leaky":
			assert.Equal(t, RedactedValue, n.Config["name"])
		case "plain":
			assert.Equal(t, "mine "+pinSecret, n.Config["name"])
		}
	}
}

// Enabling a disabled node whose hidden settings name a host and a secret
// raises the alerts, but the agent learns neither (H7): they are in settings
// it cannot read and did not write.
func TestDraftAlertsDoNotNameWhatHiddenKeptSettingsHold(t *testing.T) {
	e := newAuthEnv(t)
	f := lightsFlow()
	call := nodeOf(f, "call")
	call.Enabled = false
	call.Config["url"] = "https://billing.internal-corp.example/v1/charge?k={{secret.prod_billing_key}}"
	call.Config["headers"] = map[string]any{"Authorization": "{{secret.prod_billing_key}}"}
	require.NoError(t, e.secrets.Set("prod_billing_key", "pbk-0123456789"))
	stored := e.create(f)
	agent := e.token()
	enable := true
	saved := e.draft(agent, DraftInput{FlowID: stored.ID, BaseVersion: 1, Ops: []flow.Op{
		{Op: flow.OpUpdateNode, NodeID: "call", Enabled: &enable},
	}})
	kinds := alertKindsOf(saved.Alerts)
	assert.Contains(t, kinds, flow.AlertNewHost)
	assert.Contains(t, kinds, flow.AlertSecretAdded)
	out := mustJSON(t, saved.Alerts)
	assert.NotContains(t, out, "billing.internal-corp.example")
	assert.NotContains(t, out, "prod_billing_key")
	view, err := e.a.GetDraft(t.Context(), agent, saved.DraftID)
	require.NoError(t, err)
	assert.NotContains(t, mustJSON(t, view), "billing.internal-corp.example")
	assert.NotContains(t, mustJSON(t, view), "prod_billing_key")

	// The editor's review shows them.
	detail, err := e.a.GetDraftDetail(t.Context(), EditorActor(), saved.DraftID)
	require.NoError(t, err)
	assert.Contains(t, mustJSON(t, detail.Diff.Alerts), "billing.internal-corp.example")
}
