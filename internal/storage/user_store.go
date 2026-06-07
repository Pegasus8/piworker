// Package storage provides persistence for flows and related data.
package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Pegasus8/piworker/internal/types"
	_ "github.com/mattn/go-sqlite3"
)

// User storage errors.
var (
	ErrUserNotFound      = errors.New("user not found")
	ErrUserAlreadyExists = errors.New("user already exists")
)

// SQLiteUserStore provides SQLite-based persistence for users.
type SQLiteUserStore struct {
	db     *sql.DB
	shared bool // true when db is owned by another store; Close is then a no-op
}

// NewSQLiteUserStore creates a new SQLite user store with the given database path.
// It uses the same database file as the flow store.
func NewSQLiteUserStore(dbPath string) (*SQLiteUserStore, error) {
	db, err := openDB(dbPath)
	if err != nil {
		return nil, err
	}

	store := &SQLiteUserStore{db: db}
	if err := store.initialize(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize user store: %w", err)
	}

	return store, nil
}

// NewSQLiteUserStoreWithDB creates a user store that shares an existing database
// handle (e.g. the flow store's), avoiding a second connection pool on the same
// file. The caller retains ownership of db, so Close on this store is a no-op.
func NewSQLiteUserStoreWithDB(db *sql.DB) (*SQLiteUserStore, error) {
	store := &SQLiteUserStore{db: db, shared: true}
	if err := store.initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize user store: %w", err)
	}

	return store, nil
}

// initialize creates the users table if it doesn't exist.
func (s *SQLiteUserStore) initialize() error {
	schema := `
		CREATE TABLE IF NOT EXISTS users (
			username TEXT PRIMARY KEY,
			password_hash TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);
	`

	_, err := s.db.Exec(schema)
	return err
}

// GetUser retrieves a user by username.
// Returns nil, nil if user doesn't exist.
func (s *SQLiteUserStore) GetUser(username string) (*types.User, error) {
	var user types.User
	var createdAt time.Time

	err := s.db.QueryRow(
		`SELECT username, password_hash, created_at FROM users WHERE username = ?`,
		username,
	).Scan(&user.Username, &user.PasswordHash, &createdAt)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // User not found - return nil without error (matches interface contract)
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

// CreateUser creates a new user in the store.
func (s *SQLiteUserStore) CreateUser(user *types.User) error {
	if user == nil {
		return fmt.Errorf("user cannot be nil")
	}

	if user.Username == "" {
		return fmt.Errorf("username is required")
	}

	if user.PasswordHash == "" {
		return fmt.Errorf("password hash is required")
	}

	_, err := s.db.Exec(
		`INSERT INTO users (username, password_hash, created_at) VALUES (?, ?, ?)`,
		user.Username, user.PasswordHash, time.Now(),
	)

	if err != nil {
		if isUniqueViolation(err) {
			return ErrUserAlreadyExists
		}
		return fmt.Errorf("failed to create user: %w", err)
	}

	return nil
}

// UserExists checks if any user exists in the store.
func (s *SQLiteUserStore) UserExists() (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check user existence: %w", err)
	}
	return count > 0, nil
}

// Close closes the database connection, unless the handle is shared with another
// store (in which case the owner is responsible for closing it).
func (s *SQLiteUserStore) Close() error {
	if s.shared {
		return nil
	}
	return s.db.Close()
}
