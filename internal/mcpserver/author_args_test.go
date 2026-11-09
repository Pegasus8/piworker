package mcpserver

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nodesJSON is a JSON array of n minimal nodes.
func nodesJSON(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf(`{"id":"n%d","type":"process-debug"}`, i)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// opsJSON is a JSON array of n remove_node operations.
func opsJSON(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf(`{"op":"remove_node","nodeId":"n%d"}`, i)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// Arguments outside a tool's contract are refused before the service is
// called: unknown properties anywhere in a flow, an op or the request, wrong
// types, missing fields, every op's own fields, the combinations each tool
// takes, the views' "something was left out" markers, and the bounds. Each
// refusal is counted in the rejections and never quotes a value.
func TestAuthorAndDeployArgumentsAreValidated(t *testing.T) {
	a := newAuthorEnv(t)
	const raw = everythingToken
	flowOK := `{"nodes":[]}`
	for _, tc := range []struct {
		tool, args, pointer string
	}{
		// pw_validate_flow: one of flow and ops; ops need flowId.
		{"pw_validate_flow", `{}`, ""},
		{"pw_validate_flow", `{"flowId":"f1","flow":` + flowOK + `,"ops":[]}`, ""},
		{"pw_validate_flow", `{"ops":[{"op":"remove_node","nodeId":"a"}]}`, "/flowId"},
		{"pw_validate_flow", `{"flow":` + flowOK + `,"extra":1}`, "/extra"},
		{"pw_validate_flow", `{"flowId":"f1\n","ops":[]}`, "/flowId"},
		// Flows: closed objects at every level.
		{"pw_validate_flow", `{"flow":{}}`, "/flow/nodes"},
		{"pw_validate_flow", `{"flow":"Garage"}`, "/flow"},
		{"pw_validate_flow", `{"flow":{"nodes":[],"conections":[]}}`, "/flow/conections"},
		{"pw_validate_flow", `{"flow":{"nodes":[],"Nodes":[]}}`, "/flow/Nodes"},
		{"pw_validate_flow", `{"flow":{"nodes":[{"id":"a"}]}}`, "/flow/nodes/0/type"},
		{"pw_validate_flow", `{"flow":{"nodes":[{"type":"x"}]}}`, "/flow/nodes/0/id"},
		{"pw_validate_flow", `{"flow":{"nodes":[{"id":"a","type":"x","settings":{}}]}}`, "/flow/nodes/0/settings"},
		{"pw_validate_flow", `{"flow":{"nodes":[{"id":"a","type":"x","config":"url=1"}]}}`, "/flow/nodes/0/config"},
		{"pw_validate_flow", `{"flow":{"nodes":[{"id":"a","type":"x","enabled":"yes"}]}}`, "/flow/nodes/0/enabled"},
		{"pw_validate_flow", `{"flow":{"nodes":[{"id":"a","type":"x","position":{"x":"sk-live-123","y":2}}]}}`, "/flow/nodes/0/position/x"},
		{"pw_validate_flow", `{"flow":{"nodes":[{"id":"a","type":"x","position":{"x":1,"y":2,"z":3}}]}}`, "/flow/nodes/0/position/z"},
		{"pw_validate_flow", `{"flow":{"nodes":[{"id":"a\n","type":"x"}]}}`, "/flow/nodes/0/id"},
		{"pw_validate_flow", `{"flow":{"nodes":[{"id":"a","type":"x","category":"trigger"}]}}`, "/flow/nodes/0/category"},
		{"pw_validate_flow", `{"flow":{"nodes":[{"id":"a","type":"x","inputs":[{"name":"in"}]}]}}`, "/flow/nodes/0/inputs/0/id"},
		{"pw_validate_flow", `{"flow":{"nodes":[],"connections":[{"id":"c","sourceNode":"a","sourcePort":"o","targetNode":"b"}]}}`, "/flow/connections/0/targetPort"},
		{"pw_validate_flow", `{"flow":{"nodes":[],"connections":[{"sourceNode":"a","sourcePort":"o","targetNode":"b","targetPort":"i"}]}}`, "/flow/connections/0/id"},
		{"pw_validate_flow", `{"flow":{"nodes":[],"variables":[1]}}`, "/flow/variables"},
		{"pw_validate_flow", `{"flow":{"nodes":[],"name":"` + strings.Repeat("n", service.MaxDraftNameChars+1) + `"}}`, "/flow/name"},
		// What a view left out cannot be sent back as if it were the flow.
		{"pw_validate_flow", `{"flow":{"nodes":[],"truncated":true}}`, "/flow/truncated"},
		{"pw_validate_flow", `{"flow":{"nodes":[{"id":"a","type":"x","configOmitted":true}]}}`, "/flow/nodes/0/configOmitted"},
		{"pw_validate_flow", `{"flow":{"nodes":` + nodesJSON(service.MaxDraftNodes+1) + `}}`, "/flow/nodes"},
		// Ops: each op takes its own fields.
		{"pw_validate_flow", `{"flowId":"f1","ops":[{"nodeId":"a"}]}`, "/ops/0/op"},
		{"pw_validate_flow", `{"flowId":"f1","ops":[{"op":"rename_node","nodeId":"a"}]}`, "/ops/0/op"},
		{"pw_validate_flow", `{"flowId":"f1","ops":[{"op":"add_node"}]}`, "/ops/0/node"},
		{"pw_validate_flow", `{"flowId":"f1","ops":[{"op":"add_node","node":{"id":"a"}}]}`, "/ops/0/node/type"},
		{"pw_validate_flow", `{"flowId":"f1","ops":[{"op":"add_node","node":{"id":"bad id","type":"x"}}]}`, "/ops/0/node/id"},
		{"pw_validate_flow", `{"flowId":"f1","ops":[{"op":"add_node","node":{"type":"x","name":"y"}}]}`, "/ops/0/node/name"},
		{"pw_validate_flow", `{"flowId":"f1","ops":[{"op":"add_node","node":{"type":"x"},"nodeId":"a"}]}`, "/ops/0/nodeId"},
		{"pw_validate_flow", `{"flowId":"f1","ops":[{"op":"update_node","nodeId":"a"}]}`, "/ops/0"},
		{"pw_validate_flow", `{"flowId":"f1","ops":[{"op":"update_node","config":{}}]}`, "/ops/0/nodeId"},
		{"pw_validate_flow", `{"flowId":"f1","ops":[{"op":"update_node","nodeId":"a","enabled":"yes"}]}`, "/ops/0/enabled"},
		{"pw_validate_flow", `{"flowId":"f1","ops":[{"op":"update_node","nodeId":"a","node":{"type":"x"}}]}`, "/ops/0/node"},
		{"pw_validate_flow", `{"flowId":"f1","ops":[{"op":"remove_node","nodeId":"a","config":{}}]}`, "/ops/0/config"},
		{"pw_validate_flow", `{"flowId":"f1","ops":[{"op":"remove_node"}]}`, "/ops/0/nodeId"},
		{"pw_validate_flow", `{"flowId":"f1","ops":[{"op":"connect","sourceNode":"a","sourcePort":"o","targetNode":"b"}]}`, "/ops/0/targetPort"},
		{"pw_validate_flow", `{"flowId":"f1","ops":[{"op":"connect","sourceNode":"a","sourcePort":"o","targetNode":"b","targetPort":"i","nodeId":"x"}]}`, "/ops/0/nodeId"},
		{"pw_validate_flow", `{"flowId":"f1","ops":[{"op":"disconnect"}]}`, "/ops/0"},
		{"pw_validate_flow", `{"flowId":"f1","ops":[{"op":"disconnect","sourceNode":"a"}]}`, "/ops/0"},
		{"pw_validate_flow", `{"flowId":"f1","ops":[{"op":"disconnect","connectionId":"c","sourceNode":"a"}]}`, "/ops/0"},
		{"pw_validate_flow", `{"flowId":"f1","ops":[{"op":"remove_node","nodeId":"a"},{"op":"remove_node","nodeId":"b","extra":1}]}`, "/ops/1/extra"},
		{"pw_validate_flow", `{"flowId":"f1","ops":"remove a"}`, "/ops"},
		{"pw_validate_flow", `{"flowId":"f1","ops":` + opsJSON(201) + `}`, "/ops"},

		// pw_save_draft: a note always; flowId with baseVersion; a new flow
		// as a whole flow.
		{"pw_save_draft", `{"flow":` + flowOK + `,"name":"x"}`, "/note"},
		{"pw_save_draft", `{"flow":` + flowOK + `,"name":"x","note":""}`, "/note"},
		{"pw_save_draft", `{"flow":` + flowOK + `,"note":"` + strings.Repeat("n", service.MaxDraftNoteChars+1) + `"}`, "/note"},
		{"pw_save_draft", `{"flow":` + flowOK + `,"name":"` + strings.Repeat("n", service.MaxDraftNameChars+1) + `","note":"n"}`, "/name"},
		{"pw_save_draft", `{"note":"n"}`, ""},
		{"pw_save_draft", `{"flowId":"f1","baseVersion":3,"flow":` + flowOK + `,"ops":[],"note":"n"}`, ""},
		{"pw_save_draft", `{"ops":[{"op":"remove_node","nodeId":"a"}],"note":"n"}`, "/flowId"},
		{"pw_save_draft", `{"flowId":"f1","ops":[{"op":"remove_node","nodeId":"a"}],"note":"n"}`, "/baseVersion"},
		{"pw_save_draft", `{"baseVersion":3,"flow":` + flowOK + `,"note":"n"}`, "/flowId"},
		{"pw_save_draft", `{"flowId":"f1","baseVersion":0,"flow":` + flowOK + `,"note":"n"}`, "/baseVersion"},
		{"pw_save_draft", `{"flowId":"f1","baseVersion":2.5,"flow":` + flowOK + `,"note":"n"}`, "/baseVersion"},
		{"pw_save_draft", `{"flowId":"f1","baseVersion":"3","flow":` + flowOK + `,"note":"n"}`, "/baseVersion"},
		{"pw_save_draft", `{"flowId":"f1","baseVersion":3,"flow":` + flowOK + `,"note":"n","deploy":true}`, "/deploy"},

		// pw_get_draft.
		{"pw_get_draft", `{}`, "/draftId"},
		{"pw_get_draft", `{"draftId":""}`, "/draftId"},
		{"pw_get_draft", `{"draftId":"d1","flowId":"f1"}`, "/flowId"},

		// pw_dry_run: exactly one of flow, draftId and flowId.
		{"pw_dry_run", `{}`, ""},
		{"pw_dry_run", `{"draftId":"d1","flowId":"f1"}`, ""},
		{"pw_dry_run", `{"draftId":"d1","flow":` + flowOK + `}`, ""},
		{"pw_dry_run", `{"draftId":"d1","triggerNodeId":""}`, "/triggerNodeId"},
		{"pw_dry_run", `{"draftId":"d1","mocks":[1]}`, "/mocks"},
		{"pw_dry_run", `{"draftId":"d1","flowVars":"n=1"}`, "/flowVars"},
		{"pw_dry_run", `{"draftId":"d1","ops":[]}`, "/ops"},
		{"pw_dry_run", `{"flow":{"nodes":` + nodesJSON(51) + `}}`, "/flow/nodes"},

		// pw_test_node.
		{"pw_test_node", `{}`, "/type"},
		{"pw_test_node", `{"type":"process-transform","config":[]}`, "/config"},
		{"pw_test_node", `{"type":"process-transform","inputs":{}}`, "/inputs"},

		// pw_list_node_types: a known category or type.
		{"pw_list_node_types", `{"category":"trigger"}`, "/category"},
		{"pw_list_node_types", `{"type":"no-such-type"}`, "/type"},
		{"pw_list_node_types", `{"verbose":true}`, "/verbose"},

		// The deploy tools.
		{"pw_deploy_flow", `{}`, "/flowId"},
		{"pw_deploy_flow", `{"flowId":"f1","draftId":""}`, "/draftId"},
		{"pw_deploy_flow", `{"flowId":"f1","restart":true}`, "/restart"},
		{"pw_stop_flow", `{}`, "/flowId"},
		{"pw_stop_flow", `{"flowId":"f1","draftId":"d1"}`, "/draftId"},
	} {
		res, body := called(t, a.testEnv, raw, tc.tool, tc.args)
		label := tc.tool + " " + shorten(tc.args, 160)
		require.Equal(t, true, res["isError"], label)
		require.Equal(t, "invalid_arguments", errorCode(body), label)
		var pointers []string
		for _, i := range body["error"].(map[string]any)["issues"].([]any) {
			pointers = append(pointers, i.(map[string]any)["pointer"].(string))
		}
		assert.Contains(t, pointers, tc.pointer, label)
		assert.NotContains(t, fmt.Sprint(body), "sk-live", "values are never quoted")
	}
	assert.Empty(t, a.authoring.recorded(), "no refused call reaches the service")
	assert.NotEmpty(t, a.rejections.seen)
	want := "everythingeveryt invalid_arguments " + t0.Format(time.RFC3339)
	for _, seen := range a.rejections.seen {
		assert.Equal(t, want, seen)
	}
}

// The arguments of the flow-shaped tools are bounded as JSON before they
// are decoded: a draft's flow may be up to service.MaxDraftFlowBytes, a
// node test's config and input up to the sandbox's input bound.
func TestFlowShapedArgumentsAreBounded(t *testing.T) {
	a := newAuthorEnv(t)
	big := func(n int) string { return strings.Repeat("x", n) }
	for _, tc := range []struct{ tool, args string }{
		{"pw_validate_flow", `{"flow":{"nodes":[{"id":"a","type":"x","config":{"s":"` + big(maxFlowArgsBytes) + `"}}]}}`},
		{"pw_save_draft", `{"flow":{"nodes":[{"id":"a","type":"x","config":{"s":"` + big(maxFlowArgsBytes) + `"}}]},"name":"x","note":"n"}`},
		{"pw_dry_run", `{"draftId":"d1","payload":"` + big(maxDryRunArgsBytes) + `"}`},
		{"pw_test_node", `{"type":"process-transform","input":"` + big(maxTestNodeArgsBytes) + `"}`},
	} {
		res, body := called(t, a.testEnv, everythingToken, tc.tool, tc.args)
		require.Equal(t, true, res["isError"], tc.tool)
		assert.Equal(t, "invalid_arguments", errorCode(body), tc.tool)
		issue := body["error"].(map[string]any)["issues"].([]any)[0].(map[string]any)
		assert.Equal(t, "", issue["pointer"], tc.tool)
		assert.Contains(t, issue["message"], "bytes", tc.tool)
	}
	assert.Empty(t, a.authoring.recorded())

	// Within the bounds the largest flows pass: a draft of 200 nodes.
	res, _ := called(t, a.testEnv, everythingToken, "pw_save_draft", `{"flow":{"nodes":`+nodesJSON(service.MaxDraftNodes)+`},"name":"x","note":"n"}`)
	assert.NotEqual(t, true, res["isError"])
	assert.Less(t, maxFlowArgsBytes, DefaultMaxRequestBodyBytes, "within the request body limit")
	assert.Less(t, maxDryRunArgsBytes, DefaultMaxRequestBodyBytes)
	assert.Greater(t, maxFlowArgsBytes, service.MaxDraftFlowBytes, "room for the largest draft and its note")
}
