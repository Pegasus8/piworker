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

// commandMaxOutputBytes caps how much of stdout and of stderr is kept, so a
// chatty or runaway command can't exhaust RAM.
const commandMaxOutputBytes = 1 << 20 // 1 MiB each

// cappedBuffer keeps the first limit bytes written and discards the rest. It
// always reports a full write so the command keeps running normally instead
// of failing on a short write / broken pipe.
type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); room < len(p) {
		b.truncated = true
		if room > 0 {
			b.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return b.buf.Write(p)
}

// CommandAction executes shell commands.
type CommandAction struct {
	*node.BaseNode

	command   string
	shell     string
	timeout   time.Duration
	allowDash bool // allowDashArgs: let placeholder values start with '-'
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
		BaseNode:  base,
		command:   base.GetConfigString("command", ""),
		shell:     base.GetConfigString("shell", "/bin/sh"),
		timeout:   time.Duration(timeoutSec) * time.Second,
		allowDash: base.GetConfigBool("allowDashArgs", false),
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

	// Bind values as data, never as shell source.
	expandedCommand, values, err := bindCommand(a.command, msg, a.allowDash)
	if err != nil {
		return nil, err
	}

	// Execute the command
	args := append([]string{"-c", expandedCommand, a.shell}, values...)
	cmd := exec.CommandContext(execCtx, a.shell, args...)
	isolateCommand(cmd)

	stdout := &cappedBuffer{limit: commandMaxOutputBytes}
	stderr := &cappedBuffer{limit: commandMaxOutputBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	startTime := time.Now()
	err = cmd.Run()
	duration := time.Since(startTime)

	// Build the output payload. Command failures are surfaced in the payload
	// (success/exitCode/timedOut) rather than as a node error, so the flow can
	// branch on the result instead of aborting. This means a failed command does
	// NOT trigger the runtime's retry/error path.
	output := map[string]interface{}{
		"command":  a.command,
		"stdout":   strings.TrimSpace(stdout.buf.String()),
		"stderr":   strings.TrimSpace(stderr.buf.String()),
		"duration": duration.Milliseconds(),
		"exitCode": 0,
		"success":  true,
		"timedOut": false,

		"stdoutTruncated": stdout.truncated,
		"stderrTruncated": stderr.truncated,
		"truncated":       stdout.truncated || stderr.truncated,
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
	_, _, err := bindCommand(a.command, &types.Message{}, a.allowDash)
	return err
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
				Type:         "string",
				Title:        "Shell",
				Description:  "Path to the shell interpreter",
				Default:      "/bin/sh",
				AgentVisible: true,
			},
			"timeout": {
				Type:         "number",
				Title:        "Timeout (seconds)",
				Description:  "Maximum execution time, in seconds (1-3600)",
				Default:      30,
				Minimum:      &minTimeout,
				Maximum:      &maxTimeout,
				Unit:         "seconds",
				AgentVisible: true,
			},
			"allowDashArgs": {
				Type:         "boolean",
				RiskSwitch:   true,
				Title:        "Allow values starting with '-'",
				Description:  "Risky: let a placeholder that begins an argument take a value starting with '-', which most programs read as an option (e.g. -rf). Only enable it when the command ends its options with -- before the placeholders. Not needed behind literal text such as temp={{payload}}.",
				Default:      false,
				AgentVisible: true,
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

Runs commands using a POSIX-compatible shell on the host. Placeholder values are passed separately as literal arguments.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| command | string | - | Shell command to execute (required) |
| shell | string | /bin/sh | Path to shell interpreter |
| timeout | number | 30 | Max execution time in seconds (1-3600) |
| allowDashArgs | boolean | false | Allow placeholder values that start with ` + "`-`" + ` (risky, see below) |

On Unix systems (including Linux and macOS), timeout or flow cancellation stops
the shell and children in its process group. Commands that deliberately detach
into another session or process group are outside this cancellation scope.

## Variable Substitution

Use placeholders in your command:

| Placeholder | Description |
|-------------|-------------|
| {{payload}} | Message payload as string |
| {{topic}} | Message topic |
| {{timestamp}} | Unix timestamp in ms |
| {{meta.key}} | Specific metadata value |

Placeholders work as ordinary arguments, with no quotes, single quotes, or double
quotes, including adjacent text such as "Processing {{topic}}". Their values are
not expanded again as shell code or as other placeholders. Missing metadata is
left as its literal placeholder. NUL bytes cannot be passed to a shell argument.

Placeholders inside command substitutions, parameter/arithmetic expansions,
here-documents, here-strings, or other unsupported shell constructs are rejected
before any command executes. Do not backslash-escape placeholders. Pass the value
to an ordinary command argument instead.

A placeholder value that starts with ` + "`-`" + ` is refused by default when the
placeholder begins its argument, because the program would read it as an
option (` + "`-rf`" + `, ` + "`--output=/etc/passwd`" + `). Behind literal text in the same
argument it is allowed, so ` + "`echo \"temp={{payload}}\"`" + `, ` + "`--value={{payload}}`" + ` and
` + "`/tmp/{{payload}}`" + ` accept negative numbers. If the command really expects such
values as whole arguments, end its options with ` + "`--`" + ` before the placeholders
(` + "`grep -- {{payload}} file`" + `) and set **allowDashArgs** to true.

The configured command is trusted shell code: do not explicitly reinterpret
external input using eval, sh -c, or another interpreter. Positional arguments are
reserved for placeholder values; do not change them with set/shift in the command.

## Output

Returns an object with:

- **command** - The configured command template (values are not included)
- **stdout** - Standard output (trimmed; only the first 1 MiB is kept)
- **stderr** - Standard error (trimmed; only the first 1 MiB is kept)
- **stdoutTruncated** / **stderrTruncated** / **truncated** - Whether output was cut off
- **exitCode** - Exit code (0 = success)
- **success** - Boolean indicating success
- **timedOut** - Whether the execution deadline expired
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
  "command": "echo \"Processing {{topic}}\"",
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
		Risk:     node.RiskPrivileged,
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
