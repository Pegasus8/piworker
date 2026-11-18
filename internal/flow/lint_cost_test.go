package flow_test

import (
	"runtime"
	"strconv"
	"testing"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mistypedHTTPNode is an action-http node whose ten declared settings all
// have the wrong type (ten config_type issues on one node) and whose
// undeclared setting "zz" holds leaves: what an author token can send in one
// pw_validate_flow of about 480 KB.
func mistypedHTTPNode(leaves []any) flow.Node {
	cfg := map[string]any{"zz": leaves}
	for _, p := range []string{"url", "method", "headers", "body", "bodyType", "outputMode"} {
		cfg[p] = 1.0
	}
	for _, p := range []string{"timeout", "maxResponseSize", "allowPrivate", "retryable"} {
		cfg[p] = "x"
	}
	return lintNode("big", "action-http", cfg)
}

// allocatedBy returns the bytes fn allocates.
func allocatedBy(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// Lint hides the hidden settings of a node from each of its issues: the cost
// must not grow with issues × hidden leaves (an agent's pw_validate_flow of
// one 480 KB flow used to allocate 3.7 GB and take seconds, on a Pi tens of
// CPU-seconds).
func TestLintCostDoesNotMultiplyIssuesByHiddenLeaves(t *testing.T) {
	cases := map[string][]any{
		"repeated leaves": func() []any {
			out := make([]any, 240_000)
			for i := range out {
				out[i] = 1.0
			}
			return out
		}(),
		"distinct leaves": func() []any {
			out := make([]any, 60_000)
			for i := range out {
				out[i] = strconv.Itoa(100_000 + i)
			}
			return out
		}(),
	}
	for name, leaves := range cases {
		t.Run(name, func(t *testing.T) {
			f := flow.NewFlow("big")
			f.Nodes = []flow.Node{
				lintNode("start", "trigger-manual", nil),
				mistypedHTTPNode(leaves),
			}
			f.Connections = []flow.Connection{lintConn("c1", "start", "output", "big", "input")}
			var issues []flow.Issue
			alloc := allocatedBy(func() { issues = flow.Lint(f, node.DefaultRegistry) })
			configType := 0
			for _, is := range issues {
				if is.Code == flow.IssueConfigType {
					configType++
				}
			}
			require.Equal(t, 10, configType, "%v", issueKeys(issues))
			assert.Less(t, alloc, uint64(400<<20), "Lint allocated %d MiB", alloc>>20)
		})
	}
}
