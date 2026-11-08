package storage

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func auditEntry(i int, at time.Time) MCPAuditEntry {
	return MCPAuditEntry{
		TS:          at,
		TokenID:     "tok-a",
		TokenName:   "laptop",
		Tool:        "lights.on",
		FlowID:      "flow-1",
		RunID:       fmt.Sprintf("run-%d", i),
		ArgsSHA256:  "abc123",
		ArgsPreview: `{"room":"kitchen"}`,
		Outcome:     "completed",
		DurMs:       int64(10 + i),
	}
}

func TestInsertAndListMCPAudit(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	require.NoError(t, s.InsertMCPAudit(nil, testNow), "an empty batch is a no-op")

	at := testNow.Add(123 * time.Millisecond)
	first := auditEntry(1, at)
	first.Privileged = true
	second := auditEntry(2, at.Add(time.Second))
	require.NoError(t, s.InsertMCPAudit([]MCPAuditEntry{first, second}, testNow))

	got, err := s.ListMCPAudit(MCPAuditFilter{})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "run-2", got[0].RunID, "newest first")
	e := got[1]
	assert.NotZero(t, e.ID)
	assert.Equal(t, at, e.TS, "millisecond precision is kept")
	assert.Equal(t, "tok-a", e.TokenID)
	assert.Equal(t, "laptop", e.TokenName)
	assert.Equal(t, "lights.on", e.Tool)
	assert.Equal(t, "flow-1", e.FlowID)
	assert.Equal(t, "run-1", e.RunID)
	assert.Equal(t, "abc123", e.ArgsSHA256)
	assert.Equal(t, `{"room":"kitchen"}`, e.ArgsPreview)
	assert.Equal(t, "completed", e.Outcome)
	assert.Equal(t, int64(11), e.DurMs)
	assert.True(t, e.Privileged)
	assert.False(t, got[0].Privileged)

	empty := createTestStore(t)
	defer empty.Close()
	none, err := empty.ListMCPAudit(MCPAuditFilter{})
	require.NoError(t, err)
	assert.NotNil(t, none)
}

func TestMCPAuditPreviewIsTruncatedOnRuneBoundary(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	e := auditEntry(1, testNow)
	e.ArgsPreview = strings.Repeat("é", MaxMCPAuditPreview) // 2 bytes each
	require.NoError(t, s.InsertMCPAudit([]MCPAuditEntry{e}, testNow))
	got, err := s.ListMCPAudit(MCPAuditFilter{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.LessOrEqual(t, len(got[0].ArgsPreview), MaxMCPAuditPreview)
	assert.True(t, utf8.ValidString(got[0].ArgsPreview))
	assert.True(t, strings.HasSuffix(got[0].ArgsPreview, "…"))
}

func TestListMCPAuditFilters(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	var batch []MCPAuditEntry
	for i := range 6 {
		e := auditEntry(i, testNow.Add(time.Duration(i)*time.Minute))
		if i%2 == 1 {
			e.TokenID, e.Tool, e.FlowID, e.Outcome = "tok-b", "pumps.stop", "flow-2", "error"
		}
		batch = append(batch, e)
	}
	require.NoError(t, s.InsertMCPAudit(batch, testNow))

	runIDs := func(f MCPAuditFilter) []string {
		got, err := s.ListMCPAudit(f)
		require.NoError(t, err)
		ids := make([]string, len(got))
		for i, e := range got {
			ids[i] = e.RunID
		}
		return ids
	}
	assert.Equal(t, []string{"run-5", "run-3", "run-1"}, runIDs(MCPAuditFilter{TokenID: "tok-b"}))
	assert.Equal(t, []string{"run-4", "run-2", "run-0"}, runIDs(MCPAuditFilter{Tool: "lights.on"}))
	assert.Equal(t, []string{"run-5", "run-3", "run-1"}, runIDs(MCPAuditFilter{FlowID: "flow-2"}))
	assert.Equal(t, []string{"run-4", "run-2", "run-0"}, runIDs(MCPAuditFilter{Outcome: "completed"}))
	assert.Equal(t, []string{"run-3", "run-2"}, runIDs(MCPAuditFilter{Since: testNow.Add(2 * time.Minute), Until: testNow.Add(4 * time.Minute)}))
	assert.Equal(t, []string{"run-5", "run-4"}, runIDs(MCPAuditFilter{Limit: 2}))

	page, err := s.ListMCPAudit(MCPAuditFilter{Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, []string{"run-3", "run-2"}, runIDs(MCPAuditFilter{Limit: 2, BeforeID: page[1].ID}), "BeforeID pages backwards")
}

func TestListMCPAuditPagesInInsertionOrder(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	// Batches are written as calls finish, so ts (when a call happened) is
	// not monotonic in id: a long call finishes late, and a Pi without an
	// RTC can jump its clock at NTP sync.
	for i, offset := range []time.Duration{-time.Minute, -3 * time.Minute, -2 * time.Minute, -10 * time.Minute, 5 * time.Minute} {
		require.NoError(t, s.InsertMCPAudit([]MCPAuditEntry{auditEntry(i, testNow.Add(offset))}, testNow))
	}

	var seen []string
	var before int64
	for range 10 {
		page, err := s.ListMCPAudit(MCPAuditFilter{Limit: 2, BeforeID: before})
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		for _, e := range page {
			seen = append(seen, e.RunID)
		}
		before = page[len(page)-1].ID
	}
	assert.Equal(t, []string{"run-4", "run-3", "run-2", "run-1", "run-0"}, seen, "paging returns every row exactly once, most recently recorded first")
}

func TestMCPAuditRetentionByTime(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	old := auditEntry(1, testNow.Add(-MCPAuditRetention-time.Minute))
	old.Privileged = true
	require.NoError(t, s.InsertMCPAudit([]MCPAuditEntry{old}, testNow.Add(-MCPAuditRetention)))
	got, err := s.ListMCPAudit(MCPAuditFilter{})
	require.NoError(t, err)
	require.Len(t, got, 1)

	// The next batch drops everything older than 30 days, privileged included.
	require.NoError(t, s.InsertMCPAudit([]MCPAuditEntry{auditEntry(2, testNow)}, testNow))
	got, err = s.ListMCPAudit(MCPAuditFilter{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "run-2", got[0].RunID)
}

func TestPruneMCPAuditRowCapSparesPrivileged(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	var batch []MCPAuditEntry
	for i := range 6 {
		e := auditEntry(i, testNow.Add(time.Duration(i)*time.Second))
		e.Privileged = i == 0
		batch = append(batch, e)
	}
	require.NoError(t, s.InsertMCPAudit(batch, testNow))

	n, err := s.PruneMCPAudit(testNow, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
	got, err := s.ListMCPAudit(MCPAuditFilter{})
	require.NoError(t, err)
	ids := []string{}
	for _, e := range got {
		ids = append(ids, e.RunID)
	}
	assert.Equal(t, []string{"run-5", "run-4", "run-3", "run-0"}, ids, "privileged calls are never evicted early")

	n, err = s.PruneMCPAudit(testNow.Add(MCPAuditRetention+time.Hour), 0)
	require.NoError(t, err)
	assert.Equal(t, int64(4), n, "time retention applies to every row")
}

func TestMCPRejectionCounters(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	minute := testNow.Add(42 * time.Second)
	require.NoError(t, s.AddMCPRejections(nil, testNow))
	require.NoError(t, s.AddMCPRejections([]MCPRejection{
		{TokenID: "tok-a", At: minute, Reason: "busy", Count: 2},
		{TokenID: "tok-a", At: minute.Add(5 * time.Second), Reason: "busy", Count: 1},
		{TokenID: "tok-a", At: minute, Reason: "invalid_args", Count: 1},
		{TokenID: "tok-b", At: minute.Add(time.Minute), Reason: "cooldown", Count: 4},
		{TokenID: "tok-b", At: minute, Reason: "cooldown", Count: 0},
	}, testNow))
	require.NoError(t, s.AddMCPRejections([]MCPRejection{{TokenID: "tok-a", At: minute, Reason: "busy", Count: 3}}, testNow))

	all, err := s.ListMCPRejections(MCPAuditFilter{})
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, MCPRejection{TokenID: "tok-b", At: testNow.Add(time.Minute), Reason: "cooldown", Count: 4}, all[0])
	byReason := map[string]int{}
	for _, r := range all[1:] {
		assert.Equal(t, testNow, r.At, "counters are bucketed per minute")
		byReason[r.Reason] = r.Count
	}
	assert.Equal(t, map[string]int{"busy": 6, "invalid_args": 1}, byReason)

	onlyB, err := s.ListMCPRejections(MCPAuditFilter{TokenID: "tok-b"})
	require.NoError(t, err)
	require.Len(t, onlyB, 1)

	// Counters follow the same 30-day retention.
	require.NoError(t, s.AddMCPRejections([]MCPRejection{{TokenID: "tok-c", At: testNow.Add(MCPAuditRetention + time.Hour), Reason: "busy", Count: 1}}, testNow.Add(MCPAuditRetention+time.Hour)))
	all, err = s.ListMCPRejections(MCPAuditFilter{})
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "tok-c", all[0].TokenID)
}

func TestMCPAuditClosedStore(t *testing.T) {
	s := createTestStore(t)
	require.NoError(t, s.Close())
	require.ErrorIs(t, s.InsertMCPAudit([]MCPAuditEntry{auditEntry(1, testNow)}, testNow), ErrDatabaseClosed)
	_, err := s.ListMCPAudit(MCPAuditFilter{})
	require.ErrorIs(t, err, ErrDatabaseClosed)
}
