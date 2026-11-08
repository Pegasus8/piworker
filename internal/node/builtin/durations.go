package builtin

import "time"

// msDuration converts a millisecond count from node config into a Duration
// without overflow: non-positive values give def, and the result is clamped
// to [min, max]. time.Duration(ms)*time.Millisecond silently wraps for
// values above ~292 years, producing negative durations that make
// time.NewTicker panic.
func msDuration(ms int, def, min, max time.Duration) time.Duration {
	if ms <= 0 {
		return def
	}
	if int64(ms) > int64(max/time.Millisecond) {
		return max
	}
	d := time.Duration(ms) * time.Millisecond
	if d < min {
		return min
	}
	return d
}
