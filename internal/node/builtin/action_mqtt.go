package builtin

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/mqttpool"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// mqttConnectTimeout bounds waiting for a connection or a publish.
const mqttConnectTimeout = mqttpool.ConnectTimeout

// mqttConnectRetryInterval is the wait between connection attempts.
const mqttConnectRetryInterval = mqttpool.RetryInterval

// ownMqttClientID returns the client id a node configured for itself, or ""
// to use a pooled connection (ADR-0025) when it is empty or still the old
// shared constant default (prefix itself), which older flows stored because
// the editor pre-filled it.
func ownMqttClientID(configured, prefix string) string {
	configured = strings.TrimSpace(configured)
	if configured == prefix {
		return ""
	}
	return configured
}

// mqttEndpointFor is what a node connects with, its credentials resolved
// through ctx, in the sandbox of ctx if any: nodes in a sandbox (ADR-0020)
// never share a connection with nodes outside it.
func mqttEndpointFor(ctx context.Context, broker, username, password, clientID string) mqttpool.Endpoint {
	return mqttpool.Endpoint{Broker: broker, Username: expandSecrets(ctx, username), Password: expandSecrets(ctx, password),
		ClientID: clientID, Sandbox: node.SandboxFrom(ctx)}
}

// MqttAction publishes a message to an MQTT topic. It takes a shared
// connection (ADR-0025) on first use and holds it until Close.
type MqttAction struct {
	*node.BaseNode

	broker   string
	clientID string // own client id; "" shares a pooled connection
	username string
	password string
	topic    string
	payload  string
	qos      byte
	retained bool

	mu   sync.Mutex
	conn *mqttpool.Conn
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
		clientID: ownMqttClientID(base.GetConfigString("clientId", ""), "piworker-pub"),
		username: base.GetConfigString("username", ""), // {{secret.NAME}} resolved on connect
		password: base.GetConfigString("password", ""),
		topic:    topic,
		payload:  base.GetConfigString("payload", "{{payload}}"),
		qos:      qos,
		retained: base.GetConfigBool("retained", false),
	}, nil
}

// waitMqttToken waits for tok to complete, giving up when ctx is done or
// after timeout, whichever comes first.
func waitMqttToken(ctx context.Context, tok mqtt.Token, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-tok.Done():
		return tok.Error()
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("timed out after %s", timeout)
	}
}

// ensureClient takes the node's connection on first use and waits until it
// is up, ctx is done or mqttConnectTimeout passes. A connection that is not
// up keeps retrying in the background for the next message.
func (a *MqttAction) ensureClient(ctx context.Context) (mqtt.Client, error) {
	a.mu.Lock()
	if a.conn == nil {
		a.conn, _ = mqttpool.Acquire(mqttEndpointFor(ctx, a.broker, a.username, a.password, a.clientID), mqttConnectRetryInterval)
	}
	conn := a.conn
	a.mu.Unlock()
	if err := conn.WaitUp(ctx, mqttConnectTimeout); err != nil {
		return nil, err
	}
	return conn.Client(), nil
}

// Process renders the topic/payload and publishes the message.
func (a *MqttAction) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	client, err := a.ensureClient(ctx)
	if err != nil {
		return nil, node.Transient(fmt.Errorf("mqtt connect failed: %w", err))
	}

	topic := renderTemplate(a.topic, msg, ctx)
	payload := renderTemplate(a.payload, msg, ctx)

	if err := waitMqttToken(ctx, client.Publish(topic, a.qos, a.retained, payload), mqttConnectTimeout); err != nil {
		return nil, node.Transient(fmt.Errorf("mqtt publish failed: %w", err))
	}

	out := msg.Clone()
	out.SourcePort = "output"
	out.SetMeta("mqttTopic", topic)
	return []*types.Message{out}, nil
}

// The runtime closes nodes implementing node.Closer when a flow stops.
var _ node.Closer = (*MqttAction)(nil)

// Close releases the node's connection, which closes once no node uses it.
// The runtime calls it when the flow stops; without it every redeploy leaked
// a connection to the broker.
func (a *MqttAction) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn != nil {
		a.conn.Release()
		a.conn = nil
	}
	return nil
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
	minQoS, maxQoS := float64(0), float64(2)
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"broker":   {Type: "string", Title: "Broker", Description: "Broker URL, e.g. tcp://host:1883", Destination: true},
			"topic":    {Type: "string", Title: "Topic", Description: "Topic to publish to (supports {{templates}})", Destination: true},
			"payload":  {Type: "string", Title: "Payload", Description: "Message payload (supports {{templates}})", Default: "{{payload}}"},
			"qos":      {Type: "number", Title: "QoS", Description: "0, 1 or 2", Default: 0, Minimum: &minQoS, Maximum: &maxQoS, AgentVisible: true},
			"retained": {Type: "boolean", Title: "Retained", Description: "Set the retained flag", Default: false, AgentVisible: true},
			"clientId": {Type: "string", Title: "Client ID", Description: "Own MQTT client id; leave empty to share a connection with the other MQTT nodes on this broker", Default: ""},
			"username": {Type: "string", Title: "Username", Description: "Optional ({{secret.NAME}} supported)"},
			"password": {Type: "string", Title: "Password", Description: "Optional; use {{secret.NAME}}", Sensitive: true},
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
` + "`{{secret.NAME}}`" + `. The connection is established lazily and shared with the other
MQTT nodes on the broker that use the same credentials and no client id of
their own.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| broker | string | - | Broker URL (tcp://host:1883) |
| topic | string | - | Topic (templated) |
| payload | string | {{payload}} | Payload (templated) |
| qos | number | 0 | 0, 1 or 2 |
| retained | boolean | false | Retained flag |
| clientId | string | (shared) | Own client id; empty shares a connection |
| username/password | string | - | Optional credentials |
`,
		Category: types.NodeCategoryOutput,
		Risk:     node.RiskExternal,
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
