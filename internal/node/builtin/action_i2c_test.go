package builtin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestI2CParsesHexConfig(t *testing.T) {
	n, err := NewI2CAction(map[string]interface{}{
		"address": "0x76", "register": "0xD0", "length": 2,
	})
	require.NoError(t, err)
	a := n.(*I2CAction)
	assert.Equal(t, uint16(0x76), a.address)
	assert.True(t, a.hasRegister)
	assert.Equal(t, byte(0xD0), a.register)
	assert.Equal(t, 2, a.length)
}

func TestI2CDecimalAddress(t *testing.T) {
	n, err := NewI2CAction(map[string]interface{}{"address": "118"})
	require.NoError(t, err)
	assert.Equal(t, uint16(118), n.(*I2CAction).address)
}

func TestI2CNoRegister(t *testing.T) {
	n, err := NewI2CAction(map[string]interface{}{"address": "0x68", "length": 1})
	require.NoError(t, err)
	assert.False(t, n.(*I2CAction).hasRegister)
}

func TestI2CRequiresAddress(t *testing.T) {
	_, err := NewI2CAction(map[string]interface{}{"length": 1})
	assert.Error(t, err)
}

func TestI2CInvalidAddress(t *testing.T) {
	_, err := NewI2CAction(map[string]interface{}{"address": "not-hex"})
	assert.Error(t, err)
}

func TestI2CDefaultLength(t *testing.T) {
	n, err := NewI2CAction(map[string]interface{}{"address": "0x76"})
	require.NoError(t, err)
	assert.Equal(t, 1, n.(*I2CAction).length)
}
