package builtin

import (
	"context"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/mqtttest"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/require"
)

// zigbeeEnv is a fake broker and a context whose broker store holds the
// connection "home" to it (base topic zigbee2mqtt).
func zigbeeEnv(t *testing.T) (*mqtttest.Broker, context.Context) {
	t.Helper()
	b := mqtttest.New(t)
	store := brokers.NewStore()
	require.NoError(t, store.Set(brokers.Connection{Name: "home", Broker: b.URL()}))
	return b, brokers.WithStore(context.Background(), store)
}

func startZigbeeTrigger(t *testing.T, ctx context.Context, cfg map[string]interface{}) chan *types.Message {
	t.Helper()
	n, err := NewZigbeeTrigger(cfg)
	require.NoError(t, err)
	tr := n.(*ZigbeeTrigger)
	out := make(chan *types.Message, 16)
	ctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	require.NoError(t, tr.Start(ctx, out))
	t.Cleanup(func() { _ = tr.Stop() })
	return out
}

func nextMsg(t *testing.T, out chan *types.Message) *types.Message {
	t.Helper()
	select {
	case m := <-out:
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("no message")
		return nil
	}
}

func noMsg(t *testing.T, out chan *types.Message) {
	t.Helper()
	select {
	case m := <-out:
		t.Fatalf("unexpected message %v", m.Payload)
	case <-time.After(300 * time.Millisecond):
	}
}
