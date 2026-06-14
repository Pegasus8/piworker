package builtin

import (
	"context"
	"testing"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newJoin(t *testing.T, config map[string]interface{}) *JoinProcess {
	t.Helper()
	n, err := NewJoinProcess(config)
	require.NoError(t, err)
	return n.(*JoinProcess)
}

func joinSend(t *testing.T, j *JoinProcess, payload interface{}, sourceNode string) []*types.Message {
	t.Helper()
	msg := types.NewMessage(payload, types.DataTypeObject)
	msg.SourceNode = sourceNode
	out, err := j.Process(context.Background(), msg)
	require.NoError(t, err)
	return out
}

func TestJoinByKeyEmitsAtCount(t *testing.T) {
	j := newJoin(t, map[string]interface{}{"mode": "key", "key": "payload.id", "count": 2})

	assert.Empty(t, joinSend(t, j, map[string]interface{}{"id": "A", "v": 1}, ""), "first of key A")
	out := joinSend(t, j, map[string]interface{}{"id": "A", "v": 2}, "")
	require.Len(t, out, 1, "emits when the key reaches count")
	arr, ok := out[0].Payload.([]interface{})
	require.True(t, ok)
	assert.Len(t, arr, 2)
	assert.Equal(t, types.DataTypeArray, out[0].PayloadType)
	assert.Equal(t, "output", out[0].SourcePort)
}

func TestJoinByKeyDoesNotMixKeys(t *testing.T) {
	j := newJoin(t, map[string]interface{}{"mode": "key", "key": "payload.id", "count": 2})
	assert.Empty(t, joinSend(t, j, map[string]interface{}{"id": "A"}, ""))
	assert.Empty(t, joinSend(t, j, map[string]interface{}{"id": "B"}, ""), "different key, no emit")
	out := joinSend(t, j, map[string]interface{}{"id": "A"}, "")
	require.Len(t, out, 1, "second A completes the A batch")
}

func TestJoinBySourceEmitsWhenAllPresent(t *testing.T) {
	j := newJoin(t, map[string]interface{}{"mode": "source", "key": "payload.corr", "sources": "nodeA,nodeB"})

	assert.Empty(t, joinSend(t, j, map[string]interface{}{"corr": "x", "t": 20}, "nodeA"), "only A so far")
	out := joinSend(t, j, map[string]interface{}{"corr": "x", "h": 60}, "nodeB")
	require.Len(t, out, 1, "both sources present → emit")
	obj, ok := out[0].Payload.(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, obj, "nodeA")
	assert.Contains(t, obj, "nodeB")
	assert.Equal(t, types.DataTypeObject, out[0].PayloadType)
}

func TestJoinBySourceIgnoresUnlistedSource(t *testing.T) {
	j := newJoin(t, map[string]interface{}{"mode": "source", "key": "payload.corr", "sources": "nodeA,nodeB"})
	joinSend(t, j, map[string]interface{}{"corr": "x"}, "nodeA")
	assert.Empty(t, joinSend(t, j, map[string]interface{}{"corr": "x"}, "nodeC"), "nodeC not in sources")
	out := joinSend(t, j, map[string]interface{}{"corr": "x"}, "nodeB")
	require.Len(t, out, 1)
}

func TestJoinInvalidConfig(t *testing.T) {
	_, err := NewJoinProcess(map[string]interface{}{"mode": "weird", "key": "payload.id"})
	assert.Error(t, err)
	_, err = NewJoinProcess(map[string]interface{}{"mode": "source", "key": "payload.id"})
	assert.Error(t, err, "source mode needs sources")
}
