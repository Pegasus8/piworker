package builtin

import (
	"bytes"
	"context"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ node.ConfigurableNode = (*ReplyAction)(nil)

func TestReplyActionInfo(t *testing.T) {
	info, ok := node.DefaultRegistry.Get("action-reply")
	require.True(t, ok, "action-reply is registered")
	assert.Equal(t, types.NodeCategoryOutput, info.Category)
	assert.Equal(t, node.RiskSafe, info.Risk)
	require.Len(t, info.Inputs, 1)
	assert.Equal(t, "input", info.Inputs[0].ID)
	assert.Empty(t, info.Outputs)
	assert.NotEmpty(t, info.Documentation)

	require.NotNil(t, info.Config)
	mode := info.Config.Properties["mode"]
	assert.Equal(t, []string{"result", "error"}, mode.Enum)
	assert.Equal(t, "result", mode.Default)
	assert.True(t, mode.AgentVisible)
}

func TestReplyActionMode(t *testing.T) {
	for _, cfg := range []map[string]any{nil, {}, {"mode": ""}, {"mode": "result"}} {
		n, err := NewReplyAction(cfg)
		require.NoError(t, err, "%v", cfg)
		require.NoError(t, n.Validate())
		assert.False(t, n.(*ReplyAction).isError, "%v", cfg)
	}
	n, err := NewReplyAction(map[string]any{"mode": "error"})
	require.NoError(t, err)
	assert.True(t, n.(*ReplyAction).isError)

	for _, bad := range []any{"fail", "Error", float64(1), true} {
		_, err := NewReplyAction(map[string]any{"mode": bad})
		require.Error(t, err, "%v", bad)
		assert.Contains(t, err.Error(), "mode")
	}
}

// captureLog routes the global logger into a buffer for the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev, prevLevel := log.Logger, zerolog.GlobalLevel()
	log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	t.Cleanup(func() {
		log.Logger = prev
		zerolog.SetGlobalLevel(prevLevel)
	})
	return &buf
}

// Outside a run started by a call there is nobody to reply to: the node does
// nothing (and says so at debug level).
func TestReplyActionOutsideACall(t *testing.T) {
	buf := captureLog(t)
	n, err := NewReplyAction(nil)
	require.NoError(t, err)

	msg := types.NewMessage(map[string]any{"ok": true}, types.DataTypeObject)
	msg.SetMeta("correlationID", "not-a-call")
	out, err := n.Process(context.Background(), msg)
	assert.NoError(t, err)
	assert.Empty(t, out)
	assert.Contains(t, buf.String(), "reply not delivered")
	assert.Contains(t, buf.String(), "not-a-call")

	out, err = n.Process(context.Background(), types.NewMessage("x", types.DataTypeString))
	assert.NoError(t, err)
	assert.Empty(t, out)
}
