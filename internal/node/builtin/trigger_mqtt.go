package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/metrics"
	"github.com/Pegasus8/piworker/internal/mqttpool"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog/log"
)

// MqttTrigger subscribes to an MQTT topic and emits a message per received
// publication.
type MqttTrigger struct {
	*node.BaseNode

	broker   string
	clientID string // own client id; "" shares a pooled connection (ADR-0025)
	username string
	password string
	topic    string
	qos      byte

	initialConnectWait   time.Duration
	connectRetryInterval time.Duration

	mu      sync.Mutex
	running bool
	sub     *mqttpool.Sub
}

// NewMqttTrigger creates an MqttTrigger from configuration.
func NewMqttTrigger(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	broker := base.GetConfigString("broker", "")
	if broker == "" {
		return nil, fmt.Errorf("broker is required (e.g. tcp://host:1883)")
	}
	topic := base.GetConfigString("topic", "")
	if topic == "" {
		return nil, fmt.Errorf("topic is required")
	}
	qos := byte(base.GetConfigInt("qos", 0))
	if qos > 2 {
		qos = 0
	}

	return &MqttTrigger{
		BaseNode: base,
		broker:   broker,
		clientID: ownMqttClientID(base.GetConfigString("clientId", ""), "piworker-sub"),
		username: base.GetConfigString("username", ""), // {{secret.NAME}} resolved by Start
		password: base.GetConfigString("password", ""),
		topic:    topic,
		qos:      qos,

		initialConnectWait:   mqttInitialConnectWait,
		connectRetryInterval: mqttConnectRetryInterval,
	}, nil
}

// Process is unused for trigger nodes.
func (t *MqttTrigger) Process(context.Context, *types.Message) ([]*types.Message, error) {
	return nil, nil
}

// mqttMessagePayload normalizes a received MQTT message into a payload map,
// parsing a JSON body when possible and falling back to a string.
func mqttMessagePayload(topic string, payload []byte) map[string]interface{} {
	var body interface{} = string(payload)
	var parsed interface{}
	if json.Unmarshal(payload, &parsed) == nil {
		body = parsed
	}
	return map[string]interface{}{"topic": topic, "payload": body}
}

// mqttInitialConnectWait is how long Start waits for the first connection
// before returning and leaving the client to retry in the background.
const mqttInitialConnectWait = 2 * time.Second

// Start subscribes to the topic on a shared connection to the broker
// (ADR-0025), which subscribes again on every reconnect: with a clean
// session the broker forgets subscriptions whenever the connection drops. If
// the broker is not reachable yet (e.g. the Pi booted before the network
// came up) Start still succeeds and the connection keeps retrying in the
// background.
func (t *MqttTrigger) Start(ctx context.Context, out chan<- *types.Message) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running {
		return fmt.Errorf("trigger already running")
	}

	handler := func(m mqtt.Message) {
		msg := types.NewMessage(mqttMessagePayload(m.Topic(), m.Payload()), types.DataTypeObject)
		msg.SourcePort = "output"
		msg.Topic = "mqtt"
		select {
		case <-ctx.Done():
		case out <- msg:
		default:
			metrics.IncrementMessagesDropped()
			log.Warn().Str("topic", m.Topic()).Msg("mqtt message dropped: output channel full")
		}
	}

	ep := mqttEndpointFor(ctx, t.broker, t.username, t.password, t.clientID)
	sub, fresh := mqttpool.Subscribe(ep, t.topic, t.qos, handler, t.connectRetryInterval)
	// Wait only briefly for a new connection: the runtime starts triggers
	// synchronously, and an unreachable broker is not a configuration error.
	// Only invalid config (checked in the constructor) makes Start fail.
	if fresh {
		if err := sub.Conn().WaitUp(ctx, t.initialConnectWait); err != nil {
			log.Warn().Str("broker", t.broker).Msg("mqtt broker not reachable yet; retrying in background")
		}
	}

	t.sub = sub
	t.running = true
	return nil
}

// Stop ends the subscription; the shared connection closes (aborting a
// pending connect retry) once no node uses it.
func (t *MqttTrigger) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.running {
		return nil
	}
	t.sub.Release()
	t.sub = nil
	t.running = false
	return nil
}

// Ports returns the port definitions.
func (t *MqttTrigger) Ports() (inputs []types.Port, outputs []types.Port) {
	return nil, []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeObject, Multiple: true}}
}

// Validate checks the configuration.
func (t *MqttTrigger) Validate() error {
	if t.broker == "" {
		return fmt.Errorf("broker is required")
	}
	if t.topic == "" {
		return fmt.Errorf("topic is required")
	}
	return nil
}

func mqttTriggerConfigSchema() node.ConfigSchema {
	minQoS, maxQoS := float64(0), float64(2)
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"broker":   {Type: "string", Title: "Broker", Description: "Broker URL, e.g. tcp://host:1883", Destination: true},
			"topic":    {Type: "string", Title: "Topic", Description: "Topic filter to subscribe to (supports +/# wildcards)", Destination: true},
			"qos":      {Type: "number", Title: "QoS", Description: "0, 1 or 2", Default: 0, Minimum: &minQoS, Maximum: &maxQoS, AgentVisible: true},
			"clientId": {Type: "string", Title: "Client ID", Description: "Own MQTT client id; leave empty to share a connection with the other MQTT nodes on this broker", Default: ""},
			"username": {Type: "string", Title: "Username", Description: "Optional ({{secret.NAME}} supported)"},
			"password": {Type: "string", Title: "Password", Description: "Optional; use {{secret.NAME}}", Sensitive: true},
		},
		Required: []string{"broker", "topic"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (t *MqttTrigger) GetConfigSchema() node.ConfigSchema {
	return mqttTriggerConfigSchema()
}

// MqttTriggerInfo returns the node type info for registration.
func MqttTriggerInfo() node.NodeTypeInfo {
	schema := mqttTriggerConfigSchema()
	return node.NodeTypeInfo{
		Type:        "trigger-mqtt",
		Name:        "MQTT Subscribe",
		Description: "Trigger when a message arrives on an MQTT topic",
		Documentation: `## MQTT Subscribe

Subscribes to an MQTT topic filter and emits a message per publication. JSON
payloads are parsed; others pass through as strings. Credentials support
` + "`{{secret.NAME}}`" + `.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| broker | string | - | Broker URL (tcp://host:1883) |
| topic | string | - | Topic filter (+/# wildcards) |
| qos | number | 0 | 0, 1 or 2 |
| clientId | string | (shared) | Own client id; empty shares a connection |
| username/password | string | - | Optional credentials |

## Connection

MQTT nodes on one broker with the same credentials and no client id of their
own share a connection. It reconnects automatically after a network drop and
re-subscribes on every (re)connect. If the broker is unreachable when the flow starts (e.g. the
device booted before the network was up) the flow still starts and the node
keeps retrying in the background.

## Output payload

` + "`{ topic, payload }`" + ` — payload is the parsed JSON or the raw string.
`,
		Category: types.NodeCategoryInput,
		Risk:     node.RiskSafe,
		Inputs:   nil,
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeObject, Multiple: true}},
		Config:   &schema,
		Icon:     "radio",
	}
}

func init() {
	info := MqttTriggerInfo()
	if err := node.Register(info, NewMqttTrigger); err != nil {
		panic("failed to register mqtt trigger: " + err.Error())
	}
}
