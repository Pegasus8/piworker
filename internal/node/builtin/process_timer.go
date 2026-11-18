package builtin

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/metrics"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog/log"
)

// TimerNode is a retriggerable timer: each message starts it (or restarts
// it), and when the duration passes without a new message it emits the last
// message. "Turn the light off three minutes after the last motion."
//
// It emits after its input's run has ended, so it is also a trigger
// (node.TriggerNode): the runtime gives it an output channel at Start and
// each emission starts a new run. Process itself returns nothing.
type TimerNode struct {
	*node.BaseNode

	amount  float64
	unit    time.Duration
	restart bool

	mu       sync.Mutex
	out      chan<- *types.Message
	ctx      context.Context
	timer    *time.Timer
	pending  *types.Message // the message the timer will emit
	deadline time.Time      // when, for a message that came before Start
	gen      uint64         // invalidates a timer that was replaced
}

var timerUnits = map[string]time.Duration{"seconds": time.Second, "minutes": time.Minute, "hours": time.Hour}

const maxTimerDuration = 24 * time.Hour

// NewTimerNode creates a TimerNode from configuration.
func NewTimerNode(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	unitName := base.GetConfigString("durationUnit", "minutes")
	unit, ok := timerUnits[unitName]
	if !ok {
		return nil, fmt.Errorf("durationUnit must be seconds, minutes or hours")
	}
	amount := base.GetConfigFloat("duration", 5)
	if amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) || time.Duration(amount*float64(unit)) > maxTimerDuration {
		return nil, fmt.Errorf("duration must be above 0 and at most 24 hours")
	}
	return &TimerNode{BaseNode: base, amount: amount, unit: unit, restart: base.GetConfigBool("restart", true)}, nil
}

func (n *TimerNode) duration() time.Duration { return time.Duration(n.amount * float64(n.unit)) }

// isTimerStop reports whether a payload asks to cancel the timer: the
// string "stop" or an object with "stop": true.
func isTimerStop(payload interface{}) bool {
	switch p := payload.(type) {
	case string:
		return strings.EqualFold(strings.TrimSpace(p), "stop")
	case map[string]interface{}:
		b, _ := p["stop"].(bool)
		return b
	}
	return false
}

// Process starts, restarts or stops the timer. It returns no message: the
// timer emits later, as a new run.
func (n *TimerNode) Process(_ context.Context, msg *types.Message) ([]*types.Message, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if isTimerStop(msg.Payload) {
		n.cancelLocked()
		return nil, nil
	}
	if n.pending != nil && !n.restart {
		return nil, nil // running, not extended
	}
	n.cancelLocked()
	n.pending = msg.Clone()
	n.deadline = time.Now().Add(n.duration())
	if n.out != nil {
		n.scheduleLocked(n.duration())
	}
	return nil, nil
}

func (n *TimerNode) cancelLocked() {
	if n.timer != nil {
		n.timer.Stop()
		n.timer = nil
	}
	n.pending = nil
	n.gen++
}

func (n *TimerNode) scheduleLocked(d time.Duration) {
	gen := n.gen
	n.timer = time.AfterFunc(d, func() { n.fire(gen) })
}

// fire emits the pending message unless the timer was replaced since.
func (n *TimerNode) fire(gen uint64) {
	n.mu.Lock()
	if gen != n.gen || n.pending == nil || n.out == nil {
		n.mu.Unlock()
		return
	}
	msg, out, ctx := n.pending, n.out, n.ctx
	n.pending, n.timer = nil, nil
	n.gen++
	n.mu.Unlock()

	msg.SourcePort = "output"
	msg.SetMeta("timerDuration", n.duration().String())
	select {
	case <-ctx.Done():
	case out <- msg:
	default:
		metrics.IncrementMessagesDropped()
		log.Warn().Msg("timer message dropped: output channel full")
	}
}

// Start receives the channel the timer emits into; a message that came
// before is scheduled for its remaining time.
func (n *TimerNode) Start(ctx context.Context, out chan<- *types.Message) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.out != nil {
		return fmt.Errorf("timer already running")
	}
	n.out, n.ctx = out, ctx
	if n.pending != nil {
		n.scheduleLocked(time.Until(n.deadline))
	}
	return nil
}

// Stop cancels the timer.
func (n *TimerNode) Stop() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.cancelLocked()
	n.out = nil
	return nil
}

var _ node.Closer = (*TimerNode)(nil)

// Close cancels a timer set outside a running flow (a node test).
func (n *TimerNode) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.cancelLocked()
	return nil
}

var (
	timerInputs  = []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}}
	timerOutputs = []types.Port{{ID: "output", Name: "Elapsed", DataType: types.DataTypeAny, Multiple: true}}
)

// Ports returns the port definitions.
func (n *TimerNode) Ports() (inputs []types.Port, outputs []types.Port) {
	return timerInputs, timerOutputs
}

// Validate checks the configuration (the constructor already did).
func (n *TimerNode) Validate() error { return nil }

func timerConfigSchema() node.ConfigSchema {
	min := 0.001
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"duration": {Type: "number", Title: "Duration", Description: "How long without a new message before it fires (in the unit below; at most 24 hours)",
				Default: 5, Minimum: &min, AgentVisible: true, Rate: true},
			"durationUnit": {Type: "string", Title: "Unit", Description: "Unit of the duration",
				Default: "minutes", Enum: []string{"seconds", "minutes", "hours"}, AgentVisible: true, Rate: true},
			"restart": {Type: "boolean", Title: "Restart on each message", Description: "Each message starts the countdown again; off: a running timer is not extended",
				Default: true, AgentVisible: true},
		},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (n *TimerNode) GetConfigSchema() node.ConfigSchema { return timerConfigSchema() }

// TimerNodeInfo returns the node type info for registration.
func TimerNodeInfo() node.NodeTypeInfo {
	schema := timerConfigSchema()
	return node.NodeTypeInfo{
		Type:        "process-timer",
		Name:        "Timer",
		Description: "Fire once no message came for a while (retriggerable)",
		Documentation: `## Timer

Each message starts the countdown, or starts it again; when the duration
passes without a new message, the timer emits the last message it received.
A message whose payload is ` + "`\"stop\"`" + ` or ` + "`{\"stop\": true}`" + ` cancels it.
With **restart** off, a running timer is not extended (at most one emission
per duration).

The emission is a new run: the run that started the timer ends at it. The
timer is cancelled when the flow stops. In a dry-run the timer does not run.

## Example

Motion sensor (` + "`occupancy`" + ` true) → Zigbee Set *on* and Timer 3 minutes →
Zigbee Set *off*: the light turns off three minutes after the last motion.
`,
		Category: types.NodeCategoryProcessing,
		Risk:     node.RiskSafe,
		Inputs:   timerInputs,
		Outputs:  timerOutputs,
		Config:   &schema,
		Icon:     "timer",
	}
}

func init() {
	info := TimerNodeInfo()
	if err := node.Register(info, NewTimerNode); err != nil {
		panic("failed to register timer: " + err.Error())
	}
}
