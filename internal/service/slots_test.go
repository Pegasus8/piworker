package service

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestSlots(global, perToken int) (*slots, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)}
	return newSlots(global, perToken, clock.now), clock
}

func requireKind(t *testing.T, err error, kind error) *ToolError {
	t.Helper()
	require.Error(t, err)
	require.True(t, errors.Is(err, kind), "%v is not %v", err, kind)
	var te *ToolError
	require.True(t, errors.As(err, &te))
	return te
}

func TestSlotsPerTool(t *testing.T) {
	s, _ := newTestSlots(10, 10)
	release, err := s.acquire("f/a", 1, 0, "token:x")
	require.NoError(t, err)
	_, err = s.acquire("f/a", 1, 0, "token:y")
	requireKind(t, err, ErrBusy)
	_, err = s.acquire("f/b", 1, 0, "token:y")
	require.NoError(t, err, "another tool has its own slots")
	release()
	release() // harmless
	_, err = s.acquire("f/a", 1, 0, "token:y")
	require.NoError(t, err)
}

func TestSlotsGlobal(t *testing.T) {
	s, _ := newTestSlots(2, 10)
	_, err := s.acquire("f/a", 4, 0, "token:x")
	require.NoError(t, err)
	r2, err := s.acquire("f/b", 4, 0, "")
	require.NoError(t, err)
	_, err = s.acquire("f/c", 4, 0, "token:z")
	requireKind(t, err, ErrBusy)
	r2()
	_, err = s.acquire("f/c", 4, 0, "token:z")
	require.NoError(t, err)
}

func TestSlotsPerToken(t *testing.T) {
	s, _ := newTestSlots(10, 2)
	_, err := s.acquire("f/a", 4, 0, "token:x")
	require.NoError(t, err)
	_, err = s.acquire("f/b", 4, 0, "token:x")
	require.NoError(t, err)
	_, err = s.acquire("f/c", 4, 0, "token:x")
	requireKind(t, err, ErrBusy)
	_, err = s.acquire("f/c", 4, 0, "token:y")
	require.NoError(t, err, "another token")
	for range 3 {
		_, err = s.acquire("f/d", 4, 0, "")
		require.NoError(t, err, "the editor has no per-token limit")
	}
}

func TestSlotsCooldownStartsAtRunEnd(t *testing.T) {
	s, clock := newTestSlots(10, 10)
	release, err := s.acquire("f/a", 2, time.Second, "token:x")
	require.NoError(t, err)
	_, err = s.acquire("f/a", 2, time.Second, "token:y")
	require.NoError(t, err, "no run has ended yet")

	clock.advance(5 * time.Second)
	release()
	clock.advance(400 * time.Millisecond)
	te := requireKind(t, func() error { _, err := s.acquire("f/a", 2, time.Second, "token:z"); return err }(), ErrCooldown)
	assert.Equal(t, 600*time.Millisecond, te.RetryAfter)

	clock.advance(600 * time.Millisecond)
	_, err = s.acquire("f/a", 2, time.Second, "token:z")
	require.NoError(t, err)
}

func TestSlotsInflight(t *testing.T) {
	s, _ := newTestSlots(10, 10)
	r, err := s.acquire("f/a", 4, 0, "token:x")
	require.NoError(t, err)
	assert.Equal(t, 1, s.inflight("f/a"))
	r()
	assert.Equal(t, 0, s.inflight("f/a"))
}
