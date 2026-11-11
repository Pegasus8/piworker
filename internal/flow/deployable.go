package flow

import (
	"context"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
)

// validateDeployable runs the checks deploy runs on a flow once its runtime
// is built (rt holds the instances, not started): the structure against the
// instances' ports (which nodes are triggers is decided server-side, by the
// instance implementing node.TriggerNode or the registered category, never
// by the client-supplied Category), callable triggers, and the tool risk
// analysis (ADR-0013), whose tools and warning issues it returns; error
// issues fail it (a *ValidationError). Its checks are Lint's collectors
// (structureFindings, callTriggerFindings, analyzeTools), so Lint, and the
// dry-run and authoring that use it, refuse exactly the flows deploy refuses
// (ADR-0007).
func validateDeployable(rt *FlowRuntime, reg *node.Registry) ([]ToolInfo, []Issue, error) {
	f := rt.flow
	if err := f.validate(func(n *Node) bool {
		if _, ok := rt.triggers[n.ID]; ok {
			return true
		}
		info, ok := reg.Get(n.Type)
		return ok && info.Category == NodeCategoryInput
	}); err != nil {
		return nil, nil, err
	}
	if err := rt.ValidateCallTriggers(); err != nil {
		return nil, nil, err
	}
	specs := rt.toolSpecs()
	if len(specs) == 0 {
		return nil, nil, nil
	}
	tools, issues := AnalyzeTools(f, reg, specs)
	if err := issueError(issues); err != nil {
		return nil, nil, err
	}
	return tools, issues, nil
}

// checkRequiredSecrets refuses a deploy (a *ValidationError naming the node,
// the setting and the secret, never a value) while a secret a node requires
// (node.SecretRequirer) is not defined in the store of ctx
// (secrets.FromContext: the process-wide store for a deploy). It is Lint's
// collector (requiredSecretFindings), so Lint reports the same secrets
// (IssueUndefinedSecret). It also refuses a broker connection a node names
// (node.ConnectionRequirer) that is not defined (IssueUndefinedConnection).
// The dry-run does not call it.
func checkRequiredSecrets(ctx context.Context, rt *FlowRuntime) error {
	instances := make(map[int]node.Node, len(rt.nodes))
	for i := range rt.flow.Nodes {
		n := &rt.flow.Nodes[i]
		if inst, ok := rt.nodes[n.ID]; ok && rt.nodeDefs[n.ID] == n {
			instances[i] = inst
		}
	}
	if err := firstError(requiredSecretFindings(rt.flow, instances, secrets.FromContext(ctx))); err != nil {
		return err
	}
	// The broker connections nodes name (ADR-0026), from the store of ctx.
	return firstError(requiredConnectionFindings(rt.flow, instances, brokers.FromContext(ctx)))
}
