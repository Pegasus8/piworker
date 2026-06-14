package builtin

import (
	"context"
	"testing"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAggregate(t *testing.T, config map[string]interface{}) *AggregateProcess {
	t.Helper()
	n, err := NewAggregateProcess(config)
	require.NoError(t, err)
	return n.(*AggregateProcess)
}

func send(t *testing.T, p *AggregateProcess, payload interface{}) []*types.Message {
	t.Helper()
	out, err := p.Process(context.Background(), types.NewMessage(payload, types.DataTypeAny))
	require.NoError(t, err)
	return out
}

func TestAggregateEmitsEveryNMessages(t *testing.T) {
	p := newAggregate(t, map[string]interface{}{"count": 3})

	assert.Empty(t, send(t, p, "a"), "buffered, no emit")
	assert.Empty(t, send(t, p, "b"), "buffered, no emit")

	out := send(t, p, "c")
	require.Len(t, out, 1, "emits when the batch fills")
	batch, ok := out[0].Payload.([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{"a", "b", "c"}, batch)
	assert.Equal(t, types.DataTypeArray, out[0].PayloadType)
	assert.Equal(t, "output", out[0].SourcePort)
}

func TestAggregateStartsFreshBatchAfterEmit(t *testing.T) {
	p := newAggregate(t, map[string]interface{}{"count": 2})
	send(t, p, "a")
	out := send(t, p, "b")
	require.Len(t, out, 1)

	assert.Empty(t, send(t, p, "c"), "new batch begins empty")
	out = send(t, p, "d")
	require.Len(t, out, 1)
	assert.Equal(t, []interface{}{"c", "d"}, out[0].Payload)
}

func TestAggregateCountOneEmitsImmediately(t *testing.T) {
	p := newAggregate(t, map[string]interface{}{"count": 1})
	out := send(t, p, "x")
	require.Len(t, out, 1)
	assert.Equal(t, []interface{}{"x"}, out[0].Payload)
}

func TestAggregateDefaultCount(t *testing.T) {
	p := newAggregate(t, map[string]interface{}{})
	assert.Equal(t, 10, p.count)
}
