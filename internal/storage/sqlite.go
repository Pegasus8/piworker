// Package storage provides persistence for flows and related data.
package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	sqlite3 "github.com/mattn/go-sqlite3"
)

// buildDSN builds a SQLite DSN for the given path. File-backed databases enable
// WAL (concurrent readers), a busy_timeout (writers wait for the lock instead of
// failing with "database is locked"), immediate transaction locking, and foreign
// keys. In-memory databases use shared-cache mode so multiple handles see the
// same data.
func buildDSN(dbPath string) string {
	if dbPath == ":memory:" {
		return "file::memory:?mode=memory&cache=shared"
	}
	return dbPath + "?_busy_timeout=5000&_journal_mode=WAL&_txlock=immediate&_foreign_keys=on"
}

// openDB opens and tunes a SQLite connection pool. SQLite allows a single writer,
// so the pool is capped at one connection to eliminate lock contention; combined
// with WAL + busy_timeout this keeps concurrent requests correct.
func openDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", buildDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// Common errors for storage operations.
var (
	ErrFlowNotFound      = errors.New("flow not found")
	ErrFlowAlreadyExists = errors.New("flow already exists")
	ErrInvalidFlowData   = errors.New("invalid flow data")
	ErrDatabaseClosed    = errors.New("database is closed")
)

// SQLiteStore provides SQLite-based persistence for flows.
type SQLiteStore struct {
	db     *sql.DB
	closed bool
	mu     sync.RWMutex
}

// NewSQLiteStore creates a new SQLite store with the given database path.
// Use ":memory:" for an in-memory database.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := openDB(dbPath)
	if err != nil {
		return nil, err
	}

	store := &SQLiteStore{db: db}
	if err := store.initialize(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	return store, nil
}

// DB returns the underlying database handle so callers can share a single
// connection pool across stores (e.g. the user store), avoiding a second pool
// on the same file.
func (s *SQLiteStore) DB() *sql.DB {
	return s.db
}

// initialize creates the necessary tables if they don't exist.
func (s *SQLiteStore) initialize() error {
	schema := `
		CREATE TABLE IF NOT EXISTS flows (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			data TEXT NOT NULL,
			state TEXT NOT NULL DEFAULT 'inactive',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_flows_name ON flows(name);
		CREATE INDEX IF NOT EXISTS idx_flows_state ON flows(state);
		CREATE INDEX IF NOT EXISTS idx_flows_created_at ON flows(created_at);

		CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`

	_, err := s.db.Exec(schema)
	return err
}

// GetSetting returns a stored setting value and whether it exists.
func (s *SQLiteStore) GetSetting(key string) (string, bool, error) {
	if s.isClosed() {
		return "", false, ErrDatabaseClosed
	}

	var value string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to get setting: %w", err)
	}
	return value, true, nil
}

// SetSetting stores (or replaces) a setting value.
func (s *SQLiteStore) SetSetting(key, value string) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}

	_, err := s.db.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	if err != nil {
		return fmt.Errorf("failed to set setting: %w", err)
	}
	return nil
}

// CreateFlow saves a new flow to the database.
func (s *SQLiteStore) CreateFlow(f *flow.Flow) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}

	if f == nil {
		return ErrInvalidFlowData
	}

	if f.ID == "" {
		return fmt.Errorf("%w: empty flow ID", ErrInvalidFlowData)
	}

	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("failed to marshal flow: %w", err)
	}

	now := time.Now()
	_, err = s.db.Exec(`
		INSERT INTO flows (id, name, description, data, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, f.ID, f.Name, f.Description, string(data), f.State, now, now)

	if err != nil {
		// Check for unique constraint violation
		if isUniqueViolation(err) {
			return ErrFlowAlreadyExists
		}
		return fmt.Errorf("failed to create flow: %w", err)
	}

	return nil
}

// GetFlow retrieves a flow by ID.
func (s *SQLiteStore) GetFlow(id string) (*flow.Flow, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}

	if id == "" {
		return nil, fmt.Errorf("%w: empty flow ID", ErrInvalidFlowData)
	}

	var data string
	err := s.db.QueryRow(`SELECT data FROM flows WHERE id = ?`, id).Scan(&data)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFlowNotFound
		}
		return nil, fmt.Errorf("failed to get flow: %w", err)
	}

	var f flow.Flow
	if err := json.Unmarshal([]byte(data), &f); err != nil {
		return nil, fmt.Errorf("failed to unmarshal flow: %w", err)
	}

	return &f, nil
}

// UpdateFlow updates an existing flow.
func (s *SQLiteStore) UpdateFlow(f *flow.Flow) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}

	if f == nil {
		return ErrInvalidFlowData
	}

	if f.ID == "" {
		return fmt.Errorf("%w: empty flow ID", ErrInvalidFlowData)
	}

	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("failed to marshal flow: %w", err)
	}

	result, err := s.db.Exec(`
		UPDATE flows
		SET name = ?, description = ?, data = ?, state = ?, updated_at = ?
		WHERE id = ?
	`, f.Name, f.Description, string(data), f.State, time.Now(), f.ID)

	if err != nil {
		return fmt.Errorf("failed to update flow: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrFlowNotFound
	}

	return nil
}

// DeleteFlow removes a flow from the database.
func (s *SQLiteStore) DeleteFlow(id string) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}

	if id == "" {
		return fmt.Errorf("%w: empty flow ID", ErrInvalidFlowData)
	}

	result, err := s.db.Exec(`DELETE FROM flows WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete flow: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrFlowNotFound
	}

	return nil
}

// ListFlows returns all flows, optionally filtered by state.
func (s *SQLiteStore) ListFlows(state *flow.FlowState) ([]*flow.Flow, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}

	var rows *sql.Rows
	var err error

	if state != nil {
		rows, err = s.db.Query(`SELECT data FROM flows WHERE state = ? ORDER BY created_at DESC`, *state)
	} else {
		rows, err = s.db.Query(`SELECT data FROM flows ORDER BY created_at DESC`)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to list flows: %w", err)
	}
	defer rows.Close()

	var flows []*flow.Flow
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("failed to scan flow: %w", err)
		}

		var f flow.Flow
		if err := json.Unmarshal([]byte(data), &f); err != nil {
			return nil, fmt.Errorf("failed to unmarshal flow: %w", err)
		}

		flows = append(flows, &f)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating flows: %w", err)
	}

	return flows, nil
}

// ListFlowsPaginated returns flows with pagination support.
func (s *SQLiteStore) ListFlowsPaginated(offset, limit int) ([]*flow.Flow, int, error) {
	if s.isClosed() {
		return nil, 0, ErrDatabaseClosed
	}

	// Get total count
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM flows`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count flows: %w", err)
	}

	// Get paginated results
	rows, err := s.db.Query(`
		SELECT data FROM flows
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list flows: %w", err)
	}
	defer rows.Close()

	var flows []*flow.Flow
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, 0, fmt.Errorf("failed to scan flow: %w", err)
		}

		var f flow.Flow
		if err := json.Unmarshal([]byte(data), &f); err != nil {
			return nil, 0, fmt.Errorf("failed to unmarshal flow: %w", err)
		}

		flows = append(flows, &f)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating flows: %w", err)
	}

	return flows, total, nil
}

// SearchFlows searches for flows by name pattern.
func (s *SQLiteStore) SearchFlows(namePattern string) ([]*flow.Flow, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}

	// Escape LIKE metacharacters so a user searching for "a_b" or "50%" matches
	// those literal characters instead of treating them as wildcards.
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(namePattern)

	rows, err := s.db.Query(`
		SELECT data FROM flows
		WHERE name LIKE ? ESCAPE '\'
		ORDER BY created_at DESC
	`, "%"+escaped+"%")
	if err != nil {
		return nil, fmt.Errorf("failed to search flows: %w", err)
	}
	defer rows.Close()

	var flows []*flow.Flow
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("failed to scan flow: %w", err)
		}

		var f flow.Flow
		if err := json.Unmarshal([]byte(data), &f); err != nil {
			return nil, fmt.Errorf("failed to unmarshal flow: %w", err)
		}

		flows = append(flows, &f)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating flows: %w", err)
	}

	return flows, nil
}

// GetRunningFlows returns all flows with state='running'.
// This is used to restore flows after server restart.
func (s *SQLiteStore) GetRunningFlows() ([]*flow.Flow, error) {
	state := flow.FlowStateRunning
	return s.ListFlows(&state)
}

// UpdateFlowState updates only the state of a flow.
func (s *SQLiteStore) UpdateFlowState(id string, state flow.FlowState) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}

	if id == "" {
		return fmt.Errorf("%w: empty flow ID", ErrInvalidFlowData)
	}

	// Update the state column AND the embedded state in the JSON blob in a single
	// atomic statement. The previous read-modify-write (GetFlow + UpdateFlow)
	// re-serialised the whole flow and could lose a concurrent edit; json_set
	// touches only the state field, so nodes/connections are never clobbered.
	result, err := s.db.Exec(`
		UPDATE flows
		SET state = ?, data = json_set(data, '$.state', ?), updated_at = ?
		WHERE id = ?
	`, state, state, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update flow state: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrFlowNotFound
	}

	return nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}

// IsClosed returns whether the store is closed.
func (s *SQLiteStore) IsClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

// isClosed is an internal method to check if the store is closed.
// It assumes the caller holds at least a read lock, or will acquire one.
func (s *SQLiteStore) isClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

// isUniqueViolation checks if the error is a unique/primary-key constraint
// violation, using the typed sqlite3 error code rather than fragile string
// matching (with a string fallback for safety).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var se sqlite3.Error
	if errors.As(err, &se) {
		return se.ExtendedCode == sqlite3.ErrConstraintUnique ||
			se.ExtendedCode == sqlite3.ErrConstraintPrimaryKey
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
