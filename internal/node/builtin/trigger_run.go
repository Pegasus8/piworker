package builtin

import (
	"fmt"
	"sync"
)

// triggerRun tracks the background goroutine of a polling trigger.
//
// Each Start gets its own stop and done channels, which the goroutine
// captures. The previous design shared a t.stopCh field and a WaitGroup:
// the goroutine re-read the field (a data race), a Start racing a Stop could
// leave the old goroutine waiting on the new channel forever, and Add during
// Wait could panic on WaitGroup reuse.
type triggerRun struct {
	mu   sync.Mutex
	stop chan struct{}
	done chan struct{}
}

// begin registers a new run. The caller must start exactly one goroutine
// that watches stop and closes done when it exits.
func (r *triggerRun) begin() (stop <-chan struct{}, done chan<- struct{}, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stop != nil {
		return nil, nil, fmt.Errorf("trigger already running")
	}
	r.stop = make(chan struct{})
	r.done = make(chan struct{})
	return r.stop, r.done, nil
}

// end signals the current run to stop and waits for its goroutine to exit.
// It is a no-op when nothing is running.
func (r *triggerRun) end() {
	r.mu.Lock()
	stop, done := r.stop, r.done
	r.stop, r.done = nil, nil
	r.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
}
