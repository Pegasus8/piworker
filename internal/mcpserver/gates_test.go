package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Pegasus8/piworker/internal/agentauth"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/service"
	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Raw tokens of the gate tests, registered on the fake verifier by
// withGateTokens.
const (
	deployToken       = "pwk_deploydeploydepl_" + "gggggggggggggggggggggggggggggggggggggggggggggggggggg"
	authorDeployToken = "pwk_authordeployauth_" + "hhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhh"
	everythingToken   = "pwk_everythingeveryt_" + "iiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiiii"
)

func withGateTokens(v *fakeVerifier) {
	for raw, info := range map[string]agentauth.TokenInfo{
		deployToken:       {ID: "deploydeploydepl", Name: "deployer", Scopes: []string{storage.ScopeDeploy}},
		authorDeployToken: {ID: "authordeployauth", Name: "builder", Scopes: []string{storage.ScopeAuthor, storage.ScopeDeploy}},
		everythingToken: {ID: "everythingeveryt", Name: "all", AllTools: true,
			Scopes: []string{storage.ScopeCall, storage.ScopeRead, storage.ScopeAuthor, storage.ScopeDeploy}},
	} {
		v.set(raw, func(i *agentauth.TokenInfo) { *i = info })
	}
}

// fakeDeployLevel is the stored agent deploy level; reads counts the reads.
type fakeDeployLevel struct {
	mu    sync.Mutex
	level storage.AgentDeployLevel
	err   error
	reads atomic.Int32
}

func (f *fakeDeployLevel) AgentDeployLevel() (storage.AgentDeployLevel, error) {
	f.reads.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return storage.AgentDeployOff, f.err
	}
	if f.level == "" {
		return storage.AgentDeployOff, nil
	}
	return f.level, nil
}

func (f *fakeDeployLevel) set(level storage.AgentDeployLevel, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.level, f.err = level, err
}

// scopedTool is a built-in tool of scope that answers its name; handled
// counts its calls.
func scopedTool(name, scope string, handled *atomic.Int32) StaticTool {
	return StaticTool{
		Name: name, Title: name, Description: "A tool of the " + scope + " scope.",
		InputSchema: json.RawMessage(noArguments),
		Scope:       scope,
		Handler: func(context.Context, service.Actor, map[string]any) (*mcp.CallToolResult, error) {
			handled.Add(1)
			return StructuredResult(map[string]any{"tool": name})
		},
	}
}

func scopedPrompt(name, scope string) StaticPrompt {
	return StaticPrompt{
		Prompt: &mcp.Prompt{Name: name, Description: "A prompt of the " + scope + " scope."},
		Scope:  scope,
		Handler: func(context.Context, service.Actor, map[string]string) (*mcp.GetPromptResult, error) {
			return &mcp.GetPromptResult{Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: name}}}}, nil
		},
	}
}

type gateEnv struct {
	*testEnv
	level   *fakeDeployLevel
	handled *atomic.Int32
}

func newGateEnv(t *testing.T, authorEnabled bool, customize ...func(*Options)) gateEnv {
	t.Helper()
	g := gateEnv{level: &fakeDeployLevel{}, handled: &atomic.Int32{}}
	g.testEnv = newTestEnv(t, append([]func(*Options){func(o *Options) {
		o.StaticTools = []StaticTool{scopedTool("pw_author_thing", storage.ScopeAuthor, g.handled),
			scopedTool("pw_deploy_thing", storage.ScopeDeploy, g.handled)}
		o.Prompts = []StaticPrompt{scopedPrompt("author_prompt", storage.ScopeAuthor), scopedPrompt("deploy_prompt", storage.ScopeDeploy)}
		o.AuthorEnabled = authorEnabled
		o.DeployLevel = g.level
		o.RequestBurst, o.RequestsPerMinute = 1000, 1000
	}}, customize...)...)
	withGateTokens(g.verifier)
	return g
}

// listedPrompts returns the prompt names of a prompts/list answer.
func listedPrompts(t *testing.T, e *testEnv, raw string) []string {
	t.Helper()
	res, rpcErr := rpcResult(t, e.serve(withToken(rpc(`{"jsonrpc":"2.0","id":5,"method":"prompts/list"}`), raw)))
	require.Nil(t, rpcErr)
	var out []string
	for _, p := range res["prompts"].([]any) {
		out = append(out, p.(map[string]any)["name"].(string))
	}
	return out
}

// The author scope is served only with mcp.author_enabled, the deploy scope
// only with an agent deploy level above off: a built-in tool or prompt of a
// scope that is not served is neither listed nor callable, as if the token
// lacked the scope.
func TestAuthorAndDeployScopesAreGated(t *testing.T) {
	levels := []storage.AgentDeployLevel{storage.AgentDeployOff, storage.AgentDeployLow, storage.AgentDeployAll}
	for _, authorEnabled := range []bool{false, true} {
		for _, level := range levels {
			t.Run(fmt.Sprintf("author=%v/deploy=%s", authorEnabled, level), func(t *testing.T) {
				g := newGateEnv(t, authorEnabled)
				g.level.set(level, nil)
				deployOn := level != storage.AgentDeployOff
				for _, tc := range []struct {
					raw            string
					author, deploy bool // the token's scopes
					other          bool // call or read
				}{
					{authorToken, true, false, false},
					{deployToken, false, true, false},
					{authorDeployToken, true, true, false},
					{everythingToken, true, true, true},
					{callToken, false, false, true},
				} {
					wantAuthor, wantDeploy := tc.author && authorEnabled, tc.deploy && deployOn
					if !wantAuthor && !wantDeploy && !tc.other {
						msg := requireJSONError(t, g.serve(withToken(rpc(listToolsBody), tc.raw)), http.StatusForbidden)
						assert.Contains(t, msg, "scope", tc.raw)
						continue
					}
					tools := names(listed(t, g.testEnv, tc.raw))
					assert.Equal(t, wantAuthor, slices.Contains(tools, "pw_author_thing"), tc.raw)
					assert.Equal(t, wantDeploy, slices.Contains(tools, "pw_deploy_thing"), tc.raw)
					prompts := listedPrompts(t, g.testEnv, tc.raw)
					assert.Equal(t, wantAuthor, slices.Contains(prompts, "author_prompt"), tc.raw)
					assert.Equal(t, wantDeploy, slices.Contains(prompts, "deploy_prompt"), tc.raw)
				}

				// Called anyway, a gated tool is an unknown name to the token:
				// the tool service's not_found, the handler never runs.
				g.tools.call = func(_ context.Context, _ service.Actor, ref service.ToolRef, _ map[string]any) (flow.CallResult, error) {
					return flow.CallResult{}, &service.ToolError{Kind: service.ErrToolNotFound, Message: fmt.Sprintf("unknown tool %q", ref.Name)}
				}
				for tool, served := range map[string]bool{"pw_author_thing": authorEnabled, "pw_deploy_thing": deployOn} {
					before := g.handled.Load()
					res, body := called(t, g.testEnv, everythingToken, tool, `{}`)
					if served {
						assert.NotEqual(t, true, res["isError"], tool)
						assert.Equal(t, before+1, g.handled.Load(), tool)
					} else {
						assert.Equal(t, "not_found", errorCode(body), tool)
						assert.Equal(t, before, g.handled.Load(), tool)
					}
				}
			})
		}
	}
}

// Tokens without the deploy scope never make the endpoint read the level;
// the others read it once per tokens epoch (changing the level bumps the
// epoch), so a level change shows once the verifier sees the new epoch.
func TestDeployLevelIsReadOncePerEpoch(t *testing.T) {
	g := newGateEnv(t, true)
	for range 3 {
		listed(t, g.testEnv, callToken)
		listed(t, g.testEnv, authorToken)
	}
	assert.Zero(t, g.level.reads.Load(), "no deploy scope, no read")

	requireJSONError(t, g.serve(withToken(rpc(listToolsBody), deployToken)), http.StatusForbidden) // off by default
	assert.NotContains(t, names(listed(t, g.testEnv, authorDeployToken)), "pw_deploy_thing")
	assert.EqualValues(t, 1, g.level.reads.Load())

	g.level.set(storage.AgentDeployLow, nil)
	assert.NotContains(t, names(listed(t, g.testEnv, authorDeployToken)), "pw_deploy_thing", "the epoch has not moved yet")
	g.verifier.bumpEpoch()
	assert.Contains(t, names(listed(t, g.testEnv, deployToken)), "pw_deploy_thing", "rebuilt with the new level")
	assert.Contains(t, names(listed(t, g.testEnv, authorDeployToken)), "pw_deploy_thing")
	assert.EqualValues(t, 2, g.level.reads.Load())

	g.level.set(storage.AgentDeployOff, nil)
	g.verifier.bumpEpoch()
	msg := requireJSONError(t, g.serve(withToken(rpc(listToolsBody), deployToken)), http.StatusForbidden)
	assert.Contains(t, msg, "scope", "a deploy-only token has nothing to be served with the level off")
	assert.NotContains(t, names(listed(t, g.testEnv, authorDeployToken)), "pw_deploy_thing")
}

// A level that cannot be read is off (fail closed) and logged, and is not
// remembered: the next request reads it again.
func TestDeployLevelReadFailureFailsClosed(t *testing.T) {
	g := newGateEnv(t, true)
	g.level.set(storage.AgentDeployAll, errors.New("database is locked"))
	tools := names(listed(t, g.testEnv, authorDeployToken))
	assert.NotContains(t, tools, "pw_deploy_thing")
	assert.Contains(t, tools, "pw_author_thing")
	assert.Contains(t, g.logs.String(), "database is locked")

	g.level.set(storage.AgentDeployAll, nil)
	assert.Contains(t, names(listed(t, g.testEnv, authorDeployToken)), "pw_deploy_thing", "read again, same epoch")
}

// Without a level reader the deploy scope is never served.
func TestNoDeployLevelReaderServesNoDeployTools(t *testing.T) {
	g := newGateEnv(t, true, func(o *Options) { o.DeployLevel = nil })
	assert.NotContains(t, names(listed(t, g.testEnv, everythingToken)), "pw_deploy_thing")
}
