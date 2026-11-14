package mcpserver

import (
	"errors"
	"sync"
	"testing"

	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A deploy tool or prompt without the agent deploy level would never be
// listed (no reader is off): a wiring mistake New refuses rather than
// hiding the tools.
func TestDeployToolsNeedTheDeployLevel(t *testing.T) {
	opts := Options{Tools: &fakeTools{}, Tokens: newFakeVerifier(), StaticTools: DeployTools(&fakeAuthoring{})}
	_, err := New(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DeployLevel")

	prompt := StaticPrompt{Prompt: AuthorPrompts(nil)[0].Prompt, Scope: storage.ScopeDeploy, Handler: AuthorPrompts(nil)[0].Handler}
	_, err = New(Options{Tools: &fakeTools{}, Tokens: newFakeVerifier(), Prompts: []StaticPrompt{prompt}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DeployLevel")

	opts.DeployLevel = &fakeDeployLevel{level: storage.AgentDeployOff}
	_, err = New(opts)
	assert.NoError(t, err)
}

// New logs once what the settings let it serve, so a wiring mistake (author
// tools enabled but not registered, a level that cannot be read) shows.
func TestNewLogsWhatTheGatesServe(t *testing.T) {
	logs := &syncBuffer{}
	_, err := New(Options{Tools: &fakeTools{}, Tokens: newFakeVerifier(), Logger: zerolog.New(logs),
		StaticTools:   append(AuthorTools(&fakeAuthoring{}, testNodeTypes(t), nil), DeployTools(&fakeAuthoring{})...),
		AuthorEnabled: true, DeployLevel: &fakeDeployLevel{level: storage.AgentDeployLow}})
	require.NoError(t, err)
	out := logs.String()
	assert.Contains(t, out, `"authorTools":6`)
	assert.Contains(t, out, `"authorEnabled":true`)
	assert.Contains(t, out, `"deployTools":2`)
	assert.Contains(t, out, `"deployLevelReader":true`)

	logs = &syncBuffer{}
	_, err = New(Options{Tools: &fakeTools{}, Tokens: newFakeVerifier(), Logger: zerolog.New(logs), AuthorEnabled: true})
	require.NoError(t, err)
	assert.Contains(t, logs.String(), "no author tool is registered")

	logs = &syncBuffer{}
	level := &fakeDeployLevel{err: errors.New("database is locked")}
	_, err = New(Options{Tools: &fakeTools{}, Tokens: newFakeVerifier(), Logger: zerolog.New(logs), DeployLevel: level})
	require.NoError(t, err)
	assert.Contains(t, logs.String(), "no deploy tool is registered")
	assert.Zero(t, level.reads.Load(), "the level is read per tokens epoch, not at startup")
}

// The authoring service audits its refusals as rows but counts none:
// the endpoint counts a built-in tool's invalid_arguments refusals, exactly
// once, like its own schema refusals (contract E). Other refusals are not
// rejections.
func TestServiceInvalidArgumentsAreCounted(t *testing.T) {
	a := newAuthorEnv(t)
	a.authoring.err = &service.ToolError{Kind: service.ErrInvalidArguments, Message: "a sensitive setting holds a literal"}
	res, body := called(t, a.testEnv, everythingToken, "pw_save_draft", `{"flow":{"nodes":[]},"name":"x","note":"n"}`)
	require.Equal(t, true, res["isError"])
	assert.Equal(t, "invalid_arguments", errorCode(body))
	assert.Equal(t, []string{"everythingeveryt invalid_arguments " + t0.Format("2006-01-02T15:04:05Z07:00")}, a.rejections.seen)

	a.authoring.err = &service.ToolError{Kind: service.ErrPermissionDenied, Message: "the agent deploy level is low"}
	res, _ = called(t, a.testEnv, everythingToken, "pw_deploy_flow", `{"flowId":"f1"}`)
	require.Equal(t, true, res["isError"])
	assert.Len(t, a.rejections.seen, 1, "forbidden is audited by the service, not counted")
}

// pw_list_node_types and the author_flow prompt run no service, so the
// endpoint audits them itself, as the services audit reads: the token, the
// name (prompts as prompt:<name>), the arguments' digest and preview
// (never the goal), the outcome and the duration.
func TestNodeTypesAndAuthorFlowAreAudited(t *testing.T) {
	audit := &fakeAudit{}
	a := newAuthorEnv(t, func(o *Options) {
		o.StaticTools = append(AuthorTools(&fakeAuthoring{}, testNodeTypes(t), audit), DeployTools(&fakeAuthoring{})...)
		o.Prompts = AuthorPrompts(audit)
	})
	res, _ := called(t, a.testEnv, authorToken, "pw_list_node_types", `{"category":"input"}`)
	require.NotEqual(t, true, res["isError"])
	_, rpcErr := rpcResult(t, a.serve(withToken(rpc(`{"jsonrpc":"2.0","id":6,"method":"prompts/get","params":{"name":"author_flow","arguments":{"goal":"open the garage at 7"}}}`), authorToken)))
	require.Nil(t, rpcErr)

	rows := audit.recorded()
	require.Len(t, rows, 2)
	assert.Equal(t, "pw_list_node_types", rows[0].Tool)
	assert.Equal(t, "author", rows[0].TokenName)
	assert.NotEmpty(t, rows[0].TokenID)
	assert.Equal(t, "completed", rows[0].Outcome)
	assert.Equal(t, `{"category":"input"}`, rows[0].ArgsPreview)
	assert.Len(t, rows[0].ArgsSHA256, 64)
	assert.Equal(t, "prompt:author_flow", rows[1].Tool)
	assert.Equal(t, "completed", rows[1].Outcome)
	assert.NotContains(t, rows[1].ArgsPreview, "garage", "the goal is agent text, not recorded")

	// A refused call is counted, not audited (as the tool service does).
	res, _ = called(t, a.testEnv, authorToken, "pw_list_node_types", `{"type":"no-such-type"}`)
	require.Equal(t, true, res["isError"])
	assert.Len(t, audit.recorded(), 2)
}

// fakeAudit records the audit rows the endpoint writes itself.
type fakeAudit struct {
	mu   sync.Mutex
	rows []storage.MCPAuditEntry
}

func (f *fakeAudit) Record(e storage.MCPAuditEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, e)
}

func (f *fakeAudit) recorded() []storage.MCPAuditEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]storage.MCPAuditEntry(nil), f.rows...)
}
