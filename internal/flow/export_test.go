package flow

import (
	"testing"
	"time"
)

// SetDryRunTimeout lowers the dry-run wall-clock cap for one test, so the
// timeout path can be tested without waiting DryRunTimeout.
func SetDryRunTimeout(t testing.TB, d time.Duration) {
	t.Helper()
	old := dryRunTimeout
	dryRunTimeout = d
	t.Cleanup(func() { dryRunTimeout = old })
}

// SetTestNodeTimeout lowers the node test timeout for one test.
func SetTestNodeTimeout(t testing.TB, d time.Duration) {
	t.Helper()
	old := testNodeTimeout
	testNodeTimeout = d
	t.Cleanup(func() { testNodeTimeout = old })
}
