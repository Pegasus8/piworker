//go:build unix

package builtin

import (
	"context"
	"testing"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A chatty command (a log dump, `yes`, a runaway loop) must not buffer
// unbounded output in RAM.
func TestCommandActionCapsOutput(t *testing.T) {
	n, err := NewCommandAction(map[string]interface{}{
		"command": "head -c 3000000 /dev/zero | tr '\\0' a; head -c 3000000 /dev/zero | tr '\\0' b >&2; echo done",
	})
	require.NoError(t, err)

	out, err := n.Process(context.Background(), types.NewMessage(nil, types.DataTypeAny))
	require.NoError(t, err)
	p := out[0].Payload.(map[string]interface{})

	assert.Equal(t, true, p["success"], "the command itself must still complete: %v", p["error"])
	assert.LessOrEqual(t, len(p["stdout"].(string)), commandMaxOutputBytes)
	assert.LessOrEqual(t, len(p["stderr"].(string)), commandMaxOutputBytes)
	assert.Equal(t, true, p["stdoutTruncated"])
	assert.Equal(t, true, p["stderrTruncated"])
	assert.Equal(t, true, p["truncated"])
}

func TestCommandActionSmallOutputNotTruncated(t *testing.T) {
	n, err := NewCommandAction(map[string]interface{}{"command": "echo hi"})
	require.NoError(t, err)
	out, err := n.Process(context.Background(), types.NewMessage(nil, types.DataTypeAny))
	require.NoError(t, err)
	p := out[0].Payload.(map[string]interface{})
	assert.Equal(t, "hi", p["stdout"])
	assert.Equal(t, false, p["truncated"])
	assert.Equal(t, false, p["stdoutTruncated"])
}
