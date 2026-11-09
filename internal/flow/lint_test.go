package flow_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lintNode builds a builtin node with an explicit ID and config, and the
// category and ports of its registered type, as the editor stores them.
func lintNode(id, typ string, cfg map[string]any) flow.Node {
	n := flow.NewNode(typ, flow.NodeCategoryProcessing)
	n.ID = id
	if cfg != nil {
		n.Config = cfg
	}
	if info, ok := node.DefaultRegistry.Get(typ); ok {
		n.Category, n.Inputs, n.Outputs = info.Category, info.Inputs, info.Outputs
	}
	return *n
}

func lintConn(id, src, srcPort, dst, dstPort string) flow.Connection {
	c := flow.NewConnection(src, srcPort, dst, dstPort)
	c.ID = id
	return *c
}

// issueKeys renders issues as "severity:code@path" for comparison.
func issueKeys(issues []flow.Issue) []string {
	keys := []string{}
	for _, i := range issues {
		keys = append(keys, string(i.Severity)+":"+i.Code+"@"+i.Path)
	}
	return keys
}

// messyFlow has one problem of (almost) every kind deploy checks.
func messyFlow() *flow.Flow {
	f := flow.NewFlow("messy")
	f.Nodes = []flow.Node{
		lintNode("tick", "trigger-interval", map[string]any{"interval": "5s"}),
		lintNode("http", "action-http", map[string]any{}),
		lintNode("mystery", "nope", nil),
		lintNode("tool", "trigger-tool", strictToolConfig()),
		lintNode("sv", "set-var", map[string]any{"key": "k", "value": "payload", "scope": "global"}),
		lintNode("lonely", "trigger-tool", strictToolConfig()),
		lintNode("dup", "process-debug", nil),
		lintNode("dup", "process-debug", nil),
	}
	f.Connections = []flow.Connection{
		lintConn("c1", "tick", "output", "http", "input"),
		lintConn("c2", "tool", "output", "sv", "input"),
		lintConn("c3", "http", "output", "ghost", "input"),
		lintConn("c4", "http", "nope", "sv", "input"),
	}
	return f
}

func TestLintReportsEveryIssueInADeterministicOrder(t *testing.T) {
	f := messyFlow()
	issues := flow.Lint(f, node.DefaultRegistry)
	assert.Equal(t, []string{
		"error:" + flow.IssueConfigType + "@/nodes/tick/config/interval",
		"error:" + flow.IssueInvalidConfig + "@/nodes/http",
		"error:" + flow.IssueUnknownType + "@/nodes/mystery",
		"error:" + flow.IssueToolGlobalVariable + "@/nodes/sv/config/scope",
		"error:" + flow.IssueTriggerNotConnected + "@/nodes/lonely",
		"error:" + flow.IssueDuplicateNodeID + "@/nodes/dup",
		"error:" + flow.IssueConnectionUnknownNode + "@/connections/c3",
		"error:" + flow.IssueConnectionUnknownPort + "@/connections/c4",
	}, issueKeys(issues))
	for _, i := range issues {
		assert.NotEmpty(t, i.Message, i.Path)
		assert.NotContains(t, i.Message, "validation error:", "messages carry no Go error prefixes")
	}
	assert.Contains(t, issues[0].Message, `expected number (milliseconds), got string "5s"`)
	assert.Contains(t, issues[1].Message, "url is required")

	// Same input, same output; also after a JSON round trip.
	assert.Equal(t, issues, flow.Lint(f, node.DefaultRegistry))
	data, err := json.Marshal(f)
	require.NoError(t, err)
	copied, err := flow.FromJSON(data)
	require.NoError(t, err)
	assert.Equal(t, issues, flow.Lint(copied, node.DefaultRegistry))
}

func TestLintDoesNotModifyTheFlow(t *testing.T) {
	f := messyFlow()
	before, err := json.Marshal(f)
	require.NoError(t, err)
	flow.Lint(f, node.DefaultRegistry)
	after, err := json.Marshal(f)
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after), "lint never fills in ports or anything else")
}

func TestLintOfAValidFlow(t *testing.T) {
	f := chainFlow(strictToolConfig(), reply())
	issues := flow.Lint(f, node.DefaultRegistry)
	require.NotNil(t, issues)
	assert.Empty(t, issues)
	data, err := json.Marshal(issues)
	require.NoError(t, err)
	assert.Equal(t, "[]", string(data))

	assert.Empty(t, flow.Lint(flow.NewFlow("empty"), node.DefaultRegistry), "an empty flow is a valid draft")
}

func TestLintKeepsWarnings(t *testing.T) {
	cfg := strictToolConfig()
	cfg["timeoutMs"] = float64(10000)
	f := chainFlow(cfg, riskStep{id: "wait", typ: "process-delay", config: map[string]any{"delay": float64(20000)}}, reply())
	issues := flow.Lint(f, node.DefaultRegistry)
	assert.Equal(t, []string{"warning:" + flow.IssueToolDelayExceedsTimeout + "@/nodes/wait/config/delay"}, issueKeys(issues))
	require.NoError(t, flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop()).Deploy(context.Background(), f), "warnings never fail a deploy")
}

func TestLintIssueJSONIsCamelCase(t *testing.T) {
	data, err := json.Marshal(flow.Lint(messyFlow(), node.DefaultRegistry)[0])
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))
	assert.ElementsMatch(t, []string{"path", "code", "severity", "message"}, keysOf(fields))
}

func keysOf(m map[string]any) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Lint and deploy run the same checks (ADR-0007): for a flow with a single
// problem, Lint reports exactly that error, and deploy still fails with the
// same message and error class as before Lint existed.
func TestDeployAndLintShareTheirChecks(t *testing.T) {
	valid := func() *flow.Flow {
		f := flow.NewFlow("single")
		f.Nodes = []flow.Node{
			lintNode("tick", "trigger-interval", map[string]any{"interval": float64(1000)}),
			lintNode("log", "action-log", map[string]any{"message": "hi"}),
		}
		f.Connections = []flow.Connection{lintConn("c1", "tick", "output", "log", "input")}
		return f
	}
	type deployClass int
	const (
		validation deployClass = iota
		noTrigger
		nodeConfig
	)
	cases := []struct {
		name    string
		mutate  func(f *flow.Flow)
		code    string
		class   deployClass
		message string // in both Lint's message and deploy's error
	}{
		{"config type", func(f *flow.Flow) { f.Nodes[0].Config["interval"] = "5s" },
			flow.IssueConfigType, validation, `nodes[0].config.interval: expected number (milliseconds), got string "5s"`},
		{"validate", func(f *flow.Flow) { f.Nodes = append(f.Nodes, lintNode("http", "action-http", map[string]any{})) },
			flow.IssueInvalidConfig, nodeConfig, "url is required"},
		{"unknown type", func(f *flow.Flow) { f.Nodes[1].Type = "nope" },
			flow.IssueUnknownType, nodeConfig, "unknown node type: nope"},
		{"duplicate node", func(f *flow.Flow) { f.Nodes = append(f.Nodes, lintNode("log", "action-log", nil)) },
			flow.IssueDuplicateNodeID, validation, "duplicate node ID log"},
		{"empty node id", func(f *flow.Flow) { f.Nodes = append(f.Nodes, lintNode("", "action-log", nil)) },
			flow.IssueEmptyNodeID, validation, "node at index 2 (type action-log) has an empty ID"},
		{"no trigger", func(f *flow.Flow) { f.Nodes[0].Enabled = false },
			flow.IssueNoTrigger, noTrigger, flow.ErrNoTriggerNodes.Error()},
		{"duplicate connection", func(f *flow.Flow) { f.Connections = append(f.Connections, f.Connections[0]) },
			flow.IssueDuplicateConnectionID, validation, "duplicate connection ID c1"},
		{"unknown target", func(f *flow.Flow) { f.Connections[0].TargetNode = "ghost" },
			flow.IssueConnectionUnknownNode, validation, "connection c1 references non-existent target node ghost"},
		{"unknown port", func(f *flow.Flow) { f.Connections[0].TargetPort = "nope" },
			flow.IssueConnectionUnknownPort, validation, "connection c1 references non-existent target port nope"},
		{"self loop", func(f *flow.Flow) {
			f.Connections = append(f.Connections, lintConn("c2", "log", "output", "log", "input"))
		},
			flow.IssueSelfLoop, validation, "connection c2 connects node log to itself"},
		{"cycle", func(f *flow.Flow) {
			f.Nodes = append(f.Nodes, lintNode("dbg", "process-debug", nil))
			f.Connections = append(f.Connections,
				lintConn("c2", "log", "output", "dbg", "input"),
				lintConn("c3", "dbg", "output", "log", "input"))
		}, flow.IssueCycle, validation, "flow contains a cycle through node log"},
		{"callable trigger alone", func(f *flow.Flow) { f.Nodes = append(f.Nodes, lintNode("tool", "trigger-tool", strictToolConfig())) },
			flow.IssueTriggerNotConnected, validation, "callable trigger tool (trigger-tool) has no active connection"},
		{"tool rule", func(f *flow.Flow) {
			f.Nodes = append(f.Nodes,
				lintNode("tool", "trigger-tool", strictToolConfig()),
				lintNode("sv", "set-var", map[string]any{"key": "k", "value": "payload"}))
			f.Connections = append(f.Connections, lintConn("c2", "tool", "output", "sv", "input"))
		}, flow.IssueToolGlobalVariable, validation, "nodes[3].config.scope: set-var writes the global variable"},
		{"undefined secret", func(f *flow.Flow) {
			f.Nodes = append(f.Nodes, lintNode("tg", "action-telegram", map[string]any{
				"botToken": "{{secret.pw_lint_undefined}}", "chatId": "42"}))
		}, flow.IssueUndefinedSecret, validation, `config.botToken references secret "pw_lint_undefined", which is not defined`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := valid()
			require.Empty(t, flow.Lint(f, node.DefaultRegistry), "the base flow is valid")
			tc.mutate(f)

			issues := flow.Lint(f, node.DefaultRegistry)
			require.Len(t, issues, 1, "%v", issueKeys(issues))
			assert.Equal(t, tc.code, issues[0].Code)
			assert.Equal(t, flow.IssueError, issues[0].Severity)

			err := flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop()).Deploy(context.Background(), f)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.message)
			var verr *flow.ValidationError
			switch tc.class {
			case validation:
				assert.ErrorAs(t, err, &verr)
			case noTrigger:
				assert.ErrorIs(t, err, flow.ErrNoTriggerNodes)
			case nodeConfig:
				assert.ErrorIs(t, err, flow.ErrInvalidNodeConfig)
				var nerr *flow.NodeExecutionError
				assert.True(t, errors.As(err, &nerr))
			}
			// Lint's message is deploy's, without the legacy path prefix.
			_, legacyMessage, _ := cutLegacyPath(tc.message)
			assert.Contains(t, issues[0].Message, legacyMessage)
		})
	}
}

// cutLegacyPath strips a "nodes[i].config.x: " prefix from a deploy message.
func cutLegacyPath(msg string) (string, string, bool) {
	for i := 0; i+1 < len(msg); i++ {
		if msg[i] == ':' && msg[i+1] == ' ' && len(msg) > 6 && msg[:6] == "nodes[" {
			return msg[:i], msg[i+2:], true
		}
	}
	return "", msg, false
}

func TestFlowRisk(t *testing.T) {
	cases := []struct {
		name  string
		nodes []flow.Node
		want  node.Risk
	}{
		{"empty", nil, node.RiskSafe},
		{"log", []flow.Node{lintNode("a", "action-log", nil)}, node.RiskSafe},
		{"static http", []flow.Node{lintNode("a", "action-http", map[string]any{"url": "https://api.example.com/x/{{payload.id}}"})}, node.RiskExternal},
		{"dynamic http host", []flow.Node{lintNode("a", "action-http", map[string]any{"url": "https://{{payload.host}}/x"})}, node.RiskPrivileged},
		{"http allowPrivate", []flow.Node{lintNode("a", "action-http", map[string]any{"url": "http://10.0.0.2/", "allowPrivate": true})}, node.RiskPrivileged},
		{"dynamic notify", []flow.Node{lintNode("a", "action-notify", map[string]any{"url": "{{vars.hook}}"})}, node.RiskPrivileged},
		{"static mqtt", []flow.Node{lintNode("a", "action-mqtt", map[string]any{"broker": "tcp://broker:1883", "topic": "t"})}, node.RiskExternal},
		{"dynamic mqtt broker", []flow.Node{lintNode("a", "action-mqtt", map[string]any{"broker": "tcp://{{payload.b}}:1883", "topic": "t"})}, node.RiskPrivileged},
		{"command", []flow.Node{lintNode("a", "action-command", map[string]any{"command": "echo"})}, node.RiskPrivileged},
		{"rss", []flow.Node{lintNode("a", "trigger-rss", map[string]any{"url": "https://example.com/feed"})}, node.RiskSafe},
		{"rss allowPrivate", []flow.Node{lintNode("a", "trigger-rss", map[string]any{"url": "http://10.0.0.2/feed", "allowPrivate": true})}, node.RiskPrivileged},
		{"rss allowPrivate off", []flow.Node{lintNode("a", "trigger-rss", map[string]any{"url": "http://10.0.0.2/feed", "allowPrivate": false})}, node.RiskSafe},
		{"unknown type", []flow.Node{lintNode("a", "nope", nil)}, node.RiskPrivileged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := flow.NewFlow("risk")
			f.Nodes = append(f.Nodes, lintNode("t", "trigger-manual", nil))
			f.Nodes = append(f.Nodes, tc.nodes...)
			assert.Equal(t, tc.want, flow.Risk(f, node.DefaultRegistry))
		})
	}

	t.Run("disabled nodes do not count", func(t *testing.T) {
		f := flow.NewFlow("risk")
		cmd := lintNode("a", "action-command", map[string]any{"command": "echo"})
		cmd.Enabled = false
		f.Nodes = []flow.Node{lintNode("t", "trigger-manual", nil), cmd}
		assert.Equal(t, node.RiskSafe, flow.Risk(f, node.DefaultRegistry))
	})
	t.Run("nil flow", func(t *testing.T) {
		assert.Equal(t, node.RiskSafe, flow.Risk(nil, node.DefaultRegistry))
	})
}

// A whole flow (a draft) is held to the same position bounds as ops; deploy
// does not need positions and does not check them.
func TestLintReportsPositionsOutOfRange(t *testing.T) {
	f := flow.NewFlow("far")
	tick := lintNode("tick", "trigger-interval", map[string]any{"interval": float64(1000)})
	tick.Position = flow.Position{X: 0, Y: 1e20}
	log := lintNode("log", "action-log", map[string]any{"message": "hi"})
	log.Position = flow.Position{X: math.Inf(1), Y: 0}
	ok := lintNode("ok", "action-log", map[string]any{"message": "hi"})
	ok.Position = flow.Position{X: -flow.MaxPosition, Y: flow.MaxPosition}
	f.Nodes = []flow.Node{tick, log, ok}
	f.Connections = []flow.Connection{lintConn("c1", "tick", "output", "log", "input"), lintConn("c2", "tick", "output", "ok", "input")}

	issues := flow.Lint(f, node.DefaultRegistry)
	assert.Equal(t, []string{
		"error:" + flow.IssueInvalidPosition + "@/nodes/tick",
		"error:" + flow.IssueInvalidPosition + "@/nodes/log",
	}, issueKeys(issues))
	for _, i := range issues {
		assert.Contains(t, i.Message, "1000000")
	}
}

// A callable trigger connected to a node that fails its own checks is
// connected: only that node's problem is reported, not a misleading
// "connect it" on the trigger.
func TestLintDoesNotCallAConnectedTriggerUnconnected(t *testing.T) {
	f := flow.NewFlow("tool")
	f.Nodes = []flow.Node{
		lintNode("tool", "trigger-tool", strictToolConfig()),
		lintNode("http", "action-http", map[string]any{}),
		lintNode("typed", "action-http", map[string]any{"url": "https://api.example.com/", "timeout": "soon"}),
	}
	f.Connections = []flow.Connection{lintConn("c1", "tool", "output", "http", "input")}
	assert.Equal(t, []string{"error:" + flow.IssueInvalidConfig + "@/nodes/http", "error:" + flow.IssueConfigType + "@/nodes/typed/config/timeout"}, issueKeys(flow.Lint(f, node.DefaultRegistry)))

	f.Connections = []flow.Connection{lintConn("c1", "tool", "output", "typed", "input")}
	assert.Equal(t, []string{"error:" + flow.IssueInvalidConfig + "@/nodes/http", "error:" + flow.IssueConfigType + "@/nodes/typed/config/timeout"}, issueKeys(flow.Lint(f, node.DefaultRegistry)))

	// A disabled or unknown target is no active connection.
	f.Nodes[2].Enabled = false
	f.Nodes = append(f.Nodes, lintNode("ghost", "nope", nil))
	f.Connections = []flow.Connection{lintConn("c1", "tool", "output", "typed", "input"), lintConn("c2", "tool", "output", "ghost", "input")}
	assert.Contains(t, issueKeys(flow.Lint(f, node.DefaultRegistry)), "error:"+flow.IssueTriggerNotConnected+"@/nodes/tool")
}

// Lint checks the secrets strict settings require (node.SecretRequirer)
// against the store deploy resolves them in: the context's
// (secrets.FromContext), the process-wide store for Lint. The issue names the
// secret, never a value.
func TestLintChecksRequiredSecretsInTheContextStore(t *testing.T) {
	f := flow.NewFlow("secrets")
	f.Nodes = []flow.Node{
		lintNode("tick", "trigger-interval", map[string]any{"interval": float64(1000)}),
		lintNode("tg", "action-telegram", map[string]any{"botToken": "{{secret.pw_lint_tg}}", "chatId": "42"}),
	}
	f.Connections = []flow.Connection{lintConn("c1", "tick", "output", "tg", "input")}

	issues := flow.Lint(f, node.DefaultRegistry)
	require.Equal(t, []string{"error:" + flow.IssueUndefinedSecret + "@/nodes/tg/config/botToken"}, issueKeys(issues))
	assert.Contains(t, issues[0].Message, `"pw_lint_tg"`)

	store := secrets.NewStore()
	require.NoError(t, store.Set("pw_lint_tg", "123:very-secret-token"))
	ctx := secrets.WithStore(context.Background(), store)
	assert.Empty(t, flow.LintContext(ctx, f, node.DefaultRegistry))
	m := flow.NewRuntimeManager(node.DefaultRegistry, zerolog.Nop())
	require.NoError(t, m.Deploy(ctx, f), "a flow without Lint errors deploys")
	require.NoError(t, m.Undeploy(f.ID))

	// An empty store (a sandbox's) lacks every secret.
	empty := secrets.WithStore(context.Background(), secrets.NewStore())
	assert.Equal(t, []string{"error:" + flow.IssueUndefinedSecret + "@/nodes/tg/config/botToken"},
		issueKeys(flow.LintContext(empty, f, node.DefaultRegistry)))
}
