package builtin

import (
	"context"
	"testing"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newParse(t *testing.T, config map[string]interface{}) *ParseProcess {
	t.Helper()
	n, err := NewParseProcess(config)
	require.NoError(t, err)
	return n.(*ParseProcess)
}

func TestParseJSONObject(t *testing.T) {
	p := newParse(t, map[string]interface{}{"format": "json"})
	out, err := p.Process(context.Background(), types.NewMessage(`{"a":1,"b":"x"}`, types.DataTypeString))
	require.NoError(t, err)
	require.Len(t, out, 1)
	obj, ok := out[0].Payload.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(1), obj["a"])
	assert.Equal(t, "x", obj["b"])
}

func TestParseJSONArray(t *testing.T) {
	p := newParse(t, map[string]interface{}{"format": "json"})
	out, err := p.Process(context.Background(), types.NewMessage(`[1,2,3]`, types.DataTypeString))
	require.NoError(t, err)
	arr, ok := out[0].Payload.([]interface{})
	require.True(t, ok)
	assert.Len(t, arr, 3)
}

func TestParseJSONInvalid(t *testing.T) {
	p := newParse(t, map[string]interface{}{"format": "json"})
	_, err := p.Process(context.Background(), types.NewMessage(`{not json`, types.DataTypeString))
	assert.Error(t, err)
}

func TestParseCSVWithHeader(t *testing.T) {
	p := newParse(t, map[string]interface{}{"format": "csv"})
	out, err := p.Process(context.Background(), types.NewMessage("name,age\nbob,3\nalice,7", types.DataTypeString))
	require.NoError(t, err)
	rows, ok := out[0].Payload.([]map[string]interface{})
	require.True(t, ok)
	require.Len(t, rows, 2)
	assert.Equal(t, "bob", rows[0]["name"])
	assert.Equal(t, "3", rows[0]["age"])
	assert.Equal(t, "alice", rows[1]["name"])
}

func TestParseCSVNoHeader(t *testing.T) {
	p := newParse(t, map[string]interface{}{"format": "csv", "header": false})
	out, err := p.Process(context.Background(), types.NewMessage("a,b\nc,d", types.DataTypeString))
	require.NoError(t, err)
	rows, ok := out[0].Payload.([][]string)
	require.True(t, ok)
	assert.Equal(t, [][]string{{"a", "b"}, {"c", "d"}}, rows)
}

func TestParseNonStringPayloadErrors(t *testing.T) {
	p := newParse(t, map[string]interface{}{"format": "json"})
	_, err := p.Process(context.Background(), types.NewMessage(map[string]interface{}{"already": "object"}, types.DataTypeObject))
	assert.Error(t, err)
}

func TestParseSetsSourcePort(t *testing.T) {
	p := newParse(t, map[string]interface{}{"format": "json"})
	out, err := p.Process(context.Background(), types.NewMessage(`{}`, types.DataTypeString))
	require.NoError(t, err)
	assert.Equal(t, "output", out[0].SourcePort)
}
