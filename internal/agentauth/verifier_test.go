package agentauth

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

// fakeClock is a manually advanced clock safe for concurrent use.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

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

// countingStore wraps a real store and counts the calls the verifier makes.
type countingStore struct {
	*storage.SQLiteStore
	gets, epochs, touches, lists atomic.Int32
	onGet                        func(id string)
	failGet, failEpoch, failList error
}

func (c *countingStore) ListAPITokens() ([]storage.APIToken, error) {
	c.lists.Add(1)
	if c.failList != nil {
		return nil, c.failList
	}
	return c.SQLiteStore.ListAPITokens()
}

func (c *countingStore) GetAPIToken(id string) (*storage.APIToken, error) {
	c.gets.Add(1)
	if c.failGet != nil {
		return nil, c.failGet
	}
	tok, err := c.SQLiteStore.GetAPIToken(id)
	if c.onGet != nil {
		c.onGet(id)
	}
	return tok, err
}

func (c *countingStore) TokensEpoch() (int64, error) {
	c.epochs.Add(1)
	if c.failEpoch != nil {
		return 0, c.failEpoch
	}
	return c.SQLiteStore.TokensEpoch()
}

func (c *countingStore) TouchAPITokenLastUsed(id string, now time.Time) error {
	c.touches.Add(1)
	return c.SQLiteStore.TouchAPITokenLastUsed(id, now)
}

func newStore(t *testing.T) (*storage.SQLiteStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.db")
	s, err := storage.NewSQLiteStore(path)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s, path
}

func newVerifier(t *testing.T) (*TokenVerifier, *countingStore, *fakeClock) {
	t.Helper()
	s, _ := newStore(t)
	cs := &countingStore{SQLiteStore: s}
	clock := &fakeClock{now: t0}
	return NewTokenVerifier(cs, WithClock(clock.Now)), cs, clock
}

func createToken(t *testing.T, s interface {
	CreateAPIToken(storage.NewAPIToken, time.Time) (*storage.APIToken, string, error)
}, req storage.NewAPIToken) (*storage.APIToken, string) {
	t.Helper()
	if req.Name == "" {
		req.Name = "agent"
	}
	if req.Scopes == nil {
		req.Scopes = []string{storage.ScopeCall}
	}
	tok, raw, err := s.CreateAPIToken(req, t0)
	require.NoError(t, err)
	return tok, raw
}

func TestVerifyReturnsTokenInfo(t *testing.T) {
	v, cs, _ := newVerifier(t)
	grants := []storage.TokenGrant{{FlowID: "f", TriggerNodeID: "n", Fingerprint: "fp"}}
	tok, raw := createToken(t, cs.SQLiteStore, storage.NewAPIToken{
		Name: "desk", Scopes: []string{storage.ScopeRead, storage.ScopeCall}, Privileged: true, Grants: grants, AllTools: true,
	})
	info, err := v.Verify(raw)
	require.NoError(t, err)
	assert.Equal(t, TokenInfo{
		ID: tok.ID, Name: "desk", Scopes: []string{storage.ScopeCall, storage.ScopeRead}, Privileged: true,
		Grants: grants, AllTools: true, ExpiresAt: tok.ExpiresAt,
	}, info)
	assert.True(t, info.HasScope(storage.ScopeRead))
	assert.False(t, info.HasScope(storage.ScopeDeploy))
}

func TestVerifyRejectsBadTokens(t *testing.T) {
	v, cs, _ := newVerifier(t)
	_, raw := createToken(t, cs.SQLiteStore, storage.NewAPIToken{})
	id, secret, _ := storage.ParseAPIToken(raw)

	for _, bad := range []string{"", "Bearer " + raw, "pwk_short_x", raw + "a"} {
		_, err := v.Verify(bad)
		require.ErrorIs(t, err, ErrInvalidToken, bad)
	}
	assert.Equal(t, int32(0), cs.gets.Load(), "malformed tokens never reach the database")

	wrong := strings.Repeat("a", len(secret))
	if wrong == secret {
		wrong = strings.Repeat("b", len(secret))
	}
	_, err := v.Verify("pwk_" + id + "_" + wrong)
	require.ErrorIs(t, err, ErrInvalidToken, "wrong secret, cold cache")

	_, err = v.Verify("pwk_" + strings.Repeat("q", 16) + "_" + secret)
	require.ErrorIs(t, err, ErrInvalidToken, "unknown id")

	_, err = v.Verify(raw)
	require.NoError(t, err)
	_, err = v.Verify("pwk_" + id + "_" + wrong)
	require.ErrorIs(t, err, ErrInvalidToken, "wrong secret, warm cache")
}

func TestVerifyCachesVerifiedTokens(t *testing.T) {
	v, cs, clock := newVerifier(t)
	_, raw := createToken(t, cs.SQLiteStore, storage.NewAPIToken{})
	for range 5 {
		_, err := v.Verify(raw)
		require.NoError(t, err)
		clock.Advance(time.Second)
	}
	assert.Equal(t, int32(1), cs.gets.Load(), "verified tokens are served from memory")
	assert.LessOrEqual(t, cs.epochs.Load(), int32(2), "the epoch is re-read at most every 5 s")
}

func TestReturnedInfoIsACopy(t *testing.T) {
	v, cs, _ := newVerifier(t)
	_, raw := createToken(t, cs.SQLiteStore, storage.NewAPIToken{Grants: []storage.TokenGrant{{FlowID: "f", TriggerNodeID: "n", Fingerprint: "fp"}}})
	info, err := v.Verify(raw)
	require.NoError(t, err)
	info.Scopes[0] = storage.ScopeDeploy
	info.Grants[0].Suspended = true
	again, err := v.Verify(raw)
	require.NoError(t, err)
	assert.Equal(t, []string{storage.ScopeCall}, again.Scopes)
	assert.False(t, again.Grants[0].Suspended)
}

func TestExpiredTokenRejected(t *testing.T) {
	v, cs, clock := newVerifier(t)
	_, raw := createToken(t, cs.SQLiteStore, storage.NewAPIToken{ExpiresAt: t0.Add(time.Hour)})
	_, err := v.Verify(raw)
	require.NoError(t, err)
	clock.Advance(time.Hour)
	_, err = v.Verify(raw)
	require.ErrorIs(t, err, ErrInvalidToken, "a cached token stops working at its expiry")

	// Cold cache: a token that is already expired is never accepted.
	cold := NewTokenVerifier(cs, WithClock(clock.Now))
	_, err = cold.Verify(raw)
	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestRevokeEvictsCache(t *testing.T) {
	v, cs, _ := newVerifier(t)
	tok, raw := createToken(t, cs.SQLiteStore, storage.NewAPIToken{})
	other, otherRaw := createToken(t, cs.SQLiteStore, storage.NewAPIToken{})
	_, err := v.Verify(raw)
	require.NoError(t, err)
	_, err = v.Verify(otherRaw)
	require.NoError(t, err)

	require.NoError(t, v.Revoke(tok.ID))
	_, err = v.Verify(raw)
	require.ErrorIs(t, err, ErrInvalidToken, "revocation applies immediately in this process")
	stored, err := cs.GetAPIToken(tok.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.RevokedAt)

	_, err = v.Verify(otherRaw)
	require.NoError(t, err, "other tokens keep working")
	require.ErrorIs(t, v.Revoke("missing"), storage.ErrAPITokenNotFound)
	_ = other
}

func TestEvictDropsCachedEntry(t *testing.T) {
	v, cs, _ := newVerifier(t)
	tok, raw := createToken(t, cs.SQLiteStore, storage.NewAPIToken{})
	_, err := v.Verify(raw)
	require.NoError(t, err)
	// Changed behind the verifier's back (same process, without Revoke).
	require.NoError(t, cs.SQLiteStore.SetAPITokenGrants(tok.ID, []storage.TokenGrant{{FlowID: "f", TriggerNodeID: "n", Fingerprint: "new"}}))
	info, err := v.Verify(raw)
	require.NoError(t, err)
	assert.Empty(t, info.Grants, "still cached")
	v.Evict(tok.ID)
	info, err = v.Verify(raw)
	require.NoError(t, err)
	require.Len(t, info.Grants, 1)
	assert.Equal(t, "new", info.Grants[0].Fingerprint)
}

func TestRevokeAllRevokesAndEvicts(t *testing.T) {
	v, cs, _ := newVerifier(t)
	_, a := createToken(t, cs.SQLiteStore, storage.NewAPIToken{})
	_, b := createToken(t, cs.SQLiteStore, storage.NewAPIToken{})
	for _, raw := range []string{a, b} {
		_, err := v.Verify(raw)
		require.NoError(t, err)
	}
	n, err := v.RevokeAll()
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	for _, raw := range []string{a, b} {
		_, err := v.Verify(raw)
		require.ErrorIs(t, err, ErrInvalidToken)
	}
}

func TestEpochChangeFromSecondHandleRevokesWithinInterval(t *testing.T) {
	a, path := newStore(t)
	b, err := storage.NewSQLiteStore(path) // e.g. another piworker process
	require.NoError(t, err)
	defer b.Close()
	clock := &fakeClock{now: t0}
	v := NewTokenVerifier(a, WithClock(clock.Now))

	tok, raw := createToken(t, a, storage.NewAPIToken{})
	_, other := createToken(t, a, storage.NewAPIToken{})
	_, err = v.Verify(raw)
	require.NoError(t, err)
	_, err = v.Verify(other)
	require.NoError(t, err)

	require.NoError(t, b.RevokeAPIToken(tok.ID, t0))
	clock.Advance(DefaultEpochInterval - time.Millisecond)
	_, err = v.Verify(raw)
	require.NoError(t, err, "within the interval the cache may still answer")
	clock.Advance(time.Millisecond)
	_, err = v.Verify(raw)
	require.ErrorIs(t, err, ErrInvalidToken, "after the interval the epoch change evicts the cache")
	_, err = v.Verify(other)
	require.NoError(t, err)

	_, err = b.RevokeAllAPITokens(t0)
	require.NoError(t, err)
	clock.Advance(DefaultEpochInterval)
	_, err = v.Verify(other)
	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestRevokeDuringLoadDoesNotCacheStaleToken(t *testing.T) {
	v, cs, _ := newVerifier(t)
	tok, raw := createToken(t, cs.SQLiteStore, storage.NewAPIToken{})
	var once sync.Once
	cs.onGet = func(string) {
		// The token was read as valid; the admin revokes it before the
		// verifier stores the result.
		once.Do(func() { require.NoError(t, v.Revoke(tok.ID)) })
	}
	_, _ = v.Verify(raw)
	_, err := v.Verify(raw)
	require.ErrorIs(t, err, ErrInvalidToken, "a stale read must not be cached after a revoke")
}

func TestLastUsedTouchThrottled(t *testing.T) {
	v, cs, clock := newVerifier(t)
	tok, raw := createToken(t, cs.SQLiteStore, storage.NewAPIToken{})
	for range 10 {
		_, err := v.Verify(raw)
		require.NoError(t, err)
		clock.Advance(5 * time.Second)
	}
	assert.Equal(t, int32(1), cs.touches.Load(), "at most one write per minute per token")
	stored, err := cs.SQLiteStore.GetAPIToken(tok.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.LastUsedAt)
	assert.Equal(t, t0, *stored.LastUsedAt)

	clock.Advance(DefaultTouchInterval)
	_, err = v.Verify(raw)
	require.NoError(t, err)
	assert.Equal(t, int32(2), cs.touches.Load())

	// A fresh verifier honours the stored last_used_at instead of writing again.
	fresh := NewTokenVerifier(cs, WithClock(clock.Now))
	_, err = fresh.Verify(raw)
	require.NoError(t, err)
	assert.Equal(t, int32(2), cs.touches.Load())
}

func TestStoreFailuresAreNotInvalidToken(t *testing.T) {
	v, cs, _ := newVerifier(t)
	_, raw := createToken(t, cs.SQLiteStore, storage.NewAPIToken{})
	boom := errors.New("disk on fire")

	cs.failEpoch = boom
	_, err := v.Verify(raw)
	require.ErrorIs(t, err, boom, "without the epoch the verifier fails closed")
	require.NotErrorIs(t, err, ErrInvalidToken)

	cs.failEpoch = nil
	cs.failGet = boom
	_, err = v.Verify(raw)
	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, ErrInvalidToken)
}

// RevokeAllInTx revokes inside the caller's transaction (a password reset);
// nothing changes until it commits, and EvictAll then drops the cache.
func TestRevokeAllInTxCommitsWithTheCaller(t *testing.T) {
	v, cs, _ := newVerifier(t)
	tok, raw := createToken(t, cs.SQLiteStore, storage.NewAPIToken{})
	_, err := v.Verify(raw)
	require.NoError(t, err)

	tx, err := cs.DB().Begin()
	require.NoError(t, err)
	require.NoError(t, v.RevokeAllInTx(tx))
	require.NoError(t, tx.Rollback())
	stored, err := cs.GetAPIToken(tok.ID)
	require.NoError(t, err)
	require.Nil(t, stored.RevokedAt, "rolled back with the caller")

	tx, err = cs.DB().Begin()
	require.NoError(t, err)
	require.NoError(t, v.RevokeAllInTx(tx))
	require.NoError(t, tx.Commit())
	v.EvictAll()
	_, err = v.Verify(raw)
	require.ErrorIs(t, err, ErrInvalidToken)
}

// While one request is re-reading the epoch, the others keep using the cache
// instead of queueing on the database. If that read keeps failing (or hangs),
// the cache must not stay trusted forever: once the last successful epoch
// check is older than 3 intervals, cached tokens are refused (fail closed)
// until a read succeeds again.
func TestCachedTokensRejectedWhenEpochChecksKeepFailing(t *testing.T) {
	v, cs, clock := newVerifier(t)
	_, raw := createToken(t, cs.SQLiteStore, storage.NewAPIToken{})
	_, err := v.Verify(raw)
	require.NoError(t, err)

	boom := errors.New("database is locked")
	cs.failEpoch = boom
	// Another request is stuck reading the epoch.
	v.refreshMu.Lock()
	clock.Advance(DefaultEpochInterval)
	_, err = v.Verify(raw)
	require.NoError(t, err, "a slightly stale cache is still served while the epoch is being read")

	clock.Advance(2*DefaultEpochInterval + time.Millisecond)
	_, err = v.Verify(raw)
	require.Error(t, err, "more than 3 intervals without a successful check: fail closed")
	require.NotErrorIs(t, err, ErrInvalidToken, "a store problem, not a bad token (503, not 401)")
	v.refreshMu.Unlock()

	_, err = v.Verify(raw)
	require.ErrorIs(t, err, boom)

	cs.failEpoch = nil
	_, err = v.Verify(raw)
	require.NoError(t, err, "a successful epoch read restores the cache")
}

func TestConcurrentVerifyAndRevoke(t *testing.T) {
	v, cs, clock := newVerifier(t)
	var raws []string
	var ids []string
	for range 4 {
		tok, raw := createToken(t, cs.SQLiteStore, storage.NewAPIToken{})
		raws = append(raws, raw)
		ids = append(ids, tok.ID)
	}
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			for j := range 50 {
				_, _ = v.Verify(raws[(i+j)%len(raws)])
				if j%10 == 0 {
					clock.Advance(time.Second)
				}
			}
		})
	}
	wg.Go(func() {
		for _, id := range ids[:2] {
			_ = v.Revoke(id)
		}
		v.EvictAll()
	})
	wg.Wait()
	for i, raw := range raws {
		_, err := v.Verify(raw)
		if i < 2 {
			require.ErrorIs(t, err, ErrInvalidToken)
		} else {
			require.NoError(t, err)
		}
	}
}

// Epoch reports the tokens epoch the verifier last read, so caches built
// from verified tokens (the per-token MCP servers) notice changes that do not
// show in a token's own info, such as the agent deploy level.
func TestEpochReportsTheLastReadEpoch(t *testing.T) {
	v, cs, clock := newVerifier(t)
	_, raw := createToken(t, cs.SQLiteStore, storage.NewAPIToken{})
	_, err := v.Verify(raw)
	require.NoError(t, err)
	before := v.Epoch()
	want, err := cs.TokensEpoch()
	require.NoError(t, err)
	assert.Equal(t, want, before)

	require.NoError(t, cs.SetAgentDeployLevel(storage.AgentDeployLow)) // bumps the epoch
	_, err = v.Verify(raw)
	require.NoError(t, err)
	assert.Equal(t, before, v.Epoch(), "re-read only after the interval")
	clock.Advance(DefaultEpochInterval)
	_, err = v.Verify(raw)
	require.NoError(t, err)
	assert.Greater(t, v.Epoch(), before)
}

// VerifyCached answers from the cache only: a token not cached, or not
// matching the cached secret, is a miss without any store access, so a
// caller may use it for clients it limits for failed lookups. A cached
// token still follows revocations: the epoch is re-read when due (at most
// once per interval, only for a cached token) and evictions apply.
func TestVerifyCached(t *testing.T) {
	v, cs, clock := newVerifier(t)
	tok, raw := createToken(t, cs.SQLiteStore, storage.NewAPIToken{Name: "desk"})
	_, other := createToken(t, cs.SQLiteStore, storage.NewAPIToken{})

	for _, r := range []string{raw, "garbage", "pwk_" + tok.ID + "_" + strings.Repeat("a", 52)} {
		_, ok, err := v.VerifyCached(r)
		require.NoError(t, err)
		assert.False(t, ok, r)
	}
	assert.Zero(t, cs.gets.Load()+cs.epochs.Load()+cs.touches.Load(), "a miss reads nothing")

	want, err := v.Verify(raw)
	require.NoError(t, err)
	gets, epochs := cs.gets.Load(), cs.epochs.Load()
	info, ok, err := v.VerifyCached(raw)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, want, info)
	_, ok, err = v.VerifyCached("pwk_" + tok.ID + "_" + strings.Repeat("a", 52))
	require.NoError(t, err)
	assert.False(t, ok, "another secret for a cached id")
	assert.Equal(t, gets, cs.gets.Load(), "never looks a token up")
	assert.Equal(t, epochs, cs.epochs.Load(), "the epoch is not due yet")

	// A revocation by another process shows once the epoch is due.
	_, err = v.Verify(other)
	require.NoError(t, err)
	require.NoError(t, cs.SQLiteStore.RevokeAPIToken(tok.ID, t0))
	clock.Advance(DefaultEpochInterval)
	_, ok, err = v.VerifyCached(raw)
	require.NoError(t, err)
	assert.False(t, ok, "the epoch moved: the cache was cleared")
	assert.Equal(t, epochs+1, cs.epochs.Load())
	assert.Equal(t, gets+1, cs.gets.Load(), "only Verify(other) looked a token up")

	// Without the epoch it fails closed.
	_, err = v.Verify(other)
	require.NoError(t, err)
	clock.Advance(DefaultEpochInterval)
	cs.failEpoch = errors.New("disk on fire")
	_, ok, err = v.VerifyCached(other)
	require.Error(t, err)
	assert.False(t, ok)
	cs.failEpoch = nil

	// Evicted and expired tokens are misses.
	_, err = v.Verify(other)
	require.NoError(t, err)
	_, ok, _ = v.VerifyCached(other)
	require.True(t, ok)
	clock.Advance(storage.DefaultAPITokenLifetime)
	_, ok, err = v.VerifyCached(other)
	require.NoError(t, err)
	assert.False(t, ok, "expired")
}

// KnownID tells the ids of active tokens from made-up ones without reading
// a token: callers limit lookups of unknown ids per client address, and must
// never refuse a real token for what others sent from that address. Asking
// about ids it does not know reads the token list at most once an epoch
// interval, so a made-up id costs nothing more and a new token is known
// within an interval.
func TestKnownID(t *testing.T) {
	v, cs, clock := newVerifier(t)
	a, _ := createToken(t, cs.SQLiteStore, storage.NewAPIToken{Name: "a"})
	madeUp := strings.Repeat("z", len(a.ID))

	known, err := v.KnownID(a.ID)
	require.NoError(t, err)
	assert.True(t, known)
	assert.EqualValues(t, 1, cs.lists.Load())
	for range 5 {
		known, err = v.KnownID(madeUp)
		require.NoError(t, err)
		assert.False(t, known)
	}
	assert.EqualValues(t, 1, cs.lists.Load(), "made-up ids read the list at most once an interval")
	assert.Zero(t, cs.gets.Load(), "no token is read")

	b, _ := createToken(t, cs.SQLiteStore, storage.NewAPIToken{Name: "b"})
	known, _ = v.KnownID(b.ID)
	assert.False(t, known, "not listed yet")
	clock.Advance(DefaultEpochInterval)
	known, _ = v.KnownID(b.ID)
	assert.True(t, known, "listed again an interval later")
	assert.EqualValues(t, 2, cs.lists.Load())

	// Revoked and expired tokens drop out on the next list.
	require.NoError(t, cs.SQLiteStore.RevokeAPIToken(a.ID, t0))
	clock.Advance(DefaultEpochInterval)
	_, _ = v.KnownID(madeUp)
	known, _ = v.KnownID(a.ID)
	assert.False(t, known, "revoked")
	clock.Advance(storage.DefaultAPITokenLifetime)
	_, _ = v.KnownID(madeUp)
	known, _ = v.KnownID(b.ID)
	assert.False(t, known, "expired")

	// A store failure is an error, not an answer.
	clock.Advance(DefaultEpochInterval)
	cs.failList = errors.New("disk on fire")
	_, err = v.KnownID(madeUp)
	assert.Error(t, err)
}
