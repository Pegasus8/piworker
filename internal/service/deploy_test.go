package service

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A refused lifecycle operation keeps the status the editor path answers;
// ToolError maps it to the kind agents see (ADR-0006 statuses), always with
// a fixed message: the lifecycle path's messages quote deploy's errors,
// which may quote config values agents may not read (a webhook path, a
// URL's credentials).
func TestLifecycleErrorToolError(t *testing.T) {
	const leak = `Failed to deploy flow: node execution error [trigger-webhook:hook]: resource already in use: webhook path "zz-hidden-path-4f2a9c" is already registered, url https://user:pw@example.com`
	cases := []struct {
		name   string
		status int
		cause  error
		kind   error
		says   string
	}{
		{"invalid", http.StatusBadRequest, flow.ErrInvalidNodeConfig, ErrInvalidArguments, "pw_validate_flow"},
		{"unknown flow", http.StatusNotFound, nil, ErrFlowNotFound, "no longer exists"},
		{"resource taken", http.StatusConflict, fmt.Errorf("deploy: %w", flow.ErrResourceConflict), ErrConflict, "webhook path or tool name"},
		{"already running", http.StatusConflict, flow.ErrFlowAlreadyRunning, ErrConflict, "already running"},
		{"not running", http.StatusConflict, flow.ErrFlowNotRunning, ErrConflict, "not running"},
		{"still stopping", http.StatusConflict, fmt.Errorf("deploy: %w", flow.ErrFlowStillStopping), ErrConflict, "still stopping"},
		{"another conflict", http.StatusConflict, errors.New("something else"), ErrConflict, "conflicts"},
		{"shutting down", http.StatusServiceUnavailable, nil, ErrUnavailable, "shutting down"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			le := &LifecycleError{Status: tc.status, Message: leak, Err: tc.cause}
			te := le.ToolError()
			require.NotNil(t, te)
			assert.ErrorIs(t, te, tc.kind)
			assert.Contains(t, te.Message, tc.says)
			assert.NotContains(t, te.Message, "zz-hidden-path")
			assert.NotContains(t, te.Message, "pw@")
			if tc.cause != nil {
				assert.ErrorIs(t, le, tc.cause, "the cause stays reachable")
			}
		})
	}
	internal := &LifecycleError{Status: http.StatusInternalServerError, Message: "Failed to persist flow state"}
	assert.Nil(t, internal.ToolError(), "an internal failure is not a refusal")
	assert.Equal(t, "Failed to persist flow state", internal.Error())

	var nilErr *LifecycleError
	assert.Nil(t, nilErr.ToolError())
	assert.True(t, errors.As(fmt.Errorf("wrapped: %w", internal), &nilErr))
}

// The editor deploys as the editor; an agent as its token. The marker
// storage records says who.
func TestDeployActor(t *testing.T) {
	assert.Equal(t, DeployActor{Kind: ActorEditor}, EditorActor().DeployActor())
	agent := Actor{Kind: ActorAgent, TokenID: "t1", TokenName: "claude"}
	by := agent.DeployActor()
	assert.Equal(t, DeployActor{Kind: ActorAgent, TokenID: "t1", TokenName: "claude"}, by)
	d := by.Deployment(4)
	assert.Equal(t, 4, d.Version)
	assert.Equal(t, "agent", d.ActorKind)
	assert.Equal(t, "t1", d.TokenID)
	assert.Equal(t, "claude", d.TokenName)
	assert.True(t, d.DeployedAt.IsZero(), "storage stamps it")
	assert.Equal(t, "editor", EditorActor().DeployActor().Deployment(1).ActorKind)
}
