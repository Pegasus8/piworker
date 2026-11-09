package service

import (
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lowRefusal saves ops as a draft of the approved flow base at the low level
// and deploys it as the agent: the deploy must be refused, naming kind, the
// draft must show it, and nothing may run or be saved.
func (e *authEnv) lowRefusal(base *flow.Flow, ops []flow.Op, kind string) {
	e.t.Helper()
	t := e.t
	e.setLevel(storage.AgentDeployLow)
	agent := e.deployer()
	saved := e.draft(agent, DraftInput{FlowID: base.ID, BaseVersion: base.Version, Ops: ops})
	assert.Contains(t, alertKindsOf(saved.Alerts), kind, "the draft shows it")
	_, err := e.a.Deploy(t.Context(), agent, DeployRequest{FlowID: base.ID, DraftID: saved.DraftID})
	te := requireKind(t, err, ErrPermissionDenied)
	assert.Contains(t, te.Message, kind)
	assert.False(t, e.running(base.ID), "nothing deployed")
	assert.Equal(t, base.Version, mustFlow(t, e, base.ID).Version, "nothing saved")
}

// mqttDoorFlow is an approved external flow: an MQTT subscriber with the
// broker password as a secret feeds a credentialed HTTP POST, and a manual
// trigger feeds a log.
func mqttDoorFlow() *flow.Flow {
	f := flow.NewFlow("door")
	addNode(f, "door", "trigger-mqtt", map[string]any{
		"broker": "tcp://127.0.0.1:1", "topic": "home/door/state", "password": "{{secret.mqtt_pw}}",
	})
	addNode(f, "call", "action-http", map[string]any{
		"url": "https://api.example.com/door", "method": "POST", "headers": map[string]any{"Authorization": "{{secret.api_key}}"},
	})
	addNode(f, "start", "trigger-manual", nil)
	addNode(f, "log", "action-log", map[string]any{"message": "hi"})
	connect(f, "door", "call")
	connect(f, "start", "log")
	return f
}

// Audit finding F9: the admin approved a publish whose topic takes the
// device from the message, with the broker password. A transform inserted
// upstream steers it to any device (a lock) with the admin's credentials;
// the low level refuses that as it refuses rewriting the topic.
func TestLowDeployRefusesSteeringADynamicDestination(t *testing.T) {
	e := newAuthEnv(t)
	require.NoError(t, e.secrets.Set("mqtt_pw", "broker-password"))
	f := flow.NewFlow("lights")
	addNode(f, "start", "trigger-manual", nil)
	addNode(f, "pub", "action-mqtt", map[string]any{"broker": "tcp://192.168.1.10:1883",
		"topic": "zigbee2mqtt/{{payload.device}}/set", "payload": "{{payload.cmd}}", "password": "{{secret.mqtt_pw}}"})
	connect(f, "start", "pub")
	base := e.create(f)
	e.lowRefusal(base, []flow.Op{
		{Op: flow.OpAddNode, Node: &flow.OpNode{ID: "inject", Type: "process-transform",
			Config: map[string]any{"expression": `{device: "front_door_lock", cmd: "UNLOCK"}`}}},
		{Op: flow.OpDisconnect, SourceNode: "start", SourcePort: "output", TargetNode: "pub", TargetPort: "input"},
		{Op: flow.OpConnect, SourceNode: "start", SourcePort: "output", TargetNode: "inject", TargetPort: "input"},
		{Op: flow.OpConnect, SourceNode: "inject", SourcePort: "output", TargetNode: "pub", TargetPort: "input"},
	}, flow.AlertDynamicDestination)
}

// Audit finding F8: an MQTT subscriber removed and added again under the
// same ID as a retained publisher, with the same broker, topic and
// password, publishes forged state with the admin's credentials where the
// admin approved only reading. The low level refuses it as it refuses
// adding the publisher.
func TestLowDeployRefusesARetypedNode(t *testing.T) {
	e := newAuthEnv(t)
	require.NoError(t, e.secrets.Set("mqtt_pw", "broker-password"))
	base := e.create(mqttDoorFlow())
	e.lowRefusal(base, []flow.Op{
		{Op: flow.OpRemoveNode, NodeID: "door"},
		{Op: flow.OpAddNode, Node: &flow.OpNode{ID: "door", Type: "action-mqtt", Config: map[string]any{
			"broker": "tcp://127.0.0.1:1", "topic": "home/door/state", "password": "{{secret.mqtt_pw}}",
			"payload": `{"state":"closed"}`, "retained": true,
		}}},
		{Op: flow.OpConnect, SourceNode: "start", SourcePort: "output", TargetNode: "door", TargetPort: "input"},
	}, flow.AlertDestinationChanged)
}
