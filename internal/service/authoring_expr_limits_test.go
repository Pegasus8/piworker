package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateRefusesDeeplyNestedExpressions: pw_validate_flow with a
// transform nesting 255,000 parentheses (510 KB, within MaxDraftFlowBytes)
// made Lint's node factory grow a goroutine stack to 512 MiB (a fatal stack
// overflow on 32-bit builds) and answered valid. The flow is invalid now,
// at once.
func TestValidateRefusesDeeplyNestedExpressions(t *testing.T) {
	e := newAuthEnv(t)
	f := safeFlow()
	nodeOf(f, "calc").Config["expression"] = strings.Repeat("(", 255000) + "1" + strings.Repeat(")", 255000)

	start := time.Now()
	res, err := e.a.Validate(context.Background(), e.token(), ValidateRequest{Flow: f})
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 2*time.Second)
	assert.False(t, res.Valid)
	issue := findIssue(res.Issues, flow.IssueInvalidConfig)
	require.NotNil(t, issue, "issues: %v", issueCodes(res.Issues))
	assert.Contains(t, issue.Path, "calc")
}
