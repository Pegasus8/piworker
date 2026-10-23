package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"
)

const SessionIdleTimeout = 7 * 24 * time.Hour
const SessionLifetime = 30 * 24 * time.Hour
const AuthCodeLifetime = 30 * time.Minute

var ErrSessionNotFound = errors.New("session expired or revoked")
var ErrAuthCode = errors.New("invalid or expired code")

type Session struct {
	Username  string `json:"username"`
	CreatedAt int64  `json:"-"`
	LastSeen  int64  `json:"-"`
	ExpiresAt int64  `json:"expiresAt"`
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateSession verifies the password hash again in the transaction, so a login
// racing with a password reset cannot create a session from stale credentials.
func (s *SQLiteUserStore) CreateSession(username, verifiedHash string, now time.Time) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var hash string
	if err = tx.QueryRow("SELECT password_hash FROM users WHERE username=?", username).Scan(&hash); err != nil {
		return "", err
	}
	if hash != verifiedHash {
		return "", ErrSessionNotFound
	}
	if _, err = tx.Exec("DELETE FROM auth_sessions WHERE expires_at<=? OR last_seen<=?", now.Unix(), now.Add(-SessionIdleTimeout).Unix()); err != nil {
		return "", err
	}
	// Bound storage while allowing the administrator to use multiple devices.
	if _, err = tx.Exec(`DELETE FROM auth_sessions WHERE token_hash IN (SELECT token_hash FROM auth_sessions WHERE username=? ORDER BY created_at DESC, rowid DESC LIMIT -1 OFFSET 19)`, username); err != nil {
		return "", err
	}
	_, err = tx.Exec("INSERT INTO auth_sessions(token_hash,username,created_at,last_seen,expires_at) VALUES(?,?,?,?,?)", tokenHash(token), username, now.Unix(), now.Unix(), now.Add(SessionLifetime).Unix())
	if err != nil {
		return "", err
	}
	return token, tx.Commit()
}
func (s *SQLiteUserStore) GetSession(token string, now time.Time, touch bool) (*Session, error) {
	if len(token) != 43 {
		return nil, ErrSessionNotFound
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var session Session
	err = tx.QueryRow(`SELECT s.username,s.created_at,s.last_seen,s.expires_at FROM auth_sessions s JOIN users u ON u.username=s.username WHERE token_hash=?`, tokenHash(token)).Scan(&session.Username, &session.CreatedAt, &session.LastSeen, &session.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	if now.Unix() >= session.ExpiresAt || now.Unix() >= session.LastSeen+int64(SessionIdleTimeout/time.Second) {
		if _, err = tx.Exec("DELETE FROM auth_sessions WHERE token_hash=?", tokenHash(token)); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return nil, ErrSessionNotFound
	}
	if touch {
		if _, err = tx.Exec("UPDATE auth_sessions SET last_seen=MAX(last_seen,?) WHERE token_hash=?", now.Unix(), tokenHash(token)); err != nil {
			return nil, err
		}
	}
	return &session, tx.Commit()
}
func (s *SQLiteUserStore) DeleteSession(token string) error {
	_, err := s.db.Exec("DELETE FROM auth_sessions WHERE token_hash=?", tokenHash(token))
	return err
}
func (s *SQLiteUserStore) ChangePassword(username, oldHash, newHash string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec("UPDATE users SET password_hash=? WHERE username=? AND password_hash=?", newHash, username, oldHash)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrUserNotFound
	}
	if _, err = tx.Exec("DELETE FROM auth_sessions WHERE username=?", username); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM auth_codes WHERE username=?", username); err != nil {
		return err
	}
	return tx.Commit()
}

// NewAuthCode is called only by the local startup/CLI path, never by HTTP.
// Only hashes are persisted. Generating a new code invalidates the previous one.
func (s *SQLiteUserStore) NewAuthCode(purpose, username string, now time.Time) (string, error) {
	if purpose != "setup" && purpose != "recovery" {
		return "", ErrAuthCode
	}
	code, err := randomToken()
	if err != nil {
		return "", err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var count int
	if purpose == "setup" {
		if err = tx.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
			return "", err
		}
		if count != 0 {
			return "", ErrAuthCode
		}
	} else {
		if err = tx.QueryRow("SELECT COUNT(*) FROM users WHERE username=?", username).Scan(&count); err != nil {
			return "", err
		}
		if count != 1 {
			return "", ErrUserNotFound
		}
	}
	_, err = tx.Exec(`INSERT INTO auth_codes(purpose,username,token_hash,expires_at) VALUES(?,?,?,?) ON CONFLICT(purpose) DO UPDATE SET username=excluded.username,token_hash=excluded.token_hash,expires_at=excluded.expires_at`, purpose, username, tokenHash(code), now.Add(AuthCodeLifetime).Unix())
	if err != nil {
		return "", err
	}
	return code, tx.Commit()
}

// RedeemAuthCode consumes the code and creates/resets the account atomically.
func (s *SQLiteUserStore) RedeemAuthCode(purpose, code, username, hash string, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec("DELETE FROM auth_codes WHERE purpose=? AND token_hash=? AND expires_at>? AND (purpose='setup' OR username=?)", purpose, tokenHash(code), now.Unix(), username)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrAuthCode
	}
	if purpose == "setup" {
		result, err = tx.Exec("INSERT INTO users(username,password_hash,created_at) SELECT ?,?,? WHERE NOT EXISTS(SELECT 1 FROM users)", username, hash, now)
	} else if purpose == "recovery" {
		result, err = tx.Exec("UPDATE users SET password_hash=? WHERE username=?", hash, username)
	} else {
		return ErrAuthCode
	}
	if err != nil {
		return err
	}
	n, err = result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrAuthCode
	}
	if _, err = tx.Exec("DELETE FROM auth_sessions WHERE username=?", username); err != nil {
		return err
	}
	return tx.Commit()
}
