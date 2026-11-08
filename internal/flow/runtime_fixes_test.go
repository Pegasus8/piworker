package flow

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNegativeMaxRetriesStillExecutesOnce guards against a retry config with a
// negative MaxRetries skipping execution entirely and reporting (nil, nil).
func TestNegativeMaxRetriesStillExecutesOnce(t *testing.T) {
	rt, cn := buildRetryFlow(t, nil)
	WithRetryConfig(RetryConfig{MaxRetries: -1, InitialWait: time.Millisecond, MaxWait: time.Millisecond, Multiplier: 2})(rt)
	require.NoError(t, rt.Start(context.Background()))
	defer func() { _ = rt.Stop() }()

	require.Eventually(t, func() bool { return atomic.LoadInt32(&cn.calls) == 1 },
		time.Second, 5*time.Millisecond, "a negative MaxRetries must still run the node once")
}

func TestNegativeMaxRetriesReturnsError(t *testing.T) {
	rt, cn := buildRetryFlow(t, errors.New("boom"))
	rt.ctx, rt.cancel = context.WithCancel(context.Background())
	defer rt.cancel()
	rt.retryConfig.MaxRetries = -5

	def := rt.nodeDefs[findNodeIDByType(t, rt, "counting")]
	_, err := rt.executeNodeWithRetry(cn, RoutedMessage{Message: NewMessage("x", DataTypeString), TargetNode: def.ID}, def, zerolog.Nop())
	assert.EqualError(t, err, "boom")
	assert.Equal(t, int32(1), atomic.LoadInt32(&cn.calls))
}

func findNodeIDByType(t *testing.T, rt *FlowRuntime, typ string) string {
	t.Helper()
	for id, def := range rt.nodeDefs {
		if def.Type == typ {
			return id
		}
	}
	t.Fatalf("no node of type %s", typ)
	return ""
}
