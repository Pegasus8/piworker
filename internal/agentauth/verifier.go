// Package agentauth verifies agent API tokens (pwk_<id>_<secret>) for the MCP
// endpoint (docs/design/mcp.md §7, ADR-0010).
//
// Verified tokens are cached in memory by id so a request normally costs no
// database read. The cache is kept honest three ways: Revoke/RevokeAll/Evict
// drop entries immediately in this process; the tokens_epoch setting (bumped
// in the same transaction as every revocation or grant change) is re-read at
// most every epoch interval and clears the cache when it moved, which covers
// changes made by other processes; and every cached entry still checks its
// own expiry on each use. A request that cannot read the epoch fails, and
// requests served from the cache while another one reads it are refused once
// the last successful read is more than three intervals old, so a failing or
// hanging database never keeps revoked tokens working.
package agentauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/rs/zerolog"
)

// ErrInvalidToken is returned for malformed, unknown, wrong, expired or
// revoked tokens. Any other error from Verify is an internal failure.
var ErrInvalidToken = errors.New("invalid agent token")

// errEpochStale: the tokens epoch has not been read successfully for more
// than maxEpochAgeFactor intervals, so cached tokens are no longer trusted.
var errEpochStale = errors.New("token revocations have not been confirmed recently")

// maxEpochAgeFactor bounds how many epoch intervals cached tokens are served
// without a successful epoch read (fail closed after that).
const maxEpochAgeFactor = 3

const (
	// DefaultEpochInterval is how often the tokens epoch is re-read.
	DefaultEpochInterval = 5 * time.Second
	// DefaultTouchInterval throttles last_used_at writes per token.
	DefaultTouchInterval = time.Minute
)

// Store is the token persistence the verifier needs (*storage.SQLiteStore).
type Store interface {
	GetAPIToken(id string) (*storage.APIToken, error)
	TokensEpoch() (int64, error)
	TouchAPITokenLastUsed(id string, now time.Time) error
	RevokeAPIToken(id string, now time.Time) error
	RevokeAllAPITokens(now time.Time) (int, error)
}

// Grant is a token's permission to call one tool.
type Grant = storage.TokenGrant

// TokenInfo is what a verified token may do. Slices are copies owned by the
// caller.
type TokenInfo struct {
	ID         string
	Name       string
	Scopes     []string
	Privileged bool
	Grants     []Grant
	AllTools   bool
	ExpiresAt  time.Time
}

// HasScope reports whether the token carries scope.
func (t TokenInfo) HasScope(scope string) bool { return slices.Contains(t.Scopes, scope) }

func (t TokenInfo) clone() TokenInfo {
	t.Scopes = slices.Clone(t.Scopes)
	t.Grants = slices.Clone(t.Grants)
	return t
}

// Option configures a TokenVerifier.
type Option func(*TokenVerifier)

// WithClock sets the time source (tests).
func WithClock(now func() time.Time) Option { return func(v *TokenVerifier) { v.now = now } }

// WithEpochInterval sets how often the tokens epoch is re-read.
func WithEpochInterval(d time.Duration) Option { return func(v *TokenVerifier) { v.epochInterval = d } }

// WithTouchInterval sets the minimum time between last_used_at writes.
func WithTouchInterval(d time.Duration) Option { return func(v *TokenVerifier) { v.touchInterval = d } }

// WithLogger reports failed last_used_at writes.
func WithLogger(l zerolog.Logger) Option { return func(v *TokenVerifier) { v.log = l } }

type cachedToken struct {
	info        TokenInfo
	secretHash  [sha256.Size]byte
	lastTouched time.Time
}

// TokenVerifier checks raw tokens against the store, with a cache.
type TokenVerifier struct {
	store         Store
	now           func() time.Time
	epochInterval time.Duration
	touchInterval time.Duration
	log           zerolog.Logger

	mu    sync.Mutex
	cache map[string]*cachedToken
	// gen increments on every eviction, so a lookup that raced with one does
	// not cache what it read before the eviction.
	gen            uint64
	epoch          int64
	epochLoaded    bool
	epochCheckedAt time.Time

	// refreshMu serializes epoch reads.
	refreshMu sync.Mutex
}

// NewTokenVerifier returns a verifier backed by store.
func NewTokenVerifier(store Store, opts ...Option) *TokenVerifier {
	v := &TokenVerifier{
		store:         store,
		now:           time.Now,
		epochInterval: DefaultEpochInterval,
		touchInterval: DefaultTouchInterval,
		log:           zerolog.Nop(),
		cache:         make(map[string]*cachedToken),
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// Verify checks a raw pwk_ token and returns what it may do. It returns
// ErrInvalidToken for every credential problem and a different error when the
// store cannot be read (the caller should answer 503, not 401).
func (v *TokenVerifier) Verify(raw string) (TokenInfo, error) {
	id, secret, ok := storage.ParseAPIToken(raw)
	if !ok {
		return TokenInfo{}, ErrInvalidToken
	}
	now := v.now()
	if err := v.refreshEpoch(now); err != nil {
		return TokenInfo{}, fmt.Errorf("agentauth: cannot check token revocations: %w", err)
	}
	sum := sha256.Sum256([]byte(secret))

	info, found, gen := v.fromCache(id, sum, now)
	switch found {
	case cacheValid:
		return info, nil
	case cacheInvalid:
		return TokenInfo{}, ErrInvalidToken
	}

	tok, err := v.store.GetAPIToken(id)
	if errors.Is(err, storage.ErrAPITokenNotFound) {
		return TokenInfo{}, ErrInvalidToken
	}
	if err != nil {
		return TokenInfo{}, fmt.Errorf("agentauth: cannot load token: %w", err)
	}
	if !tok.SecretMatches(secret) || !tok.Active(now) {
		return TokenInfo{}, ErrInvalidToken
	}
	entry := &cachedToken{
		info: TokenInfo{
			ID:         tok.ID,
			Name:       tok.Name,
			Scopes:     tok.Scopes,
			Privileged: tok.Privileged,
			Grants:     tok.Grants,
			AllTools:   tok.AllTools,
			ExpiresAt:  tok.ExpiresAt,
		},
	}
	hex.Decode(entry.secretHash[:], []byte(tok.SecretSHA256)) //nolint:errcheck // validated by SecretMatches
	touch := tok.LastUsedAt == nil || now.Sub(*tok.LastUsedAt) >= v.touchInterval
	if touch {
		entry.lastTouched = now
	} else {
		entry.lastTouched = *tok.LastUsedAt
	}
	v.mu.Lock()
	if v.gen == gen {
		v.cache[id] = entry
	}
	v.mu.Unlock()
	if touch {
		v.touch(id, now)
	}
	return entry.info.clone(), nil
}

// VerifyCached is Verify answered from the cache alone: ok is true only for
// a cached token with this secret that is still valid. Anything else (a
// token never verified, evicted or expired, another secret, a malformed
// one) is a miss, with ok false and no store access, so callers may use it
// for clients they limit for failed lookups. A cached token still follows
// revocations: the tokens epoch is re-read when due, at most once per
// interval across all callers (an error then is a store failure, not a
// miss), and last_used_at is recorded as by Verify.
func (v *TokenVerifier) VerifyCached(raw string) (info TokenInfo, ok bool, err error) {
	id, secret, parsed := storage.ParseAPIToken(raw)
	if !parsed {
		return TokenInfo{}, false, nil
	}
	sum := sha256.Sum256([]byte(secret))
	v.mu.Lock()
	e := v.cache[id]
	cached := e != nil && subtle.ConstantTimeCompare(sum[:], e.secretHash[:]) == 1
	v.mu.Unlock()
	if !cached {
		return TokenInfo{}, false, nil
	}
	now := v.now()
	if err := v.refreshEpoch(now); err != nil {
		return TokenInfo{}, false, fmt.Errorf("agentauth: cannot check token revocations: %w", err)
	}
	info, found, _ := v.fromCache(id, sum, now)
	return info, found == cacheValid, nil
}

// Outcomes of a cache lookup.
const (
	cacheMiss    = iota // not cached: look the token up
	cacheValid          // cached and valid
	cacheInvalid        // cached with another secret, or expired (evicted)
)

// fromCache looks a token up in the cache, recording its use when due. gen
// is the cache generation a miss was read at.
func (v *TokenVerifier) fromCache(id string, sum [sha256.Size]byte, now time.Time) (info TokenInfo, found int, gen uint64) {
	v.mu.Lock()
	e := v.cache[id]
	if e == nil {
		gen = v.gen
		v.mu.Unlock()
		return TokenInfo{}, cacheMiss, gen
	}
	if subtle.ConstantTimeCompare(sum[:], e.secretHash[:]) != 1 {
		v.mu.Unlock()
		return TokenInfo{}, cacheInvalid, 0
	}
	if !now.Before(e.info.ExpiresAt) {
		delete(v.cache, id)
		v.mu.Unlock()
		return TokenInfo{}, cacheInvalid, 0
	}
	info = e.info.clone()
	touch := now.Sub(e.lastTouched) >= v.touchInterval
	if touch {
		e.lastTouched = now
	}
	v.mu.Unlock()
	if touch {
		v.touch(id, now)
	}
	return info, cacheValid, 0
}

// refreshEpoch re-reads the tokens epoch when due and clears the cache when it
// changed. The first read blocks; later ones are skipped while another
// goroutine is already refreshing.
func (v *TokenVerifier) refreshEpoch(now time.Time) error {
	due := func() (bool, bool) {
		v.mu.Lock()
		defer v.mu.Unlock()
		return !v.epochLoaded || now.Sub(v.epochCheckedAt) >= v.epochInterval || now.Before(v.epochCheckedAt), v.epochLoaded
	}
	isDue, loaded := due()
	if !isDue {
		return nil
	}
	if loaded {
		if !v.refreshMu.TryLock() {
			// Another request is reading the epoch: keep serving the cache,
			// but not indefinitely if that read keeps failing or hangs.
			v.mu.Lock()
			stale := now.Sub(v.epochCheckedAt) > maxEpochAgeFactor*v.epochInterval
			v.mu.Unlock()
			if stale {
				return errEpochStale
			}
			return nil
		}
	} else {
		v.refreshMu.Lock()
	}
	defer v.refreshMu.Unlock()
	if isDue, _ = due(); !isDue {
		return nil
	}
	epoch, err := v.store.TokensEpoch()
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.epochLoaded && epoch != v.epoch {
		clear(v.cache)
		v.gen++
	}
	v.epoch, v.epochLoaded, v.epochCheckedAt = epoch, true, now
	return nil
}

func (v *TokenVerifier) touch(id string, now time.Time) {
	if err := v.store.TouchAPITokenLastUsed(id, now); err != nil {
		v.log.Warn().Err(err).Str("tokenId", id).Msg("Cannot record agent token use")
	}
}

// Epoch returns the tokens epoch as last read by Verify (0 before the first
// read). It changes, at most one epoch interval late, after every revocation,
// grant change or agent deploy level change made by any process, so callers
// can key caches built from verified tokens on it.
func (v *TokenVerifier) Epoch() int64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.epoch
}

// Evict drops one cached token, e.g. after its grants changed.
func (v *TokenVerifier) Evict(id string) {
	v.mu.Lock()
	delete(v.cache, id)
	v.gen++
	v.mu.Unlock()
}

// EvictAll drops every cached token.
func (v *TokenVerifier) EvictAll() {
	v.mu.Lock()
	clear(v.cache)
	v.gen++
	v.mu.Unlock()
}

// Revoke revokes a token in the store (bumping the epoch for other
// processes) and evicts it here, so it stops working immediately.
func (v *TokenVerifier) Revoke(id string) error {
	err := v.store.RevokeAPIToken(id, v.now())
	v.Evict(id)
	return err
}

// RevokeAllInTx revokes every token (and bumps the epoch) inside tx, a
// transaction on the database that holds the tokens, typically the one that
// resets the admin password. Call EvictAll once tx committed.
func (v *TokenVerifier) RevokeAllInTx(tx *sql.Tx) error {
	_, err := storage.RevokeAllAPITokensTx(tx, v.now())
	return err
}

// RevokeAll revokes every token and clears the cache. It returns how many
// tokens were still active.
func (v *TokenVerifier) RevokeAll() (int, error) {
	n, err := v.store.RevokeAllAPITokens(v.now())
	v.EvictAll()
	return n, err
}
