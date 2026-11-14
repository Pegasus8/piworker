package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/mqttpool"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/Pegasus8/piworker/internal/zigbee"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// ZigbeeGet asks a device for a fresh reading (<base>/<device>/get) and
// passes on the state it reports next.
type ZigbeeGet struct {
	*node.BaseNode

	connection string
	device     string
	property   string
	timeout    time.Duration

	mu      sync.Mutex
	sub     *mqttpool.Sub
	base    string
	reports chan map[string]interface{}
}

const maxZigbeeGetTimeout = 30

// NewZigbeeGet creates a ZigbeeGet from configuration.
func NewZigbeeGet(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	conn, err := zigbeeConnectionName(base)
	if err != nil {
		return nil, err
	}
	device, err := zigbeeLiteralDevice(base)
	if err != nil {
		return nil, err
	}
	property := strings.TrimSpace(base.GetConfigString("property", "state"))
	if property == "" {
		property = "state"
	}
	if !zigbeePropertyRe.MatchString(property) {
		return nil, fmt.Errorf("property must be letters, digits and _ (e.g. state, power)")
	}
	timeout := base.GetConfigFloat("timeout", 5)
	if timeout < 1 || timeout > maxZigbeeGetTimeout {
		return nil, fmt.Errorf("timeout must be between 1 and %d seconds", maxZigbeeGetTimeout)
	}
	return &ZigbeeGet{BaseNode: base, connection: conn, device: device, property: property,
		timeout: time.Duration(timeout * float64(time.Second))}, nil
}

// RequiredConnections: the node's broker connection (ADR-0026).
func (g *ZigbeeGet) RequiredConnections() map[string]string {
	return map[string]string{"connection": g.connection}
}

// ensureSub subscribes to the device's state on first use.
func (g *ZigbeeGet) ensureSub(ctx context.Context) (*mqttpool.Sub, string, chan map[string]interface{}, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sub == nil {
		c, err := brokers.Lookup(ctx, g.connection)
		if err != nil {
			return nil, "", nil, err
		}
		reports := make(chan map[string]interface{}, 1)
		g.sub, _ = mqttpool.Subscribe(c.Endpoint(ctx), zigbee.DeviceTopic(c.BaseTopic, g.device), 0, func(m mqtt.Message) {
			var state map[string]interface{}
			if json.Unmarshal(m.Payload(), &state) != nil || state == nil {
				return
			}
			select { // keep only the latest report
			case <-reports:
			default:
			}
			select {
			case reports <- state:
			default:
			}
		}, mqttConnectRetryInterval)
		g.base, g.reports = c.BaseTopic, reports
	}
	return g.sub, g.base, g.reports, nil
}

// Process publishes the request and waits for the next report.
func (g *ZigbeeGet) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	sub, base, reports, err := g.ensureSub(ctx)
	if err != nil {
		return nil, err
	}
	conn := sub.Conn()
	if err := conn.WaitUp(ctx, mqttConnectTimeout); err != nil {
		return nil, node.Transient(fmt.Errorf("mqtt connect failed: %w", err))
	}
	conn.Settle() // the state subscription is in place before asking
	select {      // an earlier report is not the answer
	case <-reports:
	default:
	}
	body, _ := json.Marshal(map[string]string{g.property: ""})
	if err := waitMqttToken(ctx, conn.Client().Publish(zigbee.GetTopic(base, g.device), 1, false, body), mqttConnectTimeout); err != nil {
		return nil, node.Transient(fmt.Errorf("mqtt publish failed: %w", err))
	}
	timer := time.NewTimer(g.timeout)
	defer timer.Stop()
	select {
	case state := <-reports:
		out := msg.Clone()
		out.Payload = map[string]interface{}{"device": g.device, "property": g.property, "value": state[g.property], "state": state}
		out.PayloadType = types.DataTypeObject
		out.SourcePort = "output"
		return []*types.Message{out}, nil
	case <-timer.C:
		return nil, fmt.Errorf("no answer from %q within %s (battery devices sleep and do not answer requests)", g.device, g.timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

var _ node.Closer = (*ZigbeeGet)(nil)

// Close ends the subscription.
func (g *ZigbeeGet) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sub != nil {
		g.sub.Release()
		g.sub = nil
	}
	return nil
}

var (
	zigbeeGetInputs  = []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}}
	zigbeeGetOutputs = []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeObject, Multiple: true}}
)

// Ports returns the port definitions.
func (g *ZigbeeGet) Ports() (inputs []types.Port, outputs []types.Port) {
	return zigbeeGetInputs, zigbeeGetOutputs
}

// Validate checks the configuration (the constructor already did).
func (g *ZigbeeGet) Validate() error { return nil }

func zigbeeGetConfigSchema() node.ConfigSchema {
	one, max := float64(1), float64(maxZigbeeGetTimeout)
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"connection": zigbeeSchemaConnection(),
			"device":     zigbeeSchemaDevice(false),
			"property": {Type: "string", Title: "Property", Description: "The property to read (state, power, energy…)",
				Default: "state", AgentVisible: true, Options: node.OptionsZigbeeProperties},
			"timeout": {Type: "number", Title: "Timeout (s)", Description: "How long to wait for the answer, in seconds",
				Default: 5, Minimum: &one, Maximum: &max, Unit: "seconds", AgentVisible: true},
		},
		Required: []string{"connection", "device"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (g *ZigbeeGet) GetConfigSchema() node.ConfigSchema { return zigbeeGetConfigSchema() }

// ZigbeeGetInfo returns the node type info for registration.
func ZigbeeGetInfo() node.NodeTypeInfo {
	schema := zigbeeGetConfigSchema()
	return node.NodeTypeInfo{
		Type:        "process-zigbee-get",
		Name:        "Zigbee Get",
		Description: "Read a Zigbee device's current value",
		Documentation: `## Zigbee Get

Asks a device for a fresh reading through Zigbee2MQTT
(` + "`<base topic>/<device>/get`" + `) and passes on the state it reports next, as
` + "`{ device, property, value, state }`" + `.

Mains-powered devices (plugs such as the S60ZBTPF, lights) answer. Battery
devices (sensors such as the SNZB-02P) sleep and do not: the message fails
after the timeout. Use a Zigbee Device trigger for them instead.
`,
		Category: types.NodeCategoryProcessing,
		Risk:     node.RiskExternal,
		Inputs:   zigbeeGetInputs,
		Outputs:  zigbeeGetOutputs,
		Config:   &schema,
		Icon:     "radio",
	}
}

func init() {
	info := ZigbeeGetInfo()
	if err := node.Register(info, NewZigbeeGet); err != nil {
		panic("failed to register zigbee get: " + err.Error())
	}
}
