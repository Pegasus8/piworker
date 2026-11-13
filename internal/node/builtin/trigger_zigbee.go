package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/metrics"
	"github.com/Pegasus8/piworker/internal/mqttpool"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/Pegasus8/piworker/internal/zigbee"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog/log"
)

// ZigbeeTrigger fires when a Zigbee device reports through Zigbee2MQTT: on
// its "output" port when its state (or one property) changes, on its
// "availability" port when it goes offline or comes back.
type ZigbeeTrigger struct {
	*node.BaseNode

	connection string
	device     string
	property   string
	every      bool
	minChange  float64

	initialConnectWait time.Duration

	mu        sync.Mutex
	running   bool
	subs      []*mqttpool.Sub
	seen      bool                   // a value (or state) was fired
	last      interface{}            // the property's last fired value
	lastState map[string]interface{} // the last fired state, without volatile keys
	online    *bool                  // the last availability seen
}

// zigbeeVolatileKeys change with every report without saying anything about
// the device's state.
var zigbeeVolatileKeys = map[string]bool{"linkquality": true, "last_seen": true, "elapsed": true, "update": true, "update_available": true}

// NewZigbeeTrigger creates a ZigbeeTrigger from configuration.
func NewZigbeeTrigger(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	conn, err := zigbeeConnectionName(base)
	if err != nil {
		return nil, err
	}
	device, err := zigbeeLiteralDevice(base)
	if err != nil {
		return nil, err
	}
	mode := base.GetConfigString("mode", "change")
	if mode != "change" && mode != "every" {
		return nil, fmt.Errorf("mode must be change or every")
	}
	minChange := base.GetConfigFloat("minChange", 0)
	if minChange < 0 || math.IsNaN(minChange) || math.IsInf(minChange, 0) {
		return nil, fmt.Errorf("minChange must be 0 or more")
	}
	return &ZigbeeTrigger{
		BaseNode: base, connection: conn, device: device,
		property: strings.TrimSpace(base.GetConfigString("property", "")),
		every:    mode == "every", minChange: minChange,
		initialConnectWait: mqttInitialConnectWait,
	}, nil
}

// RequiredConnections: the node's broker connection (ADR-0026).
func (t *ZigbeeTrigger) RequiredConnections() map[string]string {
	return map[string]string{"connection": t.connection}
}

// Process is unused for trigger nodes.
func (t *ZigbeeTrigger) Process(context.Context, *types.Message) ([]*types.Message, error) {
	return nil, nil
}

var zigbeeTriggerOutputs = []types.Port{
	{ID: "output", Name: "State", DataType: types.DataTypeObject, Multiple: true},
	{ID: "availability", Name: "Availability", DataType: types.DataTypeObject, Multiple: true},
}

// Ports returns the port definitions.
func (t *ZigbeeTrigger) Ports() (inputs []types.Port, outputs []types.Port) {
	return nil, zigbeeTriggerOutputs
}

// Validate checks the configuration (the constructor already did).
func (t *ZigbeeTrigger) Validate() error { return nil }

// Start subscribes to the device's state and availability on the shared
// connection. An unreachable broker does not fail it (ADR-0006).
func (t *ZigbeeTrigger) Start(ctx context.Context, out chan<- *types.Message) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running {
		return fmt.Errorf("trigger already running")
	}
	conn, err := brokers.Lookup(ctx, t.connection)
	if err != nil {
		return err
	}
	t.seen, t.last, t.lastState, t.online = false, nil, nil, nil

	send := func(msg *types.Message) {
		select {
		case <-ctx.Done():
		case out <- msg:
		default:
			metrics.IncrementMessagesDropped()
			log.Warn().Str("device", t.device).Msg("zigbee message dropped: output channel full")
		}
	}
	ep := conn.Endpoint(ctx)
	stateSub, fresh := mqttpool.Subscribe(ep, zigbee.DeviceTopic(conn.BaseTopic, t.device), 0, func(m mqtt.Message) {
		if msg := t.onState(m.Payload()); msg != nil {
			send(msg)
		}
	}, mqttConnectRetryInterval)
	availSub, _ := mqttpool.Subscribe(ep, zigbee.AvailabilityTopic(conn.BaseTopic, t.device), 0, func(m mqtt.Message) {
		if msg := t.onAvailability(m.Payload()); msg != nil {
			send(msg)
		}
	}, mqttConnectRetryInterval)
	t.subs = []*mqttpool.Sub{stateSub, availSub}
	if fresh {
		if err := stateSub.Conn().WaitUp(ctx, t.initialConnectWait); err != nil {
			log.Warn().Str("connection", t.connection).Msg("mqtt broker not reachable yet; retrying in background")
		}
	}
	t.running = true
	return nil
}

// onState returns the message a state report fires, or nil.
func (t *ZigbeeTrigger) onState(payload []byte) *types.Message {
	var state map[string]interface{}
	if json.Unmarshal(payload, &state) != nil || state == nil {
		return nil // not a state report
	}
	out := map[string]interface{}{"device": t.device, "state": state}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.property != "" {
		v, ok := state[t.property]
		if !ok {
			return nil
		}
		out["property"], out["value"] = t.property, v
		switch {
		case t.property == "action":
			// Events: each one fires; Zigbee2MQTT may clear it with "".
			if s, isStr := v.(string); v == nil || (isStr && s == "") {
				return nil
			}
		case t.every:
		case t.seen && !zigbeeChanged(t.last, v, t.minChange):
			return nil
		}
		out["previous"] = nil
		if t.seen {
			out["previous"] = t.last
		}
		t.seen, t.last = true, v
	} else if !t.every {
		stable := map[string]interface{}{}
		for k, v := range state {
			if !zigbeeVolatileKeys[k] {
				stable[k] = v
			}
		}
		if t.seen && reflect.DeepEqual(stable, t.lastState) {
			return nil
		}
		t.seen, t.lastState = true, stable
	}
	msg := types.NewMessage(out, types.DataTypeObject)
	msg.SourcePort = "output"
	msg.Topic = "zigbee"
	return msg
}

// zigbeeChanged reports whether v differs from prev: for numbers by at least
// minChange (when above 0), otherwise by value.
func zigbeeChanged(prev, v interface{}, minChange float64) bool {
	a, aNum := prev.(float64)
	b, bNum := v.(float64)
	if aNum && bNum && minChange > 0 {
		return math.Abs(b-a) >= minChange
	}
	return !reflect.DeepEqual(prev, v)
}

// onAvailability returns the message an availability report fires: the
// first one only when offline, then each change.
func (t *ZigbeeTrigger) onAvailability(payload []byte) *types.Message {
	online, ok := zigbee.ParseAvailability(payload)
	if !ok {
		return nil
	}
	t.mu.Lock()
	first, changed := t.online == nil, t.online != nil && *t.online != online
	t.online = &online
	t.mu.Unlock()
	if !(first && !online) && !changed {
		return nil
	}
	state := "offline"
	if online {
		state = "online"
	}
	msg := types.NewMessage(map[string]interface{}{"device": t.device, "online": online, "state": state}, types.DataTypeObject)
	msg.SourcePort = "availability"
	msg.Topic = "zigbee"
	return msg
}

// Stop ends the subscriptions; the shared connection closes once unused.
func (t *ZigbeeTrigger) Stop() error {
	t.mu.Lock()
	subs := t.subs
	t.subs, t.running = nil, false
	t.mu.Unlock()
	for _, s := range subs {
		s.Release()
	}
	return nil
}

func zigbeeTriggerConfigSchema() node.ConfigSchema {
	zero := float64(0)
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"connection": zigbeeSchemaConnection(),
			"device":     zigbeeSchemaDevice(false),
			"property": {Type: "string", Title: "Property",
				Description:  "Only this property (temperature, humidity, state, power, action…); empty for the whole state",
				AgentVisible: true, Options: node.OptionsZigbeeProperties},
			"mode": {Type: "string", Title: "Mode", Description: "change: only when the value (or state) changes; every: on every report",
				Default: "change", Enum: []string{"change", "every"}, AgentVisible: true, Rate: true},
			"minChange": {Type: "number", Title: "Minimum change",
				Description: "Numbers only: fire when the value moved at least this much (in the property's own unit) since it last fired; 0 for any change",
				Default:     0, Minimum: &zero, AgentVisible: true, Rate: true},
		},
		Required: []string{"connection", "device"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (t *ZigbeeTrigger) GetConfigSchema() node.ConfigSchema { return zigbeeTriggerConfigSchema() }

// ZigbeeTriggerInfo returns the node type info for registration.
func ZigbeeTriggerInfo() node.NodeTypeInfo {
	schema := zigbeeTriggerConfigSchema()
	return node.NodeTypeInfo{
		Type:        "trigger-zigbee",
		Name:        "Zigbee Device",
		Description: "Trigger when a Zigbee device reports a change or goes offline",
		Documentation: `## Zigbee Device

Listens to one device through Zigbee2MQTT, on a broker connection defined
under **Connections**.

- **State** output: in *change* mode, when the device's state changes (link
  quality and timestamps ignored) or, with a **property**, when that property
  changes by at least **minimum change** since it last fired. The first report
  after the flow starts always fires. *every* mode fires on every report.
  Button actions (property ` + "`action`" + `) fire on every press.
- **Availability** output: when the device goes offline or comes back
  (Zigbee2MQTT's availability must be on). At start it fires only if the
  device is already offline.

## Output

State: ` + "`{ device, property, value, previous, state }`" + ` (property, value and previous
with a property). Availability: ` + "`{ device, online, state }`" + `.

## Examples

- SNZB-02P: property ` + "`humidity`" + `, minimum change 2 → fires on every 2 % step.
- S60ZBTPF: property ` + "`power`" + `, minimum change 5 → appliance started or finished.
`,
		Category: types.NodeCategoryInput,
		Risk:     node.RiskSafe,
		Outputs:  zigbeeTriggerOutputs,
		Config:   &schema,
		Icon:     "radio",
	}
}

func init() {
	info := ZigbeeTriggerInfo()
	if err := node.Register(info, NewZigbeeTrigger); err != nil {
		panic("failed to register zigbee trigger: " + err.Error())
	}
}
