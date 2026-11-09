package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/rs/zerolog"
)

// Agent authoring (docs/design/mcp.md §10, ADR-0012): agents with the author
// scope validate flows, dry-run them, test nodes and propose drafts; nothing
// an agent authors changes what runs until the admin approves it in the
// editor (or, with agent_deploy above off, the agent deploys it: deploy.go).
// Everything shown to an agent goes through the read views' rules: config
// values agents may not read (Sensitive, not AgentVisible, undeclared, of an
// unknown type) are "<redacted>" or "<hidden>", known secret values never
// appear, URLs in messages are cut after their host.

// Names under which the authoring and deploy operations are audited: the MCP
// tools that expose them.
const (
	AuthorToolValidateFlow = "pw_validate_flow"
	AuthorToolSaveDraft    = "pw_save_draft"
	AuthorToolGetDraft     = "pw_get_draft"
	AuthorToolDryRun       = "pw_dry_run"
	AuthorToolTestNode     = "pw_test_node"
	DeployToolDeployFlow   = "pw_deploy_flow"
	DeployToolStopFlow     = "pw_stop_flow"
)

// Limits of agent authoring and of the editor's listings.
const (
	// MaxDraftNameChars bounds a draft's flow name.
	MaxDraftNameChars = 100
	// MaxDraftNoteChars bounds the agent's note.
	MaxDraftNoteChars = 2000
	// MaxDraftNodes and MaxDraftConnections bound a proposed flow.
	MaxDraftNodes       = 200
	MaxDraftConnections = 400
	// MaxDraftFlowBytes bounds the JSON of a proposed flow (and of a
	// validated one) as the agent sends it.
	MaxDraftFlowBytes = 512 << 10
	// MaxRejectReasonChars bounds the admin's reason for a rejection.
	MaxRejectReasonChars = 500
	// DraftNotePreviewChars is how much of the note a DraftSummary shows.
	DraftNotePreviewChars = 200
	// DefaultDraftListLimit and MaxDraftListLimit bound ListDrafts.
	DefaultDraftListLimit = 50
	MaxDraftListLimit     = 100
	// DefaultVersionListLimit and MaxVersionListLimit bound Versions.
	DefaultVersionListLimit = 50
	MaxVersionListLimit     = 50
)

// RedactedValue is the sentinel the read views show for a config value
// agents may not read. In a draft (SaveDraft) or a validated flow it means
// "keep the base value of this property".
const RedactedValue = redactedMark

// AuthoringStore is the persistence authoring and agent deploy need
// (*storage.SQLiteStore).
type AuthoringStore interface {
	GetFlow(id string) (*flow.Flow, error)
	CreateFlowVersioned(f *flow.Flow, meta storage.VersionMeta) error
	UpdateFlowVersioned(f *flow.Flow, expected int, meta storage.VersionMeta) (int, error)
	GetFlowVersion(flowID string, version int) (*storage.FlowVersion, error)
	ListFlowVersions(flowID string, limit int) ([]storage.FlowVersion, error)
	LastAdminApprovedVersion(flowID string) (*storage.FlowVersion, error)
	GetDeployment(flowID string) (*storage.Deployment, error)
	CreateFlowDraft(d storage.FlowDraft) (*storage.FlowDraft, error)
	GetFlowDraft(id string) (*storage.FlowDraft, error)
	ListFlowDrafts(filter storage.FlowDraftFilter, limit int, beforeID string) ([]storage.FlowDraft, error)
	DecideFlowDraft(id, status, reason string, appliedVersion int) error
	GetAPIToken(id string) (*storage.APIToken, error)
	TokensEpoch() (int64, error)
	AgentDeployLevel() (storage.AgentDeployLevel, error)
}

// AuthoringOptions configures Authoring.
type AuthoringOptions struct {
	// Store holds flows, versions, drafts, tokens and the agent deploy
	// level (required).
	Store AuthoringStore
	// Lifecycle is the editor's lifecycle path (internal/api's
	// FlowsHandler). ApproveDraft, Rollback, Deploy and Stop save and
	// deploy under its lock; without it they are unavailable.
	Lifecycle FlowLifecycle
	// Registry describes and builds node types for Lint, Risk, Diff,
	// ApplyOps, DryRun and TestNode (default node.DefaultRegistry).
	Registry *node.Registry
	// Secrets is the secret store: Lint checks the secrets strict settings
	// require against it (flow.LintContext), unknown secret names are
	// warnings, and its values are redacted from everything agents see
	// (default secrets.DefaultStore).
	Secrets *secrets.Store
	// Audit records the agents' operations (nil: not audited).
	Audit *AuditWriter
	// AuthorEnabled is mcp.author_enabled: when false every authoring
	// operation of an agent is forbidden (the tools are not listed).
	AuthorEnabled bool
	Now           func() time.Time
	Logger        zerolog.Logger
}

// Authoring serves agent authoring (Validate, SaveDraft, GetDraft, DryRun,
// TestNode), the editor's side of drafts (ListDrafts, GetDraftDetail,
// ApproveDraft, RejectDraft) and deploys (Deploy, Stop, Rollback, Versions,
// Marks). It is safe for concurrent use.
type Authoring struct {
	store         AuthoringStore
	lifecycle     FlowLifecycle
	registry      *node.Registry
	secrets       *secrets.Store
	audit         *AuditWriter
	authorEnabled bool
	now           func() time.Time
	log           zerolog.Logger
	// views holds the read views' redaction rules (registry and secrets
	// only: Authoring never calls its views).
	views *Reads
}

// NewAuthoring returns the authoring and deploy service.
func NewAuthoring(opts AuthoringOptions) *Authoring {
	a := &Authoring{
		store:         opts.Store,
		lifecycle:     opts.Lifecycle,
		registry:      opts.Registry,
		secrets:       opts.Secrets,
		audit:         opts.Audit,
		authorEnabled: opts.AuthorEnabled,
		now:           opts.Now,
		log:           opts.Logger,
	}
	if a.registry == nil {
		a.registry = node.DefaultRegistry
	}
	if a.secrets == nil {
		a.secrets = secrets.DefaultStore
	}
	if a.now == nil {
		a.now = time.Now
	}
	a.views = &Reads{registry: a.registry, secrets: a.secrets, now: a.now}
	return a
}

// notImplemented is the refusal of an operation whose body is not written
// yet.
func notImplemented(op string) *ToolError {
	return toolError(ErrUnavailable, op+": not implemented yet")
}

// ValidateRequest is what Validate checks (pw_validate_flow): a whole flow,
// or with FlowID a proposal based on that flow's current version (ops, or a
// whole flow whose "<redacted>" values keep the current ones), resolved
// exactly as SaveDraft resolves it.
type ValidateRequest struct {
	FlowID string     `json:"flowId,omitempty"`
	Flow   *flow.Flow `json:"flow,omitempty"`
	Ops    []flow.Op  `json:"ops,omitempty"`
}

// ValidateResult is Validate's verdict.
type ValidateResult struct {
	// Valid: no error issue other than undefined_secret
	// (flow.BlockingIssues is empty), so SaveDraft accepts it and a
	// dry-run runs it. An undefined_secret error still blocks a deploy
	// until the admin defines the secret.
	Valid  bool         `json:"valid"`
	Issues []flow.Issue `json:"issues"`
	Risk   node.Risk    `json:"risk"`
}

// Authoring issue codes: the issues SaveDraft and Validate add to Lint's
// (flow.Issue paths are Lint's: /nodes/{nodeId}/config/{prop}).
const (
	// IssueSensitiveLiteral (error): a Sensitive property holds a literal,
	// not a {{secret.NAME}} reference or "<redacted>". The message never
	// quotes the value.
	IssueSensitiveLiteral = "sensitive_literal"
	// IssueUnresolvedRedaction (error): a "<redacted>" value has no base
	// value to keep (no base node with that ID and type, or no such
	// property in it).
	IssueUnresolvedRedaction = "unresolved_redaction"
	// IssueUnknownSecret (warning): a {{secret.NAME}} reference names a
	// secret that is not defined (the admin may define it later); a strict
	// setting's is Lint's undefined_secret error instead.
	IssueUnknownSecret = "unknown_secret"
)

// Validate lints a flow for an agent with the author scope, or the editor:
// the proposal resolved as SaveDraft resolves it, then flow.LintContext with
// the secret store (every check deploy runs; messages hide what agents may
// not read), the authoring issues (IssueSensitiveLiteral,
// IssueUnresolvedRedaction, IssueUnknownSecret) and flow.Risk. Request
// problems SaveDraft refuses outright (limits, author restrictions, a
// stale or unknown FlowID) are refused the same way. Nothing is stored.
// Audited (outcome only).
//
// With FlowID and neither Flow nor Ops, the current version is validated as
// it is. The editor's proposals are linted only: the author restrictions and
// the authoring issues are the agents'.
func (a *Authoring) Validate(ctx context.Context, actor Actor, req ValidateRequest) (_ ValidateResult, err error) {
	args := &authorArgs{FlowID: req.FlowID, Ops: len(req.Ops)}
	defer a.audited(actor, AuthorToolValidateFlow, args, a.now(), nil)(&err)
	if err := a.authorizeAuthor(ctx, actor, true); err != nil {
		return ValidateResult{}, err
	}
	p, err := a.resolveProposal(proposalRequest{
		flowID: req.FlowID, flow: req.Flow, ops: req.Ops, current: true, editor: actor.Kind == ActorEditor,
	})
	if err != nil {
		return ValidateResult{}, a.redactedRefusal(err)
	}
	issues := a.proposalIssues(ctx, p)
	if actor.isAgent() {
		issues = agentIssues(a.proposalRedactor(p.base, p.next), issues)
	}
	return ValidateResult{
		Valid:  len(flow.BlockingIssues(issues)) == 0,
		Issues: issues,
		Risk:   flow.Risk(p.next, a.registry),
	}, nil
}

// proposalIssues are Lint's issues of a proposal (with the service's secret
// store) and its authoring issues, in Lint's order. An unknown_secret
// warning where Lint reports the same setting's undefined_secret is left
// out.
func (a *Authoring) proposalIssues(ctx context.Context, p *proposal) []flow.Issue {
	lint := a.lint(ctx, p.next)
	undefined := map[string]bool{}
	for _, is := range lint {
		if is.Code == flow.IssueUndefinedSecret {
			undefined[is.Path] = true
		}
	}
	extra := make([]flow.Issue, 0, len(p.issues))
	for _, is := range p.issues {
		if is.Code == IssueUnknownSecret && undefined[is.Path] {
			continue
		}
		extra = append(extra, is)
	}
	return mergeIssues(p.next, lint, extra)
}

// redactedRefusal redacts the flow issues of a refusal for agents (an ops
// issue may name what the agent sent; known secret values never appear).
func (a *Authoring) redactedRefusal(err error) error {
	var te *ToolError
	if errors.As(err, &te) && len(te.FlowIssues) > 0 {
		te.FlowIssues = agentIssues(a.proposalRedactor(), te.FlowIssues)
	}
	return err
}

// DraftInput is a proposal (pw_save_draft): exactly one of Flow and Ops.
type DraftInput struct {
	// FlowID is the existing flow the draft changes ("" for a new flow).
	FlowID string `json:"flowId,omitempty"`
	// BaseVersion is the version the proposal is based on: required for
	// an existing flow and equal to its current version (0 for new).
	BaseVersion int `json:"baseVersion,omitempty"`
	// Flow replaces the base flow; "<redacted>" config values keep the
	// base's value of the property (same node ID and type).
	Flow *flow.Flow `json:"flow,omitempty"`
	// Ops are applied to the base flow (existing flows only).
	Ops []flow.Op `json:"ops,omitempty"`
	// Name is the proposed flow name (at most MaxDraftNameChars). A new
	// flow needs one (Name or Flow.Name); an existing flow keeps its name
	// unless Name (or a whole Flow's name) is set.
	Name string `json:"name,omitempty"`
	// Note is the agent's note to the admin (at most MaxDraftNoteChars).
	Note string `json:"note"`
}

// DraftResult is what SaveDraft stored.
type DraftResult struct {
	DraftID string `json:"draftId"`
	// Status is "pending".
	Status string    `json:"status"`
	Risk   node.Risk `json:"risk"`
	// Issues are the proposal's Lint issues: warnings, and undefined
	// secrets, which the admin may define before approving.
	Issues []flow.Issue `json:"issues"`
	// Alerts are flow.Diff's alerts against the current version (an empty
	// flow for a new one); they are agent-safe.
	Alerts      []flow.Alert `json:"alerts"`
	FlowID      string       `json:"flowId"` // "" for a new flow
	BaseVersion int          `json:"baseVersion"`
}

// SaveDraft stores a draft for an agent with the author scope (the editor:
// forbidden; it saves flows directly). An existing flow needs BaseVersion
// equal to its current version (else conflict with CurrentVersion; an
// unknown flow: flow_not_found); a new flow needs Flow (no Ops) and a name.
// Ops apply to the base (their issues: invalid_arguments with FlowIssues at
// /ops/{i}) and new nodes are laid out (flow.ApplyOps, flow.AutoLayout);
// "<redacted>" keeps the base value of the same node and type (otherwise
// IssueUnresolvedRedaction). A Sensitive property may hold only a
// {{secret.NAME}} reference or the sentinel (otherwise
// IssueSensitiveLiteral); both refuse the draft with invalid_arguments and
// FlowIssues. An unknown secret name is an IssueUnknownSecret warning. The
// author may not add,
// change or remove trigger-tool or action-reply nodes, change a webhook
// trigger's path or token, turn on a RiskSwitch property or change the
// legacy Variables (forbidden naming the path), nor have more than 20
// pending drafts (forbidden: the admin must decide pending drafts first). A
// note over MaxDraftNoteChars, a name over MaxDraftNameChars, more than
// MaxDraftNodes nodes or MaxDraftConnections connections, or a flow over
// MaxDraftFlowBytes of JSON is invalid_arguments. A proposal with blocking
// errors (flow.BlockingIssues: Lint's — duplicate or empty node IDs,
// unknown types, config typing, … — and the two authoring errors) is
// refused with invalid_arguments and FlowIssues (H2); undefined secrets
// and warnings are kept as issues. The stored draft holds the resolved
// flow, its issues, risk and alerts (flow.Diff against the current
// version; agent-safe). Audited.
func (a *Authoring) SaveDraft(ctx context.Context, actor Actor, in DraftInput) (_ DraftResult, err error) {
	args := &authorArgs{FlowID: in.FlowID, BaseVersion: in.BaseVersion, Ops: len(in.Ops)}
	defer a.audited(actor, AuthorToolSaveDraft, args, a.now(), nil)(&err)
	if err := a.authorizeAuthor(ctx, actor, false); err != nil {
		return DraftResult{}, err
	}
	if utf8.RuneCountInString(in.Note) > MaxDraftNoteChars {
		return DraftResult{}, toolError(ErrInvalidArguments, fmt.Sprintf("the note is over %d characters", MaxDraftNoteChars))
	}
	p, err := a.resolveProposal(proposalRequest{
		flowID: in.FlowID, baseVersion: in.BaseVersion, checkBase: true,
		flow: in.Flow, ops: in.Ops, name: in.Name, requireName: true,
	})
	if err != nil {
		return DraftResult{}, a.redactedRefusal(err)
	}
	red := a.proposalRedactor(p.base, p.next)
	issues := agentIssues(red, a.proposalIssues(ctx, p))
	if blocking := flow.BlockingIssues(issues); len(blocking) > 0 {
		return DraftResult{}, &ToolError{Kind: ErrInvalidArguments, FlowIssues: issues,
			Message: fmt.Sprintf("the proposal has %d error(s) deploy would refuse; fix them and save again (pw_validate_flow lists them)", len(blocking))}
	}
	risk := flow.Risk(p.next, a.registry)
	alerts := agentAlerts(red, flow.Diff(p.base, p.next, a.registry).Alerts)
	baseVersion := 0
	if p.base != nil {
		baseVersion = p.base.Version
	}
	data, err := json.Marshal(p.next)
	if err != nil {
		return DraftResult{}, fmt.Errorf("encode draft: %w", err)
	}
	issuesJSON, err := json.Marshal(issues)
	if err != nil {
		return DraftResult{}, fmt.Errorf("encode issues: %w", err)
	}
	alertsJSON, err := json.Marshal(alerts)
	if err != nil {
		return DraftResult{}, fmt.Errorf("encode alerts: %w", err)
	}
	d, err := a.store.CreateFlowDraft(storage.FlowDraft{
		FlowID: in.FlowID, BaseVersion: baseVersion, Data: data, Note: in.Note,
		AuthorTokenID: actor.TokenID, AuthorTokenName: actor.TokenName,
		Risk: string(risk), IssuesJSON: issuesJSON, AlertsJSON: alertsJSON,
	})
	var conflict *storage.VersionConflictError
	switch {
	case errors.As(err, &conflict):
		return DraftResult{}, staleBase(conflict.Current)
	case errors.Is(err, storage.ErrFlowNotFound):
		return DraftResult{}, toolError(ErrFlowNotFound, fmt.Sprintf("unknown flow %q", capText(in.FlowID, maxViewIDBytes)))
	case errors.Is(err, storage.ErrDraftLimit):
		return DraftResult{}, toolError(ErrPermissionDenied, fmt.Sprintf(
			"this token already has %d pending drafts: the admin must approve or reject pending drafts first", storage.MaxPendingDraftsPerToken))
	case errors.Is(err, storage.ErrDraftTokenRevoked):
		return DraftResult{}, toolError(ErrPermissionDenied, "this token is revoked")
	case errors.Is(err, storage.ErrInvalidFlowDraft):
		return DraftResult{}, toolError(ErrInvalidArguments, "the draft cannot be stored: it is too large")
	case err != nil:
		return DraftResult{}, fmt.Errorf("create draft: %w", err)
	}
	args.DraftID = d.ID
	return DraftResult{
		DraftID: d.ID, Status: d.Status, Risk: risk, Issues: issues, Alerts: alerts,
		FlowID: in.FlowID, BaseVersion: baseVersion,
	}, nil
}

// DraftView is a draft as its author sees it (GetDraft).
type DraftView struct {
	DraftID string `json:"draftId"`
	// Status is pending, approved, rejected or void.
	Status string `json:"status"`
	// Reason is the admin's reason for a rejection, or why the draft was
	// voided ("token revoked", "flow deleted").
	Reason      string       `json:"reason,omitempty"`
	Risk        node.Risk    `json:"risk"`
	Issues      []flow.Issue `json:"issues"`
	Alerts      []flow.Alert `json:"alerts"`
	FlowID      string       `json:"flowId"`
	BaseVersion int          `json:"baseVersion"`
	// AppliedVersion is the version an approval saved (0: none).
	AppliedVersion int        `json:"appliedVersion,omitempty"`
	Note           string     `json:"note"`
	CreatedAt      time.Time  `json:"createdAt"`
	DecidedAt      *time.Time `json:"decidedAt,omitempty"`
	// Flow is the proposed flow redacted with the read views' rules.
	Flow DraftFlowView `json:"flow"`
}

// DraftFlowView is a proposed flow as agents see it: the read views' nodes
// (default-deny redacted configs) and connections, bounded like GetFlow.
type DraftFlowView struct {
	ID               string           `json:"id,omitempty"` // "" for a new flow
	Name             string           `json:"name"`
	Nodes            []NodeView       `json:"nodes"`
	Connections      []ConnectionView `json:"connections"`
	TotalNodes       int              `json:"totalNodes"`
	TotalConnections int              `json:"totalConnections"`
	Truncated        bool             `json:"truncated"`
}

// GetDraft shows an agent with the author scope one of its own drafts
// (another token's, or an unknown one: not_found; the editor: forbidden, it
// uses GetDraftDetail). Audited.
func (a *Authoring) GetDraft(ctx context.Context, actor Actor, draftID string) (DraftView, error) {
	return DraftView{}, notImplemented("get draft")
}

// DryRunRequest is a dry-run (pw_dry_run) of exactly one of Flow (inline),
// DraftID (the agent's own draft) and FlowID (that flow's current version).
type DryRunRequest struct {
	Flow    *flow.Flow `json:"flow,omitempty"`
	DraftID string     `json:"draftId,omitempty"`
	FlowID  string     `json:"flowId,omitempty"`
	// TriggerNodeID is the enabled input node the payload is injected at
	// (may be empty when the flow has exactly one).
	TriggerNodeID string         `json:"triggerNodeId,omitempty"`
	Payload       any            `json:"payload,omitempty"`
	Mocks         map[string]any `json:"mocks,omitempty"`
	FlowVars      map[string]any `json:"flowVars,omitempty"`
}

// DryRun runs a flow once in the sandbox (flow.DryRun) for an agent with the
// author scope, or the editor. For an agent's dry-run by FlowID or DraftID
// DryRunInput.Redact hides the properties agents may not read (Sensitive,
// not AgentVisible, undeclared, unknown type): all of them for a stored
// flow, for a draft those equal to the base version's value at the same node
// and property (H4); an inline Flow is the agent's own and is not redacted.
// No dry-run ever gets real secrets. The result's previews and errors are
// redacted for agents (known secret values, URLs after their host, hidden
// values); the editor sees them unredacted except secret values. Busy maps
// to busy, flow.ErrDryRunInput to invalid_arguments. Audited (outcome only,
// no payloads).
func (a *Authoring) DryRun(ctx context.Context, actor Actor, req DryRunRequest) (flow.DryRunResult, error) {
	return flow.DryRunResult{}, notImplemented("dry run")
}

// TestNodeRequest is a node test (pw_test_node).
type TestNodeRequest struct {
	Type   string         `json:"type"`
	Config map[string]any `json:"config,omitempty"`
	Input  any            `json:"input,omitempty"`
}

// TestNode runs one safe processing node once (flow.TestNode) for an agent
// with the author scope, or the editor. Sensitive config literals are
// refused as in SaveDraft; flow.ErrTestNodeForbidden maps to forbidden,
// flow.ErrInvalidNodeType and flow.ErrDryRunInput to invalid_arguments, busy
// to busy; config problems are the result's issues. Outputs are redacted for
// agents like a dry-run's. Audited (outcome only).
func (a *Authoring) TestNode(ctx context.Context, actor Actor, req TestNodeRequest) (flow.TestNodeResult, error) {
	return flow.TestNodeResult{}, notImplemented("test node")
}
