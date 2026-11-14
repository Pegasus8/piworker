package builtin

import (
	"context"
	"testing"

	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGPIOActionParsesConfig(t *testing.T) {
	n, err := NewGPIOAction(map[string]interface{}{"pin": "GPIO17", "value": "low"})
	require.NoError(t, err)
	a := n.(*GPIOAction)
	assert.Equal(t, "GPIO17", a.pin)
	assert.Equal(t, "low", a.value)
}

func TestGPIOActionRequiresPin(t *testing.T) {
	_, err := NewGPIOAction(map[string]interface{}{"value": "high"})
	assert.Error(t, err)
}

// On a host without GPIO (CI/dev machines), Process must fail cleanly rather
// than panic when the pin can't be resolved.
func TestGPIOActionErrorsWithoutHardware(t *testing.T) {
	n, err := NewGPIOAction(map[string]interface{}{"pin": "GPIO_DOES_NOT_EXIST", "value": "high"})
	require.NoError(t, err)
	assert.NotPanics(t, func() {
		_, perr := n.Process(context.Background(), types.NewMessage(nil, types.DataTypeAny))
		assert.Error(t, perr)
	})
}
