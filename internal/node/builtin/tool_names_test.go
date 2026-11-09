package builtin

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolNameRegistryClaimsExclusively(t *testing.T) {
	r := newToolNameRegistry()
	a, b := new(int), new(int)

	require.NoError(t, r.claim("garage_open", a))
	require.NoError(t, r.claim("garage_open", a), "the owner may claim again")

	err := r.claim("garage_open", b)
	require.Error(t, err)
	assert.True(t, errors.Is(err, node.ErrResourceConflict), "%v", err)
	assert.Contains(t, err.Error(), `"garage_open"`)

	// Only the owner releases its claim: a stale instance that stops late
	// cannot free a name a newer instance holds.
	r.release("garage_open", b)
	assert.Error(t, r.claim("garage_open", b))

	r.release("garage_open", a)
	require.NoError(t, r.claim("garage_open", b))
	assert.True(t, r.taken("garage_open"))
	r.release("garage_open", b)
	assert.False(t, r.taken("garage_open"))

	// Names are exact: other names are independent.
	require.NoError(t, r.claim("garage_close", a))
	require.NoError(t, r.claim("Garage_Close", b))
}

func TestToolNameRegistryConcurrentClaims(t *testing.T) {
	r := newToolNameRegistry()
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r.claim("contended", new(int)) == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, int32(1), wins.Load())
}
