package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/rs/zerolog"
)

// AuditStore persists the MCP audit (*storage.SQLiteStore).
type AuditStore interface {
	InsertMCPAudit(batch []storage.MCPAuditEntry, now time.Time) error
	AddMCPRejections(batch []storage.MCPRejection, now time.Time) error
}

// Audit writer defaults.
const (
	DefaultAuditFlushInterval = time.Second
	DefaultAuditMaxQueue      = 1024
	// maxRejectionKeys bounds the per-token per-minute counters held
	// between flushes.
	maxRejectionKeys = 4096
)

// AuditOptions configures an AuditWriter.
type AuditOptions struct {
	// FlushInterval is how long a recorded entry may wait before it is
	// written (default DefaultAuditFlushInterval).
	FlushInterval time.Duration
	// MaxQueue bounds the entries waiting to be written; more are dropped
	// and counted (default DefaultAuditMaxQueue).
	MaxQueue int
	Now      func() time.Time
	Logger   zerolog.Logger
}

type rejectionKey struct {
	tokenID string
	minute  int64
	reason  string
}

// AuditWriter writes the MCP audit asynchronously in batches (docs/design/
// mcp.md §9) so a call never waits for SQLite. Entries wait in a bounded
// queue; when it is full new ones are dropped and counted (Dropped).
// Frequent rejections (busy, cooldown, invalid arguments) are kept as
// per-token per-minute counters instead of rows. Close flushes what is left;
// call it on shutdown, before the store closes. No goroutine runs while the
// writer is idle. A nil *AuditWriter discards everything.
type AuditWriter struct {
	store    AuditStore
	interval time.Duration
	maxQueue int
	now      func() time.Time
	log      zerolog.Logger

	mu         sync.Mutex
	queue      []storage.MCPAuditEntry
	rejections map[rejectionKey]int
	scheduled  bool
	closed     bool

	// writeMu serializes writes to the store, so batches keep their order.
	writeMu sync.Mutex
	dropped atomic.Int64
}

// NewAuditWriter returns a writer for store.
func NewAuditWriter(store AuditStore, opts AuditOptions) *AuditWriter {
	w := &AuditWriter{
		store:      store,
		interval:   opts.FlushInterval,
		maxQueue:   opts.MaxQueue,
		now:        opts.Now,
		log:        opts.Logger,
		rejections: map[rejectionKey]int{},
	}
	if w.interval <= 0 {
		w.interval = DefaultAuditFlushInterval
	}
	if w.maxQueue <= 0 {
		w.maxQueue = DefaultAuditMaxQueue
	}
	if w.now == nil {
		w.now = time.Now
	}
	return w
}

// Record queues one audited call.
func (w *AuditWriter) Record(e storage.MCPAuditEntry) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || len(w.queue) >= w.maxQueue {
		w.dropped.Add(1)
		return
	}
	w.queue = append(w.queue, e)
	w.scheduleLocked()
}

// Reject counts one rejection of reason for tokenID in the minute of at.
func (w *AuditWriter) Reject(tokenID, reason string, at time.Time) {
	if w == nil {
		return
	}
	key := rejectionKey{tokenID: tokenID, minute: at.Truncate(time.Minute).Unix(), reason: reason}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, known := w.rejections[key]; w.closed || (!known && len(w.rejections) >= maxRejectionKeys) {
		w.dropped.Add(1)
		return
	}
	w.rejections[key]++
	w.scheduleLocked()
}

// scheduleLocked arms the flush timer once. Callers hold w.mu.
func (w *AuditWriter) scheduleLocked() {
	if w.scheduled {
		return
	}
	w.scheduled = true
	time.AfterFunc(w.interval, func() {
		if err := w.Flush(); err != nil {
			w.log.Error().Err(err).Msg("Cannot write the MCP audit")
		}
	})
}

// Flush writes everything queued now. Entries of a batch the store refuses
// are lost and counted as dropped.
func (w *AuditWriter) Flush() error {
	if w == nil {
		return nil
	}
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	w.mu.Lock()
	batch := w.queue
	w.queue = nil
	counts := w.rejections
	w.rejections = map[rejectionKey]int{}
	w.scheduled = false
	w.mu.Unlock()

	now := w.now()
	var errs []error
	if len(batch) > 0 {
		if err := w.store.InsertMCPAudit(batch, now); err != nil {
			w.dropped.Add(int64(len(batch)))
			errs = append(errs, err)
		}
	}
	if len(counts) > 0 {
		rejections := make([]storage.MCPRejection, 0, len(counts))
		for k, n := range counts {
			rejections = append(rejections, storage.MCPRejection{TokenID: k.tokenID, At: time.Unix(k.minute, 0).UTC(), Reason: k.reason, Count: n})
		}
		if err := w.store.AddMCPRejections(rejections, now); err != nil {
			w.dropped.Add(int64(len(rejections)))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Close stops accepting entries and writes what is queued. It is safe to
// call more than once; ctx is not used yet (the flush is one transaction).
func (w *AuditWriter) Close(_ context.Context) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	return w.Flush()
}

// Dropped returns how many entries (and rejection counters) were lost: the
// queue was full, the writer closed, or the store refused a batch.
func (w *AuditWriter) Dropped() int64 {
	if w == nil {
		return 0
	}
	return w.dropped.Load()
}
