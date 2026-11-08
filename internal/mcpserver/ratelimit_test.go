package mcpserver

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

// fakeClock is a manually advanced clock safe for concurrent use.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *fakeClock { return &fakeClock{now: t0} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func TestBucketsAllowABurstThenTheRate(t *testing.T) {
	clock := newClock()
	b := newBuckets(30, 10, clock.Now, 16) // 30/min = one every 2 s, burst 10
	for i := range 10 {
		ok, _ := b.take("a")
		require.True(t, ok, "request %d of the burst", i)
	}
	ok, retry := b.take("a")
	require.False(t, ok)
	assert.Equal(t, 2*time.Second, retry, "the next token arrives in 2 s")

	ok, _ = b.take("b")
	assert.True(t, ok, "every key has its own bucket")

	clock.Advance(time.Second)
	ok, retry = b.take("a")
	require.False(t, ok)
	assert.Equal(t, time.Second, retry)
	clock.Advance(time.Second)
	ok, _ = b.take("a")
	assert.True(t, ok)
	ok, _ = b.take("a")
	assert.False(t, ok, "one token per 2 s once the burst is spent")

	clock.Advance(time.Hour)
	for range 10 {
		ok, _ := b.take("a")
		require.True(t, ok)
	}
	ok, _ = b.take("a")
	assert.False(t, ok, "never more than the burst")
}

func TestBucketsBlockedAndSpend(t *testing.T) {
	clock := newClock()
	b := newBuckets(10, 10, clock.Now, 16) // 10 failures/min
	blocked, _ := b.blocked("ip")
	assert.False(t, blocked)
	for range 10 {
		b.spend("ip")
	}
	blocked, retry := b.blocked("ip")
	require.True(t, blocked)
	assert.Equal(t, 6*time.Second, retry)
	b.spend("ip") // stays empty, never negative
	clock.Advance(6 * time.Second)
	blocked, _ = b.blocked("ip")
	assert.False(t, blocked)
	other, _ := b.blocked("other")
	assert.False(t, other)
}

// The number of tracked keys is bounded: full (idle) buckets are dropped
// first, since forgetting them changes nothing.
func TestBucketsAreBounded(t *testing.T) {
	clock := newClock()
	b := newBuckets(30, 10, clock.Now, 4)
	for i := range 4 {
		b.take(fmt.Sprint(i))
	}
	clock.Advance(time.Minute) // every bucket refilled
	b.spend("busy")
	b.spend("busy")
	assert.LessOrEqual(t, b.len(), 4)
	for i := range 100 {
		b.take(fmt.Sprint("k", i))
		require.LessOrEqual(t, b.len(), 4)
	}
}

func TestClientKey(t *testing.T) {
	for addr, want := range map[string]string{
		"192.168.1.20:5000":            "192.168.1.20",
		"[2001:db8:1:2:3:4:5:6]:443":   "2001:db8:1:2::/64",
		"[2001:db8:1:2:aaaa::1]:443":   "2001:db8:1:2::/64",
		"[::1]:1":                      "::1",
		"[::ffff:192.168.1.20]:80":     "192.168.1.20",
		"not an address":               "not an address",
		"[fe80::1%eth0]:9":             "fe80::/64",
		"127.0.0.1:1":                  "127.0.0.1",
		"[2001:db8:ffff:ffff::9]:1234": "2001:db8:ffff:ffff::/64",
	} {
		assert.Equal(t, want, clientKey(addr), addr)
	}
}
