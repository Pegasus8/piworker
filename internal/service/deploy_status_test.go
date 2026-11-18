package service

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/stretchr/testify/assert"
)

func TestDeployErrorStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"validation", flow.NewValidationError("cycle detected"), http.StatusBadRequest},
		{"wrapped validation", fmt.Errorf("deploy: %w", flow.NewValidationError("dup id")), http.StatusBadRequest},
		{"no triggers", flow.ErrNoTriggerNodes, http.StatusBadRequest},
		{"unknown node type", fmt.Errorf("%w: foo", flow.ErrInvalidNodeType), http.StatusBadRequest},
		{"invalid connection", fmt.Errorf("%w: x", flow.ErrInvalidConnection), http.StatusBadRequest},
		{"still stopping", fmt.Errorf("%w: flow a", flow.ErrFlowStillStopping), http.StatusConflict},
		{"already running", flow.ErrFlowAlreadyRunning, http.StatusConflict},
		{"invalid node config", flow.NewNodeExecutionError("n1", "action-http", fmt.Errorf("%w: url is required", flow.ErrInvalidNodeConfig)), http.StatusBadRequest},
		{"resource conflict", flow.NewNodeExecutionError("t1", "trigger-webhook", fmt.Errorf("%w: webhook path \"/x\" is already registered", flow.ErrResourceConflict)), http.StatusConflict},
		{"other", errors.New("broker exploded"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, DeployErrorStatus(tc.err))
		})
	}
}
