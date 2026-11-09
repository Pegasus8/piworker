package flow

import (
	"context"
	"sort"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
)

// validateDeployable runs the checks deploy runs on a flow once its runtime
// is built (rt holds the instances, not started): the structure against the
// instances' ports (which nodes are triggers is decided server-side, by the
// instance implementing node.TriggerNode or the registered category, never
// by the client-supplied Category), callable triggers, and the tool risk
// analysis (ADR-0013), whose tools and warning issues it returns; error
// issues fail it (a *ValidationError). RuntimeManager.deploy and the dry-run
// share it, so the dry-run refuses exactly the flows deploy refuses
// (ADR-0007); it is the one place to switch to flow.Lint.
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
// (secrets.FromContext: the process-wide store for a deploy). The dry-run
// does not call it.
func checkRequiredSecrets(ctx context.Context, rt *FlowRuntime) error {
	store := secrets.FromContext(ctx)
	ids := make([]string, 0, len(rt.nodes))
	for id := range rt.nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		req, ok := rt.nodes[id].(node.SecretRequirer)
		if !ok {
			continue
		}
		required := req.RequiredSecrets()
		props := make([]string, 0, len(required))
		for prop := range required {
			props = append(props, prop)
		}
		sort.Strings(props)
		for _, prop := range props {
			for _, name := range required[prop] {
				if _, defined := store.Get(name); !defined {
					nodeType := ""
					if def := rt.nodeDefs[id]; def != nil {
						nodeType = def.Type
					}
					return NewValidationError("node %s (%s): config.%s references secret %q, which is not defined", id, nodeType, prop, name)
				}
			}
		}
	}
	return nil
}
