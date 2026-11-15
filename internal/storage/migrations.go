package storage

import (
	"database/sql"
	"errors"
	"fmt"
)

// SchemaVersion is the schema this binary reads and writes: the number of
// migrations, stored in the database as PRAGMA user_version (ADR-0035).
const SchemaVersion = 1

// ErrSchemaTooNew refuses a database written by a newer PiWorker.
var ErrSchemaTooNew = errors.New("the database was written by a newer PiWorker; update PiWorker to open it")

// migration is one schema step. up runs in a transaction that also records
// the new version, so a step applies completely or not at all.
type migration struct {
	name string
	up   func(tx *sql.Tx) error
}

// migrations are applied in order, each exactly once per database; their
// position (from 1) is the schema version they produce. Never edit or
// reorder one that was released: append a new one and bump SchemaVersion.
var migrations = []migration{
	{name: "baseline", up: migrateBaseline},
}

// migrate brings db to the last version of list. A database at a higher
// version is refused untouched (ErrSchemaTooNew).
func migrate(db *sql.DB, list []migration) error {
	var current int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&current); err != nil {
		return fmt.Errorf("failed to read the schema version: %w", err)
	}
	if current > len(list) {
		return fmt.Errorf("%w (schema %d, this version supports up to %d)", ErrSchemaTooNew, current, len(list))
	}
	for i := current; i < len(list); i++ {
		if err := applyMigration(db, list[i], i+1); err != nil {
			return fmt.Errorf("migration %d (%s): %w", i+1, list[i].name, err)
		}
	}
	return nil
}

func applyMigration(db *sql.DB, m migration, version int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := m.up(tx); err != nil {
		return err
	}
	// PRAGMA takes no parameters; version is ours, not input.
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, version)); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateBaseline is schema 1: every table as of the first versioned
// release. It is idempotent, because it also upgrades the databases written
// before versions existed (user_version 0), whatever shape they have: it
// creates what is missing, adds the columns later releases added and gives
// every flow its first stored version.
func migrateBaseline(tx *sql.Tx) error {
	for _, schema := range []string{
		coreSchema, brokerConnectionsSchema, usersSchema,
	} {
		if _, err := tx.Exec(schema); err != nil {
			return err
		}
	}
	if err := migrateHistory(tx); err != nil {
		return err
	}
	if err := migrateFlowVersions(tx); err != nil {
		return err
	}
	for _, schema := range []string{apiTokensSchema, agentApprovalsSchema, mcpAuditSchema, flowDraftsSchema} {
		if _, err := tx.Exec(schema); err != nil {
			return fmt.Errorf("failed to create agent tables: %w", err)
		}
	}
	return nil
}

// hasColumn reports whether table has the named column.
func hasColumn(tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, fmt.Errorf("failed to inspect %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, fmt.Errorf("failed to inspect %s: %w", table, err)
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
