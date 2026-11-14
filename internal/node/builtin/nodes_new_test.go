package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func msgWith(payload interface{}) *types.Message {
	return types.NewMessage(payload, types.DataTypeObject)
}

func TestFilterProcess(t *testing.T) {
	ctx := context.Background()
	n, err := NewFilterProcess(map[string]interface{}{"condition": "payload.value > 10"})
	require.NoError(t, err)

	out, err := n.Process(ctx, msgWith(map[string]interface{}{"value": 20}))
	require.NoError(t, err)
	assert.Len(t, out, 1, "value > 10 should pass")

	out, err = n.Process(ctx, msgWith(map[string]interface{}{"value": 5}))
	require.NoError(t, err)
	assert.Len(t, out, 0, "value <= 10 should be dropped")
}

func TestSwitchProcess(t *testing.T) {
	ctx := context.Background()
	n, err := NewSwitchProcess(map[string]interface{}{"condition": "payload.ok"})
	require.NoError(t, err)

	out, err := n.Process(ctx, msgWith(map[string]interface{}{"ok": true}))
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "true", out[0].SourcePort)

	out, err = n.Process(ctx, msgWith(map[string]interface{}{"ok": false}))
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "false", out[0].SourcePort)
}

func TestTemplateProcess(t *testing.T) {
	ctx := context.Background()
	n, err := NewTemplateProcess(map[string]interface{}{"template": "Hi {{payload.name}}!"})
	require.NoError(t, err)

	out, err := n.Process(ctx, msgWith(map[string]interface{}{"name": "Bob"}))
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "Hi Bob!", out[0].Payload)
}

func TestSetAndGetVar(t *testing.T) {
	ctx := context.Background()

	sn, err := NewSetVarProcess(map[string]interface{}{"key": "test_k1", "value": "payload.x"})
	require.NoError(t, err)
	_, err = sn.Process(ctx, msgWith(map[string]interface{}{"x": 42}))
	require.NoError(t, err)

	gn, err := NewGetVarProcess(map[string]interface{}{"key": "test_k1"})
	require.NoError(t, err)
	out, err := gn.Process(ctx, msgWith(nil))
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, 42, out[0].Payload)

	// Missing key with default.
	gd, err := NewGetVarProcess(map[string]interface{}{"key": "missing_k", "default": "fallback"})
	require.NoError(t, err)
	out, err = gd.Process(ctx, msgWith(nil))
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "fallback", out[0].Payload)
}

func TestFileActionAppend(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "out.log")

	n, err := NewFileAction(map[string]interface{}{
		"path":    path,
		"content": "line {{payload.n}}",
		"mode":    "append",
	})
	require.NoError(t, err)

	_, err = n.Process(ctx, msgWith(map[string]interface{}{"n": 1}))
	require.NoError(t, err)
	_, err = n.Process(ctx, msgWith(map[string]interface{}{"n": 2}))
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "line 1\nline 2\n", string(data))
}
