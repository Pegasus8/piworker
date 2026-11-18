package flow_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
)

// RiskCanRise says whether some setting of a node type can raise an
// instance above the type's risk (instanceRisk's rules), so the editor can
// say so only where it is true.
func TestRiskCanRise(t *testing.T) {
	for typ, want := range map[string]bool{
		"action-http":       true,  // a URL from the message, allowPrivate
		"action-notify":     true,  // a URL from the message
		"action-mqtt":       true,  // a broker from the message
		"action-email":      false, // its host is a destination, but never escalates
		"process-transform": false,
		"action-command":    false, // already privileged
	} {
		info, ok := node.DefaultRegistry.Get(typ)
		require.True(t, ok, typ)
		assert.Equal(t, want, flow.RiskCanRise(info), typ)
	}
}
