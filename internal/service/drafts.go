package service

import (
	"context"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
)

// The editor's side of drafts (docs/design/mcp.md §10): the admin lists the
// agents' drafts, reads one with its diff and alerts, and approves (saving
// it, and deploying when asked or when the flow runs) or rejects it.
// Approving and rejecting need the session only (ADR-0017: narrowing or
// applying what the admin reviewed, not widening agent access). Agents get
// forbidden from every method here.

// DraftQuery selects drafts for ListDrafts.
type DraftQuery struct {
	// Status is "pending" (the default), "decided" or "all".
	Status string `json:"status"`
	// Limit is 1 to MaxDraftListLimit (0: DefaultDraftListLimit).
	Limit int `json:"limit"`
	// BeforeID continues a listing after the draft with that ID (a
	// DraftList.NextBefore).
	BeforeID string `json:"beforeId"`
}

// DraftList is a page of drafts, newest first.
type DraftList struct {
	Drafts []DraftSummary `json:"drafts"`
	// NextBefore is the BeforeID of the next page ("" when this is the
	// last).
	NextBefore string `json:"nextBefore"`
}

// DraftSummary is one draft in the editor's list.
type DraftSummary struct {
	ID     string `json:"id"`
	FlowID string `json:"flowId"` // "" for a new flow
	// FlowName is the stored flow's current name, or the proposed name of
	// a new flow.
	FlowName string `json:"flowName"`
	IsNew    bool   `json:"isNew"`
	// BaseVersion is the version the draft is based on (0 for new);
	// CurrentVersion the flow's current version (0 for new).
	BaseVersion    int `json:"baseVersion"`
	CurrentVersion int `json:"currentVersion"`
	// Stale: a pending draft of an existing flow whose current version is
	// no longer its base; it cannot be approved and must be redone.
	Stale  bool      `json:"stale"`
	Status string    `json:"status"` // pending, approved, rejected, void
	Risk   node.Risk `json:"risk"`
	// AlertCount and IssueCounts count the alerts and issues stored with
	// the draft (GetDraftDetail recomputes them).
	AlertCount  int         `json:"alertCount"`
	IssueCounts IssueCounts `json:"issueCounts"`
	// Note is the first DraftNotePreviewChars characters of the agent's
	// note (agent text: render as plain text).
	Note      string    `json:"note"`
	TokenID   string    `json:"tokenId"`
	TokenName string    `json:"tokenName"`
	CreatedAt time.Time `json:"createdAt"`
	// DecidedAt is when the draft stopped being pending (null while it is).
	DecidedAt *time.Time `json:"decidedAt"`
	// Reason is the rejection or void reason.
	Reason string `json:"reason,omitempty"`
	// AppliedVersion is the version an approval saved (0: none).
	AppliedVersion int `json:"appliedVersion,omitempty"`
}

// IssueCounts counts issues by severity.
type IssueCounts struct {
	Error   int `json:"error"`
	Warning int `json:"warning"`
}

// DraftDetail is one draft as the approval page shows it. Everything but the
// stored note and the token is recomputed on read against the current
// version and registry.
type DraftDetail struct {
	DraftSummary
	// Note is the agent's whole note.
	Note string `json:"note"`
	// Issues are the proposal's Lint issues now.
	Issues []flow.Issue `json:"issues"`
	// Running: the flow is running, so approving redeploys it whatever
	// "deploy" says.
	Running bool `json:"running"`
	// Diff compares the proposal with the current version (an empty flow
	// for a new one): field diffs with full values (Sensitive ones as
	// "<hidden>" unless a secret reference) and the alerts.
	Diff flow.DiffReport `json:"diff"`
	// LastApprovedVersion is the newest admin-approved version (0: none).
	LastApprovedVersion int `json:"lastApprovedVersion"`
	// DiffApproved compares the proposal with LastApprovedVersion when
	// that is not the current version (null otherwise).
	DiffApproved *flow.DiffReport `json:"diffApproved"`
	// Flow is the proposed flow, Sensitive literals as "<hidden>".
	Flow *flow.Flow `json:"flow"`
}

// ListDrafts lists drafts for the editor, newest first, at most q.Limit,
// with the cursor of the next page. An unknown status is invalid_arguments.
func (a *Authoring) ListDrafts(ctx context.Context, actor Actor, q DraftQuery) (DraftList, error) {
	return DraftList{}, notImplemented("list drafts")
}

// GetDraftDetail shows one draft to the editor (unknown: not_found).
func (a *Authoring) GetDraftDetail(ctx context.Context, actor Actor, draftID string) (DraftDetail, error) {
	return DraftDetail{}, notImplemented("get draft detail")
}

// ApproveDraft applies a pending draft for the editor, under the lifecycle
// lock: not pending or stale (base version ≠ current) is conflict (with
// CurrentVersion when stale), unknown not_found. The proposal is re-linted
// against the current registry (H2): blocking errors (flow.BlockingIssues)
// refuse it with invalid_arguments and FlowIssues, and so does any Lint
// error, undefined secrets included, when the flow will be redeployed
// (deploy, or the flow is running) — the draft stays pending and nothing is
// saved. It is saved through UpdateFlowVersioned (CreateFlowVersioned for a
// new flow) with VersionMeta{editor, AdminApproved, DraftID, note "approved
// draft <id> from <token name>"}, which closes the draft as approved in the
// same transaction (H5: never DecideFlowDraft afterwards). When deploy is
// true or the flow is running it is (re)deployed through the lifecycle path
// (recording the editor's deployment); a failed deploy is DeployError, the
// version stays saved.
func (a *Authoring) ApproveDraft(ctx context.Context, actor Actor, draftID string, deploy bool) (ApplyResult, error) {
	return ApplyResult{}, notImplemented("approve draft")
}

// RejectDraft rejects a pending draft for the editor with an optional reason
// (at most MaxRejectReasonChars; agent-visible through GetDraft). Not
// pending: conflict; unknown: not_found.
func (a *Authoring) RejectDraft(ctx context.Context, actor Actor, draftID, reason string) error {
	return notImplemented("reject draft")
}
