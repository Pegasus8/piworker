package builtin

import (
	"context"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/stretchr/testify/require"
)

// A message that arrives from a multi-output upstream node (e.g. Switch's
// "true" port) must leave a single-output node tagged with that node's own
// output port; otherwise the router looks up connections for the upstream
// port name and drops the message.
func TestSingleOutputNodesResetSourcePort(t *testing.T) {
	cases := []struct {
		name    string
		factory func(map[string]interface{}) (node.Node, error)
		config  map[string]interface{}
	}{
		{"action-log", NewLogAction, nil},
		{"process-debug", NewDebugProcess, nil},
		{"process-delay", NewDelayProcess, map[string]interface{}{"delay": 0}},
		{"process-filter", NewFilterProcess, map[string]interface{}{"condition": "true"}},
		{"process-template", NewTemplateProcess, map[string]interface{}{"template": "x"}},
		{"process-transform", NewTransformProcess, nil},
		{"process-ratelimit", NewRateLimitProcess, map[string]interface{}{"limit": 10}},
		{"process-parse", NewParseProcess, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := tc.factory(tc.config)
			require.NoError(t, err)
			_, outs := n.Ports()
			require.Len(t, outs, 1)

			in := types.NewMessage(`{"a":1}`, types.DataTypeString)
			in.SourceNode = "switch-1"
			in.SourcePort = "true"

			got, err := n.Process(context.Background(), in)
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.Equal(t, outs[0].ID, got[0].SourcePort)
			require.Equal(t, "true", in.SourcePort, "input must not be mutated")
		})
	}
}
