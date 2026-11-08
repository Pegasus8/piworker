package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAuditStore struct {
	mu         sync.Mutex
	batches    [][]storage.MCPAuditEntry
	rejections []storage.MCPRejection
	fail       error
}

func (s *fakeAuditStore) InsertMCPAudit(batch []storage.MCPAuditEntry, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.batches = append(s.batches, append([]storage.MCPAuditEntry(nil), batch...))
	return nil
}

func (s *fakeAuditStore) AddMCPRejections(batch []storage.MCPRejection, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.rejections = append(s.rejections, batch...)
	return nil
}

func (s *fakeAuditStore) entries() []storage.MCPAuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []storage.MCPAuditEntry
	for _, b := range s.batches {
		out = append(out, b...)
	}
	return out
}

func (s *fakeAuditStore) rejected() []storage.MCPRejection {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]storage.MCPRejection(nil), s.rejections...)
}

func TestAuditWriterBatchesUntilFlush(t *testing.T) {
	store := &fakeAuditStore{}
	w := NewAuditWriter(store, AuditOptions{FlushInterval: time.Hour})
	w.Record(storage.MCPAuditEntry{Tool: "a", Outcome: "completed"})
	w.Record(storage.MCPAuditEntry{Tool: "b", Outcome: "error"})
	assert.Empty(t, store.entries(), "written in batches, not per call")
	require.NoError(t, w.Flush())
	require.Len(t, store.batches, 1)
	assert.Equal(t, []string{"a", "b"}, []string{store.entries()[0].Tool, store.entries()[1].Tool})
	require.NoError(t, w.Flush(), "nothing left")
	assert.Len(t, store.batches, 1)
}

func TestAuditWriterFlushesOnItsOwn(t *testing.T) {
	store := &fakeAuditStore{}
	w := NewAuditWriter(store, AuditOptions{FlushInterval: 10 * time.Millisecond})
	defer w.Close(context.Background())
	w.Record(storage.MCPAuditEntry{Tool: "a"})
	assert.Eventually(t, func() bool { return len(store.entries()) == 1 }, 2*time.Second, 5*time.Millisecond)
}

func TestAuditWriterDropsAndCountsWhenFull(t *testing.T) {
	store := &fakeAuditStore{}
	w := NewAuditWriter(store, AuditOptions{FlushInterval: time.Hour, MaxQueue: 2})
	for range 5 {
		w.Record(storage.MCPAuditEntry{Tool: "a"})
	}
	assert.Equal(t, int64(3), w.Dropped())
	require.NoError(t, w.Flush())
	assert.Len(t, store.entries(), 2)
}

func TestAuditWriterCountsRejectionsPerTokenMinuteAndReason(t *testing.T) {
	store := &fakeAuditStore{}
	w := NewAuditWriter(store, AuditOptions{FlushInterval: time.Hour})
	at := time.Date(2026, 11, 8, 12, 0, 10, 0, time.UTC)
	w.Reject("tok-a", "busy", at)
	w.Reject("tok-a", "busy", at.Add(20*time.Second))
	w.Reject("tok-a", "cooldown", at)
	w.Reject("tok-b", "busy", at)
	w.Reject("tok-a", "busy", at.Add(time.Minute))
	require.NoError(t, w.Flush())
	got := map[string]int{}
	for _, r := range store.rejected() {
		got[r.TokenID+"/"+r.Reason+"/"+r.At.Format("15:04")] += r.Count
	}
	assert.Equal(t, map[string]int{
		"tok-a/busy/12:00":     2,
		"tok-a/cooldown/12:00": 1,
		"tok-b/busy/12:00":     1,
		"tok-a/busy/12:01":     1,
	}, got)
}

func TestAuditWriterCloseFlushesAndStops(t *testing.T) {
	store := &fakeAuditStore{}
	w := NewAuditWriter(store, AuditOptions{FlushInterval: time.Hour})
	w.Record(storage.MCPAuditEntry{Tool: "a"})
	w.Reject("tok", "busy", time.Now())
	require.NoError(t, w.Close(context.Background()))
	assert.Len(t, store.entries(), 1)
	assert.Len(t, store.rejected(), 1)

	w.Record(storage.MCPAuditEntry{Tool: "late"})
	assert.Equal(t, int64(1), w.Dropped(), "nothing is accepted after Close")
	require.NoError(t, w.Close(context.Background()), "Close is idempotent")
	assert.Len(t, store.entries(), 1)
}

func TestAuditWriterCountsLostBatches(t *testing.T) {
	store := &fakeAuditStore{fail: errors.New("disk full")}
	w := NewAuditWriter(store, AuditOptions{FlushInterval: time.Hour})
	w.Record(storage.MCPAuditEntry{Tool: "a"})
	w.Record(storage.MCPAuditEntry{Tool: "b"})
	require.Error(t, w.Flush())
	assert.Equal(t, int64(2), w.Dropped())
}

// A nil writer is a valid "no audit" sink.
func TestNilAuditWriter(t *testing.T) {
	var w *AuditWriter
	w.Record(storage.MCPAuditEntry{})
	w.Reject("t", "busy", time.Now())
	require.NoError(t, w.Flush())
	require.NoError(t, w.Close(context.Background()))
	assert.Zero(t, w.Dropped())
}
