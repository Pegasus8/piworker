package mcpserver

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

const (
	// DefaultAuditRetentionInterval is how often the MCP audit is pruned.
	DefaultAuditRetentionInterval = time.Hour
	// DefaultAuditMaxRows caps the ordinary audit rows kept within the
	// 30-day retention. A row holds at most ~1 KiB (a 512-byte argument
	// preview, a tool name the service caps at 128 bytes, a token name of at
	// most 64 and IDs), so ordinary rows stay within ~50 MiB on a Raspberry
	// Pi's SD card even when a token calls at its full rate (30/min) for the
	// whole period. Privileged rows are outside the cap and kept the full
	// 30 days: they are only what the admin let the token do on the device
	// (a privileged tool's run or queued approval, with the privileged flag
	// and a grant; a privileged flow's deploy or stop that reached the
	// lifecycle path, at agent deploy `all`), bounded by the tool's own
	// limits and the token's rate. Refusals are ordinary rows, so a loop of
	// refused calls or deploys cannot grow the table past the cap.
	DefaultAuditMaxRows = 50_000
)

// AuditPruner deletes expired MCP audit data (*storage.SQLiteStore).
type AuditPruner interface {
	PruneMCPAudit(now time.Time, maxRows int) (int64, error)
}

// RetentionOptions configures RunAuditRetention.
type RetentionOptions struct {
	// Interval between prunes (default DefaultAuditRetentionInterval).
	Interval time.Duration
	// Ticks, when set, replaces the interval's ticker (tests).
	Ticks <-chan time.Time
	// MaxRows is passed to PruneMCPAudit (default DefaultAuditMaxRows).
	MaxRows int
	Now     func() time.Time
	Logger  zerolog.Logger
}

// RunAuditRetention prunes the MCP audit once now, then at every interval,
// until ctx ends. Failures are logged and retried at the next interval.
func RunAuditRetention(ctx context.Context, p AuditPruner, opts RetentionOptions) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.MaxRows <= 0 {
		opts.MaxRows = DefaultAuditMaxRows
	}
	ticks := opts.Ticks
	if ticks == nil {
		interval := opts.Interval
		if interval <= 0 {
			interval = DefaultAuditRetentionInterval
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		ticks = ticker.C
	}
	prune := func() {
		n, err := p.PruneMCPAudit(opts.Now(), opts.MaxRows)
		if err != nil {
			opts.Logger.Error().Err(err).Msg("Cannot prune the MCP audit; retrying later")
			return
		}
		if n > 0 {
			opts.Logger.Debug().Int64("rows", n).Msg("Pruned the MCP audit")
		}
	}
	prune()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			prune()
		}
	}
}
