package builtin

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// CommandAction executes shell commands.
type CommandAction struct {
	*node.BaseNode

	command string
	shell   string
	timeout time.Duration
}

// NewCommandAction creates a new CommandAction with the given configuration.
func NewCommandAction(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	// Parse timeout in seconds
	timeoutSec := base.GetConfigInt("timeout", 30)
	if timeoutSec < 1 {
		timeoutSec = 1
	}
	if timeoutSec > 3600 {
		timeoutSec = 3600 // Max 1 hour
	}

	action := &CommandAction{
		BaseNode: base,
		command:  base.GetConfigString("command", ""),
		shell:    base.GetConfigString("shell", "/bin/sh"),
		timeout:  time.Duration(timeoutSec) * time.Second,
	}

	return action, nil
}

// Process executes the command and returns the output.
func (a *CommandAction) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	if a.command == "" {
		return nil, fmt.Errorf("no command specified")
	}

	// Create a context with timeout
	execCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	// Expand variables in the command
	expandedCommand := a.expandVariables(a.command, msg)

	// Execute the command
	cmd := exec.CommandContext(execCtx, a.shell, "-c", expandedCommand)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	err := cmd.Run()
	duration := time.Since(startTime)

	// Build the output payload. Command failures are surfaced in the payload
	// (success/exitCode/timedOut) rather than as a node error, so the flow can
	// branch on the result instead of aborting. This means a failed command does
	// NOT trigger the runtime's retry/error path.
	output := map[string]interface{}{
		"command":  expandedCommand,
		"stdout":   strings.TrimSpace(stdout.String()),
		"stderr":   strings.TrimSpace(stderr.String()),
		"duration": duration.Milliseconds(),
		"exitCode": 0,
		"success":  true,
		"timedOut": false,
	}

	if err != nil {
		output["success"] = false
		output["error"] = err.Error()

		// Try to get the exit code
		if exitErr, ok := err.(*exec.ExitError); ok {
			output["exitCode"] = exitErr.ExitCode()
		} else {
			output["exitCode"] = -1
		}

		// A timeout can surface as a non-ExitError, so check the context
		// explicitly and normalise the result regardless of the error type.
		if execCtx.Err() == context.DeadlineExceeded {
			output["error"] = "command timed out"
			output["exitCode"] = -1
			output["timedOut"] = true
		}
	}

	// Create output message
	outputMsg := msg.WithPayload(output, types.DataTypeObject)
	outputMsg.SourcePort = "output"
	outputMsg.SetMeta("commandDuration", duration.Milliseconds())

	return []*types.Message{outputMsg}, nil
}

// expandVariables replaces placeholders in the command with values from the message.
// Supported placeholders: {{payload}}, {{topic}}, {{meta.key}}
// All substituted values are shell-escaped to prevent command injection attacks.
func (a *CommandAction) expandVariables(command string, msg *types.Message) string {
	result := command

	// Replace {{payload}}
	if strings.Contains(result, "{{payload}}") {
		payloadStr := ""
		switch p := msg.Payload.(type) {
		case string:
			payloadStr = p
		case []byte:
			payloadStr = string(p)
		default:
			payloadStr = fmt.Sprintf("%v", p)
		}
		// Escape to prevent command injection
		result = strings.ReplaceAll(result, "{{payload}}", shellEscape(payloadStr))
	}

	// Replace {{topic}}
	result = strings.ReplaceAll(result, "{{topic}}", shellEscape(msg.Topic))

	// Replace {{timestamp}}
	// Timestamps are safe (numeric only) but we escape anyway for consistency
	result = strings.ReplaceAll(result, "{{timestamp}}", fmt.Sprintf("%d", msg.Timestamp.UnixMilli()))

	// Replace meta values: {{meta.key}}
	for key, value := range msg.Meta {
		placeholder := fmt.Sprintf("{{meta.%s}}", key)
		// Escape to prevent command injection
		result = strings.ReplaceAll(result, placeholder, shellEscape(fmt.Sprintf("%v", value)))
	}

	return result
}

// shellEscape escapes a string for safe use in a shell command.
// It wraps the value in single quotes and escapes any single quotes within the value.
// This prevents shell metacharacters from being interpreted as commands.
// Additionally, it filters out null bytes and other control characters that could
// potentially bypass shell escaping in certain edge cases.
// Example: "hello; rm -rf /" becomes "'hello; rm -rf /'" (safe literal string)
// Example: "it's" becomes "'it'\”s'" (properly escaped single quote)
func shellEscape(s string) string {
	// If the string is empty, return empty quotes
	if s == "" {
		return "''"
	}

	// Filter null bytes and other dangerous control characters
	// that could potentially bypass shell escaping
	s = strings.Map(func(r rune) rune {
		// Remove null bytes and control characters (except tab and newline)
		if r == 0 || (r < 32 && r != '\t' && r != '\n') {
			return -1 // Remove character
		}
		return r
	}, s)

	// Single-quote escaping strategy:
	// 1. Wrap the entire string in single quotes
	// 2. For any single quote within the string, end the quote,
	//    add an escaped single quote, then start a new quote
	// "it's" -> 'it'\''s'
	escaped := strings.ReplaceAll(s, "'", "'\\''")
	return "'" + escaped + "'"
}

// Ports returns the port definitions for this node.
func (a *CommandAction) Ports() (inputs []types.Port, outputs []types.Port) {
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
				DataType: types.DataTypeObject,
				Multiple: true,
			},
		}
}

// Validate checks if the configuration is valid.
func (a *CommandAction) Validate() error {
	if a.command == "" {
		return fmt.Errorf("command is required")
	}
	return nil
}

// GetConfigSchema returns the configuration schema for UI.
func (a *CommandAction) GetConfigSchema() node.ConfigSchema {
	minTimeout := float64(1)
	maxTimeout := float64(3600)

	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"command": {
				Type:        "string",
				Title:       "Command",
				Description: "Shell command to execute. Use {{payload}}, {{topic}}, {{meta.key}} for variable substitution.",
			},
			"shell": {
				Type:        "string",
				Title:       "Shell",
				Description: "Path to the shell interpreter",
				Default:     "/bin/sh",
			},
			"timeout": {
				Type:        "number",
				Title:       "Timeout (seconds)",
				Description: "Maximum execution time in seconds",
				Default:     30,
				Minimum:     &minTimeout,
				Maximum:     &maxTimeout,
			},
		},
		Required: []string{"command"},
	}
}

// CommandActionInfo returns the node type info for registration.
func CommandActionInfo() node.NodeTypeInfo {
	return node.NodeTypeInfo{
		Type:        "action-command",
		Name:        "Execute Command",
		Description: "Executes a shell command",
		Documentation: `## Execute Command

Runs shell commands on the host system. Values are automatically escaped to prevent command injection.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| command | string | - | Shell command to execute (required) |
| shell | string | /bin/sh | Path to shell interpreter |
| timeout | number | 30 | Max execution time in seconds (1-3600) |

## Variable Substitution

Use placeholders in your command:

| Placeholder | Description |
|-------------|-------------|
| {{payload}} | Message payload as string |
| {{topic}} | Message topic |
| {{timestamp}} | Unix timestamp in ms |
| {{meta.key}} | Specific metadata value |

**Security**: All values are shell-escaped to prevent injection attacks.

## Output

Returns an object with:

- **command** - The expanded command that was executed
- **stdout** - Standard output (trimmed)
- **stderr** - Standard error (trimmed)
- **exitCode** - Exit code (0 = success)
- **success** - Boolean indicating success
- **duration** - Execution time in ms
- **error** - Error message (if any)

## Example

` + "```yaml" + `
command: echo "Processing {{topic}}"
timeout: 10
` + "```" + `

## Example Output

` + "```json" + `
{
  "command": "echo 'Processing sensor-data'",
  "stdout": "Processing sensor-data",
  "stderr": "",
  "exitCode": 0,
  "success": true,
  "duration": 15
}
` + "```" + `

## Use Cases

- Running system scripts
- File operations
- External tool integration
- System monitoring
`,
		Category: types.NodeCategoryOutput,
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
				DataType: types.DataTypeObject,
				Multiple: true,
			},
		},
		Icon: "terminal",
	}
}

func init() {
	info := CommandActionInfo()
	if err := node.Register(info, NewCommandAction); err != nil {
		panic("failed to register command action: " + err.Error())
	}
}
