package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The hidden values of oracleFlow's request.
const (
	hiddenURL  = "http://192.168.1.50:8123/api/lock/door-4821"
	hiddenBody = `{"pin":"4821"}`
)

// oracleFlow is lightsFlow whose request's URL and body (settings agents
// may not read) hold what an agent would like to guess; its secret is
// referenced only in its hidden headers.
func oracleFlow() *flow.Flow {
	f := lightsFlow()
	nodeOf(f, "call").Config["url"] = hiddenURL
	nodeOf(f, "call").Config["body"] = hiddenBody
	return f
}

// alertKeys renders alerts as "kind@node:path message before after" for
// comparing two answers.
func alertKeys(alerts []flow.Alert) []string {
	out := []string{}
	for _, a := range alerts {
		before, _ := json.Marshal(a.Before)
		after, _ := json.Marshal(a.After)
		out = append(out, a.Kind+"@"+a.NodeID+":"+a.Path+" "+a.Message+" "+string(before)+" "+string(after))
	}
	return out
}

// Audit finding F10: the alerts an agent gets back (pw_save_draft,
// pw_get_draft) and the low level's verdict never depend on how what it
// wrote compares with the admin's hidden values (ADR-0021): a correct and a
// wrong guess of a hidden URL, body, host or secret name are answered alike
// (up to the guess itself, which alerts may echo).
func TestDraftAlertsDoNotConfirmGuesses(t *testing.T) {
	cases := map[string]struct {
		right, wrong string
		ops          func(guess string) []flow.Op
	}{
		"a hidden URL": {right: hiddenURL, wrong: "http://192.168.1.50:8123/api/lock/door-1111", ops: func(g string) []flow.Op {
			return []flow.Op{{Op: flow.OpUpdateNode, NodeID: "call", Config: map[string]any{"url": g}}}
		}},
		"a hidden body": {right: hiddenBody, wrong: `{"pin":"1111"}`, ops: func(g string) []flow.Op {
			return []flow.Op{{Op: flow.OpUpdateNode, NodeID: "call", Config: map[string]any{"body": g}}}
		}},
		// A request to a host only hidden settings use.
		"a hidden host": {right: "192.168.1.50", wrong: "192.168.1.51", ops: func(g string) []flow.Op {
			return []flow.Op{
				{Op: flow.OpAddNode, Node: &flow.OpNode{ID: "probe", Type: "action-http", Config: map[string]any{"url": "http://" + g + "/x"}}},
				{Op: flow.OpConnect, SourceNode: "calc", SourcePort: "output", TargetNode: "probe", TargetPort: "input"},
			}
		}},
		// A request using a secret only hidden settings reference.
		"a secret name in a hidden setting": {right: "api_key", wrong: "other", ops: func(g string) []flow.Op {
			return []flow.Op{
				{Op: flow.OpAddNode, Node: &flow.OpNode{ID: "probe", Type: "action-http", Config: map[string]any{
					"url": "https://api.example.com/hook/very-private-path", "headers": map[string]any{"Authorization": "{{secret." + g + "}}"}}}},
				{Op: flow.OpConnect, SourceNode: "calc", SourcePort: "output", TargetNode: "probe", TargetPort: "input"},
			}
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newAuthEnv(t)
			require.NoError(t, e.secrets.Set("other", "another-secret-value"))
			e.setLevel(storage.AgentDeployLow)
			base := e.create(oracleFlow())
			agent := e.deployer()
			answer := func(guess string) (alerts []string, verdict string) {
				t.Helper()
				saved := e.draft(agent, DraftInput{FlowID: base.ID, BaseVersion: 1, Ops: tc.ops(guess)})
				view, err := e.a.GetDraft(t.Context(), agent, saved.DraftID)
				require.NoError(t, err)
				assert.Equal(t, alertKeys(saved.Alerts), alertKeys(view.Alerts))
				_, err = e.a.Deploy(t.Context(), agent, DeployRequest{FlowID: base.ID, DraftID: saved.DraftID})
				verdict = "deployed"
				if err != nil {
					verdict = err.Error()
				}
				// Alerts may echo the guess: compare the rest.
				for _, a := range alertKeys(saved.Alerts) {
					alerts = append(alerts, strings.ReplaceAll(a, guess, "GUESS"))
				}
				return alerts, strings.ReplaceAll(verdict, guess, "GUESS")
			}
			wrongAlerts, wrongVerdict := answer(tc.wrong)
			rightAlerts, rightVerdict := answer(tc.right)
			assert.Equal(t, wrongAlerts, rightAlerts, "the alerts")
			assert.Equal(t, wrongVerdict, rightVerdict, "the low level's verdict")
			assert.NotEqual(t, "deployed", rightVerdict, "a written hidden setting is a change")
		})
	}
}
