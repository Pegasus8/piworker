package flow

import (
	"context"
	"maps"
	"slices"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/llm"
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
// (node.ConnectionRequirer) that is not defined (IssueUndefinedConnection),
// and an LLM connection a node names (node.LLMConnectionRequirer) that is
// not defined (IssueUndefinedLLMConnection). The dry-run does not call it.
func checkRequiredSecrets(ctx context.Context, rt *FlowRuntime) error {
	return firstError(environmentFindings(ctx, rt.flow, rt.instancesByIndex()))
}

// environmentFindings are the findings that depend on the stores of ctx
// rather than on the definition (EnvironmentIssue), in this order: the
// secrets the strict settings cannot work without, the undefined secrets
// of optional settings (warnings), the broker connections (ADR-0026) and
// the LLM connections (ADR-0038) nodes name. Deploy refuses the first error
// among them (checkRequiredSecrets) and Lint reports them all: one list,
// so a flow Lint finds clean is not refused here. The nodes resolve the
// LLM connections again on every call: this only refuses a flow that
// could never work.
func environmentFindings(ctx context.Context, f *Flow, instances map[int]node.Node) []finding {
	store := secrets.FromContext(ctx)
	return slices.Concat(
		requiredSecretFindings(f, instances, store),
		unresolvedSecretFindings(f, instances, store),
		requiredConnectionFindings(f, instances, brokers.FromContext(ctx)),
		requiredLLMConnectionFindings(f, instances, llm.FromContext(ctx)),
	)
}

// instancesByIndex returns the runtime's node instances keyed by their
// index in its flow, the shape Lint's collectors take.
func (r *FlowRuntime) instancesByIndex() map[int]node.Node {
	instances := make(map[int]node.Node, len(r.nodes))
	for i := range r.flow.Nodes {
		n := &r.flow.Nodes[i]
		if inst, ok := r.nodes[n.ID]; ok && r.nodeDefs[n.ID] == n {
			instances[i] = inst
		}
	}
	return instances
}

// DeployWarning is a warning a deploy went ahead with: the Issue as Lint
// renders it and, for an undefined secret (IssueUnresolvedSecret), the
// secret's name, so the editor need not read it out of the message. Only the
// editor's deploy response carries it: an agent sees Issues, whose messages
// are withheld where they name what it cannot read (ADR-0021).
type DeployWarning struct {
	Issue
	Secret string `json:"secret,omitempty"`
}

// deployWarnings are the warnings a deploy goes ahead with, as Lint renders
// them: the tool risk warnings (toolIssues, validateDeployable's) and the
// undefined secrets of optional settings (IssueUnresolvedSecret), checked
// against the store of ctx with the instances the deploy already built.
// Never nil.
func deployWarnings(ctx context.Context, rt *FlowRuntime, reg *node.Registry, toolIssues []Issue) []DeployWarning {
	out := []DeployWarning{}
	for _, is := range toolIssues {
		if is.Severity == IssueWarning {
			out = append(out, DeployWarning{Issue: is})
		}
	}
	for _, fd := range unresolvedSecretFindings(rt.flow, rt.instancesByIndex(), secrets.FromContext(ctx)) {
		for _, is := range lintIssues(rt.flow, reg, []finding{fd}) {
			out = append(out, DeployWarning{Issue: is, Secret: fd.secret})
		}
	}
	return out
}

// ConnectionsUsed returns the broker connections (ADR-0026) the runtime's
// nodes name (node.ConnectionRequirer), sorted, each once: what a change to
// one of them must restart (ADR-0029). It asks the running instances, so it
// is the definition that runs, not a later stored one.
func (r *FlowRuntime) ConnectionsUsed() []string { return r.namesUsed(brokerConnectionsOf) }

// LLMConnectionsUsed returns the LLM connections (ADR-0038) the runtime's
// nodes name (node.LLMConnectionRequirer), sorted, each once. A change to
// one restarts nothing (the nodes resolve it per call); the API refuses to
// delete one while a running flow uses it. It asks the running instances,
// so it is the definition that runs, not a later stored one.
func (r *FlowRuntime) LLMConnectionsUsed() []string { return r.namesUsed(llmConnectionsOf) }

// namesUsed returns every name of gives for the runtime's nodes, sorted,
// each once.
func (r *FlowRuntime) namesUsed(of func(node.Node) map[string]string) []string {
	set := map[string]bool{}
	for _, in := range r.nodes {
		for _, name := range of(in) {
			set[name] = true
		}
	}
	return slices.Sorted(maps.Keys(set))
}

// brokerConnectionsOf returns the broker connections a node instance names
// (node.ConnectionRequirer), by setting; nil for other nodes.
func brokerConnectionsOf(in node.Node) map[string]string {
	if req, ok := in.(node.ConnectionRequirer); ok {
		return req.RequiredConnections()
	}
	return nil
}

// llmConnectionsOf returns the LLM connections a node instance names
// (node.LLMConnectionRequirer), by setting; nil for other nodes.
func llmConnectionsOf(in node.Node) map[string]string {
	if req, ok := in.(node.LLMConnectionRequirer); ok {
		return req.RequiredLLMConnections()
	}
	return nil
}

// Warnings returns the warnings the runtime deployed with (deployWarnings).
func (r *FlowRuntime) Warnings() []DeployWarning {
	return append([]DeployWarning{}, r.warnings...)
}
