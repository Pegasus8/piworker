package service

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/stretchr/testify/assert"
)

// Every runtime refusal that wraps flow.ErrBusy started no run, so the agent
// must get a retryable "busy" error, never an internal one.
func TestCallErrorMapsEveryBusyRefusal(t *testing.T) {
	for _, err := range []error{
		flow.ErrTooManyCalls,
		flow.ErrTriggerBusy,
		fmt.Errorf("start run on t1: %w", flow.ErrTriggerBusy),
		fmt.Errorf("wrapped: %w", flow.ErrBusy),
	} {
		var te *ToolError
		if assert.True(t, errors.As(callError(err), &te), "%v", err) {
			assert.ErrorIs(t, te, ErrBusy, "%v", err)
		}
	}
}
