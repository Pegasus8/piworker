package service

import (
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tool name an agent asked for is stored capped, like the argument
// preview: an unknown name of any length cannot fill the audit.
func TestAuditedToolNameIsCapped(t *testing.T) {
	e := newEnv(t)
	agent := e.agent(storage.NewAPIToken{AllTools: true})
	_, err := e.tools.Call(t.Context(), agent, ToolByName(strings.Repeat("x", 100_000)), nil)
	require.Error(t, err)
	require.NoError(t, e.audit.Flush())
	rows, err := e.store.ListMCPAudit(storage.MCPAuditFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "not_found", rows[0].Outcome)
	assert.LessOrEqual(t, len(rows[0].Tool), maxAuditToolBytes)
	assert.True(t, strings.HasPrefix(rows[0].Tool, "xxxx"))
}
