package builtin

import (
	"context"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func near(t *testing.T, want, got time.Time, tol time.Duration, what string) {
	t.Helper()
	d := got.Sub(want)
	if d < 0 {
		d = -d
	}
	assert.LessOrEqualf(t, d, tol, "%s: want %s, got %s", what, want.Format(time.RFC3339), got.Format(time.RFC3339))
}

// Published times (minutes) for London and Buenos Aires, within 3 minutes.
func TestSunEventTimes(t *testing.T) {
	day := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	rise, ok := sunEventOn(day, 51.5074, -0.1278, "sunrise")
	require.True(t, ok)
	near(t, time.Date(2026, 6, 21, 3, 43, 0, 0, time.UTC), rise, 3*time.Minute, "London sunrise")
	set, ok := sunEventOn(day, 51.5074, -0.1278, "sunset")
	require.True(t, ok)
	near(t, time.Date(2026, 6, 21, 20, 21, 0, 0, time.UTC), set, 3*time.Minute, "London sunset")

	day = time.Date(2026, 12, 21, 0, 0, 0, 0, time.UTC)
	rise, ok = sunEventOn(day, -34.6037, -58.3816, "sunrise")
	require.True(t, ok)
	near(t, time.Date(2026, 12, 21, 8, 38, 0, 0, time.UTC), rise, 4*time.Minute, "Buenos Aires sunrise (05:38 local)")
	set, ok = sunEventOn(day, -34.6037, -58.3816, "sunset")
	require.True(t, ok)
	near(t, time.Date(2026, 12, 21, 23, 7, 0, 0, time.UTC), set, 4*time.Minute, "Buenos Aires sunset (20:07 local)")

	dusk, ok := sunEventOn(day, -34.6037, -58.3816, "dusk")
	require.True(t, ok)
	assert.True(t, dusk.After(set) && dusk.Sub(set) < time.Hour, "civil dusk comes within an hour after sunset")

	// Midnight sun: no sunset in Tromsø at the June solstice.
	_, ok = sunEventOn(time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC), 69.6492, 18.9553, "sunset")
	assert.False(t, ok)
}

func TestNextSunEvent(t *testing.T) {
	now := time.Date(2026, 12, 21, 12, 0, 0, 0, time.UTC) // 09:00 in Buenos Aires
	next, ok := nextSunEvent(now, -34.6037, -58.3816, "sunrise", 0)
	require.True(t, ok)
	assert.True(t, next.After(now))
	near(t, time.Date(2026, 12, 22, 8, 38, 0, 0, time.UTC), next, 5*time.Minute, "tomorrow's sunrise")

	next, ok = nextSunEvent(now, -34.6037, -58.3816, "sunset", -30*time.Minute)
	require.True(t, ok)
	near(t, time.Date(2026, 12, 21, 22, 37, 0, 0, time.UTC), next, 5*time.Minute, "30 minutes before today's sunset")

	// Polar night: the next sunrise in Tromsø after the solstice is weeks away.
	next, ok = nextSunEvent(time.Date(2026, 12, 21, 12, 0, 0, 0, time.UTC), 69.6492, 18.9553, "sunrise", 0)
	require.True(t, ok)
	assert.True(t, next.After(time.Date(2027, 1, 10, 0, 0, 0, 0, time.UTC)), next.String())
}

func TestSunTriggerFires(t *testing.T) {
	n, err := NewSunTrigger(map[string]interface{}{"latitude": -34.6, "longitude": -58.4, "event": "sunset"})
	require.NoError(t, err)
	tr := n.(*SunTrigger)
	fireAt := time.Now().Add(150 * time.Millisecond)
	tr.next = func(time.Time) (time.Time, bool) { at := fireAt; fireAt = fireAt.Add(time.Hour); return at, true }
	out := make(chan *types.Message, 1)
	require.NoError(t, tr.Start(context.Background(), out))
	t.Cleanup(func() { _ = tr.Stop() })
	m := nextMsg(t, out)
	p := m.Payload.(map[string]interface{})
	assert.Equal(t, "sunset", p["event"])
	assert.NotEmpty(t, p["time"])
}

func TestSunTriggerConfig(t *testing.T) {
	for _, cfg := range []map[string]interface{}{
		{"longitude": 1.0},
		{"latitude": 91.0, "longitude": 1.0},
		{"latitude": 1.0, "longitude": 181.0},
		{"latitude": 1.0, "longitude": 1.0, "event": "noonish"},
		{"latitude": 1.0, "longitude": 1.0, "offset": 721.0},
	} {
		_, err := NewSunTrigger(cfg)
		assert.Error(t, err, "%v", cfg)
	}
}
