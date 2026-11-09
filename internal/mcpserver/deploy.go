package mcpserver

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DeployService serves agent deploys (*service.Authoring): the agent deploy
// level, the token re-check and the editor's lifecycle path are the
// service's.
type DeployService interface {
	Deploy(ctx context.Context, actor service.Actor, req service.DeployRequest) (service.DeployResult, error)
	Stop(ctx context.Context, actor service.Actor, flowID string) (service.DeployResult, error)
}

var _ DeployService = (*service.Authoring)(nil)

// Names of the deploy tools (scope deploy).
const (
	DeployFlowTool = service.DeployToolDeployFlow
	StopFlowTool   = service.DeployToolStopFlow
)

var (
	// deployAnnotations: a deployed flow acts, possibly outside PiWorker.
	deployAnnotations = flow.Annotations{ReadOnlyHint: false, DestructiveHint: true, IdempotentHint: false, OpenWorldHint: true}
	// stopAnnotations: stopping halts an automation; stopping again changes
	// nothing more.
	stopAnnotations = flow.Annotations{ReadOnlyHint: false, DestructiveHint: true, IdempotentHint: true, OpenWorldHint: false}
)

// deployLevelText says what the levels allow, for the descriptions.
const deployLevelText = "PiWorker's agent deploy level decides: low allows flows whose risk is at most external and that, " +
	"compared with the version the admin last approved, add no privileged node and change no destination, host, secret " +
	"reference, risk switch or credentialed request; all allows any flow. A refusal says which alert kinds stood in the way."

// DeployTools returns the deploy tools (scope deploy; listed only while the
// agent deploy level is above off): pw_deploy_flow{flowId, draftId?} and
// pw_stop_flow{flowId}.
func DeployTools(deploys DeployService) []StaticTool {
	return []StaticTool{
		{
			Name:  DeployFlowTool,
			Title: "Deploy a flow",
			Description: "Deploys a flow as this token: starts it, or restarts it when draftId applies your pending draft of " +
				"it first (saved as a version the admin did not approve). Without draftId the current version starts (a " +
				"running flow is a conflict). " + deployLevelText + " A flow with errors is refused with error.flowIssues, a " +
				"flow changed meanwhile with conflict. The admin sees every agent deploy and can roll back.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` + flowIDProperty + `,` +
				`"draftId":{"type":"string","pattern":` + idPatternJSON + `,"maxLength":256,` +
				`"description":"One of your pending drafts of the flow, from pw_save_draft, to apply first."}},"required":["flowId"]}`),
			OutputSchema: json.RawMessage(deployResultSchema),
			Annotations:  deployAnnotations,
			Scope:        storage.ScopeDeploy,
			Handler: func(ctx context.Context, actor service.Actor, args map[string]any) (*mcp.CallToolResult, error) {
				res, err := deploys.Deploy(ctx, actor, service.DeployRequest{FlowID: stringArg(args, "flowId"), DraftID: stringArg(args, "draftId")})
				return structured(res, lifecycleRefusal(err))
			},
		},
		{
			Name:  StopFlowTool,
			Title: "Stop a flow",
			Description: "Stops a running flow as this token (a flow that is not running is a conflict). The same agent deploy " +
				"level as pw_deploy_flow applies to the flow being stopped. " + deployLevelText,
			InputSchema:  json.RawMessage(`{"type":"object","properties":{` + flowIDProperty + `},"required":["flowId"]}`),
			OutputSchema: json.RawMessage(deployResultSchema),
			Annotations:  stopAnnotations,
			Scope:        storage.ScopeDeploy,
			Handler: func(ctx context.Context, actor service.Actor, args map[string]any) (*mcp.CallToolResult, error) {
				res, err := deploys.Stop(ctx, actor, stringArg(args, "flowId"))
				return structured(res, lifecycleRefusal(err))
			},
		},
	}
}

// lifecycleRefusal maps a lifecycle refusal that reached the endpoint
// unmapped as the service maps it (service.LifecycleError.ToolError: never
// deploy's own message, which may quote settings); other errors are kept.
func lifecycleRefusal(err error) error {
	var le *service.LifecycleError
	if errors.As(err, &le) {
		if te := le.ToolError(); te != nil {
			return te
		}
	}
	return err
}
