package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestResults(entries, bytes int) (*resultStore, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)}
	return newResultStore(clock.now, entries, bytes), clock
}

func finalRecord(result string) *callRecord {
	return &callRecord{owner: "editor", final: true,
		result: flow.CallResult{Status: flow.CallStatusCompleted, Result: json.RawMessage(result)}}
}

func runningRecord() *callRecord {
	return &callRecord{owner: "editor", result: flow.CallResult{Status: flow.CallStatusRunning}}
}

// The record of a call still running is never evicted for final results:
// its owner still waits for it. Final results go first, the one closest to
// its expiry first.
func TestResultStoreEvictsFinalResultsFirst(t *testing.T) {
	s, clock := newTestResults(4, 1<<20)
	s.put("running", runningRecord(), time.Second)
	for i := range 10 {
		clock.advance(time.Millisecond)
		s.put(fmt.Sprintf("final-%d", i), finalRecord(`{}`), time.Second)
	}
	rec := s.get("running")
	require.NotNil(t, rec, "the running call is kept")
	assert.False(t, rec.final)
	assert.Len(t, s.runs, 4)
	for i := range 7 {
		assert.Nil(t, s.get(fmt.Sprintf("final-%d", i)), "the oldest final results go first")
	}
	for i := 7; i < 10; i++ {
		assert.NotNil(t, s.get(fmt.Sprintf("final-%d", i)))
	}
}

// Running records are bounded by the call slots, not by the store: they are
// kept even past the entry cap.
func TestResultStoreKeepsEveryRunningCall(t *testing.T) {
	s, _ := newTestResults(2, 1<<20)
	for i := range 3 {
		s.put(fmt.Sprintf("running-%d", i), runningRecord(), time.Second)
	}
	for i := range 3 {
		assert.NotNil(t, s.get(fmt.Sprintf("running-%d", i)))
	}
}

// The kept results are bounded in bytes too: a Raspberry Pi cannot pin
// hundreds of megabytes of results.
func TestResultStoreByteBudget(t *testing.T) {
	big := `"` + strings.Repeat("x", 38) + `"` // 40 bytes
	s, clock := newTestResults(100, 100)
	s.put("running", runningRecord(), time.Second)
	for i := range 5 {
		clock.advance(time.Millisecond)
		s.put(fmt.Sprintf("final-%d", i), finalRecord(big), time.Second)
	}
	assert.LessOrEqual(t, s.bytes, 100)
	assert.NotNil(t, s.get("running"))
	assert.Nil(t, s.get("final-2"))
	assert.NotNil(t, s.get("final-3"))
	assert.NotNil(t, s.get("final-4"))

	// Finalizing a running call makes room among the final results too.
	got := s.finalize("running", flow.CallResult{Status: flow.CallStatusCompleted, Result: json.RawMessage(big)})
	assert.JSONEq(t, big, string(got.Result))
	assert.LessOrEqual(t, s.bytes, 100)
	rec := s.get("running")
	require.NotNil(t, rec)
	assert.True(t, rec.final)
	assert.Nil(t, s.get("final-3"), "the oldest final result made room")
	assert.NotNil(t, s.get("final-4"))
}

// The first final result of a run wins: a later one (e.g. "expired" once the
// runtime forgot the run) does not replace it.
func TestResultStoreFinalizeKeepsTheFirstFinalResult(t *testing.T) {
	s, _ := newTestResults(10, 1<<20)
	s.put("run", runningRecord(), time.Second)
	first := s.finalize("run", flow.CallResult{Status: flow.CallStatusCompleted, Result: json.RawMessage(`{"ok":true}`)})
	assert.Equal(t, flow.CallStatusCompleted, first.Status)
	again := s.finalize("run", flow.CallResult{Status: flow.CallStatusExpired})
	assert.Equal(t, flow.CallStatusCompleted, again.Status)
	assert.JSONEq(t, `{"ok":true}`, string(again.Result))
	assert.Equal(t, flow.CallStatusCompleted, s.get("run").result.Status)

	unknown := s.finalize("missing", flow.CallResult{Status: flow.CallStatusExpired})
	assert.Equal(t, flow.CallStatusExpired, unknown.Status, "nothing kept: the given result")
	assert.Nil(t, s.get("missing"))
}

func TestResultStoreExpiry(t *testing.T) {
	s, clock := newTestResults(10, 1<<20)
	s.put("final", finalRecord(`{}`), time.Second)
	clock.advance(finalResultTTL)
	assert.Nil(t, s.get("final"))
	assert.Zero(t, s.bytes)
}
