package builtin

import (
	"context"
	"errors"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog/log"
)

// Reply modes of action-reply.
const (
	replyModeResult = "result"
	replyModeError  = "error"
)

// ReplyAction answers the call that started its run (action-reply): the
// message payload becomes the call's result, or its error in error mode.
// The first reply of a run wins; outside a called run it does nothing.
type ReplyAction struct {
	*node.BaseNode
	isError bool
}

// NewReplyAction creates an action-reply node.
func NewReplyAction(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)
	isError, err := parseReplyMode(base.GetConfig())
	if err != nil {
		return nil, err
	}
	return &ReplyAction{BaseNode: base, isError: isError}, nil
}

func parseReplyMode(cfg map[string]any) (bool, error) {
	v, present := cfg["mode"]
	if !present || v == nil || v == "" {
		return false, nil
	}
	switch v {
	case replyModeResult:
		return false, nil
	case replyModeError:
		return true, nil
	}
	return false, errors.New(`mode must be "result" or "error"`)
}

// Process delivers the payload to the waiting caller and emits nothing.
func (a *ReplyAction) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	corrID := ""
	if v, ok := msg.GetMeta("correlationID"); ok {
		corrID, _ = v.(string)
	}
	if !flow.ReplyFromContext(ctx).Reply(corrID, msg.Payload, a.isError) {
		log.Debug().
			Str("correlationID", corrID).
			Msg("action-reply: reply not delivered (the run was not started by a call, the call already has a result, or the payload is not serializable or too large)")
	}
	return nil, nil
}

// Ports: one input, no outputs (the reply ends the call).
func (a *ReplyAction) Ports() (inputs []types.Port, outputs []types.Port) {
	return replyActionInputs(), nil
}

func replyActionInputs() []types.Port {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny}}
}

// Validate re-checks the mode.
func (a *ReplyAction) Validate() error {
	_, err := parseReplyMode(a.GetConfig())
	return err
}

// GetConfigSchema returns the configuration schema for the editor.
func (a *ReplyAction) GetConfigSchema() node.ConfigSchema {
	return replyActionConfigSchema()
}

func replyActionConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"mode": {
				Type:         "string",
				Title:        "Mode",
				Description:  "result: the payload is the tool's result; error: the call fails with the payload as its error",
				Default:      replyModeResult,
				Enum:         []string{replyModeResult, replyModeError},
				AgentVisible: true,
			},
		},
	}
}

// ReplyActionInfo returns the node type info for registration.
func ReplyActionInfo() node.NodeTypeInfo {
	schema := replyActionConfigSchema()
	return node.NodeTypeInfo{
		Type:          "action-reply",
		Name:          "Agent Reply",
		Description:   "Answer the agent tool call that started this run",
		Documentation: replyActionDocumentation,
		Category:      types.NodeCategoryOutput,
		Risk:          node.RiskSafe,
		Inputs:        replyActionInputs(),
		Outputs:       nil,
		Config:        &schema,
		Icon:          "reply",
	}
}

func init() {
	if err := node.Register(ReplyActionInfo(), NewReplyAction); err != nil {
		panic("failed to register reply action: " + err.Error())
	}
}

const replyActionDocumentation = `## Agent Reply

Answers the call of an **Agent Tool** flow: the message **payload** becomes
the result the agent receives (as JSON). Place it where the flow has the
answer, for example after a Transform that builds
` + "`{\"moved\": true, \"door\": payload.door}`" + `.

## Configuration

| Property | Default | Description |
|----------|---------|-------------|
| mode | result | ` + "`result`" + `: the payload is the tool's result. ` + "`error`" + `: the call fails and the payload is its error (the agent sees an error result) |

## Behaviour

- The **first** reply of a run wins; later replies of the same run are
  ignored, so put one Agent Reply on each branch that can finish the call.
- The payload is copied when the reply is sent: nodes after this point
  cannot change it.
- Replies larger than 256 KiB, or that cannot be encoded as JSON, are
  refused and the call ends with an error (unless another reply wins).
  PiWorker may also shorten long results before passing them to the agent.
- If the run ends without a reply, the call completes with no result, or
  with the error of the first node that failed.
- A reply that arrives after the call's timeout is kept, and the agent
  fetches it later.
- In runs not started by a call (another trigger, a manual inject) the node
  does nothing.

This node has no outputs.
`
