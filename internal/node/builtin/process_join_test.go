package builtin

import (
	"context"
	"testing"
	"time"

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

func TestJoinExpiresStaleGroups(t *testing.T) {
	j := newJoin(t, map[string]interface{}{"mode": "key", "key": "payload.id", "count": 2})
	now := time.Unix(1000, 0)
	j.now = func() time.Time { return now }

	assert.Nil(t, joinSend(t, j, map[string]interface{}{"id": "a", "v": 1}, "n1"))
	now = now.Add(5*time.Minute + time.Second) // default TTL is 5 minutes
	assert.Nil(t, joinSend(t, j, map[string]interface{}{"id": "a", "v": 2}, "n1"),
		"a partial group older than the timeout must not be completed")
	assert.Equal(t, 1, j.pending())

	assert.Nil(t, joinSend(t, j, map[string]interface{}{"id": "b"}, "n1"))
	now = now.Add(time.Hour)
	assert.Nil(t, joinSend(t, j, map[string]interface{}{"id": "c"}, "n1"))
	assert.Equal(t, 1, j.pending(), "expired groups are swept even if their key never recurs")
}

func TestJoinCapsPendingKeys(t *testing.T) {
	j := newJoin(t, map[string]interface{}{"mode": "source", "key": "payload.id", "sources": "s1,s2", "maxKeys": 3})
	now := time.Unix(1000, 0)
	j.now = func() time.Time { now = now.Add(time.Millisecond); return now }

	for _, id := range []string{"k1", "k2", "k3", "k4", "k5"} {
		assert.Nil(t, joinSend(t, j, map[string]interface{}{"id": id}, "s1"))
	}
	assert.Equal(t, 3, j.pending())
	// The oldest groups were evicted; the newest still completes.
	assert.Nil(t, joinSend(t, j, map[string]interface{}{"id": "k1"}, "s2"))
	assert.Len(t, joinSend(t, j, map[string]interface{}{"id": "k5"}, "s2"), 1)
}

func TestJoinDefaultBoundsAndSchema(t *testing.T) {
	j := newJoin(t, map[string]interface{}{"mode": "key"})
	assert.Equal(t, 5*time.Minute, j.timeout)
	assert.Equal(t, 1000, j.maxKeys)
	schema := j.GetConfigSchema()
	assert.Contains(t, schema.Properties, "timeoutMs")
	assert.Contains(t, schema.Properties, "maxKeys")
}
