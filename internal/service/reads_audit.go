package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
)

// Names under which agent reads are audited: the MCP read tools, and the
// diagnose_flow prompt under a name no tool can have.
const (
	ReadToolListFlows      = "pw_list_flows"
	ReadToolGetFlow        = "pw_get_flow"
	ReadToolListRuns       = "pw_list_runs"
	ReadToolGetRun         = "pw_get_run"
	ReadToolSystemStatus   = "pw_system_status"
	ReadPromptDiagnoseFlow = "prompt:diagnose_flow"
)

// readArgs are the arguments of an audited read.
type readArgs struct {
	FlowID string `json:"flowId,omitempty"`
	RunID  string `json:"runId,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Before string `json:"before,omitempty"`
}

// audited records an agent's read in the MCP audit once it ends, as Tools
// records a call: the token, the read (tool), the flow and run it named and
// its arguments (known secrets redacted), the outcome (completed, the
// refusal's code, or failed) and its duration. Use it as
// defer r.audited(actor, tool, args, r.now())(&err).
func (r *Reads) audited(actor Actor, tool string, args readArgs, start time.Time) func(*error) {
	return func(errp *error) {
		if r.audit == nil || !actor.isAgent() {
			return
		}
		red := r.redactor()
		entry := storage.MCPAuditEntry{
			TS:        start,
			TokenID:   actor.TokenID,
			TokenName: actor.TokenName,
			Tool:      tool,
			FlowID:    capText(red.text(args.FlowID), maxViewIDBytes),
			RunID:     capText(red.text(args.RunID), maxViewIDBytes),
			Outcome:   "completed",
			DurMs:     r.now().Sub(start).Milliseconds(),
		}
		if data, err := json.Marshal(args); err == nil {
			sum := sha256.Sum256(data)
			entry.ArgsSHA256 = hex.EncodeToString(sum[:])
		}
		if data, err := marshalNoEscape(args); err == nil {
			entry.ArgsPreview = capText(string(red.json(data)), storage.MaxMCPAuditPreview)
		}
		var te *ToolError
		switch err := *errp; {
		case errors.As(err, &te):
			entry.Outcome = te.Code()
		case err != nil:
			entry.Outcome = "failed"
		}
		r.audit.Record(entry)
	}
}
