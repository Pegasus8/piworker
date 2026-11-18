package mcpserver

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePruner struct {
	mu    sync.Mutex
	calls []time.Time
	rows  []int
	err   error
	done  chan struct{}
}

func (p *fakePruner) PruneMCPAudit(now time.Time, maxRows int) (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, now)
	p.rows = append(p.rows, maxRows)
	if p.done != nil {
		p.done <- struct{}{}
	}
	return 3, p.err
}

func (p *fakePruner) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}

// The audit is pruned once at start, then on every tick, until the context
// ends.
func TestAuditRetentionRunsAtStartAndOnEveryTick(t *testing.T) {
	clock := newClock()
	pruner := &fakePruner{done: make(chan struct{}, 8)}
	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		RunAuditRetention(ctx, pruner, RetentionOptions{Ticks: ticks, Now: clock.Now, MaxRows: 7, Logger: zerolog.Nop()})
		close(stopped)
	}()

	<-pruner.done // at start, before any tick
	assert.Equal(t, 1, pruner.count())
	clock.Advance(time.Hour)
	ticks <- clock.Now()
	<-pruner.done
	ticks <- clock.Now()
	<-pruner.done
	require.Equal(t, 3, pruner.count())
	assert.Equal(t, []int{7, 7, 7}, pruner.rows)
	assert.Equal(t, t0, pruner.calls[0])
	assert.Equal(t, t0.Add(time.Hour), pruner.calls[1])

	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("RunAuditRetention did not stop with its context")
	}
	assert.Equal(t, 3, pruner.count())
}

// A failing prune is logged and retried at the next tick.
func TestAuditRetentionSurvivesErrors(t *testing.T) {
	pruner := &fakePruner{done: make(chan struct{}, 8), err: errors.New("database is locked")}
	ticks := make(chan time.Time)
	logs := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunAuditRetention(ctx, pruner, RetentionOptions{Ticks: ticks, Logger: zerolog.New(logs)})
	<-pruner.done
	ticks <- time.Now()
	<-pruner.done
	assert.Equal(t, 2, pruner.count())
	assert.Eventually(t, func() bool { return len(logs.String()) > 0 }, 5*time.Second, 10*time.Millisecond)
	assert.Contains(t, logs.String(), "database is locked")
}

// Without injected ticks, the interval drives the schedule (hourly by
// default).
func TestAuditRetentionUsesTheInterval(t *testing.T) {
	pruner := &fakePruner{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunAuditRetention(ctx, pruner, RetentionOptions{Interval: 5 * time.Millisecond, Logger: zerolog.Nop()})
	assert.Eventually(t, func() bool { return pruner.count() >= 3 }, 5*time.Second, time.Millisecond)
	assert.Equal(t, time.Hour, DefaultAuditRetentionInterval)
	pruner.mu.Lock()
	assert.Equal(t, DefaultAuditMaxRows, pruner.rows[0], "the default row cap")
	pruner.mu.Unlock()
}
