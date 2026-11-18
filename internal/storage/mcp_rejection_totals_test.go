package storage

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The audit page shows rejection totals per token and reason over a time
// window, summed in SQL rather than from a page of per-minute counters.
func TestMCPRejectionTotals(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	tok, _ := newTestToken(t, s, NewAPIToken{Name: "laptop"})
	require.NoError(t, s.AddMCPRejections([]MCPRejection{
		{TokenID: tok.ID, At: testNow, Reason: "busy", Count: 2},
		{TokenID: tok.ID, At: testNow.Add(time.Minute), Reason: "busy", Count: 3},
		{TokenID: tok.ID, At: testNow.Add(2 * time.Minute), Reason: "invalid_args", Count: 1},
		{TokenID: "deleted", At: testNow.Add(3 * time.Minute), Reason: "cooldown", Count: 4},
	}, testNow))

	totals, truncated, err := s.MCPRejectionTotals(MCPRejectionFilter{})
	require.NoError(t, err)
	assert.False(t, truncated)
	assert.Equal(t, []MCPRejectionTotal{
		{TokenID: tok.ID, TokenName: "laptop", Reason: "busy", Count: 5, Last: testNow.Add(time.Minute)},
		{TokenID: "deleted", TokenName: "", Reason: "cooldown", Count: 4, Last: testNow.Add(3 * time.Minute)},
		{TokenID: tok.ID, TokenName: "laptop", Reason: "invalid_args", Count: 1, Last: testNow.Add(2 * time.Minute)},
	}, totals, "largest first, with the token's name when it still exists")

	// Token and time window (since inclusive, until exclusive).
	totals, _, err = s.MCPRejectionTotals(MCPRejectionFilter{TokenID: tok.ID, Since: testNow.Add(time.Minute), Until: testNow.Add(2 * time.Minute)})
	require.NoError(t, err)
	assert.Equal(t, []MCPRejectionTotal{{TokenID: tok.ID, TokenName: "laptop", Reason: "busy", Count: 3, Last: testNow.Add(time.Minute)}}, totals)
	// A since inside a minute still counts that minute.
	totals, _, err = s.MCPRejectionTotals(MCPRejectionFilter{TokenID: tok.ID, Since: testNow.Add(90 * time.Second)})
	require.NoError(t, err)
	assert.Len(t, totals, 2)

	none, truncated, err := s.MCPRejectionTotals(MCPRejectionFilter{TokenID: "nobody"})
	require.NoError(t, err)
	assert.NotNil(t, none)
	assert.Empty(t, none)
	assert.False(t, truncated)
}

// The number of (token, reason) groups is bounded; truncation is reported.
func TestMCPRejectionTotalsAreBounded(t *testing.T) {
	s := createTestStore(t)
	defer s.Close()
	batch := make([]MCPRejection, 0, MaxMCPRejectionTotals+1)
	for i := range MaxMCPRejectionTotals + 1 {
		batch = append(batch, MCPRejection{TokenID: fmt.Sprintf("tok-%04d", i), At: testNow, Reason: "busy", Count: i + 1})
	}
	require.NoError(t, s.AddMCPRejections(batch, testNow))
	totals, truncated, err := s.MCPRejectionTotals(MCPRejectionFilter{})
	require.NoError(t, err)
	assert.True(t, truncated)
	require.Len(t, totals, MaxMCPRejectionTotals)
	assert.Equal(t, int64(MaxMCPRejectionTotals+1), totals[0].Count, "the largest totals are kept")
}

func TestMCPRejectionTotalsClosedStore(t *testing.T) {
	s := createTestStore(t)
	require.NoError(t, s.Close())
	_, _, err := s.MCPRejectionTotals(MCPRejectionFilter{})
	require.ErrorIs(t, err, ErrDatabaseClosed)
}
