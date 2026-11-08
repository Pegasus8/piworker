//go:build unix

package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A value such as "-rf" or "--output=/etc/x" bound to an argument would be
// read as an option by most programs, so it is refused by default.
func TestCommandRejectsDashLeadingPlaceholderValues(t *testing.T) {
	for _, tc := range []struct{ name, command string }{
		{"payload", "printf '%s' {{payload}}"},
		{"quoted payload", `printf '%s' "{{payload}}"`},
		{"topic", "printf '%s' {{topic}}"},
		{"meta", "printf '%s' {{meta.arg}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "ran")
			action, err := NewCommandAction(map[string]interface{}{"command": "touch '" + marker + "'; " + tc.command})
			require.NoError(t, err)
			require.NoError(t, action.Validate())

			msg := types.NewMessage("--secret-looking-value", types.DataTypeString)
			msg.Topic = "--secret-looking-value"
			msg.SetMeta("arg", "--secret-looking-value")
			_, err = action.Process(context.Background(), msg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "allowDashArgs")
			assert.NotContains(t, err.Error(), "secret-looking-value", "the value is not echoed")
			_, statErr := os.Stat(marker)
			assert.ErrorIs(t, statErr, os.ErrNotExist, "nothing may run")
		})
	}
}

func TestCommandAllowsOtherValues(t *testing.T) {
	action, err := NewCommandAction(map[string]interface{}{"command": "printf '%s' {{payload}}"})
	require.NoError(t, err)
	for _, value := range []string{"a-b", "", " -x", "x--y", "—em dash"} {
		out, err := action.Process(context.Background(), types.NewMessage(value, types.DataTypeString))
		require.NoError(t, err, value)
		assert.Equal(t, true, out[0].Payload.(map[string]interface{})["success"], value)
	}
	// A number payload is bound through fmt.Sprint: -1 starts with '-'.
	_, err = action.Process(context.Background(), types.NewMessage(float64(-1), types.DataTypeNumber))
	require.Error(t, err)
}

func TestCommandAllowDashArgsOptIn(t *testing.T) {
	action, err := NewCommandAction(map[string]interface{}{"command": "printf '%s' {{payload}}", "allowDashArgs": true})
	require.NoError(t, err)
	out, err := action.Process(context.Background(), types.NewMessage("-n", types.DataTypeString))
	require.NoError(t, err)
	result := out[0].Payload.(map[string]interface{})
	assert.Equal(t, true, result["success"])
	assert.Equal(t, "-n", result["stdout"], "the value is still passed as one literal argument")
}

func TestCommandAllowDashArgsSchema(t *testing.T) {
	info, ok := node.DefaultRegistry.Get("action-command")
	require.True(t, ok)
	require.NotNil(t, info.Config)
	p, ok := info.Config.Properties["allowDashArgs"]
	require.True(t, ok)
	assert.Equal(t, "boolean", p.Type)
	assert.Equal(t, false, p.Default)
	assert.Contains(t, info.Documentation, "allowDashArgs")
}
