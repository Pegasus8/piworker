package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
)

// Who may author and deploy, and the audit of what agents did.

// authorizeAuthor lets agents author when mcp.author_enabled is on and their
// token has the author scope; the editor only when editorToo (Validate,
// DryRun, TestNode: it saves, reads and deploys through its own paths).
func (a *Authoring) authorizeAuthor(ctx context.Context, actor Actor, editorToo bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch {
	case actor.Kind == ActorEditor && editorToo:
		return nil
	case actor.Kind == ActorEditor:
		return toolError(ErrPermissionDenied, "the editor saves, reads and deploys flows through its own pages")
	case !actor.isAgent():
		return toolError(ErrPermissionDenied, "unknown caller")
	case !a.authorEnabled:
		return toolError(ErrPermissionDenied, "agent authoring is turned off (mcp.author_enabled)")
	case !actor.HasScope(storage.ScopeAuthor):
		return toolError(ErrPermissionDenied, "this token does not have the author scope")
	}
	return nil
}

// authorizeStoredRead lets an agent's authoring read a stored flow (by
// flowId, or a draft of an existing flow) only when its token also has the
// read scope: the author scope alone works on the agent's own flows. The
// editor reads every flow.
func authorizeStoredRead(actor Actor) error {
	if actor.isAgent() && !actor.HasScope(storage.ScopeRead) {
		return toolError(ErrPermissionDenied, "this token does not have the read scope, which working on a stored flow needs (a flowId, or a draft of an existing flow)")
	}
	return nil
}

// authorizeEditor lets only the editor call the methods of the approval
// pages and the version history.
func authorizeEditor(ctx context.Context, actor Actor) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if actor.Kind != ActorEditor {
		return toolError(ErrPermissionDenied, "only the admin, in the editor, may do this")
	}
	return nil
}

// authorArgs are what the audit records of an authoring or deploy
// operation: references, never payloads (flows, ops, inputs).
type authorArgs struct {
	FlowID        string `json:"flowId,omitempty"`
	DraftID       string `json:"draftId,omitempty"`
	BaseVersion   int    `json:"baseVersion,omitempty"`
	Version       int    `json:"version,omitempty"`
	Ops           int    `json:"ops,omitempty"`
	Type          string `json:"type,omitempty"`
	TriggerNodeID string `json:"triggerNodeId,omitempty"`
}

// audited records an agent's authoring or deploy operation in the MCP audit
// once it ends: the token, the operation (tool), the flow, the references
// in args (which the operation may complete, e.g. with the draft it
// created), the outcome (completed, the refusal's code, or failed) and its
// duration. privileged, when set by the operation, marks the row privileged
// (a deploy or stop of a privileged flow that reached the lifecycle path),
// which the row cap never evicts. Busy and invalid arguments are not rows:
// the MCP endpoint counts them per token and minute (mcp_rejections), once,
// as it counts every built-in tool's, so a loop of refused requests cannot
// fill the audit. Use it as defer
// a.audited(actor, tool, args, a.now(), &priv)(&err).
func (a *Authoring) audited(actor Actor, tool string, args *authorArgs, start time.Time, privileged *bool) func(*error) {
	return func(errp *error) {
		if a.audit == nil || !actor.isAgent() {
			return
		}
		var te *ToolError
		if errors.As(*errp, &te) && (te.Kind == ErrBusy || te.Kind == ErrInvalidArguments) {
			return // counted by the endpoint
		}
		red := a.views.redactor()
		entry := storage.MCPAuditEntry{
			TS:        start,
			TokenID:   actor.TokenID,
			TokenName: actor.TokenName,
			Tool:      tool,
			FlowID:    capText(red.text(args.FlowID), maxViewIDBytes),
			Outcome:   "completed",
			DurMs:     a.now().Sub(start).Milliseconds(),
		}
		if privileged != nil {
			entry.Privileged = *privileged
		}
		if data, err := json.Marshal(args); err == nil {
			sum := sha256.Sum256(data)
			entry.ArgsSHA256 = hex.EncodeToString(sum[:])
		}
		if data, err := marshalNoEscape(args); err == nil {
			entry.ArgsPreview = capText(string(red.json(data)), storage.MaxMCPAuditPreview)
		}
		switch err := *errp; {
		case te != nil:
			entry.Outcome = te.Code()
		case err != nil:
			entry.Outcome = "failed"
		}
		a.audit.Record(entry)
	}
}
