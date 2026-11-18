package flow_test

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	_ "github.com/Pegasus8/piworker/internal/node/builtin"
	"github.com/stretchr/testify/assert"
)

// TestInstanceRisk: a node's risk is its type's declared risk, raised by the
// same instance rules the tool analysis applies (dynamic destinations,
// allowPrivate, MQTT and files as shared state); unknown types are
// privileged.
func TestInstanceRisk(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  string
		cfg  map[string]any
		want node.Risk
	}{
		{"safe processing", "process-transform", map[string]any{"expression": "payload"}, node.RiskSafe},
		{"safe output", "action-log", nil, node.RiskSafe},
		{"fixed HTTP destination", "action-http", map[string]any{"url": "https://api.example.com/x"}, node.RiskExternal},
		{"dynamic HTTP destination", "action-http", map[string]any{"url": "{{payload.url}}"}, node.RiskPrivileged},
		{"HTTP to private addresses", "action-http", map[string]any{"url": "https://api.example.com/x", "allowPrivate": true}, node.RiskPrivileged},
		{"dynamic notify destination", "action-notify", map[string]any{"url": "https://{{vars.host}}/hook"}, node.RiskPrivileged},
		{"MQTT is shared state", "action-mqtt", map[string]any{"broker": "tcp://b:1883", "topic": "t"}, node.RiskPrivileged},
		{"files are shared state", "action-file", map[string]any{"path": "/tmp/x"}, node.RiskPrivileged},
		{"privileged type", "action-command", map[string]any{"command": "true"}, node.RiskPrivileged},
		{"unknown type", "no-such-type", nil, node.RiskPrivileged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := &flow.Node{ID: "n", Type: tc.typ, Config: tc.cfg, Enabled: true}
			assert.Equal(t, tc.want, flow.InstanceRisk(n, node.DefaultRegistry))
		})
	}
}
