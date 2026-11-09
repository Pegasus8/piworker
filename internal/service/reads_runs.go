package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Pegasus8/piworker/internal/storage"
)

// Bounds of the run views.
const (
	// defaultRunsPerPage and maxRunsPerPage bound a ListRuns page.
	defaultRunsPerPage = 20
	maxRunsPerPage     = 100
	// maxRunFieldBytes bounds one recorded text field (a node error).
	maxRunFieldBytes = 2 << 10
	// maxRunViewBytes is the budget of a GetRun's events (their JSON text);
	// the events past it are left out (RunView.Truncated).
	maxRunViewBytes = 64 << 10
	// maxActorTokenBytes bounds a token name shown as a run's actor.
	maxActorTokenBytes = 128
	// runEventsPage is how many events GetRun reads at a time, until its
	// budget is spent, so a run of any size costs a bounded read.
	runEventsPage = 64
	// maxStoredErrorChars bounds the characters of a recorded error GetRun
	// reads (storage cuts longer ones), well above what it shows.
	maxStoredErrorChars = 8 << 10
)

// Who started a run (RunSummary.Actor).
const (
	// RunActorTrigger: a trigger fired (a schedule, a webhook, an inject…).
	RunActorTrigger = "trigger"
	// RunActorEditor: the admin called a tool from the editor.
	RunActorEditor = "editor"
	// RunActorAgent: an agent called a tool (RunSummary.ActorToken names
	// its token).
	RunActorAgent = "agent"
	// RunActorOther: a caller this version does not know.
	RunActorOther = "other"
)

// RunList is a page of ListRuns.
type RunList struct {
	// Runs are newest first.
	Runs []RunSummary `json:"runs"`
	// NextBefore, when set, is the before argument that lists the next
	// (older) page.
	NextBefore string `json:"nextBefore,omitempty"`
}

// RunSummary describes one recorded run of a flow.
type RunSummary struct {
	ID     string `json:"id"`
	FlowID string `json:"flowId"`
	Status string `json:"status"` // running, success or error
	// Actor says who started the run (RunActor*); ActorToken is the name of
	// the agent's token.
	Actor      string     `json:"actor"`
	ActorToken string     `json:"actorToken,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	DurationMs *int64     `json:"durationMs,omitempty"`
	// NodeCount and ErrorCount count the node executions recorded and
	// those that failed.
	NodeCount  int `json:"nodeCount"`
	ErrorCount int `json:"errorCount"`
}

// RunView is a run with its recorded node events (GetRun).
type RunView struct {
	RunSummary
	// Events are the recorded node executions in order. The recorder
	// stores no payloads, so none are shown.
	Events []RunEvent `json:"events"`
	// TotalEvents counts the recorded events, shown or not.
	TotalEvents int `json:"totalEvents"`
	// Truncated reports that the last events were left out to keep the
	// view within maxRunViewBytes.
	Truncated bool `json:"truncated"`
}

// RunEvent is one recorded node execution of a run.
type RunEvent struct {
	NodeID     string  `json:"nodeId"`
	NodeType   string  `json:"nodeType"`
	Phase      string  `json:"phase"` // success or error
	DurationMs float64 `json:"durationMs"`
	// Error is the node's error, with known secret values and URL
	// credentials redacted, bounded to maxRunFieldBytes.
	Error string    `json:"error,omitempty"`
	At    time.Time `json:"at"`
}

// ListRuns lists the recorded runs of a stored flow, newest first. limit is
// clamped to [1, maxRunsPerPage] (default defaultRunsPerPage); before, when
// set, is the ID of a run of the flow, and the page starts after it (use
// RunList.NextBefore).
func (r *Reads) ListRuns(ctx context.Context, actor Actor, flowID string, limit int, before string) (RunList, error) {
	if err := r.authorize(ctx, actor); err != nil {
		return RunList{}, err
	}
	f, err := r.loadFlow(flowID)
	if err != nil {
		return RunList{}, err
	}
	out := RunList{Runs: []RunSummary{}}
	if r.history == nil {
		return out, nil
	}
	if limit <= 0 {
		limit = defaultRunsPerPage
	}
	limit = min(limit, maxRunsPerPage)
	runs, err := r.history.ListFlowRunsBefore(flowID, before, limit+1)
	if errors.Is(err, storage.ErrFlowRunNotFound) {
		return RunList{}, unknownRun(before)
	}
	if err != nil {
		return RunList{}, fmt.Errorf("list runs: %w", err)
	}
	red := r.redactor(r.sensitiveValues(f)...)
	for i, run := range runs {
		if i == limit {
			out.NextBefore = out.Runs[limit-1].ID
			break
		}
		out.Runs = append(out.Runs, runSummary(red, run))
	}
	return out, nil
}

// GetRun shows a recorded run of a stored flow with its node events (in
// order, errors redacted and bounded, the whole within maxRunViewBytes). A
// run of another flow is unknown.
func (r *Reads) GetRun(ctx context.Context, actor Actor, flowID, runID string) (RunView, error) {
	if err := r.authorize(ctx, actor); err != nil {
		return RunView{}, err
	}
	f, err := r.loadFlow(flowID)
	if err != nil {
		return RunView{}, err
	}
	if r.history == nil {
		return RunView{}, unknownRun(runID)
	}
	run, err := r.history.GetFlowRun(flowID, runID)
	if errors.Is(err, storage.ErrFlowRunNotFound) {
		return RunView{}, unknownRun(runID)
	}
	if err != nil {
		return RunView{}, fmt.Errorf("get run: %w", err)
	}
	total, err := r.history.CountFlowRunEvents(flowID, runID)
	if err != nil {
		return RunView{}, fmt.Errorf("count run events: %w", err)
	}
	values := append(r.secretValues(), r.sensitiveValues(f)...)
	red := valuesRedactor(values)
	// What GetFlow hides of the flow's settings, redacted from the errors
	// (built for the first one), and the longest text either redactor
	// matches.
	var hidden *secretRedactor
	var longest int
	errorText := func(s string) string {
		if s == "" {
			return ""
		}
		if hidden == nil {
			hiddenValues := r.hiddenValues(f)
			h := valuesRedactor(hiddenValues)
			hidden, longest = &h, longestForm(values, hiddenValues)
		}
		return recordedText(red, *hidden, s, maxRunFieldBytes, longest)
	}
	view := RunView{RunSummary: runSummary(red, *run), Events: []RunEvent{}}
	budget := maxRunViewBytes
	var after int64
pages:
	for {
		events, err := r.history.ListFlowRunEventsAfter(flowID, runID, after, runEventsPage, maxStoredErrorChars+1)
		if err != nil {
			return RunView{}, fmt.Errorf("get run events: %w", err)
		}
		for _, e := range events {
			after = e.ID
			ev := RunEvent{
				NodeID:     capText(red.text(e.NodeID), maxViewIDBytes),
				NodeType:   capText(red.text(e.NodeType), maxViewIDBytes),
				Phase:      e.Phase,
				DurationMs: e.DurationMs,
				Error:      errorText(e.Error),
				At:         e.CreatedAt.UTC(),
			}
			data, err := marshalNoEscape(ev)
			if err != nil || len(data) > budget {
				view.Truncated = true
				break pages
			}
			budget -= len(data)
			view.Events = append(view.Events, ev)
		}
		if len(events) < runEventsPage {
			break
		}
	}
	// Events recorded after the count (a running run) are counted too.
	view.TotalEvents = max(total, len(view.Events))
	view.Truncated = view.Truncated || len(view.Events) < view.TotalEvents
	return view, nil
}

// runSummary maps a stored run.
func runSummary(red secretRedactor, run storage.FlowRun) RunSummary {
	s := RunSummary{
		ID:         capText(red.text(run.ID), maxViewIDBytes),
		FlowID:     capText(red.text(run.FlowID), maxViewIDBytes),
		Status:     run.Status,
		StartedAt:  run.StartedAt.UTC(),
		NodeCount:  run.NodeCount,
		ErrorCount: run.ErrorCount,
	}
	s.Actor, s.ActorToken = runActorOf(run.Actor)
	s.ActorToken = cutText(red.text(s.ActorToken), maxActorTokenBytes)
	if run.FinishedAt != nil {
		finished := run.FinishedAt.UTC()
		dur := finished.Sub(s.StartedAt).Milliseconds()
		s.FinishedAt, s.DurationMs = &finished, &dur
	}
	return s
}

// runActorOf maps a run's recorded actor meta ("", "editor",
// "token:<name>") to a RunActor* and the token's name.
func runActorOf(actor string) (kind, token string) {
	switch {
	case actor == "":
		return RunActorTrigger, ""
	case actor == string(ActorEditor):
		return RunActorEditor, ""
	case strings.HasPrefix(actor, "token:"):
		return RunActorAgent, strings.TrimPrefix(actor, "token:")
	}
	return RunActorOther, ""
}

// recordedText is text the runtime recorded (node errors) as agents see
// it: known secret values redacted (red), URLs hidden past their host
// (hideURLsInText), then what GetFlow hides of the flow's settings redacted
// (hidden), bounded to limit bytes. URLs are hidden before the settings are
// redacted, so an error keeps the scheme and host of the URL it names.
//
// A text longer than maxStoredErrorChars was cut in storage: a value to
// redact may straddle the cut, and its first part, which the redactors do
// not recognize, would end the text, within the shown bytes once redaction
// shrank what precedes it. Such a text therefore loses its last unsafeTail
// bytes (the longest text the redactors match) after redaction, and is
// marked truncated.
func recordedText(red, hidden secretRedactor, s string, limit, unsafeTail int) string {
	cut := utf8.RuneCountInString(s) > maxStoredErrorChars
	if cut {
		s = runePrefix(s, maxStoredErrorChars)
	}
	out := hidden.text(hideURLsInText(red.text(s)))
	if !cut {
		return cutText(out, limit)
	}
	out = utf8Prefix(out, max(0, len(out)-unsafeTail))
	const tail = "…" + truncatedMark
	return utf8Prefix(out, limit-len(tail)) + tail
}

// runePrefix is the first n runes of s.
func runePrefix(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

func unknownRun(runID string) *ToolError {
	return toolError(ErrRunNotFound, fmt.Sprintf("unknown run %q", capText(runID, maxViewIDBytes)))
}
