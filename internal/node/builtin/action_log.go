package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// LogAction logs incoming messages.
type LogAction struct {
	*node.BaseNode

	level   string
	message string
	logger  zerolog.Logger
}

// NewLogAction creates a new LogAction with the given configuration.
func NewLogAction(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	action := &LogAction{
		BaseNode: base,
		level:    base.GetConfigString("level", "info"),
		message:  base.GetConfigString("message", ""),
		logger:   log.Logger,
	}

	return action, nil
}

// Process logs the incoming message and passes it through. In a sandbox
// (a dry-run or node test, node.InSandbox) it only passes it through: what a
// sandboxed flow carries never reaches the process log.
func (a *LogAction) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	if !node.InSandbox(ctx) {
		a.log(msg)
	}

	// Pass a copy of the message through, tagged with this node's own output
	// port. Keeping the upstream SourcePort (e.g. a Switch's "true") would make
	// the router look up connections for a port this node doesn't have and
	// drop the message.
	out := msg.Clone()
	out.SourcePort = "output"
	return []*types.Message{out}, nil
}

// log writes msg to the process log at the configured level.
func (a *LogAction) log(msg *types.Message) {
	// Build the log message
	logMessage := a.message
	if logMessage == "" {
		logMessage = "Message received"
	}

	// Serialize payload for logging
	var payloadStr string
	switch p := msg.Payload.(type) {
	case string:
		payloadStr = p
	case []byte:
		payloadStr = string(p)
	default:
		if data, err := json.Marshal(p); err == nil {
			payloadStr = string(data)
		} else {
			payloadStr = fmt.Sprintf("%v", p)
		}
	}

	// Log at the configured level
	var event *zerolog.Event
	switch a.level {
	case "debug":
		event = a.logger.Debug()
	case "warn":
		event = a.logger.Warn()
	case "error":
		event = a.logger.Error()
	default:
		event = a.logger.Info()
	}

	event.
		Str("msgID", msg.ID).
		Str("topic", msg.Topic).
		Str("sourceNode", msg.SourceNode).
		Str("payload", payloadStr).
		Msg(escapeLogText(logMessage))
}

// escapeLogText escapes the control characters of a log message (newlines,
// carriage returns, terminal escapes; Unicode line separators), so a
// configured message cannot forge further lines on the console log. Fields
// need no escaping: the console writer quotes them and JSON escapes them.
func escapeLogText(s string) string {
	if !strings.ContainsFunc(s, isLogControl) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if isLogControl(r) {
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isLogControl(r rune) bool { return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' }

// Ports returns the port definitions for this node.
func (a *LogAction) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{
		{
			ID:       "input",
			Name:     "Input",
			DataType: types.DataTypeAny,
			Required: true,
		},
	}, []types.Port{
		{
			ID:       "output",
			Name:     "Output",
			DataType: types.DataTypeAny,
			Multiple: true,
		},
	}
}

// Validate checks if the configuration is valid.
func (a *LogAction) Validate() error {
	validLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLevels[a.level] {
		return fmt.Errorf("invalid log level: %s", a.level)
	}
	return nil
}

// GetConfigSchema returns the configuration schema for UI.
func (a *LogAction) GetConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"level": {
				Type:         "string",
				Title:        "Log Level",
				Description:  "The severity level for the log message",
				Default:      "info",
				Enum:         []string{"debug", "info", "warn", "error"},
				AgentVisible: true,
			},
			"message": {
				Type:        "string",
				Title:       "Message",
				Description: "Custom log message (optional)",
				Default:     "",
			},
		},
	}
}

// LogActionInfo returns the node type info for registration.
func LogActionInfo() node.NodeTypeInfo {
	return node.NodeTypeInfo{
		Type:        "action-log",
		Name:        "Log",
		Description: "Logs messages to the console/file",
		Documentation: `## Log

Writes incoming messages to the application log. Useful for debugging, monitoring, and audit trails.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| level | enum | info | Log level: debug, info, warn, error |
| message | string | - | Custom message prefix (optional) |

## Log Levels

| Level | Use Case |
|-------|----------|
| debug | Detailed debugging information |
| info | Normal operational messages |
| warn | Warning conditions |
| error | Error conditions |

## Logged Data

Each message logs:

- **msgID** - Unique message identifier
- **topic** - Message topic
- **sourceNode** - Node that produced the message
- **payload** - Message payload (serialized as JSON)

## Behavior

- **Pass-through**: A copy of the message (same payload, topic and metadata) is emitted on the output port
- **Non-blocking**: Logging doesn't affect flow performance
- Output can be connected to continue the flow

## Example Log Output

` + "```" + `
INF Message received msgID=abc123 topic=sensor sourceNode=transform-1 payload={"temp":23.5}
` + "```" + `

## Use Cases

- Debugging flow behavior
- Audit logging
- Monitoring data flow
- Development diagnostics
`,
		Category: types.NodeCategoryOutput,
		Risk:     node.RiskSafe,
		Inputs: []types.Port{
			{
				ID:       "input",
				Name:     "Input",
				DataType: types.DataTypeAny,
				Required: true,
			},
		},
		Outputs: []types.Port{
			{
				ID:       "output",
				Name:     "Output",
				DataType: types.DataTypeAny,
				Multiple: true,
			},
		},
		Icon: "text",
	}
}

func init() {
	info := LogActionInfo()
	if err := node.Register(info, NewLogAction); err != nil {
		panic("failed to register log action: " + err.Error())
	}
}
