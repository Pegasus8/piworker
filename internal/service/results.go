package service

import (
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
)

// Bounds of the results kept for Tools.Result. A runtime hands a final
// result over once (docs/design/mcp.md §4), so the service keeps what it
// returned for the caller to read again.
const (
	// finalResultTTL is how long a final result stays readable.
	finalResultTTL = 15 * time.Minute
	// maxKeptResults bounds the results kept; the one closest to its expiry
	// is dropped first. Each is at most max_result_bytes.
	maxKeptResults = 256
)

// callRecord is a call Tools.Result can report on.
type callRecord struct {
	owner   string // Actor.key of the caller
	rt      *flow.FlowRuntime
	result  flow.CallResult // redacted and capped
	final   bool
	expires time.Time
}

// resultStore keeps the results of calls by run ID.
type resultStore struct {
	mu   sync.Mutex
	now  func() time.Time
	runs map[string]*callRecord
}

func newResultStore(now func() time.Time) *resultStore {
	return &resultStore{now: now, runs: map[string]*callRecord{}}
}

// put keeps rec. A running call is kept as long as the runtime keeps its
// waiter (timeout + flow.DefaultCallExpiryGrace), a final one finalResultTTL.
func (s *resultStore) put(runID string, rec *callRecord, timeout time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if rec.final {
		rec.expires = now.Add(finalResultTTL)
	} else {
		rec.expires = now.Add(timeout + flow.DefaultCallExpiryGrace)
	}
	s.pruneLocked(now)
	if _, exists := s.runs[runID]; !exists && len(s.runs) >= maxKeptResults {
		var victim string
		for id, r := range s.runs {
			if victim == "" || r.expires.Before(s.runs[victim].expires) {
				victim = id
			}
		}
		delete(s.runs, victim)
	}
	s.runs[runID] = rec
}

// get returns a copy of the record of runID, or nil.
func (s *resultStore) get(runID string) *callRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.runs[runID]
	if rec == nil {
		return nil
	}
	if !s.now().Before(rec.expires) {
		delete(s.runs, runID)
		return nil
	}
	c := *rec
	return &c
}

// finalize records the final result of a run that was running.
func (s *resultStore) finalize(runID string, res flow.CallResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec := s.runs[runID]; rec != nil {
		rec.result, rec.final = res, true
		rec.expires = s.now().Add(finalResultTTL)
	}
}

func (s *resultStore) pruneLocked(now time.Time) {
	for id, r := range s.runs {
		if !now.Before(r.expires) {
			delete(s.runs, id)
		}
	}
}
