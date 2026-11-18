package builtin

import (
	"context"
	"database/sql/driver"
	"fmt"
	"net"
	"path/filepath"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Syntax errors and constraint violations can never succeed on retry, and a
// retried INSERT after an ambiguous failure can duplicate rows. Only failures
// that happen before the statement runs are transient.
func TestDatabaseStatementErrorsArePermanent(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "t.db")
	ctx := context.Background()
	msg := types.NewMessage(nil, types.DataTypeAny)

	_, err := newDB(t, map[string]interface{}{
		"driver": "sqlite", "dsn": dsn, "mode": "exec",
		"query": "CREATE TABLE items (id INTEGER PRIMARY KEY)",
	}).Process(ctx, msg)
	require.NoError(t, err)

	insert := newDB(t, map[string]interface{}{
		"driver": "sqlite", "dsn": dsn, "mode": "exec", "query": "INSERT INTO items (id) VALUES (1)",
	})
	_, err = insert.Process(ctx, msg)
	require.NoError(t, err)
	_, err = insert.Process(ctx, msg)
	require.Error(t, err)
	assert.False(t, node.IsRetryable(err), "constraint violation must not be retried: %v", err)

	_, err = newDB(t, map[string]interface{}{
		"driver": "sqlite", "dsn": dsn, "mode": "exec", "query": "INSRT INTO items VALUES (2)",
	}).Process(ctx, msg)
	require.Error(t, err)
	assert.False(t, node.IsRetryable(err), "syntax error must not be retried: %v", err)

	_, err = newDB(t, map[string]interface{}{
		"driver": "sqlite", "dsn": dsn, "mode": "query", "query": "SELECT nope FROM items",
	}).Process(ctx, msg)
	require.Error(t, err)
	assert.False(t, node.IsRetryable(err), "query error must not be retried: %v", err)
}

func TestDatabaseConnectionRefusedIsTransient(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	a := newDB(t, map[string]interface{}{
		"driver": "postgres", "mode": "exec", "query": "INSERT INTO t VALUES (1)",
		"dsn": fmt.Sprintf("postgres://u:p@%s/db?sslmode=disable&connect_timeout=2", addr),
	})
	_, err = a.Process(context.Background(), types.NewMessage(nil, types.DataTypeAny))
	require.Error(t, err)
	assert.True(t, node.IsRetryable(err), "dial failure happens before execution: %v", err)
}

func TestClassifyDBError(t *testing.T) {
	assert.True(t, node.IsRetryable(classifyDBError(fmt.Errorf("x: %w", driver.ErrBadConn), true)))
	assert.True(t, node.IsRetryable(classifyDBError(sqlite3.Error{Code: sqlite3.ErrBusy}, true)))
	assert.True(t, node.IsRetryable(classifyDBError(sqlite3.Error{Code: sqlite3.ErrLocked}, true)))
	assert.False(t, node.IsRetryable(classifyDBError(sqlite3.Error{Code: sqlite3.ErrConstraint}, true)))
	assert.False(t, node.IsRetryable(classifyDBError(context.DeadlineExceeded, true)),
		"a timeout may have hit mid-statement")

	readTimeout := &net.OpError{Op: "read", Net: "tcp", Err: fmt.Errorf("i/o timeout")}
	assert.False(t, node.IsRetryable(classifyDBError(readTimeout, true)), "exec: response lost, may have run")
	assert.True(t, node.IsRetryable(classifyDBError(readTimeout, false)), "query: read-only, safe to retry")
}
