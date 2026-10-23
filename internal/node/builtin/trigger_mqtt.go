package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Pegasus8/piworker/internal/metrics"
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
	clientID string
	username string
	password string
	topic    string
	qos      byte

	mu      sync.Mutex
	running bool
	client  mqtt.Client
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
		clientID: base.GetConfigString("clientId", "piworker-sub"),
		username: expandSecrets(base.GetConfigString("username", "")),
		password: expandSecrets(base.GetConfigString("password", "")),
		topic:    topic,
		qos:      qos,
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

// Start connects to the broker and subscribes to the topic.
func (t *MqttTrigger) Start(ctx context.Context, out chan<- *types.Message) error {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return fmt.Errorf("trigger already running")
	}
	t.mu.Unlock()

	opts := mqtt.NewClientOptions().
		AddBroker(t.broker).
		SetClientID(t.clientID).
		SetConnectTimeout(mqttConnectTimeout)
	if t.username != "" {
		opts.SetUsername(t.username)
	}
	if t.password != "" {
		opts.SetPassword(t.password)
	}

	client := mqtt.NewClient(opts)
	tok := client.Connect()
	if !tok.WaitTimeout(mqttConnectTimeout) {
		return fmt.Errorf("mqtt connect timed out")
	}
	if err := tok.Error(); err != nil {
		return fmt.Errorf("mqtt connect failed: %w", err)
	}

	handler := func(_ mqtt.Client, m mqtt.Message) {
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

	subTok := client.Subscribe(t.topic, t.qos, handler)
	if !subTok.WaitTimeout(mqttConnectTimeout) {
		client.Disconnect(250)
		return fmt.Errorf("mqtt subscribe timed out")
	}
	if err := subTok.Error(); err != nil {
		client.Disconnect(250)
		return fmt.Errorf("mqtt subscribe failed: %w", err)
	}

	t.mu.Lock()
	t.client = client
	t.running = true
	t.mu.Unlock()
	return nil
}

// Stop unsubscribes and disconnects.
func (t *MqttTrigger) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.running {
		return nil
	}
	if t.client != nil {
		t.client.Unsubscribe(t.topic)
		t.client.Disconnect(250)
		t.client = nil
	}
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
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"broker":   {Type: "string", Title: "Broker", Description: "Broker URL, e.g. tcp://host:1883"},
			"topic":    {Type: "string", Title: "Topic", Description: "Topic filter to subscribe to (supports +/# wildcards)"},
			"qos":      {Type: "number", Title: "QoS", Description: "0, 1 or 2", Default: 0},
			"clientId": {Type: "string", Title: "Client ID", Description: "MQTT client id", Default: "piworker-sub"},
			"username": {Type: "string", Title: "Username", Description: "Optional ({{secret.NAME}} supported)"},
			"password": {Type: "string", Title: "Password", Description: "Optional ({{secret.NAME}} supported)"},
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
| clientId | string | piworker-sub | Client id |
| username/password | string | - | Optional credentials |

## Output payload

` + "`{ topic, payload }`" + ` — payload is the parsed JSON or the raw string.
`,
		Category: types.NodeCategoryInput,
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
