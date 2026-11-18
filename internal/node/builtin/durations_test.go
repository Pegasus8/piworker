package builtin

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMsDuration(t *testing.T) {
	def, lo, hi := time.Second, 100*time.Millisecond, time.Hour
	assert.Equal(t, def, msDuration(0, def, lo, hi))
	assert.Equal(t, def, msDuration(-5, def, lo, hi))
	assert.Equal(t, lo, msDuration(1, def, lo, hi))
	assert.Equal(t, 1500*time.Millisecond, msDuration(1500, def, lo, hi))
	assert.Equal(t, hi, msDuration(math.MaxInt, def, lo, hi), "must not overflow")
	assert.Equal(t, hi, msDuration(int(math.MaxInt64/int64(time.Millisecond))+1, def, lo, hi))
}
