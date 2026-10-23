package builtin

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDB(t *testing.T, config map[string]interface{}) *DatabaseAction {
	t.Helper()
	n, err := NewDatabaseAction(config)
	require.NoError(t, err)
	return n.(*DatabaseAction)
}

func TestDatabaseExecThenQuery(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "t.db")
	ctx := context.Background()
	msg := types.NewMessage(nil, types.DataTypeAny)

	create := newDB(t, map[string]interface{}{
		"driver": "sqlite", "dsn": dsn, "mode": "exec",
		"query": "CREATE TABLE items (id INTEGER, name TEXT)",
	})
	_, err := create.Process(ctx, msg)
	require.NoError(t, err)

	insert := newDB(t, map[string]interface{}{
		"driver": "sqlite", "dsn": dsn, "mode": "exec",
		"query": "INSERT INTO items (id, name) VALUES (?, ?)", "params": `[1, "bob"]`,
	})
	out, err := insert.Process(ctx, msg)
	require.NoError(t, err)
	require.Len(t, out, 1)
	res, ok := out[0].Payload.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, int64(1), res["rowsAffected"])

	query := newDB(t, map[string]interface{}{
		"driver": "sqlite", "dsn": dsn, "mode": "query",
		"query": "SELECT id, name FROM items ORDER BY id",
	})
	out, err = query.Process(ctx, msg)
	require.NoError(t, err)
	rows, ok := out[0].Payload.([]map[string]interface{})
	require.True(t, ok)
	require.Len(t, rows, 1)
	assert.Equal(t, "bob", rows[0]["name"])
	assert.Equal(t, "output", out[0].SourcePort)
}

func TestDatabaseQueryWithParams(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "t.db")
	ctx := context.Background()
	msg := types.NewMessage(map[string]interface{}{"min": 2}, types.DataTypeObject)

	setup := newDB(t, map[string]interface{}{"driver": "sqlite", "dsn": dsn, "mode": "exec",
		"query": "CREATE TABLE n (v INTEGER)"})
	_, err := setup.Process(ctx, msg)
	require.NoError(t, err)
	for _, v := range []string{"INSERT INTO n VALUES (1)", "INSERT INTO n VALUES (2)", "INSERT INTO n VALUES (3)"} {
		ins := newDB(t, map[string]interface{}{"driver": "sqlite", "dsn": dsn, "mode": "exec", "query": v})
		_, err := ins.Process(ctx, msg)
		require.NoError(t, err)
	}

	q := newDB(t, map[string]interface{}{"driver": "sqlite", "dsn": dsn, "mode": "query",
		"query": "SELECT v FROM n WHERE v >= ? ORDER BY v", "params": "[payload.min]"})
	out, err := q.Process(ctx, msg)
	require.NoError(t, err)
	rows := out[0].Payload.([]map[string]interface{})
	assert.Len(t, rows, 2) // v >= 2 → {2, 3}
}

func TestDatabaseInvalidDriver(t *testing.T) {
	_, err := NewDatabaseAction(map[string]interface{}{"driver": "oracle", "dsn": "x", "query": "SELECT 1"})
	assert.Error(t, err)
}

func TestDatabaseMissingQuery(t *testing.T) {
	_, err := NewDatabaseAction(map[string]interface{}{"driver": "sqlite", "dsn": "x"})
	assert.Error(t, err)
}
