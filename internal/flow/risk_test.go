package flow_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	_ "github.com/Pegasus8/piworker/internal/node/builtin"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// riskStep is a node placed after the trigger-tool in a test flow.
type riskStep struct {
	id       string
	typ      string
	config   map[string]any
	disabled bool
}

func uniqueToolName() string { return "risk_" + strings.ReplaceAll(uuid.NewString(), "-", "") }

// strictToolConfig is a trigger-tool whose only argument is strict, so the
// strictness rule never gets in the way of the rule under test.
func strictToolConfig() map[string]any {
	return map[string]any{
		"toolName":    uniqueToolName(),
		"description": "Opens the left or right garage door and reports whether it moved.",
		"inputSchema": map[string]any{
			"type":     "object",
			"required": []any{"door"},
			"properties": map[string]any{
				"door": map[string]any{"type": "string", "enum": []any{"left", "right"}},
			},
		},
	}
}

// chainFlow builds trigger-tool "tool" → steps[0] → steps[1] → … (each step
// fed by the previous one's "output" port).
func chainFlow(toolCfg map[string]any, steps ...riskStep) *flow.Flow {
	f := flow.NewFlow("risk")
	tool := flow.NewNode("trigger-tool", flow.NodeCategoryInput)
	tool.ID, tool.Config = "tool", toolCfg
	f.AddNode(*tool)
	prev := "tool"
	for _, s := range steps {
		n := flow.NewNode(s.typ, flow.NodeCategoryProcessing)
		n.ID, n.Config, n.Enabled = s.id, s.config, !s.disabled
		if n.Config == nil {
			n.Config = map[string]any{}
		}
		f.AddNode(*n)
		f.AddConnection(*flow.NewConnection(prev, "output", s.id, "input"))
		prev = s.id
	}
	return f
}

// fanFlow connects trigger-tool "tool" directly to every step.
func fanFlow(toolCfg map[string]any, steps ...riskStep) *flow.Flow {
	f := flow.NewFlow("risk")
	tool := flow.NewNode("trigger-tool", flow.NodeCategoryInput)
	tool.ID, tool.Config = "tool", toolCfg
	f.AddNode(*tool)
	for _, s := range steps {
		n := flow.NewNode(s.typ, flow.NodeCategoryProcessing)
		n.ID, n.Config, n.Enabled = s.id, s.config, !s.disabled
		if n.Config == nil {
			n.Config = map[string]any{}
		}
		f.AddNode(*n)
		f.AddConnection(*flow.NewConnection("tool", "output", s.id, "input"))
	}
	return f
}

// toolSpecs instantiates the enabled tool nodes of f, as deploy does.
func toolSpecs(t *testing.T, f *flow.Flow) map[string]node.ToolSpec {
	t.Helper()
	specs := map[string]node.ToolSpec{}
	for _, n := range f.Nodes {
		if !n.Enabled {
			continue
		}
		inst, err := node.DefaultRegistry.Create(n.Type, n.Config)
		require.NoError(t, err, n.ID)
		if p, ok := inst.(node.ToolProvider); ok {
			specs[n.ID] = p.ToolSpec()
		}
	}
	return specs
}

func analyze(t *testing.T, f *flow.Flow) ([]flow.ToolInfo, []flow.Issue) {
	t.Helper()
	return flow.AnalyzeTools(f, node.DefaultRegistry, toolSpecs(t, f))
}

func onlyTool(t *testing.T, f *flow.Flow) (flow.ToolInfo, []flow.Issue) {
	t.Helper()
	tools, issues := analyze(t, f)
	require.Len(t, tools, 1)
	return tools[0], issues
}

func issueCodes(issues []flow.Issue) []string {
	codes := []string{}
	for _, i := range issues {
		codes = append(codes, string(i.Severity)+":"+i.Code+"@"+i.Path)
	}
	return codes
}

func reply() riskStep { return riskStep{id: "reply", typ: "action-reply"} }

func TestToolRiskPerNodeType(t *testing.T) {
	cases := []struct {
		name  string
		steps []riskStep
		risk  node.Risk
	}{
		{"reply only", []riskStep{reply()}, node.RiskSafe},
		{"log and reply", []riskStep{{id: "log", typ: "action-log"}, reply()}, node.RiskSafe},
		{"transform and reply", []riskStep{{id: "shape", typ: "process-transform", config: map[string]any{"expression": "payload"}}, reply()}, node.RiskSafe},
		{"static http", []riskStep{{id: "http", typ: "action-http", config: map[string]any{"url": "https://api.example.com/doors"}}}, node.RiskExternal},
		{"http with a templated path stays external", []riskStep{{id: "http", typ: "action-http", config: map[string]any{"url": "https://api.example.com/doors/{{payload.door}}?q={{payload.door}}"}}}, node.RiskExternal},
		{"command", []riskStep{{id: "cmd", typ: "action-command", config: map[string]any{"command": "echo {{payload.door}}"}}}, node.RiskPrivileged},
		{"gpio", []riskStep{{id: "pin", typ: "action-gpio", config: map[string]any{"pin": "GPIO17"}}}, node.RiskPrivileged},
		{"a disabled privileged node is not reached", []riskStep{{id: "cmd", typ: "action-command", config: map[string]any{"command": "echo hi"}, disabled: true}}, node.RiskSafe},
		{"risk passes through processing nodes", []riskStep{{id: "shape", typ: "process-transform", config: map[string]any{"expression": "payload"}}, {id: "cmd", typ: "action-command", config: map[string]any{"command": "echo hi"}}}, node.RiskPrivileged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, issues := onlyTool(t, chainFlow(strictToolConfig(), tc.steps...))
			assert.Empty(t, issues)
			assert.Equal(t, tc.risk, info.Risk)
		})
	}
}

// The maximum over every branch counts, not just the reply path.
func TestToolRiskIsTheMaximumOverBranches(t *testing.T) {
	info, issues := onlyTool(t, fanFlow(strictToolConfig(),
		reply(),
		riskStep{id: "http", typ: "action-http", config: map[string]any{"url": "https://api.example.com/x"}},
	))
	assert.Empty(t, issues)
	assert.Equal(t, node.RiskExternal, info.Risk)
}

// Nodes of the flow that the tool cannot reach do not count.
func TestToolRiskIgnoresUnreachableNodes(t *testing.T) {
	f := chainFlow(strictToolConfig(), reply())
	manual := flow.NewNode("trigger-manual", flow.NodeCategoryInput)
	manual.ID = "manual"
	cmd := flow.NewNode("action-command", flow.NodeCategoryOutput)
	cmd.ID, cmd.Config = "cmd", map[string]any{"command": "echo hi"}
	f.AddNode(*manual)
	f.AddNode(*cmd)
	f.AddConnection(*flow.NewConnection("manual", "output", "cmd", "input"))

	info, issues := onlyTool(t, f)
	assert.Empty(t, issues)
	assert.Equal(t, node.RiskSafe, info.Risk)
}

func TestToolRiskDynamicDestinationsArePrivileged(t *testing.T) {
	require.NoError(t, secrets.DefaultStore.Set("discord_hook", "https://discord.com/api/webhooks/1/abc"))
	t.Cleanup(func() { _ = secrets.DefaultStore.Delete("discord_hook") })
	cases := []struct {
		name string
		step riskStep
		risk node.Risk
	}{
		{"http url from the payload", riskStep{id: "n", typ: "action-http", config: map[string]any{"url": "{{payload.url}}"}}, node.RiskPrivileged},
		{"http host from the payload", riskStep{id: "n", typ: "action-http", config: map[string]any{"url": "https://{{payload.host}}/x"}}, node.RiskPrivileged},
		{"http port from a variable", riskStep{id: "n", typ: "action-http", config: map[string]any{"url": "https://api.example.com:{{vars.port}}/x"}}, node.RiskPrivileged},
		{"http scheme from meta", riskStep{id: "n", typ: "action-http", config: map[string]any{"url": "{{meta.scheme}}://api.example.com/x"}}, node.RiskPrivileged},
		{"http base from a flow variable", riskStep{id: "n", typ: "action-http", config: map[string]any{"url": "{{flowVars.base}}/x"}}, node.RiskPrivileged},
		{"http allowPrivate", riskStep{id: "n", typ: "action-http", config: map[string]any{"url": "http://192.168.1.10/x", "allowPrivate": true}}, node.RiskPrivileged},
		{"notify host from the payload", riskStep{id: "n", typ: "action-notify", config: map[string]any{"url": "https://{{payload.host}}/hook"}}, node.RiskPrivileged},
		{"notify allowPrivate", riskStep{id: "n", typ: "action-notify", config: map[string]any{"url": "http://10.0.0.2/hook", "allowPrivate": true}}, node.RiskPrivileged},
		{"notify url from a secret is fixed", riskStep{id: "n", typ: "action-notify", config: map[string]any{"url": "{{secret.discord_hook}}"}}, node.RiskExternal},
		{"static notify", riskStep{id: "n", typ: "action-notify", config: map[string]any{"url": "https://ntfy.sh/topic", "format": "ntfy"}}, node.RiskExternal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, issues := onlyTool(t, chainFlow(strictToolConfig(), tc.step))
			assert.Empty(t, issues)
			assert.Equal(t, tc.risk, info.Risk)
		})
	}
}

// MQTT and files are shared state other flows and devices read: reaching
// them makes the tool privileged whatever the destination.
func TestToolRiskMQTTAndFileArePrivileged(t *testing.T) {
	for _, step := range []riskStep{
		{id: "mqtt", typ: "action-mqtt", config: map[string]any{"broker": "tcp://broker.local:1883", "topic": "garage/door"}},
		{id: "file", typ: "action-file", config: map[string]any{"path": "/tmp/garage.log"}},
	} {
		t.Run(step.typ, func(t *testing.T) {
			info, issues := onlyTool(t, chainFlow(strictToolConfig(), step))
			assert.Empty(t, issues)
			assert.Equal(t, node.RiskPrivileged, info.Risk)
		})
	}
}

func TestToolRiskDynamicDestinationWithStaticCredentialsFails(t *testing.T) {
	cases := []struct {
		name string
		step riskStep
		path string
	}{
		{"http header", riskStep{id: "n", typ: "action-http", config: map[string]any{"url": "https://{{payload.host}}/x", "headers": map[string]any{"Authorization": "Bearer abc123"}}}, "nodes[1].config.headers"},
		{"http userinfo", riskStep{id: "n", typ: "action-http", config: map[string]any{"url": "https://user:pw@{{payload.host}}/x"}}, "nodes[1].config.url"},
		{"mqtt password", riskStep{id: "n", typ: "action-mqtt", config: map[string]any{"broker": "tcp://{{payload.host}}:1883", "topic": "t", "password": "hunter2"}}, "nodes[1].config.password"},
		{"notify url secret", riskStep{id: "n", typ: "action-notify", config: map[string]any{"url": "https://{{payload.host}}/hook/{{secret.hook_token}}"}}, "nodes[1].config.url"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := chainFlow(strictToolConfig(), tc.step)
			_, issues := flow.AnalyzeTools(f, node.DefaultRegistry, toolSpecs(t, f))
			assert.Equal(t, []string{"error:" + flow.IssueDynamicDestinationCredentials + "@" + tc.path}, issueCodes(issues))
		})
	}

	// A fixed destination may carry credentials.
	f := chainFlow(strictToolConfig(), riskStep{id: "n", typ: "action-http", config: map[string]any{
		"url": "https://user:pw@api.example.com/x", "headers": map[string]any{"Authorization": "Bearer abc123"},
	}})
	_, issues := analyze(t, f)
	assert.Empty(t, issues)
}

func TestToolTaintGlobalVariablesFail(t *testing.T) {
	for _, cfg := range []map[string]any{
		{"key": "door"},                    // global is the default
		{"key": "door", "scope": "global"}, // explicitly global
	} {
		f := chainFlow(strictToolConfig(), riskStep{id: "set", typ: "set-var", config: cfg})
		_, issues := analyze(t, f)
		assert.Equal(t, []string{"error:" + flow.IssueToolGlobalVariable + "@nodes[1].config.scope"}, issueCodes(issues))
	}

	f := chainFlow(strictToolConfig(), riskStep{id: "set", typ: "set-var", config: map[string]any{"key": "door", "scope": "flow"}}, reply())
	info, issues := onlyTool(t, f)
	assert.Empty(t, issues)
	assert.Equal(t, node.RiskSafe, info.Risk)
	assert.False(t, info.Annotations.ReadOnlyHint, "a tool that sets a variable is not read-only")
}

func TestToolStatefulNodes(t *testing.T) {
	cases := []struct {
		name  string
		step  riskStep
		codes []string
	}{
		{"aggregate", riskStep{id: "agg", typ: "process-aggregate", config: map[string]any{"count": float64(3)}}, []string{"error:" + flow.IssueToolAggregate + "@nodes[1]"}},
		{"join by the default key", riskStep{id: "join", typ: "process-join", config: map[string]any{"mode": "key"}}, nil},
		{"join by correlation id", riskStep{id: "join", typ: "process-join", config: map[string]any{"mode": "key", "key": " meta.correlationID "}}, nil},
		{"join by another key", riskStep{id: "join", typ: "process-join", config: map[string]any{"mode": "key", "key": "payload.door"}}, []string{"error:" + flow.IssueToolJoinKey + "@nodes[1].config.key"}},
		{"short delay", riskStep{id: "wait", typ: "process-delay", config: map[string]any{"delay": float64(10000)}}, nil},
		{"delay longer than the timeout", riskStep{id: "wait", typ: "process-delay", config: map[string]any{"delay": float64(10001)}}, []string{"warning:" + flow.IssueToolDelayExceedsTimeout + "@nodes[1].config.delay"}},
		{"default delay", riskStep{id: "wait", typ: "process-delay"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := chainFlow(strictToolConfig(), tc.step)
			tools, issues := analyze(t, f)
			require.Len(t, tools, 1)
			if tc.codes == nil {
				tc.codes = []string{}
			}
			assert.Equal(t, tc.codes, issueCodes(issues))
		})
	}

	// The tool's own timeout decides.
	cfg := strictToolConfig()
	cfg["timeoutMs"] = float64(30000)
	_, issues := analyze(t, chainFlow(cfg, riskStep{id: "wait", typ: "process-delay", config: map[string]any{"delay": float64(20000)}}))
	assert.Empty(t, issues)
}

func TestPrivilegedToolsNeedStrictArguments(t *testing.T) {
	cfg := strictToolConfig()
	cfg["inputSchema"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"door":  map[string]any{"type": "string", "enum": []any{"left", "right"}},
			"note":  map[string]any{"type": "string", "maxLength": float64(20)},
			"count": map[string]any{"type": "integer", "minimum": float64(1)},
		},
	}
	cmd := riskStep{id: "cmd", typ: "action-command", config: map[string]any{"command": "echo {{payload.door}}"}}
	tools, issues := analyze(t, chainFlow(cfg, cmd))
	require.Len(t, tools, 1)
	require.Len(t, issues, 2, "%v", issues)
	for _, issue := range issues {
		assert.Equal(t, flow.IssueError, issue.Severity)
		assert.Equal(t, flow.IssueToolStrictArguments, issue.Code)
		assert.Equal(t, "nodes[0].config.inputSchema", issue.Path)
	}
	assert.Contains(t, issues[0].Message, "/count")
	assert.Contains(t, issues[1].Message, "/note")

	// The same schema is fine for a tool that is not privileged.
	_, issues = analyze(t, chainFlow(cfg, reply()))
	assert.Empty(t, issues)
}

func TestToolAnnotations(t *testing.T) {
	cases := []struct {
		name  string
		steps []riskStep
		want  flow.Annotations
	}{
		{"read-only", []riskStep{{id: "shape", typ: "process-transform", config: map[string]any{"expression": "payload"}}, {id: "log", typ: "action-log"}, reply()}, flow.Annotations{ReadOnlyHint: true}},
		{"get-var is not on the read-only list", []riskStep{{id: "get", typ: "get-var", config: map[string]any{"key": "door"}}, reply()}, flow.Annotations{}},
		{"external", []riskStep{{id: "http", typ: "action-http", config: map[string]any{"url": "https://api.example.com/x"}}}, flow.Annotations{OpenWorldHint: true}},
		{"privileged", []riskStep{{id: "cmd", typ: "action-command", config: map[string]any{"command": "echo hi"}}}, flow.Annotations{DestructiveHint: true, OpenWorldHint: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, issues := onlyTool(t, chainFlow(strictToolConfig(), tc.steps...))
			assert.Empty(t, issues)
			assert.Equal(t, tc.want, info.Annotations)
		})
	}

	cfg := strictToolConfig()
	cfg["idempotent"] = true
	info, _ := onlyTool(t, chainFlow(cfg, reply()))
	assert.True(t, info.Annotations.IdempotentHint, "the admin's flag")
}

// The four hints are always present in the JSON, even when false.
func TestToolAnnotationsAreAlwaysExplicit(t *testing.T) {
	data, err := json.Marshal(flow.Annotations{})
	require.NoError(t, err)
	assert.JSONEq(t, `{"readOnlyHint":false,"destructiveHint":false,"idempotentHint":false,"openWorldHint":false}`, string(data))
}

func TestToolRequiresUserInteraction(t *testing.T) {
	info, _ := onlyTool(t, chainFlow(strictToolConfig(), reply()))
	assert.False(t, info.RequiresUserInteraction())

	cfg := strictToolConfig()
	cfg["confirm"] = "client"
	info, _ = onlyTool(t, chainFlow(cfg, reply()))
	assert.True(t, info.RequiresUserInteraction(), "confirm client")

	cfg = strictToolConfig()
	cfg["confirm"] = "admin"
	info, _ = onlyTool(t, chainFlow(cfg, reply()))
	assert.True(t, info.RequiresUserInteraction(), "confirm admin")

	info, _ = onlyTool(t, chainFlow(strictToolConfig(), riskStep{id: "cmd", typ: "action-command", config: map[string]any{"command": "echo hi"}}))
	assert.True(t, info.RequiresUserInteraction(), "destructive")
}

func TestToolInfoFields(t *testing.T) {
	cfg := strictToolConfig()
	cfg["title"] = "Garage"
	cfg["timeoutMs"] = float64(5000)
	cfg["cooldownMs"] = float64(1500)
	cfg["confirm"] = "client"
	cfg["outputSchema"] = map[string]any{"type": "object"}
	f := chainFlow(cfg, reply())
	info, _ := onlyTool(t, f)
	assert.Equal(t, f.ID, info.FlowID)
	assert.Equal(t, "tool", info.TriggerNodeID)
	assert.Equal(t, cfg["toolName"], info.ToolName)
	assert.Equal(t, "Garage", info.Title)
	assert.Equal(t, cfg["description"], info.Description)
	require.NotNil(t, info.Schema)
	assert.JSONEq(t, `{"type":"object"}`, string(info.OutputSchema))
	assert.Equal(t, "client", info.Confirm)
	assert.Equal(t, int64(5000), info.Timeout.Milliseconds())
	assert.Equal(t, int64(1500), info.Cooldown.Milliseconds())
	assert.Equal(t, node.ToolReadOnlyMaxConcurrent, info.MaxConcurrent, "read-only tools default to 4")

	info, _ = onlyTool(t, chainFlow(strictToolConfig(), riskStep{id: "http", typ: "action-http", config: map[string]any{"url": "https://api.example.com/x"}}))
	assert.Equal(t, node.ToolDefaultMaxConcurrent, info.MaxConcurrent)

	cfg = strictToolConfig()
	cfg["maxConcurrent"] = float64(3)
	info, _ = onlyTool(t, chainFlow(cfg, reply()))
	assert.Equal(t, 3, info.MaxConcurrent, "the admin's value wins")
}

func TestToolFingerprint(t *testing.T) {
	base := strictToolConfig()
	fp := func(cfg map[string]any, steps ...riskStep) string {
		t.Helper()
		info, _ := onlyTool(t, chainFlow(cfg, steps...))
		return info.Fingerprint
	}
	clone := func(over map[string]any) map[string]any {
		c := map[string]any{}
		for k, v := range base {
			c[k] = v
		}
		for k, v := range over {
			c[k] = v
		}
		return c
	}

	first := fp(base, reply())
	assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, first)
	assert.Equal(t, first, fp(base, reply()), "stable")
	assert.Equal(t, first, fp(clone(map[string]any{"description": "Another description that is long enough.", "title": "Other", "toolName": uniqueToolName()}), reply()),
		"names and texts are not part of it")
	assert.Equal(t, first, fp(base, riskStep{id: "other", typ: "action-reply"}), "node ids are not part of it")

	changed := map[string]string{
		"risk":       fp(base, riskStep{id: "http", typ: "action-http", config: map[string]any{"url": "https://api.example.com/x"}}),
		"confirm":    fp(clone(map[string]any{"confirm": "admin"}), reply()),
		"idempotent": fp(clone(map[string]any{"idempotent": true}), reply()),
		"schema": fp(clone(map[string]any{"inputSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{"door": map[string]any{"type": "string", "enum": []any{"left", "right", "middle"}}},
		}}), reply()),
	}
	seen := map[string]string{first: "base"}
	for what, got := range changed {
		assert.NotEqual(t, first, got, what)
		if prev, dup := seen[got]; dup {
			t.Errorf("%s and %s share a fingerprint", what, prev)
		}
		seen[got] = what
	}
}

func TestToolIssuesAreReportedOncePerNode(t *testing.T) {
	// Two tools share one global set-var: one issue.
	f := chainFlow(strictToolConfig(), riskStep{id: "set", typ: "set-var", config: map[string]any{"key": "door"}})
	second := flow.NewNode("trigger-tool", flow.NodeCategoryInput)
	second.ID, second.Config = "tool2", strictToolConfig()
	f.AddNode(*second)
	f.AddConnection(*flow.NewConnection("tool2", "output", "set", "input"))
	tools, issues := analyze(t, f)
	assert.Len(t, tools, 2)
	assert.Equal(t, []string{"error:" + flow.IssueToolGlobalVariable + "@nodes[1].config.scope"}, issueCodes(issues))
}
