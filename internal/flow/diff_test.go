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
				assert.Nil(t, tg.Before, "chatId is not AgentVisible: the field diff has it, the alert does not")
				assert.Nil(t, tg.After)
				assert.Equal(t, "mail", r.Alerts[1].NodeID)
				assert.Nil(t, r.Alerts[1].After)
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
			// A daily request with the admin's key turned into ten a
			// second: how often the flow sends is the admin's to approve.
			name: "a trigger made to fire more often on a flow that reaches another service",
			base: diffFlow(lintNode("tick", "trigger-interval", map[string]any{"interval": float64(86400000)}),
				lintNode("http", "action-http", map[string]any{"url": "https://api.example.com/a?k={{secret.KEY}}"})),
			next: diffFlow(lintNode("tick", "trigger-interval", map[string]any{"interval": float64(100)}),
				lintNode("http", "action-http", map[string]any{"url": "https://api.example.com/a?k={{secret.KEY}}"})),
			want: []string{flow.AlertRateChanged},
			check: func(t *testing.T, r flow.DiffReport) {
				a := r.Alerts[0]
				assert.Equal(t, "tick", a.NodeID)
				assert.Equal(t, "config.interval", a.Path)
				assert.Equal(t, float64(86400000), a.Before, "intervals are agent-visible")
				assert.Equal(t, float64(100), a.After)
			},
		},
		{
			name: "a cron expression changed",
			base: diffFlow(lintNode("cron", "trigger-cron", map[string]any{"expression": "0 9 * * *"}),
				lintNode("tg", "action-telegram", map[string]any{"chatId": "111"})),
			next: diffFlow(lintNode("cron", "trigger-cron", map[string]any{"expression": "* * * * *"}),
				lintNode("tg", "action-telegram", map[string]any{"chatId": "111"})),
			want: []string{flow.AlertRateChanged},
		},
		{
			name: "a rate limiter removed, and one disabled",
			base: diffFlow(trigger(),
				lintNode("rl", "process-ratelimit", map[string]any{"limit": float64(1), "windowMs": float64(60000)}),
				lintNode("rl2", "process-ratelimit", nil),
				lintNode("http", "action-http", map[string]any{"url": "https://api.example.com/a"})),
			next: diffFlow(trigger(),
				func() flow.Node {
					n := lintNode("rl2", "process-ratelimit", nil)
					n.Enabled = false
					return n
				}(),
				lintNode("http", "action-http", map[string]any{"url": "https://api.example.com/a"})),
			want: []string{flow.AlertRateChanged, flow.AlertRateChanged},
			check: func(t *testing.T, r flow.DiffReport) {
				assert.Equal(t, "rl", r.Alerts[0].NodeID, "in the base's order, like the destinations of nodes that stop")
				assert.Empty(t, r.Alerts[0].Path, "about the node, whatever its settings")
				assert.Contains(t, r.Alerts[0].Message, "limit, windowMs")
				assert.Equal(t, "rl2", r.Alerts[1].NodeID)
			},
		},
		{
			name: "a new scheduled trigger",
			base: diffFlow(trigger(), lintNode("http", "action-http", map[string]any{"url": "https://api.example.com/a"})),
			next: diffFlow(trigger(), lintNode("http", "action-http", map[string]any{"url": "https://api.example.com/a"}),
				lintNode("tick", "trigger-interval", nil)),
			want: []string{flow.AlertRateChanged},
			check: func(t *testing.T, r flow.DiffReport) {
				assert.Equal(t, "tick", r.Alerts[0].NodeID)
			},
		},
		{
			name: "rates of a flow that reaches nothing outside PiWorker",
			base: diffFlow(lintNode("tick", "trigger-interval", map[string]any{"interval": float64(60000)}), lintNode("dbg", "process-debug", nil)),
			next: diffFlow(lintNode("tick", "trigger-interval", map[string]any{"interval": float64(100)}), lintNode("dbg", "process-debug", nil),
				lintNode("rl", "process-ratelimit", nil)),
			want: []string{},
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

func disabled(n flow.Node) flow.Node {
	n.Enabled = false
	return n
}

// Alerts describe the flow that runs: a disabled node counts as absent, as
// for Risk, so enabling one is like adding it and disabling one like
// removing it.
func TestDiffAlertsTreatDisabledNodesAsAbsent(t *testing.T) {
	evil := func() flow.Node {
		return lintNode("h", "action-http", map[string]any{"url": "https://evil.example.com/x", "headers": map[string]any{"Authorization": "{{secret.api}}"}})
	}
	approved := lintNode("a", "action-http", map[string]any{"url": "https://api.example.com/", "headers": map[string]any{"Authorization": "{{secret.api}}"}})
	cases := []struct {
		name       string
		base, next *flow.Flow
		want       []string
	}{
		{
			name: "enabling a dormant destination and secret",
			base: diffFlow(trigger(), disabled(evil())),
			next: diffFlow(trigger(), evil()),
			want: []string{flow.AlertRiskRaised, flow.AlertSecretAdded, flow.AlertDestinationChanged, flow.AlertNewHost},
		},
		{
			name: "enabling a node that reuses an approved secret elsewhere",
			base: diffFlow(trigger(), approved, disabled(evil())),
			next: diffFlow(trigger(), approved, evil()),
			want: []string{flow.AlertSecretMoved, flow.AlertDestinationChanged, flow.AlertNewHost},
		},
		{
			name: "enabling a dormant risk switch",
			base: diffFlow(trigger(), disabled(lintNode("rss", "trigger-rss", map[string]any{"url": "http://10.0.0.2/feed", "allowPrivate": true}))),
			next: diffFlow(trigger(), lintNode("rss", "trigger-rss", map[string]any{"url": "http://10.0.0.2/feed", "allowPrivate": true})),
			// The feed now polls (rate_changed): a scheduled trigger that
			// starts running.
			want: []string{flow.AlertRiskRaised, flow.AlertPrivilegedNodeAdded, flow.AlertRateChanged, flow.AlertRiskSwitchOn, flow.AlertDestinationChanged, flow.AlertNewHost},
		},
		{
			name: "enabling a dormant literal credential",
			base: diffFlow(trigger(), disabled(lintNode("mq", "action-mqtt", map[string]any{"broker": "tcp://broker.lan:1883", "topic": "t", "password": "hunter2"}))),
			next: diffFlow(trigger(), lintNode("mq", "action-mqtt", map[string]any{"broker": "tcp://broker.lan:1883", "topic": "t", "password": "hunter2"})),
			want: []string{flow.AlertRiskRaised, flow.AlertDestinationChanged, flow.AlertNewHost, flow.AlertSensitiveLiteral, flow.AlertDestinationChanged},
		},
		{
			name: "disabling a destination is like removing it",
			base: diffFlow(trigger(), lintNode("tg", "action-telegram", map[string]any{"chatId": "111"})),
			next: diffFlow(trigger(), disabled(lintNode("tg", "action-telegram", map[string]any{"chatId": "111"}))),
			want: []string{flow.AlertDestinationChanged},
		},
		{
			name: "changing a node that stays disabled",
			base: diffFlow(trigger(), disabled(lintNode("tg", "action-telegram", map[string]any{"chatId": "111"}))),
			next: diffFlow(trigger(), disabled(lintNode("tg", "action-telegram", map[string]any{"chatId": "{{payload.chat}}", "botToken": "literal"}))),
			want: []string{},
		},
		{
			name: "removing a disabled node",
			base: diffFlow(trigger(), disabled(evil())),
			next: diffFlow(trigger()),
			want: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := flow.Diff(tc.base, tc.next, node.DefaultRegistry)
			assert.Equal(t, tc.want, alertKinds(r))
		})
	}
}

// Alerts are stored with a draft and returned to its author, so they hold
// no setting value an agent may not read (the read-view rule: only
// AgentVisible, non-Sensitive values) — only risks, hosts, secret names and
// risk-switch booleans. The field diffs (editor only) hold the values.
func TestDiffAlertsHoldNoHiddenValues(t *testing.T) {
	base := diffFlow(trigger(),
		lintNode("tg", "action-telegram", map[string]any{"chatId": "-100123456789", "botToken": "{{secret.bot}}"}),
		lintNode("mail", "action-email", map[string]any{"host": "smtp.corp.internal", "to": "ceo@corp.example", "port": float64(587)}),
		lintNode("http", "action-http", map[string]any{"url": "https://hooks.slack.com/services/T0/B0/XOXBSECRETPATH"}),
		lintNode("mq", "action-mqtt", map[string]any{"broker": "tcp://broker.lan:1883", "topic": "home/secret-topic"}),
		lintNode("i2c", "action-i2c", map[string]any{"bus": "1", "address": "0x76"}))
	next := diffFlow(trigger(),
		lintNode("http", "action-http", map[string]any{"url": "https://hooks.slack.com/services/T0/B0/OTHERSECRETPATH"}),
		lintNode("mq", "action-mqtt", map[string]any{"broker": "tcp://broker.lan:1883", "topic": "home/{{payload.room}}-private"}),
		lintNode("i2c", "action-i2c", map[string]any{"bus": "1", "address": "0x77"}))

	r := flow.Diff(base, next, node.DefaultRegistry)
	require.NotEmpty(t, r.Alerts)
	alerts, err := json.Marshal(r.Alerts)
	require.NoError(t, err)
	for _, hidden := range []string{"-100123456789", "smtp.corp.internal", "ceo@corp.example", "XOXBSECRETPATH", "OTHERSECRETPATH", "secret-topic", "-private"} {
		assert.NotContains(t, string(alerts), hidden)
	}
	// AgentVisible values are shown.
	var i2c *flow.Alert
	for k := range r.Alerts {
		if r.Alerts[k].NodeID == "i2c" {
			i2c = &r.Alerts[k]
		}
	}
	require.NotNil(t, i2c)
	assert.Equal(t, "0x76", i2c.Before)
	assert.Equal(t, "0x77", i2c.After)

	// The editor's field diffs still show what changed.
	report, err := json.Marshal(r.Nodes)
	require.NoError(t, err)
	assert.Contains(t, string(report), "ceo@corp.example")
}

// A second node under an existing ID is a node of its own in the report,
// with every alert, not a silent duplicate (deploy refuses the flow, but the
// approval page must still show it).
func TestDiffReportsDuplicateNodeIDs(t *testing.T) {
	base := diffFlow(trigger(), lintNode("h", "action-http", map[string]any{"url": "https://api.example.com/"}))
	next := diffFlow(trigger(),
		lintNode("h", "action-http", map[string]any{"url": "https://api.example.com/"}),
		lintNode("h", "action-http", map[string]any{"url": "https://evil.example/", "headers": map[string]any{"X": "{{secret.k}}"}}))
	r := flow.Diff(base, next, node.DefaultRegistry)
	require.Len(t, r.Nodes, 1)
	assert.Equal(t, "h", r.Nodes[0].NodeID)
	assert.Equal(t, flow.ChangeAdded, r.Nodes[0].Change)
	assert.Equal(t, []string{flow.AlertSecretAdded, flow.AlertDestinationChanged, flow.AlertNewHost}, alertKinds(r))

	same := flow.Diff(next, next, node.DefaultRegistry)
	assert.Empty(t, same.Nodes, "duplicates are paired in order")
	assert.Empty(t, same.Alerts)

	removed := flow.Diff(next, base, node.DefaultRegistry)
	require.Len(t, removed.Nodes, 1)
	assert.Equal(t, flow.ChangeRemoved, removed.Nodes[0].Change)
}

// What a credential the admin approved is used for changes when the
// request's operation (an HTTP method or body) or its input changes.
func TestDiffFlagsCredentialedRequestChanges(t *testing.T) {
	authed := func(cfg map[string]any) flow.Node {
		c := map[string]any{"url": "https://api.example.com/items", "method": "GET", "headers": map[string]any{"Authorization": "Bearer {{secret.api}}"}}
		for k, v := range cfg {
			c[k] = v
		}
		return lintNode("h", "action-http", c)
	}
	cases := []struct {
		name       string
		base, next *flow.Flow
		want       []string
		check      func(t *testing.T, r flow.DiffReport)
	}{
		{
			name: "method",
			base: diffFlow(trigger(), authed(nil)),
			next: diffFlow(trigger(), authed(map[string]any{"method": "DELETE"})),
			want: []string{flow.AlertCredentialedRequestChanged},
			check: func(t *testing.T, r flow.DiffReport) {
				a := r.Alerts[0]
				assert.Equal(t, "h", a.NodeID)
				assert.Equal(t, "config.method", a.Path)
				assert.Equal(t, "GET", a.Before, "method is AgentVisible")
				assert.Equal(t, "DELETE", a.After)
			},
		},
		{
			name: "body",
			base: diffFlow(trigger(), authed(map[string]any{"method": "POST", "body": "{\"a\":1}"})),
			next: diffFlow(trigger(), authed(map[string]any{"method": "POST", "body": "{\"drop\":true}"})),
			want: []string{flow.AlertCredentialedRequestChanged},
			check: func(t *testing.T, r flow.DiffReport) {
				assert.Nil(t, r.Alerts[0].Before, "body is not AgentVisible")
				assert.Nil(t, r.Alerts[0].After)
			},
		},
		{
			name: "a new input",
			base: diffFlow(trigger(), authed(nil)),
			next: func() *flow.Flow {
				f := diffFlow(trigger(), authed(nil), lintNode("x", "process-transform", map[string]any{"expression": "payload"}))
				f.Connections = []flow.Connection{lintConn("c1", "t", "output", "x", "input"), lintConn("c2", "x", "output", "h", "input")}
				return f
			}(),
			want: []string{flow.AlertCredentialedRequestChanged},
			check: func(t *testing.T, r flow.DiffReport) {
				assert.Equal(t, "h", r.Alerts[0].NodeID)
				assert.Empty(t, r.Alerts[0].Path)
			},
		},
		{
			name: "no credential, no alert",
			base: diffFlow(trigger(), lintNode("h", "action-http", map[string]any{"url": "https://api.example.com/", "method": "GET"})),
			next: diffFlow(trigger(), lintNode("h", "action-http", map[string]any{"url": "https://api.example.com/", "method": "DELETE"})),
			want: []string{},
		},
		{
			name: "content of a credentialed message is not an operation",
			base: diffFlow(trigger(), lintNode("tg", "action-telegram", map[string]any{"chatId": "1", "botToken": "{{secret.bot}}", "message": "hi"})),
			next: diffFlow(trigger(), lintNode("tg", "action-telegram", map[string]any{"chatId": "1", "botToken": "{{secret.bot}}", "message": "hello"})),
			want: []string{},
		},
		{
			name: "a literal credential counts too",
			base: diffFlow(trigger(), lintNode("h", "action-http", map[string]any{"url": "https://api.example.com/", "headers": map[string]any{"Authorization": "Bearer abc"}})),
			next: diffFlow(trigger(), lintNode("h", "action-http", map[string]any{"url": "https://api.example.com/", "headers": map[string]any{"Authorization": "Bearer abc"}, "method": "POST"})),
			want: []string{flow.AlertCredentialedRequestChanged},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := flow.Diff(tc.base, tc.next, node.DefaultRegistry)
			assert.Equal(t, tc.want, alertKinds(r))
			if tc.check != nil {
				tc.check(t, r)
			}
		})
	}
}

// credFlow is a manual trigger, a transform shaping a message and an HTTP
// POST that sends it with a credential (its body is the message).
func credFlow(change func(f *flow.Flow)) *flow.Flow {
	f := diffFlow(
		trigger(),
		lintNode("calc", "process-transform", map[string]any{"expression": "{temp: 20}"}),
		lintNode("h", "action-http", map[string]any{"url": "https://api.example.com/hook", "method": "POST",
			"headers": map[string]any{"Authorization": "{{secret.api_key}}"}, "body": "{{payload}}"}),
		lintNode("side", "process-transform", map[string]any{"expression": "payload"}),
		lintNode("fv", "set-var", map[string]any{"scope": "flow", "key": "count", "value": "1"}),
	)
	f.Connections = []flow.Connection{
		lintConn("c1", "t", "output", "calc", "input"),
		lintConn("c2", "calc", "output", "h", "input"),
		lintConn("c3", "t", "output", "side", "input"),
		lintConn("c4", "side", "output", "fv", "input"),
	}
	if change != nil {
		change(f)
	}
	return f
}

// A request's body is usually a template over the message: what an approved
// credential sends changes with anything that feeds the request.
func TestDiffFlagsChangesUpstreamOfCredentialedRequests(t *testing.T) {
	cases := []struct {
		name       string
		base, next *flow.Flow
		alert      bool
	}{
		{name: "nothing changed", base: credFlow(nil), next: credFlow(nil)},
		{name: "the message shaped upstream changed", base: credFlow(nil), next: credFlow(func(f *flow.Flow) {
			diffNodeOf(f, "calc").Config["expression"] = "{temp: 20, all: vars}"
		}), alert: true},
		{name: "a new input upstream", base: credFlow(nil), next: credFlow(func(f *flow.Flow) {
			f.Nodes = append(f.Nodes, lintNode("t2", "trigger-manual", nil))
			f.Connections = append(f.Connections, lintConn("c5", "t2", "output", "calc", "input"))
		}), alert: true},
		{name: "a flow variable read upstream, written elsewhere", base: credFlow(func(f *flow.Flow) {
			diffNodeOf(f, "calc").Config["expression"] = `flowVars["count"]`
		}), next: credFlow(func(f *flow.Flow) {
			diffNodeOf(f, "calc").Config["expression"] = `flowVars["count"]`
			diffNodeOf(f, "fv").Config["value"] = "vars"
		}), alert: true},
		{name: "a change on another branch", base: credFlow(nil), next: credFlow(func(f *flow.Flow) {
			diffNodeOf(f, "side").Config["expression"] = "payload * 2"
		})},
		{name: "no credential", base: credFlow(func(f *flow.Flow) {
			delete(diffNodeOf(f, "h").Config, "headers")
		}), next: credFlow(func(f *flow.Flow) {
			delete(diffNodeOf(f, "h").Config, "headers")
			diffNodeOf(f, "calc").Config["expression"] = "{temp: 20, all: vars}"
		})},
		{name: "a credential the base did not use", base: credFlow(func(f *flow.Flow) {
			delete(diffNodeOf(f, "h").Config, "headers")
		}), next: credFlow(func(f *flow.Flow) {
			diffNodeOf(f, "calc").Config["expression"] = "{temp: 20, all: vars}"
		})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := flow.Diff(tc.base, tc.next, node.DefaultRegistry)
			var got []flow.Alert
			for _, a := range r.Alerts {
				if a.Kind == flow.AlertCredentialedRequestChanged {
					got = append(got, a)
				}
			}
			if !tc.alert {
				assert.Empty(t, got, "alerts: %v", r.Alerts)
				return
			}
			require.Len(t, got, 1, "alerts: %v", r.Alerts)
			assert.Equal(t, "h", got[0].NodeID)
			assert.Empty(t, got[0].Path)
			assert.NotContains(t, got[0].Message, "vars", "values agents may not read stay out of alerts")
		})
	}
}

// credToolFlow is a tool whose answer is a credentialed request's response,
// shaped by a transform.
func credToolFlow(change func(f *flow.Flow)) *flow.Flow {
	f := diffFlow(
		lintNode("tool", "trigger-tool", map[string]any{"toolName": "status"}),
		lintNode("h", "action-http", map[string]any{"url": "https://api.example.com/items", "method": "GET",
			"headers": map[string]any{"Authorization": "{{secret.api_key}}"}, "outputMode": "status"}),
		lintNode("shape", "process-transform", map[string]any{"expression": "payload.status"}),
		lintNode("reply", "action-reply", nil),
		trigger(),
		lintNode("side", "process-transform", map[string]any{"expression": "payload"}),
	)
	f.Connections = []flow.Connection{
		lintConn("c1", "tool", "output", "h", "input"),
		lintConn("c2", "h", "output", "shape", "input"),
		lintConn("c3", "shape", "output", "reply", "input"),
		lintConn("c4", "t", "output", "side", "input"),
	}
	if change != nil {
		change(f)
	}
	return f
}

// What a credentialed request returns goes back to the agent that calls the
// tool: widening what reaches the reply (the request's own output, or a
// node between it and the reply) changes what the credential reveals.
func TestDiffFlagsWidenedCredentialedResponses(t *testing.T) {
	cases := []struct {
		name       string
		base, next *flow.Flow
		alert      bool
	}{
		{name: "nothing changed", base: credToolFlow(nil), next: credToolFlow(nil)},
		{name: "the request returns more", base: credToolFlow(nil), next: credToolFlow(func(f *flow.Flow) {
			diffNodeOf(f, "h").Config["outputMode"] = "full"
		}), alert: true},
		{name: "the shaping widened", base: credToolFlow(nil), next: credToolFlow(func(f *flow.Flow) {
			diffNodeOf(f, "shape").Config["expression"] = "payload"
		}), alert: true},
		{name: "a node off the response's way changed", base: credToolFlow(nil), next: credToolFlow(func(f *flow.Flow) {
			diffNodeOf(f, "side").Config["expression"] = "payload * 2"
		})},
		{name: "a response that reaches no reply", base: credToolFlow(func(f *flow.Flow) {
			f.Connections = f.Connections[:2]
		}), next: credToolFlow(func(f *flow.Flow) {
			f.Connections = f.Connections[:2]
			diffNodeOf(f, "shape").Config["expression"] = "payload"
		})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := flow.Diff(tc.base, tc.next, node.DefaultRegistry)
			var got []flow.Alert
			for _, a := range r.Alerts {
				if a.Kind == flow.AlertCredentialedRequestChanged {
					got = append(got, a)
				}
			}
			if !tc.alert {
				assert.Empty(t, got, "alerts: %v", r.Alerts)
				return
			}
			require.Len(t, got, 1, "alerts: %v", r.Alerts)
			assert.Equal(t, "h", got[0].NodeID)
			assert.Contains(t, got[0].Message, "agent")
		})
	}
}
