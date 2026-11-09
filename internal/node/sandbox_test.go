package node

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSandboxFromContext: only a sandboxed context carries a sandbox, and a
// derived context keeps it.
func TestSandboxFromContext(t *testing.T) {
	assert.Nil(t, SandboxFrom(context.Background()))
	//nolint:staticcheck // a nil context must not panic
	assert.Nil(t, SandboxFrom(nil))

	sb := NewSandbox(1<<10, 1<<20)
	ctx := WithSandbox(context.Background(), sb)
	assert.Same(t, sb, SandboxFrom(ctx))
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	assert.Same(t, sb, SandboxFrom(child))
}

// TestSandboxTracksGoroutines: Go runs the function in a goroutine the
// sandbox counts until it returns; Wait blocks until none is left. Outside a
// sandbox, Go just starts a goroutine.
func TestSandboxTracksGoroutines(t *testing.T) {
	sb := NewSandbox(1<<10, 1<<20)
	assert.False(t, sb.Busy())
	sb.Wait() // nothing to wait for

	release := make(chan struct{})
	started := make(chan struct{})
	Go(WithSandbox(context.Background(), sb), func() {
		close(started)
		<-release
	})
	<-started
	assert.True(t, sb.Busy())

	waited := make(chan struct{})
	go func() {
		sb.Wait()
		close(waited)
	}()
	select {
	case <-waited:
		t.Fatal("Wait returned while a tracked goroutine runs")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return once the goroutine ended")
	}
	assert.False(t, sb.Busy())

	ran := make(chan struct{})
	Go(context.Background(), func() { close(ran) })
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("Go outside a sandbox did not run the function")
	}
}

// TestSandboxCharge: a single value over the per-value cap, or charges past
// the budget, fail with ErrSandboxLimit and mark the sandbox exceeded.
func TestSandboxCharge(t *testing.T) {
	sb := NewSandbox(100, 250)
	require.NoError(t, sb.Charge(100))
	assert.False(t, sb.Exceeded())
	assert.Zero(t, sb.Refusals())

	err := sb.Charge(101)
	require.ErrorIs(t, err, ErrSandboxLimit)
	assert.Contains(t, err.Error(), "100 bytes")
	assert.True(t, sb.Exceeded())
	assert.Equal(t, int64(1), sb.Refusals(), "each refusal is counted")

	sb = NewSandbox(100, 250)
	require.NoError(t, sb.Charge(100))
	require.NoError(t, sb.Charge(100))
	err = sb.Charge(60)
	require.ErrorIs(t, err, ErrSandboxLimit, "past the budget")
	assert.True(t, sb.Exceeded())
	require.ErrorIs(t, sb.Charge(1), ErrSandboxLimit, "the budget stays spent")
	require.ErrorIs(t, sb.Charge(-1), ErrSandboxLimit, "a negative size is refused")

	// Limit: a growing value checked against the per-value cap.
	sb = NewSandbox(100, 250)
	require.NoError(t, sb.Limit(100))
	assert.False(t, sb.Exceeded())
	require.ErrorIs(t, sb.Limit(101), ErrSandboxLimit)
	assert.True(t, sb.Exceeded())
}

// TestSandboxCheckValue: values are sized like their JSON, the walk stops at
// the cap, and shared references count each time they appear (a deep copy or
// an encoding would produce them each time).
func TestSandboxCheckValue(t *testing.T) {
	sb := NewSandbox(1000, 1<<20)
	require.NoError(t, sb.CheckValue(map[string]any{"a": []any{1.0, "x", true, nil}}))
	require.NoError(t, sb.CheckValue(strings.Repeat("x", 990)))
	err := sb.CheckValue(strings.Repeat("x", 1001))
	require.ErrorIs(t, err, ErrSandboxLimit)
	assert.False(t, sb.Exceeded(), "a refused check spends nothing and marks nothing")

	shared := map[string]any{"s": strings.Repeat("y", 100)}
	many := make([]any, 1_000_000)
	for i := range many {
		many[i] = shared
	}
	start := time.Now()
	require.ErrorIs(t, sb.CheckValue(many), ErrSandboxLimit)
	assert.Less(t, time.Since(start), time.Second, "the walk stops at the cap")

	// An estimate: at least the JSON's size, a separator or so more.
	assert.Equal(t, int64(len(`"abc"`)), EstimateSize("abc", 1<<20))
	assert.InDelta(t, len(`{"k":"v"}`), EstimateSize(map[string]any{"k": "v"}, 1<<20), 1)
	assert.InDelta(t, len(`["a","b"]`), EstimateSize([]string{"a", "b"}, 1<<20), 1, "typed slices too")
	assert.Greater(t, EstimateSize(many, 5000), int64(5000), "over the limit: limit+1 or more")

	// ChargeValue checks the value and spends its size.
	sb = NewSandbox(1000, 50)
	require.NoError(t, sb.ChargeValue("abc"))
	require.ErrorIs(t, sb.ChargeValue(strings.Repeat("z", 60)), ErrSandboxLimit)
	assert.True(t, sb.Exceeded())
	assert.True(t, errors.Is(sb.ChargeValue("a"), ErrSandboxLimit))
}
