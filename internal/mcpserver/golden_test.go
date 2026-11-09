package mcpserver

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files under testdata")

// goldenToolsList is the tools/list answer of a token with the author and
// deploy scopes (author enabled, agent deploy all) at protocol 2025-11-25:
// the whole contract agents see (names, titles, descriptions, input and
// output schemas, annotations, _meta). A change shows in review as a diff
// of testdata/tools_author_deploy.golden.json (go test -run
// TestGoldenAuthorDeployToolsList -update rewrites it).
func TestGoldenAuthorDeployToolsList(t *testing.T) {
	a := newAuthorEnv(t)
	r := withToken(rpc(listToolsBody), authorDeployToken)
	r.Header.Set("Mcp-Protocol-Version", "2025-11-25")
	res, rpcErr := rpcResult(t, a.serve(r))
	require.Nil(t, rpcErr)
	tools, err := json.Marshal(res["tools"])
	require.NoError(t, err)
	var pretty bytes.Buffer
	require.NoError(t, json.Indent(&pretty, tools, "", "  "))
	pretty.WriteByte('\n')

	path := filepath.Join("testdata", "tools_author_deploy.golden.json")
	if *updateGolden {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(path, pretty.Bytes(), 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "run with -update to create the golden file")
	assert.Equal(t, string(want), pretty.String())

	var listed []map[string]any
	require.NoError(t, json.Unmarshal(tools, &listed))
	var got []string
	for _, tool := range listed {
		got = append(got, tool["name"].(string))
	}
	assert.Equal(t, append(append([]string{}, deployToolNames[:1]...), "pw_dry_run", "pw_get_draft", "pw_list_node_types",
		"pw_save_draft", "pw_stop_flow", "pw_test_node", "pw_validate_flow"), got, "sorted by name")
}
