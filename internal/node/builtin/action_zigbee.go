package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/Pegasus8/piworker/internal/mqttpool"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/Pegasus8/piworker/internal/zigbee"
)

// ZigbeeAction sends a command to a Zigbee device or group through
// Zigbee2MQTT (<base>/<device>/set).
type ZigbeeAction struct {
	*node.BaseNode

	connection string
	device     string // may hold {{templates}}
	command    string // on, off, toggle, set, raw
	property   string // set
	value      string // set; may hold {{templates}}
	payload    string // raw; a JSON object, may hold {{templates}}

	mu   sync.Mutex
	conn *mqttpool.Conn
	base string // the connection's base topic, with conn
}

// zigbeePropertyRe is what a settable property name looks like.
var zigbeePropertyRe = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

// zigbeeStateCommands are the commands that set the state.
var zigbeeStateCommands = map[string]string{"on": "ON", "off": "OFF", "toggle": "TOGGLE"}

// NewZigbeeAction creates a ZigbeeAction from configuration.
func NewZigbeeAction(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	conn, err := zigbeeConnectionName(base)
	if err != nil {
		return nil, err
	}
	device := strings.TrimSpace(base.GetConfigString("device", ""))
	if !strings.Contains(device, "{{") {
		if err := zigbee.ValidDeviceName(device); err != nil {
			return nil, err
		}
	}
	a := &ZigbeeAction{BaseNode: base, connection: conn, device: device,
		command:  base.GetConfigString("command", "toggle"),
		property: strings.TrimSpace(base.GetConfigString("property", "")),
		value:    base.GetConfigString("value", ""),
		payload:  strings.TrimSpace(base.GetConfigString("payload", "")),
	}
	switch a.command {
	case "on", "off", "toggle":
	case "set":
		if !zigbeePropertyRe.MatchString(a.property) {
			return nil, fmt.Errorf("set needs a property (letters, digits and _, e.g. brightness)")
		}
	case "raw":
		if a.payload == "" {
			return nil, fmt.Errorf("raw needs a payload: a JSON object such as {\"state\":\"ON\"}")
		}
		if !strings.HasPrefix(a.payload, "{") {
			return nil, fmt.Errorf("raw payload must be a JSON object")
		}
	default:
		return nil, fmt.Errorf("command must be on, off, toggle, set or raw")
	}
	return a, nil
}

// RequiredConnections: the node's broker connection (ADR-0026).
func (a *ZigbeeAction) RequiredConnections() map[string]string {
	return map[string]string{"connection": a.connection}
}

// command builds the command's JSON for msg.
func (a *ZigbeeAction) commandJSON(ctx context.Context, msg *types.Message) ([]byte, error) {
	if state, ok := zigbeeStateCommands[a.command]; ok {
		return json.Marshal(map[string]string{"state": state})
	}
	if a.command == "set" {
		return json.Marshal(map[string]interface{}{a.property: zigbeeTypedValue(renderTemplate(a.value, msg, ctx))})
	}
	raw := strings.TrimSpace(renderTemplate(a.payload, msg, ctx))
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("raw payload is not a JSON object once rendered")
	}
	return []byte(raw), nil // as written
}

// zigbeeTypedValue reads a rendered value as JSON when it is a number, a
// boolean, null, an object or an array, else as a string.
func zigbeeTypedValue(s string) interface{} {
	t := strings.TrimSpace(s)
	if t == "true" || t == "false" || t == "null" || strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
		var v interface{}
		if json.Unmarshal([]byte(t), &v) == nil {
			return v
		}
	}
	if f, err := strconv.ParseFloat(t, 64); err == nil && t != "" {
		return f
	}
	return s
}

// ensureConn takes the node's shared connection on first use and waits for
// it to be up (within ctx and mqttConnectTimeout).
func (a *ZigbeeAction) ensureConn(ctx context.Context) (*mqttpool.Conn, string, error) {
	a.mu.Lock()
	if a.conn == nil {
		c, err := zigbeeConnection(ctx, a.connection)
		if err != nil {
			a.mu.Unlock()
			return nil, "", err
		}
		a.conn, _ = mqttpool.Acquire(c.Endpoint(ctx), mqttConnectRetryInterval)
		a.base = c.BaseTopic
	}
	conn, base := a.conn, a.base
	a.mu.Unlock()
	if err := conn.WaitUp(ctx, mqttConnectTimeout); err != nil {
		return nil, "", node.Transient(fmt.Errorf("mqtt connect failed: %w", err))
	}
	return conn, base, nil
}

// Process renders the device and command and publishes the command.
func (a *ZigbeeAction) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	device := strings.TrimSpace(renderTemplate(a.device, msg, ctx))
	if err := zigbee.ValidDeviceName(device); err != nil {
		return nil, fmt.Errorf("device %q: %w", capRunes(device, 64), err)
	}
	body, err := a.commandJSON(ctx, msg)
	if err != nil {
		return nil, err
	}
	conn, base, err := a.ensureConn(ctx)
	if err != nil {
		return nil, err
	}
	if err := waitMqttToken(ctx, conn.Client().Publish(zigbee.SetTopic(base, device), 1, false, body), mqttConnectTimeout); err != nil {
		return nil, node.Transient(fmt.Errorf("mqtt publish failed: %w", err))
	}
	out := msg.Clone()
	out.SourcePort = "output"
	out.SetMeta("zigbeeDevice", device)
	out.SetMeta("zigbeeCommand", string(body))
	return []*types.Message{out}, nil
}

// capRunes cuts s to n runes, marking the cut.
func capRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

var _ node.Closer = (*ZigbeeAction)(nil)

// Close releases the node's connection.
func (a *ZigbeeAction) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn != nil {
		a.conn.Release()
		a.conn = nil
	}
	return nil
}

var (
	zigbeeActionInputs  = []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}}
	zigbeeActionOutputs = []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}}
)

// Ports returns the port definitions.
func (a *ZigbeeAction) Ports() (inputs []types.Port, outputs []types.Port) {
	return zigbeeActionInputs, zigbeeActionOutputs
}

// Validate checks the configuration (the constructor already did).
func (a *ZigbeeAction) Validate() error { return nil }

func zigbeeActionConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"connection": zigbeeSchemaConnection(),
			"device":     zigbeeSchemaDevice(true),
			"command": {Type: "string", Title: "Command", Description: "on, off, toggle (the state), set (one property) or raw (a JSON object)",
				Default: "toggle", Enum: []string{"on", "off", "toggle", "set", "raw"}, AgentVisible: true, Operation: true},
			"property": {Type: "string", Title: "Property", Description: "For set: the property to set (brightness, color_temp, power_on_behavior…)",
				AgentVisible: true, Operation: true, Options: node.OptionsZigbeeSettable},
			"value": {Type: "string", Title: "Value", Description: "For set: the value (supports {{templates}}; numbers and true/false are sent as such)"},
			"payload": {Type: "string", Title: "Payload", Description: "For raw: a JSON object sent as is (supports {{templates}})",
				Operation: true},
		},
		Required: []string{"connection", "device"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (a *ZigbeeAction) GetConfigSchema() node.ConfigSchema { return zigbeeActionConfigSchema() }

// ZigbeeActionInfo returns the node type info for registration.
func ZigbeeActionInfo() node.NodeTypeInfo {
	schema := zigbeeActionConfigSchema()
	return node.NodeTypeInfo{
		Type:        "action-zigbee",
		Name:        "Zigbee Set",
		Description: "Turn a Zigbee device on or off or set one of its properties",
		Documentation: `## Zigbee Set

Sends a command to a device or group through Zigbee2MQTT
(` + "`<base topic>/<device>/set`" + `), on a broker connection defined under
**Connections**.

| Command | Sends |
|---------|-------|
| on / off / toggle | ` + "`{\"state\":\"ON\"}`" + ` … |
| set | ` + "`{\"<property>\": <value>}`" + ` (numbers and true/false typed) |
| raw | the payload, a JSON object |

The device may come from the message (` + "`{{payload.device}}`" + `). The bridge
(` + "`bridge/…`" + `: permit join, renames, removal) is never a target, and names
with ` + "`+`" + `, ` + "`#`" + ` or a ` + "`/set`" + ` suffix are refused.

## Output

The input message, with ` + "`meta.zigbeeDevice`" + ` and ` + "`meta.zigbeeCommand`" + `.

## Examples

- S60ZBTPF plug: command ` + "`on`" + ` / ` + "`off`" + `.
- Set ` + "`power_on_behavior`" + ` to ` + "`previous`" + ` so the plug keeps its state after a power cut.
`,
		Category: types.NodeCategoryOutput,
		Risk:     node.RiskExternal,
		Inputs:   zigbeeActionInputs,
		Outputs:  zigbeeActionOutputs,
		Config:   &schema,
		Icon:     "power",
	}
}

func init() {
	info := ZigbeeActionInfo()
	if err := node.Register(info, NewZigbeeAction); err != nil {
		panic("failed to register zigbee action: " + err.Error())
	}
}
