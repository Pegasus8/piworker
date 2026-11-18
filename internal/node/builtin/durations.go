package builtin

import "time"

// maxTriggerInterval caps polling intervals of trigger nodes. Anything longer
// is almost certainly a typo, and it keeps every interval far from overflow.
const maxTriggerInterval = 365 * 24 * time.Hour

// msDuration converts a millisecond count from node config into a Duration
// without overflow: non-positive values give def, and the result is clamped
// to [min, max]. time.Duration(ms)*time.Millisecond silently wraps for
// values above ~292 years, producing negative durations that make
// time.NewTicker panic.
func msDuration(ms int, def, min, max time.Duration) time.Duration {
	return scaledDuration(ms, time.Millisecond, def, min, max)
}

// secDuration is msDuration for a count of seconds.
func secDuration(sec int, def, min, max time.Duration) time.Duration {
	return scaledDuration(sec, time.Second, def, min, max)
}

func scaledDuration(n int, unit, def, min, max time.Duration) time.Duration {
	if n <= 0 {
		return def
	}
	if int64(n) > int64(max/unit) {
		return max
	}
	d := time.Duration(n) * unit
	if d < min {
		return min
	}
	return d
}
