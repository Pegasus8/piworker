package flow_test

import (
	"encoding/json"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// diffFlow builds a flow from nodes (placed apart so nothing looks moved)
// with no connections.
func diffFlow(nodes ...flow.Node) *flow.Flow {
	f := flow.NewFlow("diff")
	for i := range nodes {
		if nodes[i].Position == (flow.Position{}) {
			nodes[i].Position = flow.Position{X: float64(100 + 240*i), Y: 100}
		}
	}
	f.Nodes = nodes
	return f
}

func trigger() flow.Node { return lintNode("t", "trigger-manual", nil) }

func alertKinds(r flow.DiffReport) []string {
	kinds := []string{}
	for _, a := range r.Alerts {
		kinds = append(kinds, a.Kind)
	}
	return kinds
}

func alertOf(t *testing.T, r flow.DiffReport, kind string) flow.Alert {
	t.Helper()
	for _, a := range r.Alerts {
		if a.Kind == kind {
			return a
		}
	}
	require.Failf(t, "alert missing", "%s not in %v", kind, alertKinds(r))
	return flow.Alert{}
}

func TestDiffNodesFieldsAndConnections(t *testing.T) {
	base := diffFlow(
		trigger(),
		lintNode("http", "action-http", map[string]any{"url": "https://api.example.com/a", "method": "GET", "timeout": float64(5000)}),
		lintNode("log", "action-log", map[string]any{"message": "old"}),
	)
	base.Connections = []flow.Connection{
		lintConn("c1", "t", "output", "http", "input"),
		lintConn("c2", "http", "output", "log", "input"),
	}
	next := diffFlow(
		trigger(),
		lintNode("http", "action-http", map[string]any{"url": "https://api.example.com/b", "method": "GET", "bodyType": "json"}),
		lintNode("dbg", "process-debug", map[string]any{"name": "probe"}),
	)
	next.Nodes[0].Position = flow.Position{X: 1, Y: 2} // moved only
	next.Nodes[1].Enabled = false
	next.Connections = []flow.Connection{
		lintConn("other-id", "t", "output", "http", "input"), // same endpoints: unchanged
		lintConn("c3", "http", "output", "dbg", "input"),
	}

	r := flow.Diff(base, next, node.DefaultRegistry)
	require.Len(t, r.Nodes, 4)

	moved := r.Nodes[0]
	assert.Equal(t, "t", moved.NodeID)
	assert.Equal(t, flow.ChangeChanged, moved.Change)
	assert.True(t, moved.Moved)
	assert.Empty(t, moved.Fields, "a position-only change has no fields")
	assert.Equal(t, "Manual Inject", moved.TypeName)

	http := r.Nodes[1]
	assert.Equal(t, "http", http.NodeID)
	assert.Equal(t, "action-http", http.Type)
	assert.Equal(t, flow.ChangeChanged, http.Change)
	assert.False(t, http.Moved)
	assert.Equal(t, []flow.FieldDiff{
		{Path: "enabled", Change: flow.ChangeChanged, Before: true, After: false},
		{Path: "config.bodyType", Change: flow.ChangeAdded, After: "json"},
		{Path: "config.timeout", Change: flow.ChangeRemoved, Before: float64(5000)},
		{Path: "config.url", Change: flow.ChangeChanged, Before: "https://api.example.com/a", After: "https://api.example.com/b", Destination: true},
	}, http.Fields)

	added := r.Nodes[2]
	assert.Equal(t, "dbg", added.NodeID)
	assert.Equal(t, flow.ChangeAdded, added.Change)
	assert.Equal(t, []flow.FieldDiff{{Path: "config.name", Change: flow.ChangeAdded, After: "probe"}}, added.Fields)

	removed := r.Nodes[3]
	assert.Equal(t, "log", removed.NodeID)
	assert.Equal(t, flow.ChangeRemoved, removed.Change)
	assert.Equal(t, []flow.FieldDiff{{Path: "config.message", Change: flow.ChangeRemoved, Before: "old"}}, removed.Fields)

	require.Len(t, r.Connections.Added, 1)
	assert.Equal(t, "c3", r.Connections.Added[0].ID)
	require.Len(t, r.Connections.Removed, 1)
	assert.Equal(t, "c2", r.Connections.Removed[0].ID)
	assert.Equal(t, node.RiskExternal, r.RiskBefore)
	assert.Equal(t, node.RiskSafe, r.RiskAfter, "the disabled http node no longer counts")

	assert.Equal(t, r, flow.Diff(base, next, node.DefaultRegistry), "deterministic")
}

func TestDiffOfAnUnchangedFlowIsEmpty(t *testing.T) {
	f := diffFlow(trigger(), lintNode("http", "action-http", map[string]any{"url": "https://{{payload.host}}/x", "allowPrivate": true}))
	r := flow.Diff(f, f, node.DefaultRegistry)
	assert.Empty(t, r.Nodes)
	assert.Empty(t, r.Connections.Added)
	assert.Empty(t, r.Connections.Removed)
	assert.Empty(t, r.Alerts, "alerts are about changes, not about what was already approved")
	assert.Equal(t, node.RiskPrivileged, r.RiskBefore)
	assert.Equal(t, node.RiskPrivileged, r.RiskAfter)

	data, err := json.Marshal(r)
	require.NoError(t, err)
	assert.JSONEq(t, `{"nodes":[],"connections":{"added":[],"removed":[]},"riskBefore":"privileged","riskAfter":"privileged","alerts":[]}`, string(data))
}

func TestDiffAgainstNoBase(t *testing.T) {
	next := diffFlow(trigger(), lintNode("log", "action-log", map[string]any{"message": "hi"}))
	r := flow.Diff(nil, next, node.DefaultRegistry)
	require.Len(t, r.Nodes, 2)
	for _, n := range r.Nodes {
		assert.Equal(t, flow.ChangeAdded, n.Change)
	}
	assert.Equal(t, node.RiskSafe, r.RiskBefore)
}

func TestDiffAlerts(t *testing.T) {
	cases := []struct {
		name       string
		base, next *flow.Flow
		want       []string // alert kinds, in order
		check      func(t *testing.T, r flow.DiffReport)
	}{
		{
			name: "risk raised and a privileged node added",
			base: diffFlow(trigger()),
			next: diffFlow(trigger(), lintNode("cmd", "action-command", map[string]any{"command": "uptime"})),
			want: []string{flow.AlertRiskRaised, flow.AlertPrivilegedNodeAdded},
			check: func(t *testing.T, r flow.DiffReport) {
				a := alertOf(t, r, flow.AlertRiskRaised)
				assert.Equal(t, node.RiskSafe, a.Before)
				assert.Equal(t, node.RiskPrivileged, a.After)
				assert.Equal(t, "cmd", alertOf(t, r, flow.AlertPrivilegedNodeAdded).NodeID)
			},
		},
		{
			name: "a disabled privileged node enabled",
			base: diffFlow(trigger(), func() flow.Node {
				n := lintNode("cmd", "action-command", map[string]any{"command": "uptime"})
				n.Enabled = false
				return n
			}()),
			next: diffFlow(trigger(), lintNode("cmd", "action-command", map[string]any{"command": "uptime"})),
			want: []string{flow.AlertRiskRaised, flow.AlertPrivilegedNodeAdded},
		},
		{
			name: "destination changed on a changed, an added and a removed node",
			base: diffFlow(trigger(),
				lintNode("tg", "action-telegram", map[string]any{"chatId": "111"}),
				lintNode("gone", "action-mqtt", map[string]any{"broker": "tcp://broker.lan:1883", "topic": "a"})),
			next: diffFlow(trigger(),
				lintNode("tg", "action-telegram", map[string]any{"chatId": "222"}),
				lintNode("mail", "action-email", map[string]any{"to": "ops@example.com"})),
			want: []string{flow.AlertDestinationChanged, flow.AlertDestinationChanged, flow.AlertDestinationChanged, flow.AlertDestinationChanged},
			check: func(t *testing.T, r flow.DiffReport) {
				tg := r.Alerts[0]
				assert.Equal(t, "tg", tg.NodeID)
				assert.Equal(t, "config.chatId", tg.Path)
				assert.Equal(t, "111", tg.Before)
				assert.Equal(t, "222", tg.After)
				assert.Equal(t, "mail", r.Alerts[1].NodeID)
				assert.Equal(t, "ops@example.com", r.Alerts[1].After)
				assert.Equal(t, "gone", r.Alerts[2].NodeID)
				assert.Equal(t, "config.broker", r.Alerts[2].Path)
				assert.Equal(t, "config.topic", r.Alerts[3].Path)
			},
		},
		{
			name: "dynamic destination",
			base: diffFlow(trigger(), lintNode("tg", "action-telegram", map[string]any{"chatId": "111"})),
			next: diffFlow(trigger(), lintNode("tg", "action-telegram", map[string]any{"chatId": "{{payload.chat}}"})),
			want: []string{flow.AlertDestinationChanged, flow.AlertDynamicDestination},
		},
		{
			name: "a secret-only template is not dynamic",
			base: diffFlow(trigger(), lintNode("tg", "action-telegram", map[string]any{"chatId": "111"})),
			next: diffFlow(trigger(), lintNode("tg", "action-telegram", map[string]any{"chatId": "{{secret.chat}}"})),
			want: []string{flow.AlertDestinationChanged, flow.AlertSecretAdded},
		},
		{
			name: "new host",
			base: diffFlow(trigger(), lintNode("http", "action-http", map[string]any{"url": "https://api.example.com/a"})),
			next: diffFlow(trigger(),
				lintNode("http", "action-http", map[string]any{"url": "https://evil.example.net/a"}),
				lintNode("mail", "action-email", map[string]any{"host": "SMTP.Example.org:587", "to": "x@example.org"})),
			want: []string{flow.AlertDestinationChanged, flow.AlertNewHost, flow.AlertDestinationChanged, flow.AlertNewHost, flow.AlertDestinationChanged},
			check: func(t *testing.T, r flow.DiffReport) {
				assert.Equal(t, "evil.example.net", r.Alerts[1].After)
				assert.Equal(t, "smtp.example.org", r.Alerts[3].After)
				assert.Equal(t, "config.host", r.Alerts[3].Path)
			},
		},
		{
			name: "same host, other path: no new host",
			base: diffFlow(trigger(), lintNode("http", "action-http", map[string]any{"url": "https://api.example.com/a"})),
			next: diffFlow(trigger(), lintNode("http", "action-http", map[string]any{"url": "https://API.example.com:443/b?x=1"})),
			want: []string{flow.AlertDestinationChanged},
		},
		{
			name: "secret moved to another place",
			base: diffFlow(trigger(),
				lintNode("a", "action-http", map[string]any{"url": "https://api.example.com/a?k={{secret.api_key}}"}),
				lintNode("b", "action-mqtt", map[string]any{"broker": "tcp://broker.lan:1883", "topic": "t"})),
			next: diffFlow(trigger(),
				lintNode("a", "action-http", map[string]any{"url": "https://api.example.com/a?k={{secret.api_key}}"}),
				lintNode("b", "action-mqtt", map[string]any{"broker": "tcp://broker.lan:1883", "topic": "t", "payload": "{{secret.api_key}}"})),
			want: []string{flow.AlertSecretMoved},
			check: func(t *testing.T, r flow.DiffReport) {
				a := r.Alerts[0]
				assert.Equal(t, "b", a.NodeID)
				assert.Equal(t, "config.payload", a.Path)
				assert.Equal(t, "api_key", a.After)
			},
		},
		{
			name: "risk switch turned on, and added on",
			base: diffFlow(trigger(), lintNode("http", "action-http", map[string]any{"url": "http://10.0.0.2/", "allowPrivate": false})),
			next: diffFlow(trigger(),
				lintNode("http", "action-http", map[string]any{"url": "http://10.0.0.2/", "allowPrivate": true}),
				lintNode("cmd", "action-command", map[string]any{"command": "ls", "allowDashArgs": true})),
			want: []string{flow.AlertRiskRaised, flow.AlertPrivilegedNodeAdded, flow.AlertRiskSwitchOn, flow.AlertPrivilegedNodeAdded, flow.AlertRiskSwitchOn},
			check: func(t *testing.T, r flow.DiffReport) {
				a := r.Alerts[2]
				assert.Equal(t, "config.allowPrivate", a.Path)
				assert.Equal(t, false, a.Before)
				assert.Equal(t, true, a.After)
			},
		},
		{
			name: "sensitive literal",
			base: diffFlow(trigger(), lintNode("mq", "action-mqtt", map[string]any{"broker": "tcp://broker.lan:1883", "topic": "t", "password": "{{secret.mqtt}}"})),
			next: diffFlow(trigger(), lintNode("mq", "action-mqtt", map[string]any{"broker": "tcp://broker.lan:1883", "topic": "t", "password": "hunter2"})),
			want: []string{flow.AlertSensitiveLiteral},
			check: func(t *testing.T, r flow.DiffReport) {
				a := r.Alerts[0]
				assert.Equal(t, "config.password", a.Path)
				assert.Nil(t, a.Before, "no values for literals")
				assert.Nil(t, a.After)
			},
		},
		{
			name: "an unchanged literal is not reported again",
			base: diffFlow(trigger(), lintNode("mq", "action-mqtt", map[string]any{"broker": "tcp://b:1883", "topic": "t", "password": "hunter2"})),
			next: diffFlow(trigger(), lintNode("mq", "action-mqtt", map[string]any{"broker": "tcp://b:1883", "topic": "t", "password": "hunter2", "qos": float64(1)})),
			want: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := flow.Diff(tc.base, tc.next, node.DefaultRegistry)
			assert.Equal(t, tc.want, alertKinds(r))
			for _, a := range r.Alerts {
				assert.NotEmpty(t, a.Message)
			}
			if tc.check != nil {
				tc.check(t, r)
			}
		})
	}
}

// Values of Sensitive properties never reach the report; secret reference
// names do.
func TestDiffHidesSensitiveValues(t *testing.T) {
	base := diffFlow(trigger(),
		lintNode("mq", "action-mqtt", map[string]any{"broker": "tcp://broker.lan:1883", "topic": "t", "password": "old-pass-999"}),
		lintNode("hook", "action-notify", map[string]any{"url": "https://hooks.slack.com/services/T0/B0/OLDSECRETPATH"}),
		lintNode("gone", "action-telegram", map[string]any{"chatId": "1", "botToken": "123:REMOVEDBOTTOKEN"}))
	next := diffFlow(trigger(),
		lintNode("mq", "action-mqtt", map[string]any{"broker": "tcp://broker.lan:1883", "topic": "t", "password": "hunter2-literal"}),
		lintNode("hook", "action-notify", map[string]any{"url": "https://hooks.example.net/services/T0/B0/NEWSECRETPATH"}),
		lintNode("http", "action-http", map[string]any{"url": "https://api.example.com/", "headers": map[string]any{"Authorization": "Bearer sk-live-123456"}}),
		lintNode("db", "action-database", map[string]any{"dsn": "{{secret.db_dsn}}"}))

	r := flow.Diff(base, next, node.DefaultRegistry)
	data, err := json.Marshal(r)
	require.NoError(t, err)
	for _, literal := range []string{"old-pass-999", "hunter2-literal", "OLDSECRETPATH", "NEWSECRETPATH", "sk-live-123456", "REMOVEDBOTTOKEN"} {
		assert.NotContains(t, string(data), literal)
	}
	hidden, err := json.Marshal(flow.HiddenValue)
	require.NoError(t, err)
	assert.Contains(t, string(data), string(hidden))
	assert.Contains(t, string(data), `"{{secret.db_dsn}}"`, "a pure secret reference is shown")
	assert.Contains(t, string(data), "hooks.example.net", "only the host of a sensitive URL is shown")

	var mq flow.NodeDiff
	for _, n := range r.Nodes {
		if n.NodeID == "mq" {
			mq = n
		}
	}
	require.Len(t, mq.Fields, 1)
	assert.Equal(t, flow.FieldDiff{Path: "config.password", Change: flow.ChangeChanged, Before: flow.HiddenValue, After: flow.HiddenValue, Sensitive: true}, mq.Fields[0])
	assert.Contains(t, alertKinds(r), flow.AlertSensitiveLiteral)
}
