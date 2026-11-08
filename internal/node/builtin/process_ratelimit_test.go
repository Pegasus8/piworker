package builtin

import (
	"context"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRateLimit(t *testing.T, config map[string]interface{}) *RateLimitProcess {
	t.Helper()
	n, err := NewRateLimitProcess(config)
	require.NoError(t, err)
	return n.(*RateLimitProcess)
}

func passes(t *testing.T, p *RateLimitProcess) bool {
	t.Helper()
	out, err := p.Process(context.Background(), types.NewMessage("x", types.DataTypeString))
	require.NoError(t, err)
	return len(out) == 1
}

func TestRateLimitAllowsUpToLimitThenDrops(t *testing.T) {
	p := newRateLimit(t, map[string]interface{}{"limit": 2, "windowMs": 1000})
	now := time.Now()
	p.now = func() time.Time { return now }

	assert.True(t, passes(t, p), "1st within limit")
	assert.True(t, passes(t, p), "2nd within limit")
	assert.False(t, passes(t, p), "3rd exceeds the limit and is dropped")
}

func TestRateLimitWindowSlides(t *testing.T) {
	p := newRateLimit(t, map[string]interface{}{"limit": 1, "windowMs": 1000})
	now := time.Now()
	p.now = func() time.Time { return now }

	assert.True(t, passes(t, p))
	assert.False(t, passes(t, p), "second message within the window is dropped")

	now = now.Add(1100 * time.Millisecond) // advance past the window
	assert.True(t, passes(t, p), "message passes once the window slides past")
}

func TestRateLimitSetsSourcePort(t *testing.T) {
	p := newRateLimit(t, map[string]interface{}{"limit": 5, "windowMs": 1000})
	out, err := p.Process(context.Background(), types.NewMessage("x", types.DataTypeString))
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "output", out[0].SourcePort)
}

// A huge window used to overflow into a negative Duration, which put the
// cutoff in the future, forgot every timestamp and disabled the limit.
func TestRateLimitHugeWindowStillLimits(t *testing.T) {
	for _, w := range []interface{}{1e13, float64(9_300_000_000_000)} {
		p := newRateLimit(t, map[string]interface{}{"limit": 1, "windowMs": w})
		assert.Greater(t, p.window, time.Duration(0))
		assert.True(t, passes(t, p))
		assert.False(t, passes(t, p), "windowMs %v: second message must be dropped", w)
	}
}

func TestRateLimitDefaultsAreSane(t *testing.T) {
	p := newRateLimit(t, map[string]interface{}{})
	assert.Equal(t, 1, p.limit)
	assert.Equal(t, time.Second, p.window)
}
