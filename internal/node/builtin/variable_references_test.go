package builtin

import (
	"context"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/Pegasus8/piworker/internal/vars"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestVariableReferencesInExpressionsAndTemplates(t *testing.T) {
	vars.DefaultStore.Set("threshold-test", 25)
	defer vars.DefaultStore.Delete("threshold-test")
	require.NoError(t, secrets.DefaultStore.Set("PRIVATE_TEST", "never-in-payload"))
	defer secrets.DefaultStore.Delete("PRIVATE_TEST")
	n, err := NewTransformProcess(map[string]interface{}{"expression": `payload + vars["threshold-test"]`})
	require.NoError(t, err)
	out, err := n.Process(context.Background(), types.NewMessage(5, types.DataTypeNumber))
	require.NoError(t, err)
	require.EqualValues(t, 30, out[0].Payload)
	require.Equal(t, "limit=25", renderTemplate("limit={{vars.threshold-test}}", types.NewMessage(nil, types.DataTypeAny)))
	require.NotContains(t, renderTemplate("{{secret.PRIVATE_TEST}}", types.NewMessage(nil, types.DataTypeAny)), "never-in-payload")
}
