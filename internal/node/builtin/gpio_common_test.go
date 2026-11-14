package builtin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"periph.io/x/conn/v3/gpio"
)

func TestParseLevel(t *testing.T) {
	assert.Equal(t, gpio.High, parseLevel("high"))
	assert.Equal(t, gpio.High, parseLevel("1"))
	assert.Equal(t, gpio.High, parseLevel("ON"))
	assert.Equal(t, gpio.Low, parseLevel("low"))
	assert.Equal(t, gpio.Low, parseLevel("anything-else"))
}

func TestParseEdge(t *testing.T) {
	assert.Equal(t, gpio.RisingEdge, parseEdge("rising"))
	assert.Equal(t, gpio.FallingEdge, parseEdge("falling"))
	assert.Equal(t, gpio.BothEdges, parseEdge("both"))
	assert.Equal(t, gpio.BothEdges, parseEdge(""))
}

func TestParsePull(t *testing.T) {
	assert.Equal(t, gpio.PullUp, parsePull("up"))
	assert.Equal(t, gpio.PullDown, parsePull("down"))
	assert.Equal(t, gpio.Float, parsePull("none"))
}

func TestTruthy(t *testing.T) {
	assert.True(t, truthy(true))
	assert.True(t, truthy("on"))
	assert.True(t, truthy(1.0))
	assert.True(t, truthy(42))
	assert.False(t, truthy(false))
	assert.False(t, truthy(nil))
	assert.False(t, truthy("0"))
	assert.False(t, truthy("false"))
	assert.False(t, truthy(0.0))
}
