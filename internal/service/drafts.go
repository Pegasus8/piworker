package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/storage"
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
	// AppliedBy is who applied an approved draft: "editor" (the admin
	// approved it) or "agent" (its author deployed it at agent_deploy low
	// or all; nobody reviewed it); absent when not applied.
	AppliedBy string `json:"appliedBy,omitempty"`
}

// TextChange is a text field a draft changes.
type TextChange struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

// textChange is the change from before to after (nil when equal).
func textChange(before, after string) *TextChange {
	if before == after {
		return nil
	}
	return &TextChange{Before: before, After: after}
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
	// NameChange and DescriptionChange are the proposed flow's name and
	// description where they differ from the current version's (an empty
	// flow's for a new one); null when unchanged. Agent text: render as
	// plain text.
	NameChange        *TextChange `json:"nameChange"`
	DescriptionChange *TextChange `json:"descriptionChange"`
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
	if err := authorizeEditor(ctx, actor); err != nil {
		return DraftList{}, err
	}
	status := q.Status
	switch status {
	case "":
		status = storage.DraftPending
	case storage.DraftPending, storage.DraftFilterDecided, storage.DraftFilterAll:
	default:
		return DraftList{}, toolError(ErrInvalidArguments, "status must be pending, decided or all")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultDraftListLimit
	}
	limit = min(limit, MaxDraftListLimit)
	drafts, err := a.store.ListFlowDrafts(storage.FlowDraftFilter{Status: status}, limit+1, q.BeforeID)
	if err != nil {
		return DraftList{}, fmt.Errorf("list drafts: %w", err)
	}
	out := DraftList{Drafts: make([]DraftSummary, 0, min(len(drafts), limit))}
	if len(drafts) > limit {
		drafts = drafts[:limit]
		out.NextBefore = drafts[limit-1].ID
	}
	flows := map[string]*flow.Flow{}
	for i := range drafts {
		current, err := a.draftFlow(flows, drafts[i].FlowID)
		if err != nil {
			return DraftList{}, err
		}
		out.Drafts = append(out.Drafts, draftSummary(&drafts[i], current))
	}
	return out, nil
}

// draftFlow returns the stored flow a draft changes (nil for a new flow or
// one deleted since), reading each flow once per call (cache).
func (a *Authoring) draftFlow(cache map[string]*flow.Flow, flowID string) (*flow.Flow, error) {
	if flowID == "" {
		return nil, nil
	}
	if f, ok := cache[flowID]; ok {
		return f, nil
	}
	f, err := a.store.GetFlow(flowID)
	if errors.Is(err, storage.ErrFlowNotFound) || errors.Is(err, storage.ErrInvalidFlowData) {
		f, err = nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get flow: %w", err)
	}
	cache[flowID] = f
	return f, nil
}

// draftSummary summarizes a stored draft for the editor; current is the
// flow it changes (nil: new, or deleted). Agent text (note, names) is plain
// text the editor renders as such.
func draftSummary(d *storage.FlowDraft, current *flow.Flow) DraftSummary {
	s := DraftSummary{
		ID:             d.ID,
		FlowID:         d.FlowID,
		FlowName:       d.Name,
		IsNew:          d.BaseVersion == 0,
		BaseVersion:    d.BaseVersion,
		Status:         d.Status,
		Risk:           node.Risk(d.Risk),
		AlertCount:     len(decodeList[flow.Alert](d.AlertsJSON)),
		Note:           runePrefix(d.Note, DraftNotePreviewChars),
		TokenID:        d.AuthorTokenID,
		TokenName:      d.AuthorTokenName,
		CreatedAt:      d.CreatedAt,
		DecidedAt:      d.DecidedAt,
		Reason:         d.Reason,
		AppliedVersion: d.AppliedVersion,
		AppliedBy:      d.AppliedBy,
	}
	for _, is := range decodeList[flow.Issue](d.IssuesJSON) {
		if is.Severity == flow.IssueError {
			s.IssueCounts.Error++
		} else {
			s.IssueCounts.Warning++
		}
	}
	if current != nil {
		s.CurrentVersion = current.Version
		if !s.IsNew {
			s.FlowName = current.Name
		}
	}
	s.Stale = d.Status == storage.DraftPending && !s.IsNew && (current == nil || current.Version != d.BaseVersion)
	return s
}

// GetDraftDetail shows one draft to the editor (unknown: not_found),
// recomputed against the current version and registry: its Lint issues and
// authoring warnings now, the diff with the current version (an empty flow
// for a new one) and, when the admin last approved another version, with
// that one; whether the flow runs; the proposed flow with Sensitive
// literals "<hidden>".
func (a *Authoring) GetDraftDetail(ctx context.Context, actor Actor, draftID string) (DraftDetail, error) {
	if err := authorizeEditor(ctx, actor); err != nil {
		return DraftDetail{}, err
	}
	d, err := a.store.GetFlowDraft(draftID)
	if errors.Is(err, storage.ErrFlowDraftNotFound) {
		return DraftDetail{}, unknownDraft(draftID)
	}
	if err != nil {
		return DraftDetail{}, fmt.Errorf("get draft: %w", err)
	}
	proposed, err := flow.FromJSON(d.Data)
	if err != nil {
		return DraftDetail{}, fmt.Errorf("decode draft %s: %w", d.ID, err)
	}
	proposed.ID = d.FlowID
	current, err := a.draftFlow(map[string]*flow.Flow{}, d.FlowID)
	if err != nil {
		return DraftDetail{}, err
	}
	var base *flow.Flow // the version the proposal replaces (nil: an empty flow)
	if d.BaseVersion != 0 {
		base = current
	}
	shown, err := cloneFlow(proposed)
	if err != nil {
		return DraftDetail{}, fmt.Errorf("copy draft %s: %w", d.ID, err)
	}
	detail := DraftDetail{
		DraftSummary: draftSummary(d, current),
		Note:         d.Note,
		Issues:       a.proposalIssues(ctx, &proposal{base: base, next: proposed, issues: a.unknownSecretIssues(proposed, changedValues(base, proposed))}),
		Diff:         flow.Diff(base, proposed, a.registry),
		Flow:         hideSensitiveLiterals(a.registry, shown),
	}
	wasName, wasDescription := "", "" // what they replace (an empty flow's: new)
	if base != nil {
		wasName, wasDescription = base.Name, base.Description
	}
	detail.NameChange = textChange(wasName, proposed.Name)
	detail.DescriptionChange = textChange(wasDescription, proposed.Description)
	if current != nil && a.lifecycle != nil {
		// Advisory, so no lock: ApproveDraft reads it again under it.
		detail.Running = a.lifecycle.Running(current.ID)
	}
	if current != nil {
		approved, err := a.store.LastAdminApprovedVersion(current.ID)
		if err != nil {
			return DraftDetail{}, fmt.Errorf("get the last approved version: %w", err)
		}
		if approved != nil {
			detail.LastApprovedVersion = approved.Version
			if approved.Version != current.Version {
				diff := flow.Diff(approved.Flow, proposed, a.registry)
				detail.DiffApproved = &diff
			}
		}
	}
	return detail, nil
}

// hideSensitiveLiterals returns f with every Sensitive value that holds a
// literal (anything but {{secret.NAME}} references) as flow.HiddenValue,
// and every config value of an unregistered type hidden too: the approval
// page shows names of secrets, never credentials.
func hideSensitiveLiterals(reg *node.Registry, f *flow.Flow) *flow.Flow {
	for i := range f.Nodes {
		n := &f.Nodes[i]
		info, ok := reg.Get(n.Type)
		for prop, v := range n.Config {
			switch {
			case !ok:
				n.Config[prop] = flow.HiddenValue
			case info.Config == nil:
			default:
				if p, declared := info.Config.Properties[prop]; declared && p.Sensitive && !secretOnly(v) {
					n.Config[prop] = flow.HiddenValue
				}
			}
		}
	}
	return f
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
	if err := authorizeEditor(ctx, actor); err != nil {
		return ApplyResult{}, err
	}
	if a.lifecycle == nil {
		return ApplyResult{}, toolError(ErrUnavailable, "drafts cannot be applied: the lifecycle path is not available")
	}
	var out ApplyResult
	err := a.lifecycle.WithLock(ctx, func(l LockedLifecycle) error {
		d, err := a.store.GetFlowDraft(draftID)
		if errors.Is(err, storage.ErrFlowDraftNotFound) {
			return unknownDraft(draftID)
		}
		if err != nil {
			return fmt.Errorf("get draft: %w", err)
		}
		if d.Status != storage.DraftPending {
			return toolError(ErrConflict, fmt.Sprintf("the draft is no longer pending (%s)", d.Status))
		}
		proposed, err := flow.FromJSON(d.Data)
		if err != nil {
			return fmt.Errorf("decode draft %s: %w", d.ID, err)
		}
		isNew := d.BaseVersion == 0
		running := false
		if !isNew {
			current, err := a.loadFlow(d.FlowID)
			if err != nil {
				return err
			}
			if current.Version != d.BaseVersion {
				return staleBase(current.Version)
			}
			running = l.Running(d.FlowID)
		}
		proposed.ID = d.FlowID
		redeploy := deploy || running
		issues := a.lint(ctx, proposed)
		blocking := flow.BlockingIssues(issues)
		if redeploy {
			blocking = errorIssues(issues) // deploy refuses undefined secrets too
		}
		if len(blocking) > 0 {
			return &ToolError{Kind: ErrInvalidArguments, FlowIssues: issues,
				Message: fmt.Sprintf("the proposal has %d error(s) the editor's save or deploy would refuse", len(blocking))}
		}
		meta := storage.VersionMeta{
			ActorKind: storage.VersionActorEditor, AdminApproved: true, DraftID: d.ID,
			Note: fmt.Sprintf("approved draft %s from %s", d.ID, d.AuthorTokenName),
		}
		version := 1
		if isNew {
			created := flow.NewFlow(proposed.Name)
			proposed.ID, proposed.CreatedAt, proposed.UpdatedAt = created.ID, created.CreatedAt, created.UpdatedAt
			err = a.store.CreateFlowVersioned(proposed, meta)
		} else {
			version, err = a.store.UpdateFlowVersioned(proposed, d.BaseVersion, meta)
		}
		if err != nil {
			return applyError(err, draftID)
		}
		out = ApplyResult{FlowID: proposed.ID, Version: version}
		if redeploy {
			out.Version, out.Deployed, out.DeployError = a.redeploy(ctx, l, proposed.ID, version, running)
		}
		return nil
	})
	return out, err
}

// redeploy deploys a version just saved for the editor (restarting the flow
// when it runs) and reports the outcome; a refusal is a DeployError, the
// version stays saved.
func (a *Authoring) redeploy(ctx context.Context, l LockedLifecycle, flowID string, version int, running bool) (int, bool, *DeployError) {
	deployed, err := l.Deploy(ctx, flowID, EditorActor().DeployActor(), running)
	if err == nil {
		return deployed, true, nil
	}
	var le *LifecycleError
	if errors.As(err, &le) {
		return version, false, &DeployError{Status: le.Status, Message: le.Message}
	}
	a.log.Error().Err(err).Str("flowID", flowID).Msg("Failed to deploy an applied version")
	return version, false, &DeployError{Status: http.StatusInternalServerError, Message: "Failed to deploy flow"}
}

// errorIssues are the error-severity issues (deploy refuses any of them).
func errorIssues(issues []flow.Issue) []flow.Issue {
	var out []flow.Issue
	for _, is := range issues {
		if is.Severity == flow.IssueError {
			out = append(out, is)
		}
	}
	return out
}

// applyError maps a refused save of a draft (storage applies it in the
// save's transaction, H5).
func applyError(err error, draftID string) error {
	var conflict *storage.VersionConflictError
	switch {
	case errors.As(err, &conflict):
		return staleBase(conflict.Current)
	case errors.Is(err, storage.ErrFlowDraftNotFound):
		return unknownDraft(draftID)
	case errors.Is(err, storage.ErrDraftNotPending):
		return toolError(ErrConflict, "the draft is no longer pending")
	case errors.Is(err, storage.ErrDraftTokenRevoked):
		return toolError(ErrPermissionDenied, "the draft's token is revoked")
	case errors.Is(err, storage.ErrFlowNotFound):
		return toolError(ErrFlowNotFound, "the flow no longer exists")
	case errors.Is(err, storage.ErrInvalidFlowDraft), errors.Is(err, storage.ErrInvalidFlowData):
		return toolError(ErrInvalidArguments, "the draft cannot be applied to this flow")
	}
	return fmt.Errorf("save draft: %w", err)
}

// RejectDraft rejects a pending draft for the editor with an optional reason
// (at most MaxRejectReasonChars; agent-visible through GetDraft). Not
// pending: conflict; unknown: not_found.
func (a *Authoring) RejectDraft(ctx context.Context, actor Actor, draftID, reason string) error {
	if err := authorizeEditor(ctx, actor); err != nil {
		return err
	}
	if utf8.RuneCountInString(reason) > MaxRejectReasonChars {
		return toolError(ErrInvalidArguments, fmt.Sprintf("the reason is over %d characters", MaxRejectReasonChars))
	}
	err := a.store.DecideFlowDraft(draftID, storage.DraftRejected, reason, 0)
	switch {
	case errors.Is(err, storage.ErrFlowDraftNotFound):
		return unknownDraft(draftID)
	case errors.Is(err, storage.ErrDraftNotPending):
		return toolError(ErrConflict, "the draft is no longer pending")
	case err != nil:
		return fmt.Errorf("reject draft: %w", err)
	}
	return nil
}
