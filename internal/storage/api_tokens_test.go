package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testNow = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

func newTestToken(t *testing.T, s *SQLiteStore, req NewAPIToken) (*APIToken, string) {
	t.Helper()
	if req.Name == "" {
		req.Name = "laptop"
	}
	if req.Scopes == nil {
		req.Scopes = []string{ScopeCall}
	}
	tok, raw, err := s.CreateAPIToken(req, testNow)
	require.NoError(t, err)
	return tok, raw
}

func TestCreateAPITokenFormatAndOnlyHashStored(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	tok, raw := newTestToken(t, s, NewAPIToken{})

	require.Regexp(t, regexp.MustCompile(`^pwk_[a-z2-7]{16}_[a-z2-7]{52}$`), raw)
	id, secret, ok := ParseAPIToken(raw)
	require.True(t, ok)
	assert.Equal(t, tok.ID, id)
	assert.Equal(t, "pwk_"+id, tok.Prefix)
	sum := sha256.Sum256([]byte(secret))
	assert.Equal(t, hex.EncodeToString(sum[:]), tok.SecretSHA256)

	// Nothing persisted may contain the secret: dump every column of the row.
	rows, err := s.db.Query(`SELECT * FROM api_tokens`)
	require.NoError(t, err)
	defer rows.Close()
	cols, err := rows.Columns()
	require.NoError(t, err)
	n := 0
	for rows.Next() {
		n++
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		require.NoError(t, rows.Scan(ptrs...))
		for i, v := range vals {
			assert.NotContains(t, fmt.Sprint(v), secret, "column %s leaks the secret", cols[i])
			assert.NotContains(t, fmt.Sprintf("%s", v), secret, "column %s leaks the secret", cols[i])
		}
	}
	require.Equal(t, 1, n)

	// Two tokens never share an id or secret.
	_, raw2 := newTestToken(t, s, NewAPIToken{})
	id2, secret2, _ := ParseAPIToken(raw2)
	assert.NotEqual(t, id, id2)
	assert.NotEqual(t, secret, secret2)
}

func TestAPITokenSecretMatchesIsExact(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	tok, raw := newTestToken(t, s, NewAPIToken{})
	_, secret, _ := ParseAPIToken(raw)
	assert.True(t, tok.SecretMatches(secret))
	assert.False(t, tok.SecretMatches(strings.ToUpper(secret)))
	assert.False(t, tok.SecretMatches(secret[:51]))
	assert.False(t, tok.SecretMatches(""))
	broken := *tok
	broken.SecretSHA256 = "not-hex"
	assert.False(t, broken.SecretMatches(secret))
}

func TestParseAPITokenRejectsMalformed(t *testing.T) {
	good := "pwk_" + strings.Repeat("a", 16) + "_" + strings.Repeat("b", 52)
	_, _, ok := ParseAPIToken(good)
	require.True(t, ok)
	for _, raw := range []string{
		"",
		"pwk_",
		strings.Replace(good, "pwk_", "pwx_", 1),
		strings.Replace(good, "pwk_", "PWK_", 1),
		good + "c",
		good[:len(good)-1],
		"pwk_" + strings.Repeat("a", 15) + "_" + strings.Repeat("b", 53),
		"pwk_" + strings.Repeat("A", 16) + "_" + strings.Repeat("b", 52),
		"pwk_" + strings.Repeat("a", 16) + "_" + strings.Repeat("1", 52),
		"pwk_" + strings.Repeat("a", 16) + "-" + strings.Repeat("b", 52),
		" " + good,
	} {
		_, _, ok := ParseAPIToken(raw)
		assert.False(t, ok, raw)
	}
}

func TestAPITokenFieldsRoundTrip(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	grants := []TokenGrant{{FlowID: "f1", TriggerNodeID: "t1", Fingerprint: "fp1"}, {FlowID: "f2", TriggerNodeID: "t2", Fingerprint: "fp2", Suspended: true}}
	created, _ := newTestToken(t, s, NewAPIToken{
		Name:       "  ci agent  ",
		Scopes:     []string{ScopeRead, ScopeCall, ScopeCall},
		Privileged: true,
		Grants:     grants,
		AllTools:   true,
	})
	got, err := s.GetAPIToken(created.ID)
	require.NoError(t, err)
	assert.Equal(t, "ci agent", got.Name)
	assert.Equal(t, []string{ScopeCall, ScopeRead}, got.Scopes, "scopes are deduplicated and sorted")
	assert.True(t, got.Privileged)
	assert.True(t, got.AllTools)
	assert.Equal(t, grants, got.Grants)
	assert.Equal(t, testNow.Add(DefaultAPITokenLifetime), got.ExpiresAt, "default expiry is 90 days")
	assert.Equal(t, testNow, got.CreatedAt)
	assert.Nil(t, got.LastUsedAt)
	assert.Nil(t, got.RevokedAt)
	assert.Equal(t, created.SecretSHA256, got.SecretSHA256)
	assert.True(t, got.Active(testNow))
	assert.False(t, got.Active(got.ExpiresAt), "a token is expired at its expiry instant")

	explicit := testNow.Add(24 * time.Hour)
	tok, _ := newTestToken(t, s, NewAPIToken{ExpiresAt: explicit})
	assert.Equal(t, explicit, tok.ExpiresAt)
	assert.Empty(t, tok.Grants)
	assert.NotNil(t, tok.Grants, "grants serialize as [] rather than null")
}

func TestCreateAPITokenValidates(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	cases := map[string]NewAPIToken{
		"empty name":       {Name: "   ", Scopes: []string{ScopeCall}},
		"long name":        {Name: strings.Repeat("x", 65), Scopes: []string{ScopeCall}},
		"control in name":  {Name: "a\nb", Scopes: []string{ScopeCall}},
		"no scopes":        {Name: "x", Scopes: []string{}},
		"unknown scope":    {Name: "x", Scopes: []string{"admin"}},
		"grant no flow":    {Name: "x", Scopes: []string{ScopeCall}, Grants: []TokenGrant{{TriggerNodeID: "t", Fingerprint: "f"}}},
		"grant no node":    {Name: "x", Scopes: []string{ScopeCall}, Grants: []TokenGrant{{FlowID: "f", Fingerprint: "f"}}},
		"grant no print":   {Name: "x", Scopes: []string{ScopeCall}, Grants: []TokenGrant{{FlowID: "f", TriggerNodeID: "t"}}},
		"duplicate grant":  {Name: "x", Scopes: []string{ScopeCall}, Grants: []TokenGrant{{FlowID: "f", TriggerNodeID: "t", Fingerprint: "a"}, {FlowID: "f", TriggerNodeID: "t", Fingerprint: "b"}}},
		"expiry in past":   {Name: "x", Scopes: []string{ScopeCall}, ExpiresAt: testNow.Add(-time.Second)},
		"expiry too far":   {Name: "x", Scopes: []string{ScopeCall}, ExpiresAt: testNow.Add(MaxAPITokenLifetime + time.Hour)},
		"too many grants":  {Name: "x", Scopes: []string{ScopeCall}, Grants: manyGrants(MaxAPITokenGrants + 1)},
		"oversized grant":  {Name: "x", Scopes: []string{ScopeCall}, Grants: []TokenGrant{{FlowID: strings.Repeat("f", 257), TriggerNodeID: "t", Fingerprint: "a"}}},
		"empty scope item": {Name: "x", Scopes: []string{""}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := s.CreateAPIToken(req, testNow)
			require.ErrorIs(t, err, ErrInvalidAPITokenRequest)
		})
	}
	all, err := s.ListAPITokens()
	require.NoError(t, err)
	assert.Empty(t, all)
}

func manyGrants(n int) []TokenGrant {
	out := make([]TokenGrant, n)
	for i := range out {
		out[i] = TokenGrant{FlowID: fmt.Sprintf("f%d", i), TriggerNodeID: "t", Fingerprint: "fp"}
	}
	return out
}

func TestListAPITokensNewestFirst(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	a, _, err := s.CreateAPIToken(NewAPIToken{Name: "a", Scopes: []string{ScopeCall}}, testNow)
	require.NoError(t, err)
	b, _, err := s.CreateAPIToken(NewAPIToken{Name: "b", Scopes: []string{ScopeRead}}, testNow.Add(time.Minute))
	require.NoError(t, err)
	require.NoError(t, s.RevokeAPIToken(a.ID, testNow.Add(2*time.Minute)))
	list, err := s.ListAPITokens()
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, b.ID, list[0].ID)
	assert.Equal(t, a.ID, list[1].ID)
	require.NotNil(t, list[1].RevokedAt, "revoked tokens stay listed for the audit trail")
}

func TestGetAPITokenNotFound(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	_, err := s.GetAPIToken("nope")
	require.ErrorIs(t, err, ErrAPITokenNotFound)
}

func TestRevokeAPITokenBumpsEpoch(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	epoch, err := s.TokensEpoch()
	require.NoError(t, err)
	require.Equal(t, int64(0), epoch)

	tok, _ := newTestToken(t, s, NewAPIToken{})
	require.NoError(t, s.RevokeAPIToken(tok.ID, testNow.Add(time.Minute)))
	got, err := s.GetAPIToken(tok.ID)
	require.NoError(t, err)
	require.NotNil(t, got.RevokedAt)
	assert.Equal(t, testNow.Add(time.Minute), *got.RevokedAt)
	assert.False(t, got.Active(testNow.Add(time.Minute)))
	epoch, err = s.TokensEpoch()
	require.NoError(t, err)
	assert.Equal(t, int64(1), epoch)

	// Revoking again keeps the original instant.
	require.NoError(t, s.RevokeAPIToken(tok.ID, testNow.Add(time.Hour)))
	got, err = s.GetAPIToken(tok.ID)
	require.NoError(t, err)
	assert.Equal(t, testNow.Add(time.Minute), *got.RevokedAt)

	require.ErrorIs(t, s.RevokeAPIToken("missing", testNow), ErrAPITokenNotFound)
}

func TestRevokeAllAPITokens(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	a, _ := newTestToken(t, s, NewAPIToken{})
	b, _ := newTestToken(t, s, NewAPIToken{})
	require.NoError(t, s.RevokeAPIToken(a.ID, testNow))
	before, err := s.TokensEpoch()
	require.NoError(t, err)

	n, err := s.RevokeAllAPITokens(testNow.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, n, "only still-active tokens are counted")
	got, err := s.GetAPIToken(b.ID)
	require.NoError(t, err)
	require.NotNil(t, got.RevokedAt)
	after, err := s.TokensEpoch()
	require.NoError(t, err)
	assert.Equal(t, before+1, after)

	// An empty revoke-all still bumps the epoch, so caches always resync.
	n, err = s.RevokeAllAPITokens(testNow.Add(2 * time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	again, err := s.TokensEpoch()
	require.NoError(t, err)
	assert.Equal(t, after+1, again)
}

// The count is what the editor reports as "N active tokens revoked", so a
// token that already expired is revoked too but not counted.
func TestRevokeAllAPITokensCountsOnlyTokensActiveNow(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	active, _ := newTestToken(t, s, NewAPIToken{})
	expired, _ := newTestToken(t, s, NewAPIToken{ExpiresAt: testNow.Add(time.Hour)})
	at := testNow.Add(2 * time.Hour)

	n, err := s.RevokeAllAPITokens(at)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "the expired token was not active")
	for _, id := range []string{active.ID, expired.ID} {
		got, err := s.GetAPIToken(id)
		require.NoError(t, err)
		require.NotNil(t, got.RevokedAt, "every unrevoked token is revoked")
	}

	other := createTestStore(t)
	defer other.Close()
	newTestToken(t, other, NewAPIToken{})
	newTestToken(t, other, NewAPIToken{ExpiresAt: testNow.Add(time.Hour)})
	tx, err := other.DB().Begin()
	require.NoError(t, err)
	defer tx.Rollback() //nolint:errcheck // committed below
	n, err = RevokeAllAPITokensTx(tx, at)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	require.NoError(t, tx.Commit())
}

func TestTouchAPITokenLastUsed(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	tok, _ := newTestToken(t, s, NewAPIToken{})
	require.NoError(t, s.TouchAPITokenLastUsed(tok.ID, testNow.Add(time.Minute)))
	got, err := s.GetAPIToken(tok.ID)
	require.NoError(t, err)
	require.NotNil(t, got.LastUsedAt)
	assert.Equal(t, testNow.Add(time.Minute), *got.LastUsedAt)
	// Never moves backwards.
	require.NoError(t, s.TouchAPITokenLastUsed(tok.ID, testNow))
	got, err = s.GetAPIToken(tok.ID)
	require.NoError(t, err)
	assert.Equal(t, testNow.Add(time.Minute), *got.LastUsedAt)
}

func TestSetAPITokenGrants(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	tok, _ := newTestToken(t, s, NewAPIToken{Grants: []TokenGrant{{FlowID: "f", TriggerNodeID: "t", Fingerprint: "old", Suspended: true}}})
	before, err := s.TokensEpoch()
	require.NoError(t, err)

	next := []TokenGrant{{FlowID: "f", TriggerNodeID: "t", Fingerprint: "new"}}
	require.NoError(t, s.SetAPITokenGrants(tok.ID, next))
	got, err := s.GetAPIToken(tok.ID)
	require.NoError(t, err)
	assert.Equal(t, next, got.Grants)
	after, err := s.TokensEpoch()
	require.NoError(t, err)
	assert.Equal(t, before+1, after, "grant changes invalidate verifier caches in every process")

	require.ErrorIs(t, s.SetAPITokenGrants(tok.ID, []TokenGrant{{FlowID: "f"}}), ErrInvalidAPITokenRequest)
	require.ErrorIs(t, s.SetAPITokenGrants("missing", next), ErrAPITokenNotFound)
	require.NoError(t, s.RevokeAPIToken(tok.ID, testNow))
	require.ErrorIs(t, s.SetAPITokenGrants(tok.ID, next), ErrAPITokenNotFound, "revoked tokens cannot regain grants")
}

func TestUpdateAPITokenGrants(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	tok, _ := newTestToken(t, s, NewAPIToken{Grants: []TokenGrant{{FlowID: "f", TriggerNodeID: "t", Fingerprint: "v1", Suspended: true}}})
	before, err := s.TokensEpoch()
	require.NoError(t, err)

	updated, err := s.UpdateAPITokenGrants(tok.ID, func(grants []TokenGrant) ([]TokenGrant, error) {
		assert.Equal(t, []TokenGrant{{FlowID: "f", TriggerNodeID: "t", Fingerprint: "v1", Suspended: true}}, grants, "fn sees the stored grants")
		grants[0].Suspended = false
		return append(grants, TokenGrant{FlowID: "g", TriggerNodeID: "u", Fingerprint: "w1"}), nil
	})
	require.NoError(t, err)
	want := []TokenGrant{{FlowID: "f", TriggerNodeID: "t", Fingerprint: "v1"}, {FlowID: "g", TriggerNodeID: "u", Fingerprint: "w1"}}
	assert.Equal(t, want, updated.Grants)
	assert.Equal(t, tok.ID, updated.ID)
	got, err := s.GetAPIToken(tok.ID)
	require.NoError(t, err)
	assert.Equal(t, want, got.Grants)
	after, err := s.TokensEpoch()
	require.NoError(t, err)
	assert.Equal(t, before+1, after)

	// An error from fn aborts: nothing changes and the epoch stays.
	refused := errors.New("grant not found")
	_, err = s.UpdateAPITokenGrants(tok.ID, func([]TokenGrant) ([]TokenGrant, error) { return nil, refused })
	require.ErrorIs(t, err, refused)
	_, err = s.UpdateAPITokenGrants(tok.ID, func([]TokenGrant) ([]TokenGrant, error) { return []TokenGrant{{FlowID: "f"}}, nil })
	require.ErrorIs(t, err, ErrInvalidAPITokenRequest)
	got, err = s.GetAPIToken(tok.ID)
	require.NoError(t, err)
	assert.Equal(t, want, got.Grants)
	epoch, err := s.TokensEpoch()
	require.NoError(t, err)
	assert.Equal(t, after, epoch)

	called := false
	noop := func(g []TokenGrant) ([]TokenGrant, error) { called = true; return g, nil }
	_, err = s.UpdateAPITokenGrants("missing", noop)
	require.ErrorIs(t, err, ErrAPITokenNotFound)
	require.NoError(t, s.RevokeAPIToken(tok.ID, testNow))
	_, err = s.UpdateAPITokenGrants(tok.ID, noop)
	require.ErrorIs(t, err, ErrAPITokenNotFound, "revoked tokens cannot regain grants")
	assert.False(t, called, "fn only runs for live tokens")
}

func TestUpdateAPITokenGrantsSerializesWriters(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	tok, _ := newTestToken(t, s, NewAPIToken{})
	const n = 16
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			_, err := s.UpdateAPITokenGrants(tok.ID, func(g []TokenGrant) ([]TokenGrant, error) {
				return append(g, TokenGrant{FlowID: fmt.Sprintf("f%d", i), TriggerNodeID: "t", Fingerprint: "v1"}), nil
			})
			assert.NoError(t, err)
		})
	}
	wg.Wait()
	got, err := s.GetAPIToken(tok.ID)
	require.NoError(t, err)
	assert.Len(t, got.Grants, n, "no concurrent update is lost")
}

func TestTokensEpochVisibleAcrossHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	a, err := NewSQLiteStore(path)
	require.NoError(t, err)
	defer a.Close()
	b, err := NewSQLiteStore(path)
	require.NoError(t, err)
	defer b.Close()

	tok, _ := newTestToken(t, a, NewAPIToken{})
	require.NoError(t, b.RevokeAPIToken(tok.ID, testNow))
	epoch, err := a.TokensEpoch()
	require.NoError(t, err)
	assert.Equal(t, int64(1), epoch)
}

func TestAPITokensClosedStore(t *testing.T) {
	s := createTestStore(t)
	require.NoError(t, s.Close())
	_, _, err := s.CreateAPIToken(NewAPIToken{Name: "x", Scopes: []string{ScopeCall}}, testNow)
	require.ErrorIs(t, err, ErrDatabaseClosed)
	_, err = s.TokensEpoch()
	require.ErrorIs(t, err, ErrDatabaseClosed)
}
