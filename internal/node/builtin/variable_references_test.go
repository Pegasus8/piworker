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

func TestFlowScopeDoesNotFallBackToGlobals(t *testing.T) {
	global, local := vars.NewStore(), vars.NewStore()
	require.NoError(t, global.Set("count", 100))
	ctx := vars.WithFlowStore(vars.WithStore(context.Background(), global), local)
	setter, err := NewSetVarProcess(map[string]interface{}{"scope": "flow", "key": "count", "value": "payload"})
	require.NoError(t, err)
	_, err = setter.Process(ctx, types.NewMessage(7, types.DataTypeNumber))
	require.NoError(t, err)
	getter, err := NewGetVarProcess(map[string]interface{}{"scope": "flow", "key": "count"})
	require.NoError(t, err)
	out, err := getter.Process(ctx, types.NewMessage(nil, types.DataTypeAny))
	require.NoError(t, err)
	require.EqualValues(t, 7, out[0].Payload)
	n, err := NewTransformProcess(map[string]interface{}{"expression": `vars.count + flowVars.count`})
	require.NoError(t, err)
	out, err = n.Process(ctx, types.NewMessage(nil, types.DataTypeAny))
	require.NoError(t, err)
	require.EqualValues(t, 107, out[0].Payload)
	require.Equal(t, "100/7", renderTemplate("{{vars.count}}/{{flowVars.count}}", types.NewMessage(nil, types.DataTypeAny), ctx))
	_, err = setter.Process(context.Background(), types.NewMessage(1, types.DataTypeNumber))
	require.Error(t, err)
	_, err = NewSetVarProcess(map[string]interface{}{"scope": "invalid", "key": "count"})
	require.Error(t, err)
}
