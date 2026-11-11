package brokers

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/mqtttest"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Endpoint resolves the credentials' secret references through the context
// and carries the context's sandbox.
func TestEndpointResolvesSecrets(t *testing.T) {
	store := secrets.NewStore()
	require.NoError(t, store.Set("mqtt_pw", "pa55"))
	ctx := secrets.WithStore(context.Background(), store)
	c := Connection{Name: "home", Broker: "tcp://x:1883", Username: "pw", Password: "{{secret.mqtt_pw}}", BaseTopic: "z2m"}
	ep := c.Endpoint(ctx)
	assert.Equal(t, "tcp://x:1883", ep.Broker)
	assert.Equal(t, "pw", ep.Username)
	assert.Equal(t, "pa55", ep.Password)
	assert.Empty(t, ep.ClientID, "a pooled connection")
	assert.Nil(t, ep.Sandbox)

	sb := node.NewSandbox(1, 1)
	assert.Same(t, sb, c.Endpoint(node.WithSandbox(ctx, sb)).Sandbox)
}

func TestTesterConnects(t *testing.T) {
	b := mqtttest.New(t)
	require.NoError(t, Tester{}.TestConnection(context.Background(), Connection{Name: "a", Broker: b.URL()}))
	require.Eventually(t, func() bool { return b.ConnCount() == 0 }, 5*time.Second, 20*time.Millisecond, "disconnects after")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	assert.Error(t, Tester{}.TestConnection(ctx, Connection{Name: "a", Broker: "tcp://" + addr}))
}
