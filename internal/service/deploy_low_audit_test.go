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

// garageTools is the flow of audit finding F1 (variant A): garage_status
// (confirm none, safe) answers through a transform; garage_open (confirm
// admin) posts to a home-automation webhook with no credential.
func garageTools() *flow.Flow {
	schema := map[string]any{"type": "object", "properties": map[string]any{}}
	f := flow.NewFlow("garage")
	addNode(f, "status", "trigger-tool", map[string]any{"toolName": "garage_status", "description": "Tells whether the garage door is open.", "inputSchema": schema})
	addNode(f, "answer", "process-transform", map[string]any{"expression": `"closed"`})
	addNode(f, "sreply", "action-reply", nil)
	addNode(f, "open", "trigger-tool", map[string]any{"toolName": "garage_open", "description": "Opens the garage door for a visitor.",
		"inputSchema": schema, "confirm": "admin"})
	addNode(f, "pub", "action-http", map[string]any{"url": "https://ha.example.com/api/webhook/abc", "method": "POST"})
	addNode(f, "oreply", "action-reply", nil)
	connect(f, "status", "answer")
	connect(f, "answer", "sreply")
	connect(f, "open", "pub")
	connect(f, "pub", "oreply")
	return f
}

// Audit finding F1: at the low level an agent may not change what a
// published tool runs. Wiring the confirm:none tool to the action behind
// the confirm:admin tool would run that action at will, with no approval,
// while the tool's grant and fingerprint stay valid.
func TestLowDeployRefusesRewiringATool(t *testing.T) {
	cases := map[string]struct {
		base func() *flow.Flow
		ops  []flow.Op
	}{
		// Variant A: the safe tool reaches the gated action.
		"a safe tool wired to the gated action": {base: garageTools, ops: []flow.Op{
			{Op: flow.OpConnect, SourceNode: "status", SourcePort: "output", TargetNode: "pub", TargetPort: "input"},
		}},
		// Variant B: a granted tool's action swapped for the gated one;
		// its risk, and so its fingerprint, stay the same.
		"a granted tool repointed": {base: func() *flow.Flow {
			f := garageTools()
			addNode(f, "lights", "action-http", map[string]any{"url": "https://lights.example.com/on", "method": "POST"})
			nodeOf(f, "answer").Config["expression"] = "payload"
			f.Connections = nil
			connect(f, "status", "lights")
			connect(f, "lights", "sreply")
			connect(f, "open", "pub")
			connect(f, "pub", "oreply")
			return f
		}, ops: []flow.Op{
			{Op: flow.OpDisconnect, SourceNode: "status", SourcePort: "output", TargetNode: "lights", TargetPort: "input"},
			{Op: flow.OpConnect, SourceNode: "status", SourcePort: "output", TargetNode: "pub", TargetPort: "input"},
		}},
		// The gated tool also fires another action once approved.
		"a gated tool wired to another action": {base: garageTools, ops: []flow.Op{
			{Op: flow.OpAddNode, Node: &flow.OpNode{ID: "log", Type: "action-log", Config: map[string]any{"message": "opened"}}},
			{Op: flow.OpConnect, SourceNode: "open", SourcePort: "output", TargetNode: "log", TargetPort: "input"},
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newAuthEnv(t)
			base := e.create(tc.base())
			e.lowRefusal(base, tc.ops, flow.AlertToolPathChanged)
		})
	}
}

// Audit finding F2: at the low level an agent may not deploy a node that
// reads variables or message metadata, or what a credential returned: its
// errors and which nodes run come back through pw_get_run.
func TestLowDeployRefusesNodesReadingProtectedData(t *testing.T) {
	heartbeat := func() *flow.Flow {
		f := flow.NewFlow("heartbeat")
		addNode(f, "tick", "trigger-interval", map[string]any{"interval": 60000})
		addNode(f, "log", "action-log", map[string]any{"message": "tick"})
		connect(f, "tick", "log")
		return f
	}
	peek := func(from, expression string) []flow.Op {
		return []flow.Op{
			{Op: flow.OpAddNode, Node: &flow.OpNode{ID: "peek", Type: "process-transform", Config: map[string]any{"expression": expression}}},
			{Op: flow.OpConnect, SourceNode: from, SourcePort: "output", TargetNode: "peek", TargetPort: "input"},
		}
	}
	cases := map[string]struct {
		base func() *flow.Flow
		ops  []flow.Op
	}{
		"global variables":     {base: heartbeat, ops: peek("tick", "duration(toJSON(vars))")},
		"a hidden expression":  {base: lightsFlow, ops: peek("calc", `duration(join(split(meta.expression, ""), "."))`)},
		"a credential's reply": {base: lightsFlow, ops: peek("call", `int(join(split(toJSON(payload), ""), "."))`)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newAuthEnv(t)
			base := e.create(tc.base())
			e.lowRefusal(base, tc.ops, flow.AlertDataReadChanged)
		})
	}
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
