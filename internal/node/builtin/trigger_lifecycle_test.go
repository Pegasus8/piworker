package builtin

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Huge interval values used to overflow time.Duration into a negative value,
// and time.NewTicker panics on a non-positive duration, inside the trigger's
// goroutine, before its recover was installed: the whole process crashed.
func TestPollingTriggersSurviveExtremeIntervals(t *testing.T) {
	huge := float64(math.MaxInt64) // JSON numbers arrive as float64
	cases := []struct {
		name   string
		new    func(map[string]interface{}) (node.Node, error)
		config map[string]interface{}
		get    func(node.Node) time.Duration
	}{
		{"interval", NewIntervalTrigger, map[string]interface{}{}, func(n node.Node) time.Duration { return n.(*IntervalTrigger).interval }},
		{"sysmetric", NewSysMetricTrigger, map[string]interface{}{}, func(n node.Node) time.Duration { return n.(*SysMetricTrigger).interval }},
		{"rss", NewRSSTrigger, map[string]interface{}{"url": "http://example.com/feed"}, func(n node.Node) time.Duration { return n.(*RSSTrigger).interval }},
		{"ds18b20", NewDS18B20Trigger, map[string]interface{}{"deviceId": "28-test"}, func(n node.Node) time.Duration { return n.(*DS18B20Trigger).interval }},
	}
	keys := map[string]string{"interval": "interval", "sysmetric": "interval", "rss": "intervalSec", "ds18b20": "intervalSec"}
	for _, tc := range cases {
		for _, v := range []interface{}{huge, 1e13, int(9_300_000_000_000), -5, 0} {
			cfg := map[string]interface{}{keys[tc.name]: v}
			for k, val := range tc.config {
				cfg[k] = val
			}
			n, err := tc.new(cfg)
			require.NoError(t, err, "%s %v", tc.name, v)
			d := tc.get(n)
			assert.Greater(t, d, time.Duration(0), "%s with %v", tc.name, v)
			assert.LessOrEqual(t, d, maxTriggerInterval, "%s with %v", tc.name, v)

			trig := n.(node.TriggerNode)
			out := make(chan *types.Message, 1)
			require.NoError(t, trig.Start(context.Background(), out), "%s with %v", tc.name, v)
			require.NoError(t, trig.Stop())
		}
	}
}

// Concurrent Start/Stop used to race on t.stopCh (the goroutine read the
// field while a new Start replaced it) and on WaitGroup reuse. Run with -race.
func TestPollingTriggersConcurrentStartStop(t *testing.T) {
	makers := map[string]func() node.TriggerNode{
		"interval": func() node.TriggerNode {
			n, _ := NewIntervalTrigger(map[string]interface{}{"interval": 100})
			return n.(node.TriggerNode)
		},
		"sysmetric": func() node.TriggerNode {
			n, _ := NewSysMetricTrigger(map[string]interface{}{"interval": 1})
			return n.(node.TriggerNode)
		},
		"rss": func() node.TriggerNode {
			n, _ := NewRSSTrigger(map[string]interface{}{"url": "http://127.0.0.1:1/feed"})
			return n.(node.TriggerNode)
		},
		"ds18b20": func() node.TriggerNode {
			n, _ := NewDS18B20Trigger(map[string]interface{}{"deviceId": "28-test", "intervalSec": 1})
			return n.(node.TriggerNode)
		},
	}
	for name, mk := range makers {
		t.Run(name, func(t *testing.T) {
			trig := mk()
			out := make(chan *types.Message, 8)
			var wg sync.WaitGroup
			for i := 0; i < 4; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < 25; j++ {
						_ = trig.Start(context.Background(), out)
						_ = trig.Stop()
					}
				}()
			}
			done := make(chan struct{})
			go func() { wg.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(20 * time.Second):
				t.Fatal("Start/Stop deadlocked")
			}
			require.NoError(t, trig.Stop())
		})
	}
}

// The 1-Wire driver enumerates sensors asynchronously after boot: an absent
// sensor must not fail Start (which would fail the deploy); it is looked for
// again on every poll.
func TestDS18B20StartWithoutDeviceDoesNotFail(t *testing.T) {
	n, err := NewDS18B20Trigger(map[string]interface{}{"intervalSec": 1})
	require.NoError(t, err)
	trig := n.(node.TriggerNode)
	require.NoError(t, trig.Start(context.Background(), make(chan *types.Message, 1)))
	require.NoError(t, trig.Stop())
}
