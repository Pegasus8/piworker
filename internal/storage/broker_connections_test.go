package storage

import (
	"path/filepath"
	"testing"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrokerConnectionsPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conns.db")
	store, err := NewSQLiteStore(path)
	require.NoError(t, err)

	loaded, err := store.LoadConnections()
	require.NoError(t, err)
	assert.Empty(t, loaded)

	home := brokers.Connection{Name: "home", Broker: "tcp://10.0.0.2:1883", Username: "pw", Password: "{{secret.mqtt}}", BaseTopic: "zigbee2mqtt"}
	require.NoError(t, store.SaveConnection(home))
	home.Broker = "tcp://10.0.0.3:1883"
	require.NoError(t, store.SaveConnection(home), "upsert")
	require.NoError(t, store.SaveConnection(brokers.Connection{Name: "lab", Broker: "mqtt://lab", BaseTopic: "z2m"}))
	require.NoError(t, store.Close())

	// The brokers.Store attaches across a restart.
	store, err = NewSQLiteStore(path)
	require.NoError(t, err)
	defer store.Close()
	s := brokers.NewStore()
	require.NoError(t, s.Attach(store))
	got, ok := s.Get("home")
	require.True(t, ok)
	assert.Equal(t, home, got)
	assert.Equal(t, []string{"home", "lab"}, s.Names())

	require.NoError(t, store.DeleteConnection("home"))
	loaded, err = store.LoadConnections()
	require.NoError(t, err)
	assert.Len(t, loaded, 1)
}
