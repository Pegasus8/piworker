package service

import (
	"fmt"
	"sync"
	"time"
)

// slots bounds concurrent calls: per tool (its maxConcurrent), across every
// tool (mcp.max_inflight) and per token. A slot is held from the call until
// its run ends (FlowRuntime.RunDone), not just until the reply, so a slow
// side branch still counts. A tool's cooldown runs from the end of its last
// run.
type slots struct {
	mu          sync.Mutex
	now         func() time.Time
	maxGlobal   int
	maxPerToken int
	global      int
	perToken    map[string]int
	tools       map[string]*toolSlots
}

type toolSlots struct {
	inflight int
	lastEnd  time.Time
}

func newSlots(maxGlobal, maxPerToken int, now func() time.Time) *slots {
	return &slots{
		now:         now,
		maxGlobal:   maxGlobal,
		maxPerToken: maxPerToken,
		perToken:    map[string]int{},
		tools:       map[string]*toolSlots{},
	}
}

// acquire takes a slot of tool (at most maxConcurrent at once, cooldown
// after each run) for tokenKey ("" for callers without a per-token limit).
// It never waits: a full limit is ErrBusy, a running cooldown ErrCooldown.
// release frees the slot and, when ran is set (a run started and ended),
// starts the cooldown; calling it again does nothing.
func (s *slots) acquire(tool string, maxConcurrent int, cooldown time.Duration, tokenKey string) (release func(ran bool), err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := s.tools[tool]
	if ts == nil {
		ts = &toolSlots{}
		s.tools[tool] = ts
	}
	if cooldown > 0 && !ts.lastEnd.IsZero() {
		if wait := ts.lastEnd.Add(cooldown).Sub(s.now()); wait > 0 {
			return nil, &ToolError{Kind: ErrCooldown, RetryAfter: wait,
				Message: fmt.Sprintf("the tool is cooling down; retry in %d ms", wait.Milliseconds())}
		}
	}
	switch {
	case ts.inflight >= max(maxConcurrent, 1):
		return nil, toolError(ErrBusy, fmt.Sprintf("the tool already runs %d call(s), its limit; retry when one finishes", ts.inflight))
	case s.maxGlobal > 0 && s.global >= s.maxGlobal:
		return nil, toolError(ErrBusy, "too many tool calls are running; retry when one finishes")
	case tokenKey != "" && s.maxPerToken > 0 && s.perToken[tokenKey] >= s.maxPerToken:
		return nil, toolError(ErrBusy, fmt.Sprintf("this token already runs %d call(s), its limit; retry when one finishes", s.perToken[tokenKey]))
	}
	ts.inflight++
	s.global++
	if tokenKey != "" {
		s.perToken[tokenKey]++
	}
	var once sync.Once
	return func(ran bool) { once.Do(func() { s.release(tool, tokenKey, ran) }) }, nil
}

func (s *slots) release(tool, tokenKey string, ran bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ts := s.tools[tool]; ts != nil {
		ts.inflight--
		if ran {
			ts.lastEnd = s.now()
		}
	}
	s.global--
	if tokenKey != "" {
		if s.perToken[tokenKey]--; s.perToken[tokenKey] <= 0 {
			delete(s.perToken, tokenKey)
		}
	}
}

// inflight reports the calls of tool holding a slot.
func (s *slots) inflight(tool string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ts := s.tools[tool]; ts != nil {
		return ts.inflight
	}
	return 0
}
