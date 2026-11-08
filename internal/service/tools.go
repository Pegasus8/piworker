package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/Pegasus8/piworker/internal/config"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/redact"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/Pegasus8/piworker/internal/toolschema"
	"github.com/rs/zerolog"
)

// Defaults of the call limits.
const (
	// DefaultPerTokenInflight is how many calls one token may run at once.
	DefaultPerTokenInflight = 2
	// maxErrorMessageBytes bounds a CallError message shown to callers.
	maxErrorMessageBytes = 1024
)

// Runtimes is what the service needs from the flow runtime manager
// (*flow.RuntimeManager).
type Runtimes interface {
	Tools() (uint64, []flow.ToolInfo)
	GetRuntime(flowID string) (*flow.FlowRuntime, bool)
}

// Store is the persistence the tool service needs (*storage.SQLiteStore):
// token grants, approvals and their results.
type Store interface {
	GetAPIToken(id string) (*storage.APIToken, error)
	UpdateAPITokenGrants(id string, fn func([]storage.TokenGrant) ([]storage.TokenGrant, error)) (*storage.APIToken, error)
	CreateAgentApproval(a storage.AgentApproval, now time.Time) (*storage.AgentApproval, error)
	GetAgentApproval(id string) (*storage.AgentApproval, error)
	SetAgentApprovalResult(id string, result json.RawMessage) error
}

// TokenEvictor drops a token from the verifier's cache after its grants
// changed (*agentauth.TokenVerifier).
type TokenEvictor interface {
	Evict(id string)
}

// Options configures Tools.
type Options struct {
	// Runtimes provides the running flows and their tools (required).
	Runtimes Runtimes
	// Store holds grants and approvals; without it agents' confirm:admin
	// calls and grant suspension are unavailable.
	Store Store
	// Secrets are redacted from results and errors (default
	// secrets.DefaultStore).
	Secrets SecretSource
	// Audit records agent calls (nil: no audit).
	Audit *AuditWriter
	// Tokens is told when a token's grants changed (optional).
	Tokens TokenEvictor
	// MaxInflight bounds calls running at once across all tools
	// (mcp.max_inflight; default from config.DefaultMCP).
	MaxInflight int
	// MaxResultBytes caps the serialized result (mcp.max_result_bytes;
	// default from config.DefaultMCP).
	MaxResultBytes int
	// PerTokenInflight bounds the calls of one token (default
	// DefaultPerTokenInflight).
	PerTokenInflight int
	Now              func() time.Time
	Logger           zerolog.Logger
}

// Tools calls published flow tools for agents (MCP) and for the editor (the
// REST twin), enforcing permissions, the input schema, the confirm policy,
// concurrency limits and cooldowns, and redacting and capping results.
type Tools struct {
	runtimes       Runtimes
	store          Store
	secrets        SecretSource
	audit          *AuditWriter
	tokens         TokenEvictor
	maxResultBytes int
	now            func() time.Time
	log            zerolog.Logger
	slots          *slots
	results        *resultStore
}

// NewTools returns the tool service.
func NewTools(opts Options) *Tools {
	defaults := config.DefaultMCP()
	t := &Tools{
		runtimes:       opts.Runtimes,
		store:          opts.Store,
		secrets:        opts.Secrets,
		audit:          opts.Audit,
		tokens:         opts.Tokens,
		maxResultBytes: opts.MaxResultBytes,
		now:            opts.Now,
		log:            opts.Logger,
	}
	if t.secrets == nil {
		t.secrets = secrets.DefaultStore
	}
	if t.maxResultBytes <= 0 {
		t.maxResultBytes = defaults.MaxResultBytes
	}
	if t.now == nil {
		t.now = time.Now
	}
	maxInflight := opts.MaxInflight
	if maxInflight <= 0 {
		maxInflight = defaults.MaxInflight
	}
	perToken := opts.PerTokenInflight
	if perToken <= 0 {
		perToken = DefaultPerTokenInflight
	}
	t.slots = newSlots(maxInflight, perToken, t.now)
	t.results = newResultStore(t.now, maxKeptResults, maxKeptResultBytes)
	return t
}

// Call calls a tool with args. The editor may call any running tool and is
// never held for approval; an agent needs the call scope and a grant whose
// fingerprint matches the tool (or all_tools, which never covers privileged
// tools), and the privileged flag for privileged tools. Arguments outside the
// input schema, a full limit or a running cooldown refuse the call with a
// *ToolError and start nothing. An agent's call of a confirm:admin tool
// creates an approval and returns Status "approval_required" with the
// approval's ID as RunID.
//
// Otherwise a run starts; Call returns its (redacted, size-capped) result,
// or Status "running" when the tool's timeout passed first (see Result). The
// call's slot is held until the run ends. An error with a non-empty RunID
// (ctx ended while waiting) still started a run.
func (t *Tools) Call(ctx context.Context, actor Actor, ref ToolRef, args map[string]any) (flow.CallResult, error) {
	return t.call(ctx, actor, ref, args, nil)
}

// approvedTool is the tool an approved request was made for, as recorded on
// its approval (id): the call runs only that version of the tool.
type approvedTool struct {
	id, name, fingerprint string
}

// call is Call. approved (an approved request) skips the confirm policy,
// refuses a tool whose name or fingerprint differs from the approved one,
// and stores the call's result on the approval (the final one too, once a
// run that reported running ends).
func (t *Tools) call(ctx context.Context, actor Actor, ref ToolRef, args map[string]any, approved *approvedTool) (res flow.CallResult, err error) {
	start := t.now()
	var info *flow.ToolInfo
	if actor.isAgent() {
		defer func() { t.record(actor, ref, info, args, res, err, start) }()
	}

	ti, rt, err := t.resolve(ref)
	if err != nil {
		return flow.CallResult{}, err
	}
	if approved != nil && (approved.fingerprint == "" || ti.ToolName != approved.name || ti.Fingerprint != approved.fingerprint) {
		return flow.CallResult{}, toolError(ErrUnavailable, "the tool changed since the request; ask again")
	}
	if err := t.authorize(actor, ti); err != nil {
		if errors.Is(err, ErrToolNotFound) {
			return flow.CallResult{}, err // hidden: audited as unknown
		}
		info = &ti
		return flow.CallResult{}, err
	}
	info = &ti
	if issues := ti.Schema.Validate(args); len(issues) > 0 {
		return flow.CallResult{}, invalidArguments(issues)
	}
	if actor.isAgent() && approved == nil && ti.Confirm == node.ToolConfirmAdmin {
		return t.requestApproval(actor, ti, args)
	}

	release, err := t.slots.acquire(toolKey(ti), ti.MaxConcurrent, ti.Cooldown, actor.slotKey())
	if err != nil {
		return flow.CallResult{}, err
	}
	res, err = rt.Call(ctx, flow.CallRequest{
		TriggerNodeID: ti.TriggerNodeID,
		Args:          args,
		Actor:         actor.runActor(),
		Timeout:       ti.Timeout,
	})
	if res.RunID == "" {
		release(false) // nothing ran: no cooldown
		return flow.CallResult{}, callError(err)
	}
	done := rt.RunDone(res.RunID)
	res = t.finish(res)
	running := res.Status == flow.CallStatusRunning
	t.results.put(res.RunID, &callRecord{
		owner:  actor.key(),
		rt:     rt,
		result: res,
		final:  !running,
	}, ti.Timeout)
	approvalID := ""
	if approved != nil {
		approvalID = approved.id
		// Stored before the run's end can store the final result.
		t.storeApprovalResult(approvalID, res)
	}
	go func() {
		<-done
		release(true)
		if running {
			final := t.collect(rt, res.RunID)
			if approvalID != "" {
				t.storeApprovalResult(approvalID, final)
			}
		}
	}()
	return res, err
}

// collectTimeout bounds reading the result of a run that just ended.
const collectTimeout = 5 * time.Second

// collect reads the final result of a run that ended (its RunDone closed)
// after Call reported it running, and keeps it for Result, so it outlives
// the runtime's waiter. It returns the final result kept.
func (t *Tools) collect(rt *flow.FlowRuntime, runID string) flow.CallResult {
	ctx, cancel := context.WithTimeout(context.Background(), collectTimeout)
	defer cancel()
	res, _ := rt.Wait(ctx, runID)
	if res.Status == flow.CallStatusRunning {
		// RunDone closes only once the result is final (or the waiter is
		// gone, which reads as expired); never keep a run "running".
		res = flow.CallResult{Status: flow.CallStatusError, RunID: runID,
			Error: &flow.CallError{Message: "the result of the run could not be read"}}
	}
	return t.results.finalize(runID, t.finish(res))
}

// toolKey identifies a tool for its slots.
func toolKey(info flow.ToolInfo) string { return info.FlowID + "/" + info.TriggerNodeID }

// resolve finds a tool and the runtime that publishes it. The tool is read
// from that runtime, so a redeploy in between cannot mix two versions.
func (t *Tools) resolve(ref ToolRef) (flow.ToolInfo, *flow.FlowRuntime, error) {
	flowID, nodeID := ref.FlowID, ref.TriggerNodeID
	if ref.Name != "" {
		_, tools := t.runtimes.Tools()
		flowID, nodeID = "", ""
		for _, ti := range tools {
			if ti.ToolName == ref.Name {
				flowID, nodeID = ti.FlowID, ti.TriggerNodeID
				break
			}
		}
	}
	notFound := toolError(ErrToolNotFound, fmt.Sprintf("unknown tool %q", ref.String()))
	if flowID == "" || nodeID == "" {
		return flow.ToolInfo{}, nil, notFound
	}
	rt, ok := t.runtimes.GetRuntime(flowID)
	if !ok || !rt.IsRunning() {
		return flow.ToolInfo{}, nil, notFound
	}
	for _, ti := range rt.Tools() {
		if ti.TriggerNodeID == nodeID && (ref.Name == "" || ti.ToolName == ref.Name) {
			return ti, rt, nil
		}
	}
	return flow.ToolInfo{}, nil, notFound
}

// authorize applies the permission rules of ADR-0010 to an agent, and
// suspends a grant whose tool changed since it was granted.
func (t *Tools) authorize(actor Actor, info flow.ToolInfo) error {
	changed, err := access(actor, info)
	if changed {
		t.suspendGrant(actor.TokenID, info)
	}
	return err
}

// checkAccess is authorize without side effects: nil when actor may call
// info now.
func (t *Tools) checkAccess(actor Actor, info flow.ToolInfo) error {
	_, err := access(actor, info)
	return err
}

// access decides whether actor may call info (nil) or why not. changed is
// set when the actor's grant names an older version of the tool (its
// fingerprint differs), which authorize suspends.
func access(actor Actor, info flow.ToolInfo) (changed bool, err error) {
	switch actor.Kind {
	case ActorEditor:
		return false, nil
	case ActorAgent:
	default:
		return false, toolError(ErrPermissionDenied, "unknown caller")
	}
	privileged := info.Risk == node.RiskPrivileged
	var g *storage.TokenGrant
	for i := range actor.Grants {
		if actor.Grants[i].FlowID == info.FlowID && actor.Grants[i].TriggerNodeID == info.TriggerNodeID {
			g = &actor.Grants[i]
			break
		}
	}
	if g == nil && !(actor.AllTools && !privileged) {
		// Tools an agent may not call are hidden, like unknown ones.
		return false, toolError(ErrToolNotFound, fmt.Sprintf("unknown tool %q", info.ToolName))
	}
	if !actor.HasScope(storage.ScopeCall) {
		return false, toolError(ErrPermissionDenied, "this token does not have the call scope")
	}
	if privileged && !actor.Privileged {
		return false, toolError(ErrPermissionDenied, fmt.Sprintf("tool %q is privileged and this token may not call privileged tools", info.ToolName))
	}
	if g == nil {
		return false, nil // all_tools
	}
	reconfirm := toolError(ErrReconfirmRequired, fmt.Sprintf(
		"tool %q changed since it was granted to this token; ask the admin to re-confirm the grant in PiWorker (Settings, Agent tokens)", info.ToolName))
	if g.Suspended {
		return false, reconfirm
	}
	if g.Fingerprint != info.Fingerprint {
		return true, reconfirm
	}
	return false, nil
}

// suspendGrant suspends the token's grant for info while its fingerprint
// differs from the tool's, until the admin re-confirms it.
func (t *Tools) suspendGrant(tokenID string, info flow.ToolInfo) {
	if t.store == nil {
		return
	}
	_, err := t.store.UpdateAPITokenGrants(tokenID, func(grants []storage.TokenGrant) ([]storage.TokenGrant, error) {
		for i := range grants {
			g := &grants[i]
			if g.FlowID == info.FlowID && g.TriggerNodeID == info.TriggerNodeID && g.Fingerprint != info.Fingerprint {
				g.Suspended = true
			}
		}
		return grants, nil
	})
	if err != nil {
		t.log.Error().Err(err).Str("tokenId", tokenID).Str("tool", info.ToolName).Msg("Cannot suspend the grant of a changed tool")
		return
	}
	if t.tokens != nil {
		t.tokens.Evict(tokenID)
	}
	t.log.Warn().Str("tokenId", tokenID).Str("tool", info.ToolName).Str("flowID", info.FlowID).
		Msg("Agent grant suspended: the tool changed since it was granted; the admin must re-confirm it")
}

// requestApproval queues a confirm:admin call for the admin.
func (t *Tools) requestApproval(actor Actor, info flow.ToolInfo, args map[string]any) (flow.CallResult, error) {
	if t.store == nil {
		return flow.CallResult{}, toolError(ErrUnavailable, "this tool needs the admin's approval, which is not available")
	}
	data, err := json.Marshal(nonNilArgs(args))
	if err != nil {
		return flow.CallResult{}, fmt.Errorf("encode arguments: %w", err)
	}
	summary := fmt.Sprintf("Agent token %q asks to run tool %q (%s risk)", actor.TokenName, info.ToolName, info.Risk)
	a, err := t.store.CreateAgentApproval(storage.AgentApproval{
		Kind:            storage.ApprovalKindToolCall,
		TokenID:         actor.TokenID,
		FlowID:          info.FlowID,
		Ref:             info.TriggerNodeID,
		ToolName:        info.ToolName,
		ToolFingerprint: info.Fingerprint,
		Args:            data,
		Summary:         capText(summary, storage.MaxApprovalSummaryLength),
	}, t.now())
	if errors.Is(err, storage.ErrTooManyPendingApprovals) {
		return flow.CallResult{}, toolError(ErrBusy, "too many calls of this token are already waiting for the admin's approval")
	}
	if errors.Is(err, storage.ErrInvalidAgentApproval) {
		// The arguments exceed what an approval keeps.
		return flow.CallResult{}, toolError(ErrInvalidArguments, "the arguments are too large to queue for approval")
	}
	if err != nil {
		return flow.CallResult{}, fmt.Errorf("queue approval: %w", err)
	}
	return flow.CallResult{Status: flow.CallStatusApprovalRequired, RunID: a.ID}, nil
}

// invalidArguments lists schema issues sorted by pointer, so agents get a
// stable answer.
func invalidArguments(issues []toolschema.Issue) *ToolError {
	sorted := slices.Clone(issues)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].Pointer < sorted[b].Pointer })
	return &ToolError{Kind: ErrInvalidArguments, Issues: sorted, Message: "the arguments do not match the tool's input schema"}
}

// callError maps an error of FlowRuntime.Call that started no run.
func callError(err error) error {
	var argErr *node.ToolArgumentsError
	switch {
	case err == nil:
		return toolError(ErrUnavailable, "the call did not start")
	case errors.As(err, &argErr):
		return invalidArguments(argErr.Issues)
	case errors.Is(err, flow.ErrBusy):
		// ErrTooManyCalls (waiter cap) or ErrTriggerBusy (hand-off timed out):
		// no run started, so the call can be retried.
		return toolError(ErrBusy, "too many calls of this flow are in progress; retry later")
	case errors.Is(err, flow.ErrFlowNotRunning), errors.Is(err, flow.ErrNodeNotFound),
		errors.Is(err, flow.ErrTriggerNotCallable), errors.Is(err, flow.ErrNoActiveTargets):
		return toolError(ErrUnavailable, "the tool's flow is not running")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	}
	return fmt.Errorf("call tool: %w", err)
}

// finish redacts known secrets (and credential-bearing URLs in errors) and
// caps the result size.
func (t *Tools) finish(res flow.CallResult) flow.CallResult {
	red := newSecretRedactor(t.secrets)
	if len(res.Result) > 0 {
		res.Result = capResult(red.json(res.Result), t.maxResultBytes)
	}
	if res.Error != nil {
		e := *res.Error
		e.Message = capText(redact.URLsInText(red.text(e.Message)), maxErrorMessageBytes)
		res.Error = &e
	}
	return res
}

// Result returns the result of a run the actor started with Call (or of the
// approval an agent's confirm:admin call created). A run still executing is
// waited for until ctx ends; then its "running" result is returned. Runs of
// other actors are reported as unknown (ErrRunNotFound).
func (t *Tools) Result(ctx context.Context, actor Actor, runID string) (flow.CallResult, error) {
	if res, ok, err := t.runResult(ctx, actor, runID); ok || err != nil {
		return res, err
	}
	if actor.isAgent() && t.store != nil {
		return t.approvalResult(ctx, actor, runID)
	}
	return flow.CallResult{}, toolError(ErrRunNotFound, "unknown run")
}

// runResult reads a run from the in-memory results; ok is false when the
// actor has no such run.
func (t *Tools) runResult(ctx context.Context, actor Actor, runID string) (flow.CallResult, bool, error) {
	rec := t.results.get(runID)
	if rec == nil {
		return flow.CallResult{}, false, nil
	}
	if rec.owner != actor.key() {
		return flow.CallResult{}, false, toolError(ErrRunNotFound, "unknown run")
	}
	if rec.final {
		return rec.result, true, nil
	}
	res, err := rec.rt.Wait(ctx, runID)
	if res.Status == flow.CallStatusRunning {
		if err != nil && ctx.Err() == nil {
			return flow.CallResult{}, true, err
		}
		return t.finish(res), true, nil
	}
	return t.results.finalize(runID, t.finish(res)), true, nil
}

// record audits an agent call: a row per call, or a per-minute counter for
// the frequent rejections (busy, cooldown, invalid arguments).
func (t *Tools) record(actor Actor, ref ToolRef, info *flow.ToolInfo, args map[string]any, res flow.CallResult, err error, start time.Time) {
	if t.audit == nil {
		return
	}
	var te *ToolError
	if errors.As(err, &te) && (te.Kind == ErrBusy || te.Kind == ErrCooldown || te.Kind == ErrInvalidArguments) {
		t.audit.Reject(actor.TokenID, te.Code(), start)
		return
	}
	entry := storage.MCPAuditEntry{
		TS:        start,
		TokenID:   actor.TokenID,
		TokenName: actor.TokenName,
		Tool:      ref.String(),
		FlowID:    ref.FlowID,
		RunID:     res.RunID,
		DurMs:     t.now().Sub(start).Milliseconds(),
	}
	if info != nil {
		entry.Tool, entry.FlowID = info.ToolName, info.FlowID
		entry.Privileged = info.Risk == node.RiskPrivileged
	}
	if data, mErr := json.Marshal(nonNilArgs(args)); mErr == nil {
		sum := sha256.Sum256(data)
		entry.ArgsSHA256 = hex.EncodeToString(sum[:])
	}
	if data, mErr := marshalNoEscape(nonNilArgs(args)); mErr == nil {
		// Redacted on the decoded strings, so JSON escaping cannot hide a
		// secret, and cut to what storage keeps before it is queued.
		preview := newSecretRedactor(t.secrets).json(data)
		entry.ArgsPreview = capText(string(preview), storage.MaxMCPAuditPreview)
	}
	switch {
	case te != nil:
		entry.Outcome = te.Code()
	case err != nil && res.RunID == "":
		entry.Outcome = "failed"
	default:
		entry.Outcome = string(res.Status)
	}
	t.audit.Record(entry)
}

func nonNilArgs(args map[string]any) map[string]any {
	if args == nil {
		return map[string]any{}
	}
	return args
}
