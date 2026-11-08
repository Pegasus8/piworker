package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Agent API tokens (docs/design/mcp.md §7). A token is shown to the admin once
// as pwk_<id>_<secret>; only SHA-256(secret) is stored. Tokens are looked up
// by id and the secret hash is compared in constant time.

// Token scopes.
const (
	ScopeCall   = "call"
	ScopeRead   = "read"
	ScopeAuthor = "author"
	ScopeDeploy = "deploy"
)

const (
	// APITokenPrefix starts every agent token.
	APITokenPrefix = "pwk_"
	// DefaultAPITokenLifetime applies when no expiry is requested.
	DefaultAPITokenLifetime = 90 * 24 * time.Hour
	// MaxAPITokenLifetime bounds explicit expiries.
	MaxAPITokenLifetime = 365 * 24 * time.Hour
	// MaxAPITokenNameLength bounds the display name (bytes).
	MaxAPITokenNameLength = 64
	// MaxAPITokenGrants bounds the grants of one token.
	MaxAPITokenGrants = 256
	// maxGrantFieldLength bounds each grant field (bytes).
	maxGrantFieldLength = 256

	tokenIDBytes     = 10 // 16 base32 characters
	tokenSecretBytes = 32 // 52 base32 characters
	tokenIDLen       = 16
	tokenSecretLen   = 52

	// tokensEpochKey is the settings row bumped on every revocation or grant
	// change, so verifier caches in any process can notice and resync.
	tokensEpochKey = "tokens_epoch"
)

var (
	// ErrAPITokenNotFound is returned for unknown (or, for grant updates,
	// revoked) token ids.
	ErrAPITokenNotFound = errors.New("api token not found")
	// ErrInvalidAPITokenRequest wraps validation failures; the message after
	// the colon is safe to show to the admin.
	ErrInvalidAPITokenRequest = errors.New("invalid api token request")
)

var tokenEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

var validScopes = map[string]bool{ScopeCall: true, ScopeRead: true, ScopeAuthor: true, ScopeDeploy: true}

// ValidScope reports whether s is a known token scope.
func ValidScope(s string) bool { return validScopes[s] }

// TokenGrant allows a token to call one tool, identified by its flow, its
// trigger-tool node and the fingerprint the admin approved. A suspended grant
// is kept for re-confirmation but does not allow calls.
type TokenGrant struct {
	FlowID        string `json:"flowId"`
	TriggerNodeID string `json:"triggerNodeId"`
	Fingerprint   string `json:"fingerprint"`
	Suspended     bool   `json:"suspended"`
}

// APIToken is a stored agent token. SecretSHA256 never leaves the server.
type APIToken struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Prefix       string       `json:"prefix"`
	SecretSHA256 string       `json:"-"`
	Scopes       []string     `json:"scopes"`
	Privileged   bool         `json:"privileged"`
	Grants       []TokenGrant `json:"grants"`
	AllTools     bool         `json:"allTools"`
	ExpiresAt    time.Time    `json:"expiresAt"`
	LastUsedAt   *time.Time   `json:"lastUsedAt,omitempty"`
	RevokedAt    *time.Time   `json:"revokedAt,omitempty"`
	CreatedAt    time.Time    `json:"createdAt"`
}

// Active reports whether the token is neither revoked nor expired at now.
func (t *APIToken) Active(now time.Time) bool {
	return t.RevokedAt == nil && now.Before(t.ExpiresAt)
}

// SecretMatches compares secret against the stored hash in constant time.
func (t *APIToken) SecretMatches(secret string) bool {
	want, err := hex.DecodeString(t.SecretSHA256)
	if err != nil || len(want) != sha256.Size {
		return false
	}
	got := sha256.Sum256([]byte(secret))
	return subtle.ConstantTimeCompare(got[:], want) == 1
}

// NewAPIToken describes a token to create.
type NewAPIToken struct {
	Name       string
	Scopes     []string
	Privileged bool
	Grants     []TokenGrant
	AllTools   bool
	// ExpiresAt is the expiry; zero means now + DefaultAPITokenLifetime.
	ExpiresAt time.Time
}

// ParseAPIToken splits a raw pwk_<id>_<secret> token. It only checks the
// shape; the secret is verified against the stored hash by the caller.
func ParseAPIToken(raw string) (id, secret string, ok bool) {
	rest, found := strings.CutPrefix(raw, APITokenPrefix)
	if !found || len(rest) != tokenIDLen+1+tokenSecretLen || rest[tokenIDLen] != '_' {
		return "", "", false
	}
	id, secret = rest[:tokenIDLen], rest[tokenIDLen+1:]
	if !isTokenAlphabet(id) || !isTokenAlphabet(secret) {
		return "", "", false
	}
	return id, secret, true
}

func isTokenAlphabet(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '2' || c > '7') {
			return false
		}
	}
	return true
}

func randomTokenPart(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return tokenEncoding.EncodeToString(b), nil
}

func invalidToken(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidAPITokenRequest, fmt.Sprintf(format, args...))
}

// NormalizeAPITokenName trims a display name and checks its length and characters.
func NormalizeAPITokenName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > MaxAPITokenNameLength {
		return "", invalidToken("name must be 1-%d characters", MaxAPITokenNameLength)
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		return "", invalidToken("name must not contain control characters")
	}
	return name, nil
}

// NormalizeScopes validates, deduplicates and sorts scopes.
func NormalizeScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return nil, invalidToken("at least one scope is required")
	}
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		if !ValidScope(s) {
			return nil, invalidToken("unknown scope %q", s)
		}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out, nil
}

// ValidateGrants checks grant fields and rejects duplicates per tool.
func ValidateGrants(grants []TokenGrant) error {
	if len(grants) > MaxAPITokenGrants {
		return invalidToken("at most %d grants per token", MaxAPITokenGrants)
	}
	seen := make(map[[2]string]bool, len(grants))
	for i, g := range grants {
		for _, f := range []string{g.FlowID, g.TriggerNodeID, g.Fingerprint} {
			if f == "" || len(f) > maxGrantFieldLength {
				return invalidToken("grants[%d]: flowId, triggerNodeId and fingerprint are required (at most %d bytes)", i, maxGrantFieldLength)
			}
		}
		key := [2]string{g.FlowID, g.TriggerNodeID}
		if seen[key] {
			return invalidToken("grants[%d]: duplicate grant for the same tool", i)
		}
		seen[key] = true
	}
	return nil
}

func marshalGrants(grants []TokenGrant) (string, error) {
	if grants == nil {
		grants = []TokenGrant{}
	}
	b, err := json.Marshal(grants)
	return string(b), err
}

// CreateAPIToken validates req, stores the token and returns it together with
// the plaintext, which is never retrievable again.
func (s *SQLiteStore) CreateAPIToken(req NewAPIToken, now time.Time) (*APIToken, string, error) {
	if s.isClosed() {
		return nil, "", ErrDatabaseClosed
	}
	name, err := NormalizeAPITokenName(req.Name)
	if err != nil {
		return nil, "", err
	}
	scopes, err := NormalizeScopes(req.Scopes)
	if err != nil {
		return nil, "", err
	}
	if err := ValidateGrants(req.Grants); err != nil {
		return nil, "", err
	}
	expires := req.ExpiresAt
	if expires.IsZero() {
		expires = now.Add(DefaultAPITokenLifetime)
	}
	if !expires.After(now) || expires.After(now.Add(MaxAPITokenLifetime)) {
		return nil, "", invalidToken("expiry must be in the future and at most %d days away", int(MaxAPITokenLifetime/(24*time.Hour)))
	}
	id, err := randomTokenPart(tokenIDBytes)
	if err != nil {
		return nil, "", err
	}
	secret, err := randomTokenPart(tokenSecretBytes)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256([]byte(secret))
	grants := slices.Clone(req.Grants)
	if grants == nil {
		grants = []TokenGrant{}
	}
	tok := &APIToken{
		ID:           id,
		Name:         name,
		Prefix:       APITokenPrefix + id,
		SecretSHA256: hex.EncodeToString(sum[:]),
		Scopes:       scopes,
		Privileged:   req.Privileged,
		Grants:       grants,
		AllTools:     req.AllTools,
		ExpiresAt:    time.Unix(expires.Unix(), 0).UTC(),
		CreatedAt:    time.Unix(now.Unix(), 0).UTC(),
	}
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return nil, "", err
	}
	grantsJSON, err := marshalGrants(grants)
	if err != nil {
		return nil, "", err
	}
	_, err = s.db.Exec(`
		INSERT INTO api_tokens (id, name, prefix, secret_sha256, scopes, privileged, grants_json, all_tools, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, tok.ID, tok.Name, tok.Prefix, tok.SecretSHA256, string(scopesJSON), tok.Privileged, grantsJSON, tok.AllTools, tok.ExpiresAt.Unix(), tok.CreatedAt.Unix())
	if err != nil {
		return nil, "", fmt.Errorf("failed to create api token: %w", err)
	}
	return tok, APITokenPrefix + id + "_" + secret, nil
}

const apiTokenColumns = `id, name, prefix, secret_sha256, scopes, privileged, grants_json, all_tools, expires_at, last_used_at, revoked_at, created_at`

type rowScanner interface{ Scan(dest ...any) error }

func unixPtr(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := time.Unix(v.Int64, 0).UTC()
	return &t
}

func scanAPIToken(row rowScanner) (*APIToken, error) {
	var (
		t                  APIToken
		scopes, grants     string
		expires, created   int64
		lastUsed, revokedA sql.NullInt64
	)
	if err := row.Scan(&t.ID, &t.Name, &t.Prefix, &t.SecretSHA256, &scopes, &t.Privileged, &grants, &t.AllTools, &expires, &lastUsed, &revokedA, &created); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(scopes), &t.Scopes); err != nil {
		return nil, fmt.Errorf("corrupt scopes for api token %s: %w", t.ID, err)
	}
	if err := json.Unmarshal([]byte(grants), &t.Grants); err != nil {
		return nil, fmt.Errorf("corrupt grants for api token %s: %w", t.ID, err)
	}
	if t.Grants == nil {
		t.Grants = []TokenGrant{}
	}
	t.ExpiresAt = time.Unix(expires, 0).UTC()
	t.CreatedAt = time.Unix(created, 0).UTC()
	t.LastUsedAt = unixPtr(lastUsed)
	t.RevokedAt = unixPtr(revokedA)
	return &t, nil
}

// GetAPIToken returns a token by id, including revoked and expired ones.
func (s *SQLiteStore) GetAPIToken(id string) (*APIToken, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	tok, err := scanAPIToken(s.db.QueryRow(`SELECT `+apiTokenColumns+` FROM api_tokens WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAPITokenNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get api token: %w", err)
	}
	return tok, nil
}

// ListAPITokens returns every token, newest first.
func (s *SQLiteStore) ListAPITokens() ([]APIToken, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	rows, err := s.db.Query(`SELECT ` + apiTokenColumns + ` FROM api_tokens ORDER BY created_at DESC, rowid DESC`)
	if err != nil {
		return nil, fmt.Errorf("failed to list api tokens: %w", err)
	}
	defer rows.Close()
	out := []APIToken{}
	for rows.Next() {
		tok, err := scanAPIToken(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan api token: %w", err)
		}
		out = append(out, *tok)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating api tokens: %w", err)
	}
	return out, nil
}

func bumpTokensEpoch(tx *sql.Tx) error {
	_, err := tx.Exec(`
		INSERT INTO settings (key, value) VALUES (?, '1')
		ON CONFLICT(key) DO UPDATE SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT)
	`, tokensEpochKey)
	if err != nil {
		return fmt.Errorf("failed to bump tokens epoch: %w", err)
	}
	return nil
}

// inTokenTx runs fn and bumps the tokens epoch in one transaction.
func (s *SQLiteStore) inTokenTx(fn func(tx *sql.Tx) error) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin api token update: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	if err := fn(tx); err != nil {
		return err
	}
	if err := bumpTokensEpoch(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// RevokeAPIToken revokes one token (keeping the first revocation instant),
// rejects its pending approvals so nothing it queued can still run, and bumps
// the tokens epoch, all in one transaction.
func (s *SQLiteStore) RevokeAPIToken(id string, now time.Time) error {
	return s.inTokenTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE api_tokens SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`, now.Unix(), id)
		if err != nil {
			return fmt.Errorf("failed to revoke api token: %w", err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return ErrAPITokenNotFound
		}
		return voidPendingApprovals(tx, `a.token_id = ?`, []any{id}, now)
	})
}

// RevokeAllAPITokens revokes every token not yet revoked, rejects every
// pending approval (no token can act any more), bumps the tokens epoch (even
// when nothing changed) and returns how many tokens were revoked.
func (s *SQLiteStore) RevokeAllAPITokens(now time.Time) (int, error) {
	var n int64
	err := s.inTokenTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE api_tokens SET revoked_at = ? WHERE revoked_at IS NULL`, now.Unix())
		if err != nil {
			return fmt.Errorf("failed to revoke api tokens: %w", err)
		}
		if n, err = res.RowsAffected(); err != nil {
			return err
		}
		return voidPendingApprovals(tx, `NOT `+approvalTokenActive, []any{now.Unix()}, now)
	})
	return int(n), err
}

// voidPendingApprovals closes, as rejected because the token was revoked,
// the pending approvals still within their expiry that match cond (on
// agent_approvals aliased a). Expired ones are left to the expiry pass.
func voidPendingApprovals(tx *sql.Tx, cond string, condArgs []any, now time.Time) error {
	args := append([]any{ApprovalRejected, now.Unix(), ApprovalDecidedByTokenRevoked, ApprovalPending, now.Unix()}, condArgs...)
	_, err := tx.Exec(`
		UPDATE agent_approvals AS a SET status = ?, decided_at = ?, decided_by = ?
		WHERE a.status = ? AND a.expires_at > ? AND `+cond, args...)
	if err != nil {
		return fmt.Errorf("failed to reject pending approvals: %w", err)
	}
	return nil
}

// SetAPITokenGrants replaces the grants of a non-revoked token and bumps the
// tokens epoch so cached verifications pick up the change. It overwrites
// whatever is stored: to change grants based on their current state, use
// UpdateAPITokenGrants.
func (s *SQLiteStore) SetAPITokenGrants(id string, grants []TokenGrant) error {
	if err := ValidateGrants(grants); err != nil {
		return err
	}
	_, err := s.UpdateAPITokenGrants(id, func([]TokenGrant) ([]TokenGrant, error) {
		return slices.Clone(grants), nil
	})
	return err
}

// UpdateAPITokenGrants changes the grants of a non-revoked token by applying
// fn to the stored grants inside one write transaction, so concurrent changes
// (two editor tabs, a redeploy suspending a grant) never overwrite each other.
// fn gets a copy it may modify and must not use the store, whose only
// connection the transaction holds. An error from fn aborts the update and is
// returned unchanged. The result is validated and stored, and the tokens epoch
// bumped, in the same transaction. It returns the updated token, or
// ErrAPITokenNotFound (without calling fn) for unknown or revoked tokens.
func (s *SQLiteStore) UpdateAPITokenGrants(id string, fn func([]TokenGrant) ([]TokenGrant, error)) (*APIToken, error) {
	var tok *APIToken
	err := s.inTokenTx(func(tx *sql.Tx) error {
		cur, err := scanAPIToken(tx.QueryRow(`SELECT `+apiTokenColumns+` FROM api_tokens WHERE id = ? AND revoked_at IS NULL`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAPITokenNotFound
		}
		if err != nil {
			return fmt.Errorf("failed to read api token: %w", err)
		}
		grants, err := fn(slices.Clone(cur.Grants))
		if err != nil {
			return err
		}
		if grants == nil {
			grants = []TokenGrant{}
		}
		if err := ValidateGrants(grants); err != nil {
			return err
		}
		grantsJSON, err := marshalGrants(grants)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE api_tokens SET grants_json = ? WHERE id = ?`, grantsJSON, id); err != nil {
			return fmt.Errorf("failed to update api token grants: %w", err)
		}
		cur.Grants = slices.Clone(grants)
		tok = cur
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tok, nil
}

// TouchAPITokenLastUsed records use of a token; the timestamp never moves
// backwards. Callers throttle this (at most once per minute per token).
func (s *SQLiteStore) TouchAPITokenLastUsed(id string, now time.Time) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	if _, err := s.db.Exec(`UPDATE api_tokens SET last_used_at = MAX(COALESCE(last_used_at, 0), ?) WHERE id = ?`, now.Unix(), id); err != nil {
		return fmt.Errorf("failed to touch api token: %w", err)
	}
	return nil
}

// TokensEpoch returns the counter bumped on every revocation or grant change
// (0 before the first one).
func (s *SQLiteStore) TokensEpoch() (int64, error) {
	v, ok, err := s.GetSetting(tokensEpochKey)
	if err != nil || !ok {
		return 0, err
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("corrupt %s setting %q: %w", tokensEpochKey, v, err)
	}
	return n, nil
}

const apiTokensSchema = `
	CREATE TABLE IF NOT EXISTS api_tokens (
		id            TEXT PRIMARY KEY,
		name          TEXT NOT NULL,
		prefix        TEXT NOT NULL,
		secret_sha256 TEXT NOT NULL,
		scopes        TEXT NOT NULL,
		privileged    INTEGER NOT NULL DEFAULT 0,
		grants_json   TEXT NOT NULL DEFAULT '[]',
		all_tools     INTEGER NOT NULL DEFAULT 0,
		expires_at    INTEGER NOT NULL,
		last_used_at  INTEGER,
		revoked_at    INTEGER,
		created_at    INTEGER NOT NULL
	);
`

// migrateAgent creates the agent tables (tokens, approvals, audit). Every
// statement is idempotent.
func (s *SQLiteStore) migrateAgent() error {
	for _, schema := range []string{apiTokensSchema, agentApprovalsSchema, mcpAuditSchema} {
		if _, err := s.db.Exec(schema); err != nil {
			return fmt.Errorf("failed to migrate agent tables: %w", err)
		}
	}
	return nil
}
