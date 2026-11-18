package builtin

import (
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Get asks the device for a fresh reading and passes on the state it
// reports next.
func TestZigbeeGetAsksAndWaits(t *testing.T) {
	b, ctx := zigbeeEnv(t)
	n, err := NewZigbeeGet(map[string]interface{}{"connection": "home", "device": "living/plug", "property": "power"})
	require.NoError(t, err)
	g := n.(*ZigbeeGet)
	t.Cleanup(func() { _ = g.Close() })

	// The device answers each /get with its state.
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		answered := 0
		for {
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Millisecond):
			}
			_, _, pubs, _ := b.Snapshot()
			gets := 0
			for _, p := range pubs {
				if strings.HasPrefix(p, "zigbee2mqtt/living/plug/get=") {
					gets++
				}
			}
			for ; answered < gets; answered++ {
				b.Route("zigbee2mqtt/living/plug", `{"state":"ON","power":1834.5}`)
			}
		}
	}()

	out, err := g.Process(ctx, types.NewMessage("x", types.DataTypeString))
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, map[string]interface{}{"device": "living/plug", "property": "power", "value": 1834.5,
		"state": map[string]interface{}{"state": "ON", "power": 1834.5}}, out[0].Payload)
	_, _, pubs, _ := b.Snapshot()
	assert.Contains(t, pubs, `zigbee2mqtt/living/plug/get={"power":""}`)

	_, err = g.Process(ctx, types.NewMessage("again", types.DataTypeString))
	require.NoError(t, err, "a second request on the same subscription")
}

// A device that does not answer (a sleeping battery sensor) fails the
// message after the timeout, without retries.
func TestZigbeeGetTimeout(t *testing.T) {
	_, ctx := zigbeeEnv(t)
	n, err := NewZigbeeGet(map[string]interface{}{"connection": "home", "device": "bedroom_climate", "property": "temperature", "timeout": float64(1)})
	require.NoError(t, err)
	g := n.(*ZigbeeGet)
	t.Cleanup(func() { _ = g.Close() })
	start := time.Now()
	_, err = g.Process(ctx, types.NewMessage(nil, types.DataTypeAny))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no answer")
	assert.False(t, node.IsRetryable(err))
	assert.Less(t, time.Since(start), 3*time.Second)
}

func TestZigbeeGetConfig(t *testing.T) {
	for _, cfg := range []map[string]interface{}{
		{"device": "plug"},
		{"connection": "home", "device": "bridge"},
		{"connection": "home", "device": "plug", "property": "a b"},
		{"connection": "home", "device": "plug", "timeout": float64(0)},
		{"connection": "home", "device": "plug", "timeout": float64(31)},
	} {
		_, err := NewZigbeeGet(cfg)
		assert.Error(t, err, "%v", cfg)
	}
	n, err := NewZigbeeGet(map[string]interface{}{"connection": "home", "device": "plug"})
	require.NoError(t, err)
	assert.Equal(t, "state", n.(*ZigbeeGet).property)
}
