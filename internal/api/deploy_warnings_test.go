package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// optionalSecretNode resolves {{secret.NAME}} in its password setting when
// it runs, but works without it (node.SecretReferencer).
type optionalSecretNode struct {
	mockNode
	password string
}

func (n *optionalSecretNode) Ports() ([]flow.Port, []flow.Port) {
	return []flow.Port{{ID: "input", Name: "Input", DataType: flow.DataTypeAny}}, nil
}

func (n *optionalSecretNode) SecretReferences() map[string][]string {
	if n.password == "" {
		return nil
	}
	return map[string][]string{"password": {n.password}}
}

// A deploy that goes ahead with Lint warnings answers them in
// data.warnings (an undefined secret an optional credential references:
// the node would get an empty value), so the editor can show them; a
// deploy without warnings answers an empty list.
func TestDeployAnswersItsWarnings(t *testing.T) {
	server, store := createTestServer(t)
	t.Cleanup(server.handler.manager.StopAll)
	require.NoError(t, server.handler.registry.Register(node.NodeTypeInfo{
		Type: "mock-optional-secret", Name: "Optional secret", Category: flow.NodeCategoryOutput,
		Inputs: []flow.Port{{ID: "input", Name: "Input", DataType: flow.DataTypeAny}},
	}, func(cfg map[string]any) (node.Node, error) {
		pw, _ := cfg["password"].(string)
		return &optionalSecretNode{password: strings.TrimSuffix(strings.TrimPrefix(pw, "{{secret."), "}}")}, nil
	}))

	f := createTestFlow("warned")
	sink := flow.NewNode("mock-optional-secret", flow.NodeCategoryOutput)
	sink.ID = "sink"
	sink.Inputs = []flow.Port{{ID: "input", Name: "Input", DataType: flow.DataTypeAny}}
	sink.Config = map[string]any{"password": "{{secret.pw_api_missing}}"}
	f.AddNode(*sink)
	f.AddConnection(*flow.NewConnection(f.Nodes[0].ID, "output", "sink", "input"))
	require.NoError(t, store.CreateFlow(f))

	type deployed struct {
		Data struct {
			Running  bool         `json:"running"`
			Warnings []flow.Issue `json:"warnings"`
		} `json:"data"`
	}
	rr := performRequest(server, http.MethodPost, "/api/flows/"+f.ID+"/deploy", nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var got deployed
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.True(t, got.Data.Running)
	require.Len(t, got.Data.Warnings, 1, rr.Body.String())
	assert.Equal(t, flow.IssueUnresolvedSecret, got.Data.Warnings[0].Code)
	assert.Equal(t, "/nodes/sink/config/password", got.Data.Warnings[0].Path)
	assert.Contains(t, got.Data.Warnings[0].Message, `"pw_api_missing"`)

	require.Equal(t, http.StatusOK, performRequest(server, http.MethodPost, "/api/flows/"+f.ID+"/stop", nil).Code)
	require.NoError(t, secrets.DefaultStore.Set("pw_api_missing", "defined-now-value"))
	t.Cleanup(func() { _ = secrets.DefaultStore.Delete("pw_api_missing") })
	rr = performRequest(server, http.MethodPost, "/api/flows/"+f.ID+"/deploy", nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	got = deployed{}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.NotNil(t, got.Data.Warnings)
	assert.Empty(t, got.Data.Warnings)
	assert.Contains(t, rr.Body.String(), `"warnings":[]`)
}
