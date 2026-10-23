package builtin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const w1Good = `3b 01 4b 46 7f ff 0c 10 e7 : crc=e7 YES
3b 01 4b 46 7f ff 0c 10 e7 t=19687`

const w1Crc = `3b 01 4b 46 7f ff 0c 10 e7 : crc=e7 NO
3b 01 4b 46 7f ff 0c 10 e7 t=19687`

func TestParseW1Slave(t *testing.T) {
	c, err := parseW1Slave(w1Good)
	require.NoError(t, err)
	assert.InDelta(t, 19.687, c, 0.0001)
}

func TestParseW1SlaveCRCFails(t *testing.T) {
	_, err := parseW1Slave(w1Crc)
	assert.Error(t, err)
}

func TestParseW1SlaveMalformed(t *testing.T) {
	_, err := parseW1Slave("garbage")
	assert.Error(t, err)
}

func TestDS18B20ParsesConfig(t *testing.T) {
	n, err := NewDS18B20Trigger(map[string]interface{}{"deviceId": "28-abc", "intervalSec": 5})
	require.NoError(t, err)
	tr := n.(*DS18B20Trigger)
	assert.Equal(t, "28-abc", tr.deviceID)
	assert.Equal(t, 5, tr.intervalSec)
}

func TestDS18B20DefaultInterval(t *testing.T) {
	n, err := NewDS18B20Trigger(map[string]interface{}{"deviceId": "28-abc"})
	require.NoError(t, err)
	assert.Equal(t, 10, n.(*DS18B20Trigger).intervalSec)
}
