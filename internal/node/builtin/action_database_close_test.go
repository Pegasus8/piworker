package builtin

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/require"
)

func dbPoolSize() int {
	dbPoolMu.Lock()
	defer dbPoolMu.Unlock()
	return len(dbPool)
}

// The shared *sql.DB for a DSN is closed once the last node using it closes,
// so stopping flows doesn't leave connections (and file handles) open.
func TestDatabasePoolClosedWhenLastNodeCloses(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "x.db")
	before := dbPoolSize()
	newNode := func() *DatabaseAction {
		n, err := NewDatabaseAction(map[string]interface{}{"driver": "sqlite", "dsn": dsn, "query": "SELECT 1"})
		require.NoError(t, err)
		return n.(*DatabaseAction)
	}
	a, b := newNode(), newNode()
	var _ node.Closer = a

	for _, n := range []*DatabaseAction{a, b, a} {
		_, err := n.Process(context.Background(), types.NewMessage("x", types.DataTypeString))
		require.NoError(t, err)
	}
	require.Equal(t, before+1, dbPoolSize())

	require.NoError(t, a.Close())
	require.NoError(t, a.Close()) // idempotent
	require.Equal(t, before+1, dbPoolSize(), "b still uses the pool")
	_, err := b.Process(context.Background(), types.NewMessage("x", types.DataTypeString))
	require.NoError(t, err)

	require.NoError(t, b.Close())
	require.Equal(t, before, dbPoolSize())

	// A closed node can be used again (e.g. if the runtime reuses it) by reopening.
	_, err = a.Process(context.Background(), types.NewMessage("x", types.DataTypeString))
	require.NoError(t, err)
	require.NoError(t, a.Close())
	require.Equal(t, before, dbPoolSize())
}
