package builtin

import (
	"context"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/mqtttest"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/Pegasus8/piworker/internal/zigbee"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Expressions read a device's last reported state with zigbee(device) or
// zigbee(connection, device); a sandbox (dry-run, node test) reads nil.
func TestExpressionsReadZigbeeState(t *testing.T) {
	b := mqtttest.New(t)
	store := brokers.NewStore()
	require.NoError(t, store.Set(brokers.Connection{Name: "home", Broker: b.URL()}))
	m := zigbee.NewManager(store)
	m.Start()
	t.Cleanup(m.Stop)
	prev := zigbee.Default
	zigbee.Default = m
	t.Cleanup(func() { zigbee.Default = prev })
	require.Eventually(t, func() bool { n, _ := m.Network("home"); return n.Connected }, 5*time.Second, 10*time.Millisecond)
	b.Route("zigbee2mqtt/bedroom_climate", `{"humidity":72,"temperature":21}`)
	require.Eventually(t, func() bool { _, ok := m.DeviceState("home", "bedroom_climate"); return ok }, 5*time.Second, 10*time.Millisecond)

	eval := func(ctx context.Context, src string) interface{} {
		p, err := compileExpr(src)
		require.NoError(t, err)
		v, err := evalExpr(ctx, p, types.NewMessage(nil, types.DataTypeAny))
		require.NoError(t, err, src)
		return v
	}
	ctx := context.Background()
	assert.Equal(t, true, eval(ctx, `zigbee("bedroom_climate").humidity > 70`))
	assert.Equal(t, 21.0, eval(ctx, `zigbee("home", "bedroom_climate").temperature`))
	assert.Nil(t, eval(ctx, `zigbee("nobody")`))
	assert.Nil(t, eval(ctx, `zigbee("other", "bedroom_climate")`))
	assert.Nil(t, eval(ctx, `zigbee()`))

	sandboxed := node.WithSandbox(ctx, node.NewSandbox(1<<20, 1<<20))
	assert.Nil(t, eval(sandboxed, `zigbee("bedroom_climate")`), "a sandbox never reads the house")
}
