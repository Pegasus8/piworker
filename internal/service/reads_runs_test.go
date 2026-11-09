package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var runsBase = time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)

// addRun records a finished run of flowID started at runsBase+offset.
func (e *readsEnv) addRun(flowID, runID, actor string, offset, took time.Duration, status string, events ...storage.NodeEventRecord) {
	e.t.Helper()
	start := runsBase.Add(offset)
	require.NoError(e.t, e.store.UpsertFlowRunWithActor(runID, flowID, actor, start))
	errs := 0
	for i := range events {
		events[i].RunID, events[i].FlowID = runID, flowID
		if events[i].CreatedAt.IsZero() {
			events[i].CreatedAt = start.Add(time.Duration(i+1) * time.Millisecond)
		}
		if events[i].Phase == "error" {
			errs++
		}
	}
	require.NoError(e.t, e.store.InsertNodeEvents(events))
	if status != "running" {
		require.NoError(e.t, e.store.FinishFlowRun(runID, status, start.Add(took), len(events), errs))
	}
}

func runListIDs(list RunList) []string {
	ids := []string{}
	for _, r := range list.Runs {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestListRunsShowsStatusActorAndTimings(t *testing.T) {
	e := newReadsEnv(t)
	f := e.save(manualFlow("runs"))
	other := e.save(manualFlow("other"))
	e.addRun(f.ID, "by-trigger", "", 0, 1500*time.Millisecond, "success")
	e.addRun(f.ID, "by-editor", "editor", time.Minute, 20*time.Millisecond, "error")
	e.addRun(f.ID, "by-agent", "token:claude-laptop", 2*time.Minute, 0, "running")
	e.addRun(f.ID, "by-future", "scheduler:x", 3*time.Minute, time.Millisecond, "success")
	e.addRun(other.ID, "elsewhere", "", 0, 0, "success")

	list, err := e.reads.ListRuns(context.Background(), readsAgent(storage.ScopeRead), f.ID, 10, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"by-future", "by-agent", "by-editor", "by-trigger"}, runListIDs(list))
	assert.Empty(t, list.NextBefore)

	byID := map[string]RunSummary{}
	for _, r := range list.Runs {
		byID[r.ID] = r
	}
	trig := byID["by-trigger"]
	assert.Equal(t, "success", trig.Status)
	assert.Equal(t, RunActorTrigger, trig.Actor)
	assert.Empty(t, trig.ActorToken)
	assert.Equal(t, f.ID, trig.FlowID)
	assert.True(t, runsBase.Equal(trig.StartedAt), "%v", trig.StartedAt)
	require.NotNil(t, trig.FinishedAt)
	require.NotNil(t, trig.DurationMs)
	assert.Equal(t, int64(1500), *trig.DurationMs)

	assert.Equal(t, RunActorEditor, byID["by-editor"].Actor)
	assert.Equal(t, "error", byID["by-editor"].Status)

	agent := byID["by-agent"]
	assert.Equal(t, RunActorAgent, agent.Actor)
	assert.Equal(t, "claude-laptop", agent.ActorToken)
	assert.Equal(t, "running", agent.Status)
	assert.Nil(t, agent.FinishedAt)
	assert.Nil(t, agent.DurationMs)

	assert.Equal(t, RunActorOther, byID["by-future"].Actor, "unknown actors are not shown verbatim")
	assert.NotContains(t, mustJSON(t, list), "scheduler:x")

	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(mustJSON(t, list)), &raw))
	r0 := raw["runs"].([]any)[0].(map[string]any)
	for _, key := range []string{"id", "flowId", "status", "actor", "startedAt", "finishedAt", "durationMs", "nodeCount", "errorCount"} {
		assert.Containsf(t, r0, key, "run summary field %s", key)
	}
}

func TestListRunsPages(t *testing.T) {
	e := newReadsEnv(t)
	f := e.save(manualFlow("paged"))
	for i := range 5 {
		e.addRun(f.ID, fmt.Sprintf("run-%d", i), "", time.Duration(i)*time.Second, time.Millisecond, "success")
	}
	ctx := context.Background()
	actor := readsAgent(storage.ScopeRead)

	page, err := e.reads.ListRuns(ctx, actor, f.ID, 2, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"run-4", "run-3"}, runListIDs(page))
	assert.Equal(t, "run-3", page.NextBefore)

	page, err = e.reads.ListRuns(ctx, actor, f.ID, 2, page.NextBefore)
	require.NoError(t, err)
	assert.Equal(t, []string{"run-2", "run-1"}, runListIDs(page))
	assert.Equal(t, "run-1", page.NextBefore)

	page, err = e.reads.ListRuns(ctx, actor, f.ID, 2, page.NextBefore)
	require.NoError(t, err)
	assert.Equal(t, []string{"run-0"}, runListIDs(page))
	assert.Empty(t, page.NextBefore, "no older runs")

	// An exactly full last page has no next page either.
	page, err = e.reads.ListRuns(ctx, actor, f.ID, 5, "")
	require.NoError(t, err)
	assert.Len(t, page.Runs, 5)
	assert.Empty(t, page.NextBefore)
}

func TestListRunsLimits(t *testing.T) {
	e := newReadsEnv(t)
	f := e.save(manualFlow("limits"))
	for i := range maxRunsPerPage + 5 {
		require.NoError(t, e.store.UpsertFlowRun(fmt.Sprintf("run-%03d", i), f.ID, runsBase.Add(time.Duration(i)*time.Second)))
	}
	ctx := context.Background()
	for limit, want := range map[int]int{0: defaultRunsPerPage, -3: defaultRunsPerPage, 7: 7, 1000: maxRunsPerPage} {
		page, err := e.reads.ListRuns(ctx, EditorActor(), f.ID, limit, "")
		require.NoError(t, err)
		assert.Lenf(t, page.Runs, want, "limit %d", limit)
	}
}

func TestListRunsUnknownFlowAndCursor(t *testing.T) {
	e := newReadsEnv(t)
	f := e.save(manualFlow("cursor"))
	other := e.save(manualFlow("other"))
	e.addRun(other.ID, "foreign", "", 0, 0, "success")
	ctx := context.Background()

	_, err := e.reads.ListRuns(ctx, EditorActor(), "missing", 10, "")
	requireKind(t, err, ErrFlowNotFound)
	_, err = e.reads.ListRuns(ctx, EditorActor(), f.ID, 10, "missing")
	assert.Equal(t, "run_not_found", requireKind(t, err, ErrRunNotFound).Code())
	_, err = e.reads.ListRuns(ctx, EditorActor(), f.ID, 10, "foreign")
	requireKind(t, err, ErrRunNotFound)

	empty, err := e.reads.ListRuns(ctx, EditorActor(), f.ID, 10, "")
	require.NoError(t, err)
	assert.Equal(t, `{"runs":[]}`, mustJSON(t, empty), "runs is a list even when empty")
}

func TestReadsWithoutHistory(t *testing.T) {
	e := newReadsEnv(t, func(o *ReadsOptions) { o.History = nil })
	f := e.save(manualFlow("no history"))
	list, err := e.reads.ListRuns(context.Background(), EditorActor(), f.ID, 10, "")
	require.NoError(t, err)
	assert.Empty(t, list.Runs)
	_, err = e.reads.GetRun(context.Background(), EditorActor(), f.ID, "run-1")
	requireKind(t, err, ErrRunNotFound)
	flows, err := e.reads.ListFlows(context.Background(), EditorActor())
	require.NoError(t, err)
	assert.Nil(t, flows.Flows[0].LastRun)
}

func TestGetRunShowsTheNodeEvents(t *testing.T) {
	e := newReadsEnv(t)
	f := e.save(manualFlow("events"))
	e.approvedDeployment(f.ID)
	e.addRun(f.ID, "run-1", "token:bot", 0, 40*time.Millisecond, "error",
		storage.NodeEventRecord{NodeID: "trigger", NodeType: "trigger-manual", Phase: "success", MsgID: "m1", DurationMs: 0.5},
		storage.NodeEventRecord{NodeID: "log", NodeType: "action-log", Phase: "error", MsgID: "m1", DurationMs: 12.25, Error: "boom"},
	)

	run, err := e.reads.GetRun(context.Background(), readsAgent(storage.ScopeRead), f.ID, "run-1")
	require.NoError(t, err)
	assert.Equal(t, "run-1", run.ID)
	assert.Equal(t, f.ID, run.FlowID)
	assert.Equal(t, "error", run.Status)
	assert.Equal(t, RunActorAgent, run.Actor)
	assert.Equal(t, "bot", run.ActorToken)
	assert.Equal(t, 2, run.NodeCount)
	assert.Equal(t, 1, run.ErrorCount)
	require.NotNil(t, run.DurationMs)
	assert.Equal(t, int64(40), *run.DurationMs)
	assert.Equal(t, 2, run.TotalEvents)
	assert.False(t, run.Truncated)
	require.Len(t, run.Events, 2)
	assert.Equal(t, RunEvent{NodeID: "trigger", NodeType: "trigger-manual", Phase: "success", DurationMs: 0.5, At: runsBase.Add(time.Millisecond)}, run.Events[0])
	assert.Equal(t, RunEvent{NodeID: "log", NodeType: "action-log", Phase: "error", DurationMs: 12.25, Error: "boom", At: runsBase.Add(2 * time.Millisecond)}, run.Events[1])

	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(mustJSON(t, run)), &raw))
	for _, key := range []string{"id", "flowId", "status", "actor", "actorToken", "startedAt", "events", "totalEvents", "truncated"} {
		assert.Containsf(t, raw, key, "run view field %s", key)
	}
	ev := raw["events"].([]any)[1].(map[string]any)
	assert.Equal(t, map[string]any{"nodeId": "log", "nodeType": "action-log", "phase": "error", "durationMs": 12.25, "error": "boom", "at": "2026-11-08T10:00:00.002Z"}, ev,
		"events carry no message IDs, payloads or config")
}

func TestGetRunUnknown(t *testing.T) {
	e := newReadsEnv(t)
	f := e.save(manualFlow("a"))
	other := e.save(manualFlow("b"))
	e.addRun(other.ID, "foreign", "", 0, 0, "success")
	ctx := context.Background()

	_, err := e.reads.GetRun(ctx, EditorActor(), "missing", "foreign")
	requireKind(t, err, ErrFlowNotFound)
	_, err = e.reads.GetRun(ctx, EditorActor(), f.ID, "missing")
	requireKind(t, err, ErrRunNotFound)
	// A run is only read through its own flow.
	_, err = e.reads.GetRun(ctx, EditorActor(), f.ID, "foreign")
	requireKind(t, err, ErrRunNotFound)
}

func TestGetRunCapsErrorsAndTheRun(t *testing.T) {
	e := newReadsEnv(t)
	f := e.save(manualFlow("big"))
	e.approvedDeployment(f.ID)
	long := strings.Repeat("é", maxRunFieldBytes)
	events := []storage.NodeEventRecord{{NodeID: "n0", NodeType: "action-log", Phase: "error", Error: long}}
	for i := range 200 {
		events = append(events, storage.NodeEventRecord{NodeID: fmt.Sprintf("n%d", i+1), NodeType: "action-log", Phase: "error", Error: strings.Repeat("x", 1000)})
	}
	e.addRun(f.ID, "run-1", "", 0, time.Second, "error", events...)

	run, err := e.reads.GetRun(context.Background(), EditorActor(), f.ID, "run-1")
	require.NoError(t, err)
	first := run.Events[0].Error
	assert.LessOrEqual(t, len(first), maxRunFieldBytes)
	assert.True(t, strings.HasSuffix(first, truncatedMark))
	assert.True(t, strings.HasPrefix(first, "é"))

	assert.True(t, run.Truncated)
	assert.Equal(t, len(events), run.TotalEvents)
	assert.Less(t, len(run.Events), len(events))
	assert.Equal(t, "n0", run.Events[0].NodeID, "the first events are kept")
	assert.LessOrEqual(t, len(mustJSON(t, run)), maxReadViewBytes, "the whole view, summary included")
}

// A page of runs with long ids stays within maxReadViewBytes: it ends
// early, and its nextBefore continues after its last run, so paging still
// lists every run once.
func TestListRunsBoundsThePage(t *testing.T) {
	e := newReadsEnv(t)
	f := manualFlow("long ids")
	f.ID = strings.Repeat("f", maxViewIDBytes)
	e.save(f)
	token := "token:" + strings.Repeat("t", maxActorTokenBytes)
	for i := range maxRunsPerPage {
		e.addRun(f.ID, fmt.Sprintf("%03d%s", i, strings.Repeat("r", maxViewIDBytes-3)), token, time.Duration(i)*time.Second, time.Millisecond, "success")
	}
	ctx := context.Background()
	seen := map[string]bool{}
	before := ""
	for pages := 0; ; pages++ {
		require.Less(t, pages, maxRunsPerPage, "paging ends")
		page, err := e.reads.ListRuns(ctx, EditorActor(), f.ID, maxRunsPerPage, before)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(mustJSON(t, page)), maxReadViewBytes)
		require.NotEmpty(t, page.Runs)
		for _, r := range page.Runs {
			assert.False(t, seen[r.ID], "listed twice: %s", r.ID)
			seen[r.ID] = true
		}
		if pages == 0 {
			assert.Less(t, len(page.Runs), maxRunsPerPage, "the first page ends early")
		}
		if page.NextBefore == "" {
			break
		}
		assert.Equal(t, page.Runs[len(page.Runs)-1].ID, page.NextBefore)
		before = page.NextBefore
	}
	assert.Len(t, seen, maxRunsPerPage)
}

// Secret values that leaked into recorded node errors or token names are
// never shown, in any encoding; URL credentials in errors are dropped too.
func TestRunViewsRedactSecrets(t *testing.T) {
	e := newReadsEnv(t)
	const value = "Zq9-Secret/Value+7&k"
	// Quotes, backslashes and HTML characters change when JSON- or
	// Go-quoted.
	const quoted = "Kx\"7\\q<9>"
	require.NoError(t, e.secrets.Set("API_KEY", value))
	require.NoError(t, e.secrets.Set("QUOTED", quoted))
	f := e.save(manualFlow("leaky"))
	e.approvedDeployment(f.ID)

	var events []storage.NodeEventRecord
	for _, v := range []string{value, quoted} {
		for name, leak := range secretLeaks(v) {
			events = append(events, storage.NodeEventRecord{NodeID: "http", NodeType: "action-http", Phase: "error", Error: name + ": request failed: " + leak})
		}
	}
	events = append(events, storage.NodeEventRecord{NodeID: "http", NodeType: "action-http", Phase: "error",
		Error: "GET https://admin:pa55word@example.com/x?key=abc123 failed"})
	e.addRun(f.ID, "run-1", "token:bot-"+value, 0, time.Second, "error", events...)

	ctx := context.Background()
	actor := readsAgent(storage.ScopeRead)
	run, err := e.reads.GetRun(ctx, actor, f.ID, "run-1")
	require.NoError(t, err)
	list, err := e.reads.ListRuns(ctx, actor, f.ID, 10, "")
	require.NoError(t, err)
	flows, err := e.reads.ListFlows(ctx, actor)
	require.NoError(t, err)

	for view, out := range map[string]string{"GetRun": leakText(t, run), "ListRuns": leakText(t, list), "ListFlows": leakText(t, flows)} {
		for _, v := range []string{value, quoted} {
			for name, form := range secretForms(v) {
				assert.NotContainsf(t, out, form, "%s leaked the secret %q as %s", view, v, name)
			}
		}
		assert.NotContainsf(t, out, "pa55word", "%s leaked URL credentials", view)
		assert.NotContainsf(t, out, "abc123", "%s leaked a URL query", view)
	}
	assert.Contains(t, mustJSON(t, run), redactedMark)
}

// A credential typed literally into a Sensitive property (instead of a
// {{secret.NAME}} reference) is redacted like a stored secret wherever the
// flow's views could echo it: node errors of its runs and visible config.
func TestReadsRedactLiteralSensitiveConfig(t *testing.T) {
	e := newReadsEnv(t)
	const botToken = "123456:AAH-literalBotToken"
	const headerCred = "literal-Header-Cred"
	f := manualFlow("Bot " + botToken)
	tg := flow.NewNode("action-telegram", flow.NodeCategoryOutput)
	tg.ID = "telegram"
	tg.Config = map[string]any{"botToken": botToken, "chatId": "42", "message": "hi"}
	httpN := flow.NewNode("action-http", flow.NodeCategoryOutput)
	httpN.ID = "http"
	httpN.Config = map[string]any{
		"url":     "https://example.com/{{secret.API_KEY}}",
		"headers": map[string]any{"Authorization": "Bearer " + headerCred, "X-Ref": "{{secret.API_KEY}}"},
	}
	debug := flow.NewNode("process-debug", flow.NodeCategoryProcessing)
	debug.ID = "debug"
	debug.Config = map[string]any{"name": "copy of " + botToken + ", see {{secret.API_KEY}}"}
	f.AddNode(*tg)
	f.AddNode(*httpN)
	f.AddNode(*debug)
	e.save(f)
	e.approvedDeployment(f.ID)
	e.addRun(f.ID, "run-1", "token:bot "+headerCred, 0, time.Second, "error",
		storage.NodeEventRecord{NodeID: "telegram", NodeType: "action-telegram", Phase: "error", Error: "unauthorized bot " + botToken + `; reply {"token":` + jsonString(botToken) + `}`},
		storage.NodeEventRecord{NodeID: "http", NodeType: "action-http", Phase: "error", Error: "header Bearer " + headerCred + " rejected"},
	)
	// Another flow without these settings shows its name as it is.
	plain := e.save(manualFlow("Plain"))

	ctx := context.Background()
	actor := readsAgent(storage.ScopeRead)
	run, err := e.reads.GetRun(ctx, actor, f.ID, "run-1")
	require.NoError(t, err)
	view, err := e.reads.GetFlow(ctx, actor, f.ID)
	require.NoError(t, err)
	flows, err := e.reads.ListFlows(ctx, actor)
	require.NoError(t, err)
	runs, err := e.reads.ListRuns(ctx, actor, f.ID, 10, "")
	require.NoError(t, err)
	prompt, err := e.reads.DiagnoseFlow(ctx, actor, f.ID)
	require.NoError(t, err)
	for name, out := range map[string]string{"GetRun": leakText(t, run), "GetFlow": leakText(t, view),
		"ListFlows": leakText(t, flows), "ListRuns": leakText(t, runs), "DiagnoseFlow": prompt} {
		for _, cred := range []string{botToken, headerCred} {
			for form, text := range secretForms(cred) {
				assert.NotContainsf(t, out, text, "%s leaked %q as %s", name, cred, form)
			}
		}
	}
	assert.Contains(t, run.Events[0].Error, "unauthorized bot "+redactedMark)
	names := map[string]string{}
	for _, fs := range flows.Flows {
		names[fs.ID] = fs.Name
	}
	assert.Equal(t, "Bot "+redactedMark, names[f.ID], "every view of the flow redacts its literal credentials")
	assert.Equal(t, "Plain", names[plain.ID])
	assert.Equal(t, "bot "+redactedMark, runs.Runs[0].ActorToken)
	// Templated Sensitive values are references, not credentials: a
	// visible value keeps the same reference.
	assert.Equal(t, "copy of "+redactedMark+", see {{secret.API_KEY}}", view.Nodes[4].Config["name"])
}

// countingHistory counts what the run views read of the run history.
type countingHistory struct {
	*storage.SQLiteStore
	rows, longestError int
}

func (h *countingHistory) ListFlowRunEventsAfter(flowID, runID string, afterID int64, limit, maxErrorChars int) ([]storage.NodeEventRecord, error) {
	events, err := h.SQLiteStore.ListFlowRunEventsAfter(flowID, runID, afterID, limit, maxErrorChars)
	h.rows += len(events)
	for _, e := range events {
		h.longestError = max(h.longestError, len(e.Error))
	}
	return events, err
}

// GetRun reads a bounded part of a run, however many events it has and
// however long their errors: the events it can show (a page at a time)
// with their errors cut in storage, and the count of all of them.
func TestGetRunReadsABoundedPartOfTheRun(t *testing.T) {
	history := &countingHistory{}
	e := newReadsEnv(t, func(o *ReadsOptions) { o.History = history })
	history.SQLiteStore = e.store
	f := e.save(manualFlow("huge"))
	e.approvedDeployment(f.ID)

	var events []storage.NodeEventRecord
	for i := range 1500 {
		events = append(events, storage.NodeEventRecord{NodeID: fmt.Sprintf("n%d", i), NodeType: "action-log", Phase: "error",
			Error: strings.Repeat("x", 3*maxStoredErrorChars)})
	}
	e.addRun(f.ID, "long-errors", "", 0, time.Second, "error", events...)
	run, err := e.reads.GetRun(context.Background(), EditorActor(), f.ID, "long-errors")
	require.NoError(t, err)
	assert.Equal(t, 1500, run.TotalEvents)
	assert.True(t, run.Truncated)
	assert.LessOrEqual(t, history.rows, len(run.Events)+runEventsPage, "only the pages the view needs are read")
	assert.LessOrEqual(t, history.longestError, maxStoredErrorChars+1, "errors are cut in storage")

	events = events[:0]
	for i := range 3000 {
		events = append(events, storage.NodeEventRecord{NodeID: fmt.Sprintf("n%d", i), NodeType: "action-log", Phase: "success"})
	}
	e.addRun(f.ID, "many-events", "", time.Minute, time.Second, "success", events...)
	history.rows = 0
	run, err = e.reads.GetRun(context.Background(), EditorActor(), f.ID, "many-events")
	require.NoError(t, err)
	assert.Equal(t, 3000, run.TotalEvents)
	assert.True(t, run.Truncated)
	assert.Less(t, len(run.Events), 3000)
	assert.LessOrEqual(t, history.rows, len(run.Events)+runEventsPage)
	assert.Equal(t, "n0", run.Events[0].NodeID)
	assert.Equal(t, fmt.Sprintf("n%d", len(run.Events)-1), run.Events[len(run.Events)-1].NodeID, "in order, none skipped")
}

// A secret straddling the storage cut of a long error is not recognized
// by the redactor; the text before it can shrink under redaction until the
// secret's first part would be shown, so a cut error loses its last bytes
// (as many as the longest redacted form) after redaction.
func TestGetRunHidesASecretCutInStorage(t *testing.T) {
	e := newReadsEnv(t)
	long := strings.Repeat("L", 64)
	const straddling = "PREFIXabcd-and-the-rest-of-the-secret"
	require.NoError(t, e.secrets.Set("LONG", long))
	require.NoError(t, e.secrets.Set("STRADDLING", straddling))
	f := e.save(manualFlow("cut"))
	e.approvedDeployment(f.ID)
	// 120 long secrets (shrinking by 54 bytes each), filler, then the
	// straddling secret starting 10 characters before the cut.
	head := strings.Repeat(long, 120)
	msg := head + strings.Repeat("-", maxStoredErrorChars-len(head)-10) + straddling
	e.addRun(f.ID, "run-1", "", 0, time.Second, "error",
		storage.NodeEventRecord{NodeID: "n", NodeType: "action-log", Phase: "error", Error: msg})

	run, err := e.reads.GetRun(context.Background(), readsAgent(storage.ScopeRead), f.ID, "run-1")
	require.NoError(t, err)
	shown := run.Events[0].Error
	assert.NotContains(t, shown, "PREFIX")
	assert.True(t, strings.HasPrefix(shown, redactedMark), shown)
	assert.True(t, strings.HasSuffix(shown, truncatedMark), shown)
}
