package mcpserver

import (
	"net"
	"net/netip"
	"sync"
	"time"
)

// buckets is a set of token buckets keyed by a string (a token id, a client
// address): each refills at perMinute tokens per minute up to burst. It
// serves both the per-token request rate (take) and the per-client limit of
// failed authentications (take before a lookup, refund unless it failed).
// At most maxKeys buckets are
// tracked; full buckets are forgotten first, since a full bucket is what a
// new key gets anyway.
type buckets struct {
	mu      sync.Mutex
	rate    float64 // tokens per second
	burst   float64
	now     func() time.Time
	maxKeys int
	m       map[string]*bucket
}

type bucket struct {
	tokens float64
	at     time.Time
}

func newBuckets(perMinute, burst int, now func() time.Time, maxKeys int) *buckets {
	return &buckets{
		rate:    float64(perMinute) / 60,
		burst:   float64(burst),
		now:     now,
		maxKeys: maxKeys,
		m:       map[string]*bucket{},
	}
}

// take spends one token of key's bucket. When none is left it spends
// nothing and returns how long until one is.
func (b *buckets) take(key string) (ok bool, retryAfter time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	bk := b.getLocked(key)
	if bk.tokens >= 1 {
		bk.tokens--
		return true, 0
	}
	return false, b.waitLocked(bk)
}

// refund gives back one token of key's bucket, taken for something that
// did not happen, never past the burst. An unknown key is left alone: its
// bucket would be full.
func (b *buckets) refund(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	bk, ok := b.m[key]
	if !ok {
		return
	}
	b.refillLocked(bk)
	bk.tokens = min(b.burst, bk.tokens+1)
}

func (b *buckets) len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.m)
}

// getLocked returns key's bucket, refilled up to now, creating it (full)
// when unknown.
func (b *buckets) getLocked(key string) *bucket {
	if bk, ok := b.m[key]; ok {
		b.refillLocked(bk)
		return bk
	}
	if len(b.m) >= b.maxKeys {
		b.makeRoomLocked()
	}
	bk := &bucket{tokens: b.burst, at: b.now()}
	b.m[key] = bk
	return bk
}

func (b *buckets) refillLocked(bk *bucket) {
	now := b.now()
	if elapsed := now.Sub(bk.at); elapsed > 0 {
		bk.tokens = min(b.burst, bk.tokens+elapsed.Seconds()*b.rate)
	}
	bk.at = now
}

// waitLocked is how long until bk holds one token.
func (b *buckets) waitLocked(bk *bucket) time.Duration {
	if b.rate <= 0 {
		return time.Hour
	}
	return time.Duration((1 - bk.tokens) / b.rate * float64(time.Second)).Round(time.Millisecond)
}

// makeRoomLocked forgets the full buckets or, when none is full, the one
// closest to full.
func (b *buckets) makeRoomLocked() {
	var fullest string
	best := -1.0
	for k, bk := range b.m {
		b.refillLocked(bk)
		if bk.tokens >= b.burst {
			delete(b.m, k)
			continue
		}
		if bk.tokens > best {
			fullest, best = k, bk.tokens
		}
	}
	if len(b.m) >= b.maxKeys && fullest != "" {
		delete(b.m, fullest)
	}
}

// clientKey is the limiter key of a client address (http.Request.RemoteAddr):
// the IP address, or its /64 prefix for IPv6 (a client usually controls a
// whole /64, so per-address limits would be easy to dodge). Loopback
// addresses are kept whole.
func clientKey(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	addr = addr.WithZone("").Unmap()
	if addr.Is4() || addr.IsLoopback() {
		return addr.String()
	}
	prefix, err := addr.Prefix(64)
	if err != nil {
		return addr.String()
	}
	return prefix.String()
}
