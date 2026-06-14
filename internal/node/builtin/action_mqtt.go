package builtin

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const mqttConnectTimeout = 10 * time.Second

// MqttAction publishes a message to an MQTT topic. The client connects lazily on
// first use and is reused for the node's lifetime.
type MqttAction struct {
	*node.BaseNode

	broker   string
	clientID string
	username string
	password string
	topic    string
	payload  string
	qos      byte
	retained bool

	mu     sync.Mutex
	client mqtt.Client
}

// NewMqttAction creates an MqttAction from configuration.
func NewMqttAction(config map[string]interface{}) (node.Node, error) {
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

	return &MqttAction{
		BaseNode: base,
		broker:   broker,
		clientID: base.GetConfigString("clientId", "piworker-pub"),
		username: expandSecrets(base.GetConfigString("username", "")),
		password: expandSecrets(base.GetConfigString("password", "")),
		topic:    topic,
		payload:  base.GetConfigString("payload", "{{payload}}"),
		qos:      qos,
		retained: base.GetConfigBool("retained", false),
	}, nil
}

func (a *MqttAction) ensureClient() (mqtt.Client, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil {
		opts := mqtt.NewClientOptions().
			AddBroker(a.broker).
			SetClientID(a.clientID).
			SetConnectTimeout(mqttConnectTimeout)
		if a.username != "" {
			opts.SetUsername(a.username)
		}
		if a.password != "" {
			opts.SetPassword(a.password)
		}
		a.client = mqtt.NewClient(opts)
	}
	if !a.client.IsConnected() {
		tok := a.client.Connect()
		if !tok.WaitTimeout(mqttConnectTimeout) {
			return nil, fmt.Errorf("mqtt connect timed out")
		}
		if err := tok.Error(); err != nil {
			return nil, err
		}
	}
	return a.client, nil
}

// Process renders the topic/payload and publishes the message.
func (a *MqttAction) Process(_ context.Context, msg *types.Message) ([]*types.Message, error) {
	client, err := a.ensureClient()
	if err != nil {
		return nil, node.Transient(fmt.Errorf("mqtt connect failed: %w", err))
	}

	topic := renderTemplate(a.topic, msg)
	payload := renderTemplate(a.payload, msg)

	tok := client.Publish(topic, a.qos, a.retained, payload)
	if !tok.WaitTimeout(mqttConnectTimeout) {
		return nil, node.Transient(fmt.Errorf("mqtt publish timed out"))
	}
	if err := tok.Error(); err != nil {
		return nil, node.Transient(fmt.Errorf("mqtt publish failed: %w", err))
	}

	out := msg.Clone()
	out.SourcePort = "output"
	out.SetMeta("mqttTopic", topic)
	return []*types.Message{out}, nil
}

// Ports returns the port definitions.
func (a *MqttAction) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}}
}

// Validate checks the configuration.
func (a *MqttAction) Validate() error {
	if a.broker == "" {
		return fmt.Errorf("broker is required")
	}
	if a.topic == "" {
		return fmt.Errorf("topic is required")
	}
	return nil
}

func mqttActionConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"broker":   {Type: "string", Title: "Broker", Description: "Broker URL, e.g. tcp://host:1883"},
			"topic":    {Type: "string", Title: "Topic", Description: "Topic to publish to (supports {{templates}})"},
			"payload":  {Type: "string", Title: "Payload", Description: "Message payload (supports {{templates}})", Default: "{{payload}}"},
			"qos":      {Type: "number", Title: "QoS", Description: "0, 1 or 2", Default: 0},
			"retained": {Type: "boolean", Title: "Retained", Description: "Set the retained flag", Default: false},
			"clientId": {Type: "string", Title: "Client ID", Description: "MQTT client id", Default: "piworker-pub"},
			"username": {Type: "string", Title: "Username", Description: "Optional ({{secret.NAME}} supported)"},
			"password": {Type: "string", Title: "Password", Description: "Optional ({{secret.NAME}} supported)"},
		},
		Required: []string{"broker", "topic"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (a *MqttAction) GetConfigSchema() node.ConfigSchema {
	return mqttActionConfigSchema()
}

// MqttActionInfo returns the node type info for registration.
func MqttActionInfo() node.NodeTypeInfo {
	schema := mqttActionConfigSchema()
	return node.NodeTypeInfo{
		Type:        "action-mqtt",
		Name:        "MQTT Publish",
		Description: "Publish a message to an MQTT topic",
		Documentation: `## MQTT Publish

Publishes the rendered payload to an MQTT topic. Credentials support
` + "`{{secret.NAME}}`" + `. The connection is established lazily and reused.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| broker | string | - | Broker URL (tcp://host:1883) |
| topic | string | - | Topic (templated) |
| payload | string | {{payload}} | Payload (templated) |
| qos | number | 0 | 0, 1 or 2 |
| retained | boolean | false | Retained flag |
| clientId | string | piworker-pub | Client id |
| username/password | string | - | Optional credentials |
`,
		Category: types.NodeCategoryOutput,
		Inputs:   []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}},
		Config:   &schema,
		Icon:     "radio",
	}
}

func init() {
	info := MqttActionInfo()
	if err := node.Register(info, NewMqttAction); err != nil {
		panic("failed to register mqtt action: " + err.Error())
	}
}
