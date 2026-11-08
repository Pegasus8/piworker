package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/storage"
)

// ExecuteApproval runs an approved tool call (api.ApprovalExecutor) as the
// token that requested it, with the stored arguments, and stores its
// CallResult on the approval so the agent can fetch it with Result. The
// token must still be active, the tool must still be the one the admin was
// shown (same name and fingerprint), and the token's permission is checked
// again against it. A call that cannot run is stored as a failed (or, for a
// revoked token, rejected) result and returned as an error.
func (t *Tools) ExecuteApproval(ctx context.Context, a storage.AgentApproval) error {
	if a.Kind != storage.ApprovalKindToolCall {
		return toolError(ErrUnavailable, fmt.Sprintf("%s approvals are not run by the tool service", a.Kind))
	}
	if t.store == nil {
		return toolError(ErrUnavailable, "approvals are not available")
	}
	tok, err := t.store.GetAPIToken(a.TokenID)
	if err != nil && !errors.Is(err, storage.ErrAPITokenNotFound) {
		return fmt.Errorf("read the requesting token: %w", err)
	}
	if err != nil || !tok.Active(t.now()) {
		msg := "the token that asked for this call was revoked or has expired"
		t.storeApprovalResult(a.ID, flow.CallResult{Status: flow.CallStatusRejected, RunID: a.ID, Error: &flow.CallError{Message: msg}})
		return toolError(ErrPermissionDenied, msg)
	}
	var args map[string]any
	if len(a.Args) > 0 {
		if err := json.Unmarshal(a.Args, &args); err != nil {
			msg := "the stored arguments cannot be read"
			t.storeApprovalResult(a.ID, flow.CallResult{Status: flow.CallStatusError, RunID: a.ID, Error: &flow.CallError{Message: msg}})
			return toolError(ErrInvalidArguments, msg)
		}
	}
	actor := actorFromToken(tok)
	res, err := t.call(ctx, actor, ToolByNode(a.FlowID, a.Ref), args, &approvedTool{id: a.ID, name: a.ToolName, fingerprint: a.ToolFingerprint})
	if res.RunID == "" {
		msg := "the approved call did not run"
		if err != nil {
			msg = "the approved call did not run: " + err.Error()
		}
		t.storeApprovalResult(a.ID, flow.CallResult{Status: flow.CallStatusError, RunID: a.ID, Error: &flow.CallError{Message: capText(msg, maxErrorMessageBytes)}})
		if err == nil {
			err = toolError(ErrUnavailable, msg)
		}
		return err
	}
	// call stored the result on the approval (and stores the final one
	// when a run that reported running ends).
	return nil
}

func (t *Tools) storeApprovalResult(id string, res flow.CallResult) {
	data, err := json.Marshal(res)
	if err == nil {
		err = t.store.SetAgentApprovalResult(id, data)
	}
	if err != nil {
		t.log.Error().Err(err).Str("approvalId", id).Msg("Cannot store the result of an approved tool call")
	}
}

// approvalResult reports on an approval created by an agent's confirm:admin
// call: waiting, rejected, expired, or the stored result once it ran.
func (t *Tools) approvalResult(ctx context.Context, actor Actor, id string) (flow.CallResult, error) {
	notFound := toolError(ErrRunNotFound, "unknown run")
	a, err := t.store.GetAgentApproval(id)
	if errors.Is(err, storage.ErrAgentApprovalNotFound) {
		return flow.CallResult{}, notFound
	}
	if err != nil {
		return flow.CallResult{}, fmt.Errorf("read approval: %w", err)
	}
	if a.Kind != storage.ApprovalKindToolCall || a.TokenID != actor.TokenID {
		return flow.CallResult{}, notFound
	}
	switch a.EffectiveStatus(t.now()) {
	case storage.ApprovalPending:
		return flow.CallResult{Status: flow.CallStatusApprovalRequired, RunID: a.ID}, nil
	case storage.ApprovalExpired:
		return flow.CallResult{Status: flow.CallStatusExpired, RunID: a.ID}, nil
	case storage.ApprovalRejected:
		res := flow.CallResult{Status: flow.CallStatusRejected, RunID: a.ID}
		if a.DecidedBy == storage.ApprovalDecidedByTokenRevoked || a.DecidedBy == storage.ApprovalDecidedByTokenExpired {
			res.Error = &flow.CallError{Message: "the request was voided: " + a.DecidedBy}
		}
		return res, nil
	}
	if len(a.Result) == 0 {
		// Approved and being run right now.
		return flow.CallResult{Status: flow.CallStatusRunning, RunID: a.ID}, nil
	}
	var res flow.CallResult
	if err := json.Unmarshal(a.Result, &res); err != nil {
		return flow.CallResult{}, fmt.Errorf("read approval result: %w", err)
	}
	if res.Status == flow.CallStatusRunning {
		if live, ok, err := t.runResult(ctx, actor, res.RunID); ok && err == nil {
			return live, nil
		}
	}
	return res, nil
}
