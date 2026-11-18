package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// LLMAgentProcess is an AI agent: a model that may call the admin's flow
// tools (trigger-tool flows) in a loop until it answers (ADR-0039,
// docs/design/llm.md §3-4). It calls them through the node.ToolCaller of
// its context, which applies the rules of every tool call (only the tools
// the node lists, never its own flow's, privileged ones only with
// allowPrivileged, only those that need no confirmation; the tools' own
// limits) and redacts the results as an MCP agent's.
type LLMAgentProcess struct {
	*node.BaseNode
	llmSettings

	system          string // template
	prompt          string // template
	tools           []string
	maxSteps        int
	allowPrivileged bool
	maxTokens       int
	effort          string
	memory          llmMemory
	resultField     string
}

const (
	llmAgentDefaultMaxTokens = 4096
	llmAgentDefaultTimeout   = 300
	llmAgentDefaultMaxSteps  = 8
	llmAgentMaxSteps         = 32
	// llmAgentMaxTools bounds the tools an agent node lists.
	llmAgentMaxTools = 64
	// providerToolNameLength bounds a tool name as providers accept it.
	providerToolNameLength = 64
)

// Step statuses of meta.llm.steps.
const (
	agentStepOK      = "ok"      // the run answered
	agentStepError   = "error"   // the run failed
	agentStepRunning = "running" // no answer within the tool's timeout; the run goes on
	agentStepRefused = "refused" // no run: an unknown tool, bad arguments or a refused call
)

// NewLLMAgentProcess creates an LLMAgentProcess from configuration.
func NewLLMAgentProcess(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	cfg := base.GetConfig()
	settings, err := parseLLMSettings(base, llmAgentDefaultTimeout)
	if err != nil {
		return nil, err
	}
	a := &LLMAgentProcess{BaseNode: base, llmSettings: settings}
	if a.system, err = textSetting(cfg, "system"); err != nil {
		return nil, err
	}
	if a.prompt, err = templateSetting(cfg, "prompt", "{{payload}}"); err != nil {
		return nil, err
	}
	if a.tools, err = stringList(cfg, "tools"); err != nil {
		return nil, fmt.Errorf("%w (a list of tool names)", err)
	}
	if err := checkAgentTools(a.tools); err != nil {
		return nil, err
	}
	if a.maxSteps, err = intSetting(cfg, "maxSteps", llmAgentDefaultMaxSteps, 1, llmAgentMaxSteps, "steps"); err != nil {
		return nil, err
	}
	if a.allowPrivileged, err = boolSetting(cfg, "allowPrivileged"); err != nil {
		return nil, err
	}
	if a.maxTokens, err = parseLLMMaxTokens(base, llmAgentDefaultMaxTokens); err != nil {
		return nil, err
	}
	if a.effort, err = parseLLMEffort(base); err != nil {
		return nil, err
	}
	if a.memory, err = parseLLMMemory(base); err != nil {
		return nil, err
	}
	if a.resultField, err = parseResultField(base); err != nil {
		return nil, err
	}
	return a, nil
}

// checkAgentTools checks the tool names: at most llmAgentMaxTools distinct
// trigger-tool names.
func checkAgentTools(tools []string) error {
	if len(tools) > llmAgentMaxTools {
		return fmt.Errorf("tools may list at most %d tools", llmAgentMaxTools)
	}
	seen := map[string]bool{}
	for i, name := range tools {
		switch {
		case !toolNamePattern.MatchString(name):
			return fmt.Errorf("tools: item %d is not a tool name (1 to 128 letters, digits, '_', '.' or '-')", i+1)
		case seen[name]:
			return fmt.Errorf("tools lists %q twice", name)
		}
		seen[name] = true
	}
	return nil
}

// Process runs the agent: it asks the model, runs the tool calls the answer
// asks for and sends their results back, until an answer asks for no tool
// (its text is the result) or maxSteps requests were sent. Once a tool
// was called, a failure is permanent (afterToolsError): a retry would run
// the loop again, and the tools with it.
func (a *LLMAgentProcess) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	var out *types.Message
	toolsCalled := false
	err := a.run(ctx, func(ctx context.Context) error {
		s, err := a.open(ctx, "")
		if err != nil {
			return err
		}
		conv, history, err := a.memory.conversation(ctx, msg)
		if err != nil {
			return err
		}
		prompt := renderTemplate(a.prompt, msg, ctx)
		if strings.TrimSpace(prompt) == "" {
			return errors.New("the prompt is empty for this message")
		}

		info := node.NodeInfoFromContext(ctx)
		caller := node.AgentCaller{FlowID: info.FlowID, NodeID: info.NodeID, Tools: a.tools, AllowPrivileged: a.allowPrivileged,
			// The flows waiting for this message's run (a tool call of
			// another flow's agent led here): their tools would wait for
			// this node.
			Waiting: node.WaitingFlows(ctx, correlationIDOf(msg))}
		tc := node.ToolCallerFromContext(ctx)
		var tools []llm.Tool
		var fromProvider map[string]string
		if len(a.tools) > 0 {
			available := tc.AgentTools(caller)
			names := make([]string, len(available))
			for i, t := range available {
				names[i] = t.Name
			}
			var toProvider map[string]string
			toProvider, fromProvider = providerToolNames(names)
			for _, t := range available {
				tools = append(tools, llm.Tool{Name: toProvider[t.Name], Description: t.Description, InputSchema: t.InputSchema})
			}
		}

		messages := append(append([]llm.Message(nil), history...), llm.Message{Role: llm.RoleUser, Text: prompt})
		system := strings.TrimSpace(renderTemplate(a.system, msg, ctx))
		var usage llm.Usage
		steps := []any{}
		for request := 1; ; request++ {
			resp, err := s.client.Chat(ctx, llm.Request{
				Model:     s.model,
				System:    system,
				Messages:  messages,
				Tools:     tools,
				MaxTokens: a.maxTokens,
				Effort:    a.effort,
				// Every step sends the conversation so far again.
				Cache: true,
			})
			if err != nil {
				return err
			}
			usage = usage.Add(resp.Usage)
			if len(resp.ToolCalls) == 0 {
				a.memory.remember(ctx, conv, prompt, resp.Text)
				out = llmResult(msg, a.resultField, resp.Text)
				meta := llmMeta(s.conn.Name, resp.Model, usage, resp.Stop)
				meta["steps"] = steps
				out.SetMeta("llm", meta)
				out.SourcePort = "output"
				return nil
			}
			if resp.Stop == llm.StopMaxTokens {
				return fmt.Errorf("the agent's answer was cut at maxTokens (%d) while it asked for a tool: raise maxTokens", a.maxTokens)
			}
			if request >= a.maxSteps {
				return fmt.Errorf("the agent did not finish within %d steps (maxSteps): it still asked for a tool", a.maxSteps)
			}
			messages = append(messages, resp.Message)
			results := make([]llm.ToolResult, 0, len(resp.ToolCalls))
			for _, call := range resp.ToolCalls {
				result, step, called := a.callTool(ctx, tc, caller, fromProvider, call)
				toolsCalled = toolsCalled || called
				if err := ctx.Err(); err != nil {
					return fmt.Errorf("the agent stopped while calling a tool: %w", err)
				}
				results = append(results, result)
				steps = append(steps, step)
			}
			messages = append(messages, llm.Message{Role: llm.RoleUser, ToolResults: results})
		}
	})
	if err != nil {
		if toolsCalled {
			err = afterToolsError{err: err}
		}
		return nil, err
	}
	return []*types.Message{out}, nil
}

// correlationIDOf returns the run msg belongs to ("" when it has none).
func correlationIDOf(msg *types.Message) string {
	v, _ := msg.GetMeta("correlationID")
	id, _ := v.(string)
	return id
}

// afterToolsError is a failure of an agent after it called a tool: it is
// permanent whatever it wraps (a 429, a 529 or a network error is
// transient otherwise), because the runtime would retry the whole loop and
// the model would call the tools it already called, each a real run of
// another flow, again. Its message and the error it wraps are unchanged.
type afterToolsError struct{ err error }

func (e afterToolsError) Error() string { return e.err.Error() }
func (e afterToolsError) Unwrap() error { return e.err }

// Retryable implements node.Retryable: never.
func (e afterToolsError) Retryable() bool { return false }

// callTool runs one tool call of the model's and returns its result for
// the model, its step for meta.llm.steps (the tool's name, the status and
// the duration: never arguments or results) and whether the tool caller
// was asked to run it. Whatever goes wrong is a result with IsError, so
// the model can react: an unknown name, arguments that are not an object,
// a refusal (its message is safe to show the model) and a failed run.
func (a *LLMAgentProcess) callTool(ctx context.Context, tc node.ToolCaller, caller node.AgentCaller, fromProvider map[string]string, call llm.ToolCall) (result llm.ToolResult, step map[string]any, called bool) {
	start := time.Now()
	result = llm.ToolResult{CallID: call.ID}
	name, known := fromProvider[call.Name]
	status := agentStepRefused
	var args map[string]any
	switch {
	case !known:
		name = capRunes(call.Name, providerToolNameLength)
		result.Content = fmt.Sprintf("unknown tool %q: call only the tools you were given", name)
		result.IsError = true
	case json.Unmarshal(call.Input, &args) != nil || args == nil:
		result.Content = "the arguments must be a JSON object matching the tool's input schema"
		result.IsError = true
	default:
		called = true
		outcome, err := tc.CallAgentTool(ctx, caller, name, args)
		switch {
		case err != nil:
			result.Content = err.Error()
			result.IsError = true
		case outcome.Status == node.ToolCallOK:
			status = agentStepOK
			result.Content = string(outcome.Result)
			if strings.TrimSpace(result.Content) == "" {
				result.Content = "The tool finished without a reply."
			}
		case outcome.Status == node.ToolCallRunning:
			status = agentStepRunning
			result.Content = "The tool is still running: its answer did not arrive in time. It may still finish; do not call it again for the same request."
		default:
			status = agentStepError
			result.Content = outcome.Error
			if result.Content == "" {
				result.Content = "the tool failed"
			}
			result.IsError = true
		}
	}
	step = map[string]any{"tool": name, "status": status, "durationMs": time.Since(start).Milliseconds()}
	return result, step, called
}

// providerToolNames maps tool names to names every provider accepts
// (1 to 64 letters, digits, '_' or '-'; flow tool names may also hold '.'
// and be 128 characters long) and back. Each name is mapped once, in
// order; a sanitized name another one already took gets a suffix (_2, _3,
// …), so the mapping is one to one.
func providerToolNames(names []string) (toProvider, fromProvider map[string]string) {
	toProvider = make(map[string]string, len(names))
	fromProvider = make(map[string]string, len(names))
	for _, name := range names {
		if _, done := toProvider[name]; done {
			continue
		}
		base := sanitizeToolName(name)
		candidate := base
		for i := 2; ; i++ {
			if _, taken := fromProvider[candidate]; !taken {
				break
			}
			suffix := "_" + strconv.Itoa(i)
			candidate = base[:min(len(base), providerToolNameLength-len(suffix))] + suffix
		}
		toProvider[name] = candidate
		fromProvider[candidate] = name
	}
	return toProvider, fromProvider
}

// sanitizeToolName replaces what providers refuse in a tool name with '_'
// and cuts it to providerToolNameLength.
func sanitizeToolName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if b.Len() == providerToolNameLength {
			break
		}
	}
	if b.Len() == 0 {
		return "tool"
	}
	return b.String()
}

// Ports returns the port definitions.
func (a *LLMAgentProcess) Ports() (inputs []types.Port, outputs []types.Port) {
	return llmInputs, llmOutputs
}

// Validate checks the tool names (the constructor checked everything).
func (a *LLMAgentProcess) Validate() error { return checkAgentTools(a.tools) }

func llmAgentConfigSchema() node.ConfigSchema {
	one, maxSteps := float64(1), float64(llmAgentMaxSteps)
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"connection": llmSchemaConnection(),
			"model":      llmSchemaModel("Model to ask; it must support tool use (empty: the connection's default model)"),
			"system": {Type: "string", Title: "System prompt",
				Description: "Instructions for the agent: its role, what its tools are for, its rules (supports {{templates}})"},
			"prompt": {Type: "string", Title: "Prompt",
				Description: "The request, built from the message (supports {{templates}})", Default: "{{payload}}"},
			"tools": {Type: "array", Title: "Tools",
				Description:  fmt.Sprintf("Flow tools the agent may call, by tool name (at most %d; tools that ask for confirmation are never called)", llmAgentMaxTools),
				AgentVisible: true, Destination: true, Options: node.OptionsTools},
			"maxSteps": {Type: "integer", Title: "Max steps",
				Description: fmt.Sprintf("Most requests to the model before the agent gives up, in steps (1 to %d)", llmAgentMaxSteps),
				Default:     llmAgentDefaultMaxSteps, Minimum: &one, Maximum: &maxSteps, Unit: "steps", AgentVisible: true},
			"allowPrivileged": {Type: "boolean", Title: "Allow privileged tools",
				Description: "Let the agent call tools that run commands, write files or touch hardware",
				Default:     false, RiskSwitch: true, AgentVisible: true},
			"maxTokens":   llmSchemaMaxTokens(llmAgentDefaultMaxTokens),
			"effort":      llmSchemaEffort(),
			"memoryKey":   llmSchemaMemoryKey(),
			"memoryTurns": llmSchemaMemoryTurns(),
			"resultField": llmSchemaResultField(),
			"timeout":     llmSchemaTimeout(llmAgentDefaultTimeout),
		},
		Required: []string{"connection", "prompt"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (a *LLMAgentProcess) GetConfigSchema() node.ConfigSchema { return llmAgentConfigSchema() }

// LLMAgentProcessInfo returns the node type info for registration.
func LLMAgentProcessInfo() node.NodeTypeInfo {
	schema := llmAgentConfigSchema()
	return node.NodeTypeInfo{
		Type:        "process-llm-agent",
		Name:        "AI Agent",
		Description: "A language model that calls your flow tools until it can answer",
		Documentation: `## AI Agent

An agent: a language model that may call your **flow tools** (flows that
start with a **Tool** trigger) to do what the prompt asks. It calls the
tools the model asks for, sends their results back, and repeats until the
model answers without a tool call. The answer is the result.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| connection | string | - | LLM connection (required) |
| model | string | connection's | Model to ask (it must support tool use) |
| system | string | - | The agent's role and rules (templates) |
| prompt | string | {{payload}} | The request (templates) |
| tools | list | - | Tool names the agent may call |
| maxSteps | integer | 8 | Most requests to the model, in steps (1-32) |
| allowPrivileged | boolean | false | Allow tools that run commands, files or hardware |
| maxTokens | integer | 4096 | Longest answer per step, in tokens (1-64000) |
| effort | enum | - | low, medium, high, xhigh or max, only for models that support it (others may reject the request) |
| memoryKey | string | - | Conversation to remember (templates; empty: none) |
| memoryTurns | integer | 20 | Past messages kept and sent, in messages (2-200) |
| resultField | string | - | Payload field for the answer (empty: replace the payload) |
| timeout | integer | 300 | Longest time for the whole run, tools included, in seconds (1-600) |

## Which tools it may call

Only the tools listed in **tools**, and of those only the ones that:

- are published by a running flow other than this node's own;
- need no confirmation (` + "`confirm: none`" + `): an agent node has no one to
  ask and cannot wait for your approval;
- are not privileged, unless **allowPrivileged** is on.

Each tool's own limits (concurrent calls, cooldown) apply. A refused or
failed call is reported to the model as an error, so it can try something
else. Results are redacted as for MCP agents: secrets never reach the model.

## Output

The message with the final answer (text), as the payload or in
` + "`resultField`" + `. **meta.llm** holds the connection, the model, the total
` + "`inputTokens`" + ` and ` + "`outputTokens`" + `, ` + "`requests`" + `, ` + "`stop`" + ` and ` + "`steps`" + `: one
` + "`{tool, status, durationMs}`" + ` per tool call, with status ` + "`ok`" + `, ` + "`error`" + `,
` + "`running`" + ` (no answer within the tool's timeout) or ` + "`refused`" + `. Steps never
hold arguments or results.

If the model still asks for a tool after **maxSteps** requests, the message
fails with *the agent did not finish within N steps*.

Rate limits, overloaded providers and network errors are transient, so the
flow's retries apply to them, but only until the agent has called a tool:
after that every failure is permanent, because a retry would start again
and call the same tools (each a run of another flow) a second time.

## Example

A home assistant answering Telegram messages (received by a **Webhook**
trigger):

` + "```" + `
system: You control the house. Use the tools; answer in one short sentence.
prompt: {{payload.body.message.text}}
tools: lights_set, thermostat_get, thermostat_set
memoryKey: telegram:{{payload.body.message.chat.id}}
` + "```" + `

## Security: prompt injection

Text that reaches the agent from outside (webhooks, chats, e-mail, web
pages, even tool results) can contain instructions written to make the model
misuse its tools: a **prompt injection**. No prompt fully prevents it.
What the agent can do is bounded by what you allow: list only the tools it
needs, prefer tools that only read, keep **allowPrivileged** off, keep
**maxSteps** low and give risky tools their own limits and checks. The
messages and tool results are sent to the connection's provider.
`,
		Category: types.NodeCategoryProcessing,
		Risk:     node.RiskExternal,
		Inputs:   llmInputs,
		Outputs:  llmOutputs,
		Config:   &schema,
		Icon:     "bot",
	}
}

func init() {
	info := LLMAgentProcessInfo()
	if err := node.Register(info, NewLLMAgentProcess); err != nil {
		panic("failed to register llm agent process: " + err.Error())
	}
}
