package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/toolschema"
	"github.com/Pegasus8/piworker/internal/types"
)

// Limits of trigger-tool settings.
const (
	toolDefaultTimeoutMs     = 10000
	toolMinTimeoutMs         = 1000
	toolMaxTimeoutMs         = 30000
	toolDefaultMaxConcurrent = 1
	toolMaxConcurrent        = 16
	toolMaxCooldownMs        = 3600000
	toolMinDescription       = 20
	toolMaxDescription       = 4096
	toolMaxTitle             = 128
)

// toolNamePattern is the MCP-compatible tool name syntax.
var toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

var (
	// errToolNotRunning: Emit on a stopped tool (or one whose flow is stopping).
	errToolNotRunning = errors.New("tool trigger is not running")
	// errInvalidToolArguments: Emit with arguments outside the input schema.
	errInvalidToolArguments = errors.New("invalid tool arguments")
)

// ToolTrigger publishes its flow as an MCP tool (trigger-tool). A call
// validates the arguments against the input schema and starts a run whose
// payload is the arguments; an action-reply node answers the call.
type ToolTrigger struct {
	*node.BaseNode

	spec node.ToolSpec

	mu      sync.Mutex
	running bool
	out     chan<- *types.Message
	ctx     context.Context
}

// NewToolTrigger creates a trigger-tool from its configuration, refusing an
// invalid one with every problem listed.
func NewToolTrigger(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	spec, err := parseToolSpec(base.GetConfig())
	if err != nil {
		return nil, err
	}
	return &ToolTrigger{BaseNode: base, spec: spec}, nil
}

// configErrors lists every problem of a configuration; errors.As reaches
// each one (e.g. the *toolschema.Error of the input schema).
type configErrors []error

func (e configErrors) Error() string {
	parts := make([]string, len(e))
	for i, err := range e {
		parts[i] = err.Error()
	}
	return strings.Join(parts, "; ")
}

func (e configErrors) Unwrap() []error { return e }

// parseToolSpec reads and checks a trigger-tool configuration.
func parseToolSpec(cfg map[string]any) (node.ToolSpec, error) {
	var errs configErrors
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	spec := node.ToolSpec{}

	switch name, ok := cfg["toolName"].(string); {
	case !ok || name == "":
		fail("toolName is required")
	case !toolNamePattern.MatchString(name):
		fail("toolName must be 1 to 128 letters, digits, '_', '.' or '-'")
	case strings.HasPrefix(strings.ToLower(name), "pw_"):
		fail("toolName must not start with pw_ (reserved for PiWorker's own tools)")
	default:
		spec.Name = name
	}

	if v, present := cfg["title"]; present && v != nil {
		title, ok := v.(string)
		switch {
		case !ok:
			fail("title must be text")
		case utf8.RuneCountInString(title) > toolMaxTitle:
			fail("title must be at most %d characters", toolMaxTitle)
		default:
			spec.Title = strings.TrimSpace(title)
		}
	}

	switch desc, ok := cfg["description"].(string); {
	case !ok:
		fail("description is required: tell agents what the tool does and when to use it")
	case utf8.RuneCountInString(strings.TrimSpace(desc)) < toolMinDescription:
		fail("description must have at least %d characters: tell agents what the tool does and when to use it", toolMinDescription)
	case utf8.RuneCountInString(desc) > toolMaxDescription:
		fail("description must be at most %d characters", toolMaxDescription)
	default:
		spec.Description = strings.TrimSpace(desc)
	}

	if raw, err := jsonObjectSetting(cfg, "inputSchema"); err != nil {
		errs = append(errs, err)
	} else if raw == nil {
		fail("inputSchema is required: a JSON Schema object describing the arguments")
	} else if schema, err := toolschema.Compile(raw); err != nil {
		errs = append(errs, fmt.Errorf("inputSchema: %w", err))
	} else {
		spec.InputSchema = schema
	}

	if raw, err := jsonObjectSetting(cfg, "outputSchema"); err != nil {
		errs = append(errs, err)
	} else if raw != nil {
		if out, err := toolschema.CheckOutputSchema(raw); err != nil {
			errs = append(errs, fmt.Errorf("outputSchema: %w", err))
		} else {
			spec.OutputSchema = out
		}
	}

	if ms, err := intSetting(cfg, "timeoutMs", toolDefaultTimeoutMs, toolMinTimeoutMs, toolMaxTimeoutMs, "milliseconds"); err != nil {
		errs = append(errs, err)
	} else {
		spec.Timeout = time.Duration(ms) * time.Millisecond
	}

	spec.Confirm = node.ToolConfirmNone
	if v, present := cfg["confirm"]; present && v != nil && v != "" {
		confirm, _ := v.(string)
		if !containsString(node.ToolConfirmModes(), confirm) {
			fail("confirm must be one of none, client or admin")
		} else {
			spec.Confirm = confirm
		}
	}

	if v, present := cfg["idempotent"]; present && v != nil {
		b, ok := v.(bool)
		if !ok {
			fail("idempotent must be true or false")
		}
		spec.Idempotent = b
	}

	if n, err := intSetting(cfg, "maxConcurrent", toolDefaultMaxConcurrent, 1, toolMaxConcurrent, "calls"); err != nil {
		errs = append(errs, err)
	} else {
		spec.MaxConcurrent = n
	}

	if ms, err := intSetting(cfg, "cooldownMs", 0, 0, toolMaxCooldownMs, "milliseconds"); err != nil {
		errs = append(errs, err)
	} else {
		spec.Cooldown = time.Duration(ms) * time.Millisecond
	}

	if len(errs) > 0 {
		return node.ToolSpec{}, errs
	}
	return spec, nil
}

// jsonObjectSetting reads a "json" setting: a JSON object, or text holding
// one. It returns nil when the setting is absent, null or "".
func jsonObjectSetting(cfg map[string]any, key string) (json.RawMessage, error) {
	switch v := cfg[key].(type) {
	case nil:
		return nil, nil
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, nil
		}
		return json.RawMessage(v), nil
	case map[string]any:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		return data, nil
	default:
		return nil, fmt.Errorf("%s must be a JSON object", key)
	}
}

// intSetting reads a whole number within [min, max], or def when absent.
func intSetting(cfg map[string]any, key string, def, min, max int, unit string) (int, error) {
	v, present := cfg[key]
	if !present || v == nil || v == "" {
		return def, nil
	}
	var f float64
	switch n := v.(type) {
	case float64:
		f = n
	case int:
		f = float64(n)
	case int64:
		f = float64(n)
	case json.Number:
		parsed, err := n.Float64()
		if err != nil {
			return 0, fmt.Errorf("%s must be a whole number of %s", key, unit)
		}
		f = parsed
	default:
		return 0, fmt.Errorf("%s must be a whole number of %s", key, unit)
	}
	if math.IsNaN(f) || f != math.Trunc(f) {
		return 0, fmt.Errorf("%s must be a whole number of %s", key, unit)
	}
	if f < float64(min) || f > float64(max) {
		return 0, fmt.Errorf("%s must be between %d and %d %s", key, min, max, unit)
	}
	return int(f), nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// ToolSpec implements node.ToolProvider.
func (t *ToolTrigger) ToolSpec() node.ToolSpec { return t.spec }

// Process is not used: calls arrive through Emit.
func (t *ToolTrigger) Process(context.Context, *types.Message) ([]*types.Message, error) {
	return nil, nil
}

// Ports returns the single output carrying the call arguments.
func (t *ToolTrigger) Ports() (inputs []types.Port, outputs []types.Port) {
	return nil, toolTriggerOutputs()
}

func toolTriggerOutputs() []types.Port {
	return []types.Port{{ID: "output", Name: "Arguments", DataType: types.DataTypeObject}}
}

// Validate re-checks the current configuration.
func (t *ToolTrigger) Validate() error {
	_, err := parseToolSpec(t.GetConfig())
	return err
}

// Start claims the tool name; it fails with node.ErrResourceConflict when
// another running flow publishes the same name.
func (t *ToolTrigger) Start(ctx context.Context, out chan<- *types.Message) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running {
		return errors.New("trigger already running")
	}
	if err := toolNames.claim(t.spec.Name, t); err != nil {
		return err
	}
	t.running, t.out, t.ctx = true, out, ctx
	return nil
}

// Stop releases the tool name.
func (t *ToolTrigger) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.running {
		return nil
	}
	toolNames.release(t.spec.Name, t)
	t.running, t.out, t.ctx = false, nil, nil
	return nil
}

// Emit starts a run with msg (node.Emitter). The payload must be arguments
// that satisfy the input schema: they were validated by the caller and are
// checked again here, so the flow never sees anything else. Emit adds the
// toolName meta, routes the message to the output port, and blocks until
// the runtime takes it, the caller's ctx ends or the trigger stops.
func (t *ToolTrigger) Emit(ctx context.Context, msg *types.Message) error {
	t.mu.Lock()
	running, out, runCtx := t.running, t.out, t.ctx
	t.mu.Unlock()
	if !running {
		return errToolNotRunning
	}
	if msg == nil {
		return fmt.Errorf("%w: no message", errInvalidToolArguments)
	}
	args, ok := msg.Payload.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: arguments must be a JSON object", errInvalidToolArguments)
	}
	if issues := t.spec.InputSchema.Validate(args); len(issues) > 0 {
		parts := make([]string, len(issues))
		for i, issue := range issues {
			parts[i] = issue.String()
		}
		return fmt.Errorf("%w: %s", errInvalidToolArguments, strings.Join(parts, "; "))
	}

	msg.SourcePort = "output"
	msg.SetMeta("toolName", t.spec.Name)
	if msg.Topic == "" {
		msg.Topic = t.spec.Name
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if runCtx.Err() != nil {
		return errToolNotRunning
	}
	select {
	case out <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-runCtx.Done():
		return errToolNotRunning
	}
}

// GetConfigSchema returns the configuration schema for the editor.
func (t *ToolTrigger) GetConfigSchema() node.ConfigSchema {
	return toolTriggerConfigSchema()
}

func toolTriggerConfigSchema() node.ConfigSchema {
	minTimeout, maxTimeout := float64(toolMinTimeoutMs), float64(toolMaxTimeoutMs)
	minConc, maxConc := 1.0, float64(toolMaxConcurrent)
	minCool, maxCool := 0.0, float64(toolMaxCooldownMs)
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"toolName": {
				Type:         "string",
				Title:        "Tool name",
				Description:  "Name agents call the tool by: 1-128 letters, digits, _ . or -, unique across running flows, not starting with pw_",
				AgentVisible: true,
			},
			"title": {
				Type:         "string",
				Title:        "Title",
				Description:  "Short human-readable name shown by MCP clients (optional)",
				AgentVisible: true,
			},
			"description": {
				Type:         "string",
				Title:        "Description",
				Description:  "What the tool does and when to use it (at least 20 characters); agents read it to decide when to call the tool",
				AgentVisible: true,
			},
			"inputSchema": {
				Type:         "json",
				Title:        "Input schema",
				Description:  "JSON Schema of the arguments (closed subset: object root, typed properties, anchored patterns, arrays with maxItems); see the documentation",
				Default:      map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
				AgentVisible: true,
			},
			"outputSchema": {
				Type:         "json",
				Title:        "Output schema",
				Description:  "Optional JSON Schema of the reply payload (type object)",
				AgentVisible: true,
			},
			"timeoutMs": {
				Type:         "integer",
				Title:        "Timeout",
				Description:  "How long a call waits for the reply, in milliseconds (1000-30000); after that the call reports running and the result can be fetched later",
				Default:      toolDefaultTimeoutMs,
				Minimum:      &minTimeout,
				Maximum:      &maxTimeout,
				Unit:         "milliseconds",
				AgentVisible: true,
			},
			"confirm": {
				Type:         "string",
				Title:        "Confirmation",
				Description:  "none: runs when called; client: MCP clients ask their user first (not enforced); admin: every call waits for your approval in the editor",
				Default:      node.ToolConfirmNone,
				Enum:         node.ToolConfirmModes(),
				AgentVisible: true,
			},
			"idempotent": {
				Type:         "boolean",
				Title:        "Idempotent",
				Description:  "Calling the tool again with the same arguments has no additional effect (advertised to agents as idempotentHint)",
				Default:      false,
				AgentVisible: true,
			},
			"maxConcurrent": {
				Type:         "integer",
				Title:        "Concurrent calls",
				Description:  "Maximum number of calls of this tool running at once (count, 1-16)",
				Default:      toolDefaultMaxConcurrent,
				Minimum:      &minConc,
				Maximum:      &maxConc,
				Unit:         "count",
				AgentVisible: true,
			},
			"cooldownMs": {
				Type:         "integer",
				Title:        "Cooldown",
				Description:  "Minimum time between the end of a call and the next one, in milliseconds (0-3600000)",
				Default:      0,
				Minimum:      &minCool,
				Maximum:      &maxCool,
				Unit:         "milliseconds",
				AgentVisible: true,
			},
		},
		Required: []string{"toolName", "description", "inputSchema"},
	}
}

// ToolTriggerInfo returns the node type info for registration.
func ToolTriggerInfo() node.NodeTypeInfo {
	schema := toolTriggerConfigSchema()
	return node.NodeTypeInfo{
		Type:          "trigger-tool",
		Name:          "Agent Tool",
		Description:   "Publish this flow as a tool that AI agents can call over MCP",
		Documentation: toolTriggerDocumentation,
		Category:      types.NodeCategoryInput,
		// The tool's effective risk is computed from everything the flow can
		// reach (ADR-0013); the trigger itself only receives arguments.
		Risk:    node.RiskSafe,
		Inputs:  nil,
		Outputs: toolTriggerOutputs(),
		Config:  &schema,
		Icon:    "wrench",
	}
}

func init() {
	if err := node.Register(ToolTriggerInfo(), NewToolTrigger); err != nil {
		panic("failed to register tool trigger: " + err.Error())
	}
}

const toolTriggerDocumentation = `## Agent Tool

Publishes the flow as a **tool** that AI agents (Claude Code, Claude Desktop
and other MCP clients) can call through PiWorker's MCP endpoint. Agents send
**data only**: the arguments described by the input schema. They never see
or change the flow, its configuration or your secrets.

A call starts a run whose payload is the arguments. End the flow with an
**Agent Reply** node to answer the call; its payload becomes the result.

## What an agent sees

- **name** – the tool name (unique across running flows; ` + "`pw_`" + ` is
  reserved for PiWorker's own tools)
- **title** and **description** – write the description for the agent: what
  the tool does, when to use it, what it returns (at least 20 characters)
- **input schema** – the arguments, as JSON Schema
- **output schema** – optional, the shape of the reply
- hints computed by PiWorker from what the flow can reach (read-only,
  destructive, open-world), plus your **idempotent** flag

An agent can only call the tool when its token was granted the tool.

## Input schema

A closed subset of JSON Schema, checked when the flow is deployed:

- the root is ` + "`\"type\": \"object\"`" + `; objects are always closed
  (` + "`additionalProperties: false`" + ` is added) and nest at most 3 levels
- each property has exactly one type: ` + "`string`, `integer`, `number`, `boolean`" + `,
  an ` + "`array`" + ` of one of those with ` + "`maxItems`" + ` (at most 100), or an ` + "`object`" + `
- strings: ` + "`enum`, `pattern`, `minLength`, `maxLength`" + ` (256 when not set,
  at most 4096), ` + "`format`" + ` (informative only)
- numbers and integers: ` + "`minimum`, `maximum`, `enum`" + `
- ` + "`title`, `description`, `default`" + ` (must be valid) and ` + "`examples`" + `
- patterns must be anchored: start with ` + "`^`" + `, end with ` + "`$`" + `, and keep
  alternatives in a group (` + "`^(on|off)$`" + `, not ` + "`^on|off$`" + `)

Not accepted: ` + "`$ref`, `$defs`, `anyOf`, `oneOf`, `allOf`, `not`, `if`/`then`/`else`,\n`patternProperties`, `const`" + `, type lists, untyped or boolean schemas and
any other keyword.

` + "```json" + `
{
  "type": "object",
  "required": ["door"],
  "properties": {
    "door":    {"type": "string", "enum": ["left", "right"], "description": "Which door"},
    "seconds": {"type": "integer", "minimum": 1, "maximum": 60, "default": 10},
    "label":   {"type": "string", "pattern": "^[a-z][a-z0-9_]*$", "maxLength": 32}
  }
}
` + "```" + `

Arguments that reach a **privileged** node (command, file, database, GPIO,
I2C) must be strict: strings need an ` + "`enum`" + ` or a pattern that rejects
option flags (` + "`-x`, `--a=b`" + `), paths (` + "`..`, `/`" + `), spaces, line breaks and
shell syntax (` + "`$(`, `;`, `|`" + `, backtick); numbers need ` + "`minimum`" + ` and
` + "`maximum`" + `. To accept one of these on purpose, list it in
` + "`\"x-piworker-allow\": [\"/\"]`" + ` on that property (agents do not see it).

## Calls and replies

- **payload** – the validated arguments (an object)
- **meta.correlationID** – the run ID of the call (used by Agent Reply)
- **meta.toolName** – this tool's name; **meta.actor** – who called
- The call waits up to **timeoutMs** (default 10 s, at most 30 s) for an Agent
  Reply. If the reply comes later, the call reports *running* and the agent
  fetches the result afterwards. A run that ends without a reply completes
  with no result, or with the error of the first node that failed.
- **maxConcurrent** (default 1) and **cooldownMs** limit how often the tool
  runs.

## Confirmation

| confirm | Effect |
|---------|--------|
| none | The tool runs when called |
| client | MCP clients ask their user before calling (a convenience, not a security control) |
| admin | Every call waits for your approval in the editor's queue |

Destructive tools are always marked as needing user interaction.
`
