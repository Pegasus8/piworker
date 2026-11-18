package flow_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTestNodeBoundsArrayBuiltins: pw_test_node with concat over 30 copies
// of a 999,990-element range allocated gigabytes past the node test's
// deadline (an OOM kill on a Pi; the finding's 1000 copies on any machine)
// with no sandbox refusal. It fails at once now, and the sandbox is free
// for the next call.
func TestTestNodeBoundsArrayBuiltins(t *testing.T) {
	waitFreeSandbox(t)
	copies := strings.TrimSuffix(strings.Repeat("r, ", 30), ", ")
	for _, src := range []string{
		"let r = 1..999990; len(concat(" + copies + "))",
		"let r = 1..999000; len(flatten([" + copies + "]))",
	} {
		start := time.Now()
		res, err := flow.TestNode(context.Background(), node.DefaultRegistry, "process-transform",
			map[string]any{"expression": src}, 1)
		require.NoError(t, err, src)
		assert.NotEmpty(t, res.Error, src)
		assert.False(t, res.TimedOut, src)
		assert.Less(t, time.Since(start), time.Second, src)

		_, err = flow.TestNode(context.Background(), node.DefaultRegistry, "process-transform",
			map[string]any{"expression": "payload"}, 1)
		require.NoError(t, err, "the sandbox is free right after")
	}
}

// TestTestNodeUniqFreesTheSandbox: uniq(1..40000) kept running after its
// node test timed out, holding the process-wide sandbox for 20 s (every
// other dry-run and node test was busy). It stops at the deadline now.
func TestTestNodeUniqFreesTheSandbox(t *testing.T) {
	waitFreeSandbox(t)
	flow.SetTestNodeTimeout(t, 200*time.Millisecond)
	res, err := flow.TestNode(context.Background(), node.DefaultRegistry, "process-transform",
		map[string]any{"expression": "len(uniq(1..40000))"}, 1)
	require.NoError(t, err)
	assert.True(t, res.TimedOut)

	deadline := time.Now().Add(time.Second)
	for {
		_, err = flow.TestNode(context.Background(), node.DefaultRegistry, "process-transform",
			map[string]any{"expression": "payload"}, 1)
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NoError(t, err, "the sandbox is free within a second of the deadline")
}
