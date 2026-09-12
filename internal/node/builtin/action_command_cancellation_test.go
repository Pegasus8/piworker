//go:build unix

package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCommandActionStopsShellChildren(t *testing.T) {
	for _, tc := range []struct {
		name         string
		cancelParent bool
	}{
		{"timeout", false}, {"parent cancellation", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "child-finished")
			// Keep the shell and a child alive on both macOS and Linux. A bare sleep
			// may be exec-optimized by the shell, hiding descendant cancellation bugs.
			action, err := NewCommandAction(map[string]interface{}{
				"command": "(sleep 3; printf survived > {{payload}}) & wait", "timeout": 1,
			})
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelParent {
				timer := time.AfterFunc(200*time.Millisecond, cancel)
				defer timer.Stop()
			}
			start := time.Now()
			outputs, err := action.Process(ctx, types.NewMessage(marker, types.DataTypeString))
			require.NoError(t, err)
			require.Len(t, outputs, 1)
			elapsed := time.Since(start)
			payload := outputs[0].Payload.(map[string]interface{})
			// Non-fatal assertions let us also check for lingering child side effects.
			if elapsed >= 2500*time.Millisecond {
				t.Errorf("cancellation took %v; child pipes delayed completion", elapsed)
			}
			require.Equal(t, false, payload["success"])
			require.Equal(t, !tc.cancelParent, payload["timedOut"])
			time.Sleep(max(0, 3500*time.Millisecond-time.Since(start)))
			_, err = os.Stat(marker)
			require.ErrorIs(t, err, os.ErrNotExist, "a child continued executing after cancellation")
		})
	}
}
