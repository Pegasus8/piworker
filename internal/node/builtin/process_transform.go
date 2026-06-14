package builtin

import (
	"context"
	"fmt"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// Limits for sandboxed expression evaluation.
const (
	// DefaultExprMaxNodes caps the compiled AST size to reject absurdly complex
	// expressions at compile time.
	DefaultExprMaxNodes uint = 100000
	// DefaultExprTimeout bounds how long a single evaluation may run.
	DefaultExprTimeout = 5 * time.Second
)

// TransformProcess transforms message payloads using expressions.
// It uses the expr-lang library for safe, sandboxed expression evaluation.
type TransformProcess struct {
	*node.BaseNode

	expression string
	program    *vm.Program
}

// NewTransformProcess creates a new TransformProcess with the given configuration.
func NewTransformProcess(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	expression := base.GetConfigString("expression", "payload")
	if expression == "" {
		return nil, fmt.Errorf("expression is required")
	}

	program, err := compileExpr(expression)
	if err != nil {
		return nil, fmt.Errorf("invalid expression: %w", err)
	}

	process := &TransformProcess{
		BaseNode:   base,
		expression: expression,
		program:    program,
	}

	return process, nil
}

// compileExpr compiles a sandboxed expression with the shared policy used by all
// expr-backed nodes: AllowUndefinedVariables (payload shape varies at runtime),
// MaxNodes (caps AST complexity), and WithContext("ctx") so evalExpr's per-message
// timeout can cancel function calls during evaluation. Extra options (e.g.
// expr.AsBool() for boolean conditions) are appended. Keeping the option list in
// one place keeps the sandbox contract consistent across every node.
func compileExpr(src string, extra ...expr.Option) (*vm.Program, error) {
	opts := append([]expr.Option{
		expr.AllowUndefinedVariables(),
		expr.MaxNodes(DefaultExprMaxNodes),
		expr.WithContext("ctx"),
	}, extra...)
	return expr.Compile(src, opts...)
}

// evalExpr runs a compiled expression against the message with a bounded
// timeout. WithContext("ctx") lets expr honour the deadline during function
// calls, and running expr.Run in a goroutine guarantees the caller returns even
// if a pathological expression gets stuck. Shared by the transform, filter,
// switch and set-var nodes.
func evalExpr(ctx context.Context, program *vm.Program, msg *types.Message) (interface{}, error) {
	runCtx, cancel := context.WithTimeout(ctx, DefaultExprTimeout)
	defer cancel()

	env := map[string]interface{}{
		"payload":   msg.Payload,
		"meta":      msg.Meta,
		"topic":     msg.Topic,
		"timestamp": msg.Timestamp.Format(time.RFC3339),
		"id":        msg.ID,
		"ctx":       runCtx,
	}
	if env["meta"] == nil {
		env["meta"] = map[string]interface{}{}
	}

	type exprResult struct {
		val interface{}
		err error
	}
	resCh := make(chan exprResult, 1)
	go func() {
		val, err := expr.Run(program, env)
		resCh <- exprResult{val: val, err: err}
	}()

	select {
	case <-runCtx.Done():
		return nil, fmt.Errorf("expression evaluation timed out after %s", DefaultExprTimeout)
	case r := <-resCh:
		return r.val, r.err
	}
}

// Process evaluates the expression against the message and returns the result.
func (p *TransformProcess) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	result, err := evalExpr(ctx, p.program, msg)
	if err != nil {
		return nil, fmt.Errorf("expression evaluation failed: %w", err)
	}

	outputMsg := msg.Clone()
	outputMsg.Payload = result
	outputMsg.PayloadType = inferDataType(result)
	outputMsg.SourcePort = "output"
	outputMsg.SetMeta("transformed", true)
	outputMsg.SetMeta("expression", p.expression)

	return []*types.Message{outputMsg}, nil
}

// Ports returns the port definitions for this node.
func (p *TransformProcess) Ports() (inputs []types.Port, outputs []types.Port) {
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
func (p *TransformProcess) Validate() error {
	if p.expression == "" {
		return fmt.Errorf("expression is required")
	}
	if p.program == nil {
		return fmt.Errorf("expression failed to compile")
	}
	return nil
}

// GetConfigSchema returns the configuration schema for UI.
func (p *TransformProcess) GetConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"expression": {
				Type:        "string",
				Title:       "Expression",
				Description: "Expression to evaluate (e.g., payload.price * 1.21, upper(payload.name))",
				Default:     "payload",
			},
		},
		Required: []string{"expression"},
	}
}

// TransformProcessInfo returns the node type info for registration.
func TransformProcessInfo() node.NodeTypeInfo {
	return node.NodeTypeInfo{
		Type:        "process-transform",
		Name:        "Transform",
		Description: "Transform payload using expressions",
		Documentation: `## Transform

Transforms message payloads using powerful expressions. Built on expr-lang for safe, sandboxed evaluation.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| expression | string | payload | Expression to evaluate |

## Available Variables

| Variable | Type | Description |
|----------|------|-------------|
| payload | any | Message payload |
| meta | object | Message metadata |
| topic | string | Message topic |
| timestamp | string | ISO timestamp |
| id | string | Message ID |

## Expression Examples

**Access fields:**
` + "```" + `
payload.user.name
meta.requestId
` + "```" + `

**Math operations:**
` + "```" + `
payload.price * 1.21
payload.quantity * payload.unitPrice
` + "```" + `

**String operations:**
` + "```" + `
upper(payload.name)
payload.firstName + " " + payload.lastName
len(payload.items)
` + "```" + `

**Conditionals:**
` + "```" + `
payload.age >= 18 ? "adult" : "minor"
payload.status == "active"
` + "```" + `

**Create new objects:**
` + "```" + `
{"name": payload.user, "total": payload.qty * payload.price}
` + "```" + `

**Array operations:**
` + "```" + `
payload.items[0]
len(payload.items)
filter(payload.items, {.price > 100})
map(payload.items, {.name})
` + "```" + `

## Builtin Functions

| Function | Description |
|----------|-------------|
| len(x) | Length of string/array |
| upper(s) | Uppercase string |
| lower(s) | Lowercase string |
| trim(s) | Remove whitespace |
| contains(s, sub) | Check substring |
| filter(arr, pred) | Filter array |
| map(arr, fn) | Transform array |
| max(arr) | Maximum value |
| min(arr) | Minimum value |
| sum(arr) | Sum of array |

## Output

The expression result becomes the new payload. Output metadata:

- **meta.transformed** - Always true
- **meta.expression** - The expression used

## Use Cases

- Data mapping and reshaping
- Calculations and aggregations
- Conditional transformations
- Field extraction
`,
		Category: types.NodeCategoryProcessing,
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
		Icon: "wand-2",
	}
}

func init() {
	info := TransformProcessInfo()
	if err := node.Register(info, NewTransformProcess); err != nil {
		panic("failed to register transform process: " + err.Error())
	}
}
