package flow

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A node definition written by hand, by an older editor or by an agent may omit
// "enabled". Absent must mean enabled: decoding it as false would silently drop
// the node (and, for a trigger, fail the deploy with "no trigger nodes").
func TestNodeEnabledDefaultsToTrueWhenAbsent(t *testing.T) {
	var n Node
	require.NoError(t, json.Unmarshal([]byte(`{"id":"a","type":"trigger-manual"}`), &n))
	assert.True(t, n.Enabled)
	assert.Equal(t, "a", n.ID)
	assert.Equal(t, "trigger-manual", n.Type)
}

func TestNodeEnabledNullKeepsDefault(t *testing.T) {
	var n Node
	require.NoError(t, json.Unmarshal([]byte(`{"id":"a","enabled":null}`), &n))
	assert.True(t, n.Enabled)
}

func TestNodeEnabledExplicitValuesAreKept(t *testing.T) {
	var off, on Node
	require.NoError(t, json.Unmarshal([]byte(`{"id":"a","enabled":false}`), &off))
	require.NoError(t, json.Unmarshal([]byte(`{"id":"b","enabled":true}`), &on))
	assert.False(t, off.Enabled)
	assert.True(t, on.Enabled)
}

func TestFlowFromJSONDefaultsNodesToEnabled(t *testing.T) {
	f, err := FromJSON([]byte(`{"id":"f","nodes":[{"id":"a","config":{"x":1}},{"id":"b","enabled":false}]}`))
	require.NoError(t, err)
	require.Len(t, f.Nodes, 2)
	assert.True(t, f.Nodes[0].Enabled)
	assert.Equal(t, map[string]interface{}{"x": float64(1)}, f.Nodes[0].Config)
	assert.False(t, f.Nodes[1].Enabled)
}

func TestNodeDecodeRejectsWrongEnabledType(t *testing.T) {
	var n Node
	require.Error(t, json.Unmarshal([]byte(`{"id":"a","enabled":"yes"}`), &n))
}

// Encoding is unchanged: "enabled" is always written.
func TestNodeEnabledRoundTrip(t *testing.T) {
	data, err := json.Marshal(Node{ID: "a"})
	require.NoError(t, err)
	assert.Contains(t, string(data), `"enabled":false`)
	var back Node
	require.NoError(t, json.Unmarshal(data, &back))
	assert.False(t, back.Enabled)
}
