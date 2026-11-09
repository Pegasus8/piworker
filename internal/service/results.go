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
	// runningRecordSlack is how much longer than the runtime's waiter a
	// record of a running call is kept, as a safety net: the run's end
	// (FlowRuntime.RunDone, closed at the latest when the waiter expires)
	// finalizes it before.
	runningRecordSlack = time.Minute
	// maxKeptResults bounds the final results kept; the one closest to its
	// expiry is dropped first. Each is at most max_result_bytes.
	maxKeptResults = 256
	// maxKeptResultBytes bounds the bytes of the final results kept (up to
	// 256 results of max_result_bytes, which may be 1 MiB, would pin far
	// more memory than a Raspberry Pi can spare).
	maxKeptResultBytes = 8 << 20
)

// callRecord is a call Tools.Result can report on.
type callRecord struct {
	owner   string // Actor.key of the caller
	rt      *flow.FlowRuntime
	view    resultView      // how the caller sees the results
	result  flow.CallResult // redacted and capped
	final   bool
	expires time.Time
}

// size is what a record's result weighs against maxKeptResultBytes.
func (r *callRecord) size() int {
	n := len(r.result.Result)
	if r.result.Error != nil {
		n += len(r.result.Error.Message)
	}
	return n
}

// resultStore keeps the results of calls by run ID. Records of calls still
// running are never evicted for room (their callers wait for them, and the
// call slots bound how many there are); final results are, oldest expiry
// first, beyond maxEntries records or maxBytes of results.
type resultStore struct {
	mu         sync.Mutex
	now        func() time.Time
	maxEntries int
	maxBytes   int
	bytes      int // sum of the kept records' size
	runs       map[string]*callRecord
}

func newResultStore(now func() time.Time, maxEntries, maxBytes int) *resultStore {
	return &resultStore{now: now, maxEntries: maxEntries, maxBytes: maxBytes, runs: map[string]*callRecord{}}
}

// put keeps rec. A running call is kept until its run ends (finalize), a
// final one finalResultTTL.
func (s *resultStore) put(runID string, rec *callRecord, timeout time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if rec.final {
		rec.expires = now.Add(finalResultTTL)
	} else {
		rec.expires = now.Add(timeout + flow.DefaultCallExpiryGrace + runningRecordSlack)
	}
	s.pruneLocked(now)
	if s.runs[runID] != nil {
		s.dropLocked(runID)
	}
	s.makeRoomLocked(runID, 1, rec.size())
	s.runs[runID] = rec
	s.bytes += rec.size()
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
		s.dropLocked(runID)
		return nil
	}
	c := *rec
	return &c
}

// finalize records the final result of a run that was running and returns
// the final result kept: res, or the one recorded first (a later result,
// e.g. "expired" once the runtime forgot the run, never replaces it). A run
// no longer kept returns res.
func (s *resultStore) finalize(runID string, res flow.CallResult) flow.CallResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.runs[runID]
	if rec == nil {
		return res
	}
	if rec.final {
		return rec.result
	}
	s.bytes -= rec.size()
	rec.result, rec.final = res, true
	rec.expires = s.now().Add(finalResultTTL)
	s.makeRoomLocked(runID, 0, rec.size())
	s.bytes += rec.size()
	return res
}

// makeRoomLocked evicts final records other than keep, the one closest to
// its expiry first, until adding entries records of size bytes fits the
// bounds, or no final record is left to evict.
func (s *resultStore) makeRoomLocked(keep string, entries, size int) {
	for len(s.runs)+entries > s.maxEntries || s.bytes+size > s.maxBytes {
		victim := ""
		for id, r := range s.runs {
			if id != keep && r.final && (victim == "" || r.expires.Before(s.runs[victim].expires)) {
				victim = id
			}
		}
		if victim == "" {
			return
		}
		s.dropLocked(victim)
	}
}

func (s *resultStore) dropLocked(runID string) {
	if rec := s.runs[runID]; rec != nil {
		s.bytes -= rec.size()
		delete(s.runs, runID)
	}
}

func (s *resultStore) pruneLocked(now time.Time) {
	for id, r := range s.runs {
		if !now.Before(r.expires) {
			s.dropLocked(id)
		}
	}
}
