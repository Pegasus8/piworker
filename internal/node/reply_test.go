package node

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingReplier struct{ got []string }

func (r *recordingReplier) Reply(id string, _ any, _ bool) bool {
	r.got = append(r.got, id)
	return true
}

func TestReplierFromContext(t *testing.T) {
	none := ReplierFromContext(context.Background())
	require.NotNil(t, none, "never nil, so a reply node can call it unconditionally")
	assert.False(t, none.Reply("run", 1, false))

	rec := &recordingReplier{}
	ctx := WithReplier(context.Background(), rec)
	assert.True(t, ReplierFromContext(ctx).Reply("run-1", 1, false))
	assert.Equal(t, []string{"run-1"}, rec.got)

	assert.False(t, ReplierFromContext(WithReplier(context.Background(), nil)).Reply("x", 1, false))
}
