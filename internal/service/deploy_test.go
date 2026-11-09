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
// ToolError maps it to the kind agents see (ADR-0006 statuses).
func TestLifecycleErrorToolError(t *testing.T) {
	cause := fmt.Errorf("deploy: %w", flow.ErrResourceConflict)
	cases := []struct {
		status int
		kind   error
	}{
		{http.StatusBadRequest, ErrInvalidArguments},
		{http.StatusNotFound, ErrFlowNotFound},
		{http.StatusConflict, ErrConflict},
		{http.StatusServiceUnavailable, ErrUnavailable},
	}
	for _, tc := range cases {
		le := &LifecycleError{Status: tc.status, Message: "refused: url https://user:pw@example.com", Err: cause}
		te := le.ToolError()
		require.NotNil(t, te, "%d", tc.status)
		assert.ErrorIs(t, te, tc.kind, "%d", tc.status)
		if tc.status == http.StatusBadRequest {
			assert.NotContains(t, te.Message, "pw@", "deploy's validation messages may quote config values")
		} else {
			assert.Equal(t, le.Message, te.Message)
		}
		assert.ErrorIs(t, le, flow.ErrResourceConflict, "the cause stays reachable")
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
