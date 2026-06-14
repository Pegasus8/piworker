package builtin

import (
	"context"
	"testing"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGPIOTriggerParsesConfig(t *testing.T) {
	n, err := NewGPIOTrigger(map[string]interface{}{"pin": "GPIO27", "edge": "rising", "pull": "up"})
	require.NoError(t, err)
	tr := n.(*GPIOTrigger)
	assert.Equal(t, "GPIO27", tr.pin)
	assert.Equal(t, "rising", tr.edge)
	assert.Equal(t, "up", tr.pull)
}

func TestGPIOTriggerRequiresPin(t *testing.T) {
	_, err := NewGPIOTrigger(map[string]interface{}{"edge": "both"})
	assert.Error(t, err)
}

// On a host without GPIO, Start must return an error (not hang or panic) and
// leave no goroutine running.
func TestGPIOTriggerStartErrorsWithoutHardware(t *testing.T) {
	n, err := NewGPIOTrigger(map[string]interface{}{"pin": "GPIO_DOES_NOT_EXIST", "edge": "both"})
	require.NoError(t, err)
	out := make(chan *types.Message, 1)
	err = n.(*GPIOTrigger).Start(context.Background(), out)
	assert.Error(t, err)
}
