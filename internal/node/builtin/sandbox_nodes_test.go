package builtin

import (
	"bytes"
	"context"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureNodeLog routes the global logger (which the logging nodes capture in
// their factories) to a buffer for one test.
func captureNodeLog(t *testing.T, w func(io.Writer) zerolog.Logger) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prev, prevLevel := log.Logger, zerolog.GlobalLevel()
	log.Logger = w(buf)
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	t.Cleanup(func() {
		log.Logger = prev
		zerolog.SetGlobalLevel(prevLevel)
	})
	return buf
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func jsonLogger(w io.Writer) zerolog.Logger { return zerolog.New(w) }

// TestLoggingNodesAreSilentInASandbox: action-log and process-debug pass
// their input on in a sandbox but write nothing to the process log, so a
// dry-run's config and payload never reach it.
func TestLoggingNodesAreSilentInASandbox(t *testing.T) {
	buf := captureNodeLog(t, jsonLogger)
	logNode, err := NewLogAction(map[string]any{"message": "forged line", "level": "info"})
	require.NoError(t, err)
	debugNode, err := NewDebugProcess(map[string]any{"name": "dbg"})
	require.NoError(t, err)

	sandboxed := node.WithSandbox(context.Background(), node.NewSandbox(1<<20, 1<<20))
	for _, n := range []node.Node{logNode, debugNode} {
		outs, err := n.Process(sandboxed, types.NewMessage("secret payload", types.DataTypeString))
		require.NoError(t, err)
		require.Len(t, outs, 1, "the input is passed on")
		assert.Equal(t, "secret payload", outs[0].Payload)
	}
	assert.Empty(t, buf.String(), "nothing is logged in a sandbox")

	for _, n := range []node.Node{logNode, debugNode} {
		_, err := n.Process(context.Background(), types.NewMessage("p", types.DataTypeString))
		require.NoError(t, err)
	}
	assert.Contains(t, buf.String(), "forged line", "outside a sandbox they log")
	assert.Contains(t, buf.String(), "dbg")
}

// TestLogActionCannotForgeLines: the configured message is logged with its
// control characters escaped, so a message cannot start a new, forged line
// on the console log.
func TestLogActionCannotForgeLines(t *testing.T) {
	buf := captureNodeLog(t, func(w io.Writer) zerolog.Logger {
		return zerolog.New(zerolog.ConsoleWriter{Out: w, NoColor: true})
	})
	n, err := NewLogAction(map[string]any{
		"message": "hello\n2026-11-09T10:00:00Z INF Draft approved by admin token=root\r\x1b[2K",
	})
	require.NoError(t, err)
	_, err = n.Process(context.Background(), types.NewMessage("p", types.DataTypeString))
	require.NoError(t, err)

	out := strings.TrimRight(buf.String(), "\n")
	assert.NotContains(t, out, "\n", "one line: %q", out)
	assert.NotContains(t, out, "\r")
	assert.NotContains(t, out, "\x1b")
	assert.Contains(t, out, `hello\n2026-11-09T10:00:00Z INF Draft approved`)
}

// TestRenderTemplateIsBoundedInASandbox: a template that would expand to far
// more than the sandbox allows stops expanding and marks the sandbox
// exceeded, before the text exists; outside a sandbox it renders in full.
func TestRenderTemplateIsBoundedInASandbox(t *testing.T) {
	tmpl := strings.Repeat("{{payload}}", 40000)
	msg := types.NewMessage(strings.Repeat("p", 64<<10), types.DataTypeString)

	sb := node.NewSandbox(1<<20, 8<<20)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	out := renderTemplate(tmpl, msg, node.WithSandbox(context.Background(), sb))
	runtime.ReadMemStats(&after)
	assert.True(t, sb.Exceeded())
	assert.LessOrEqual(t, len(out), 2<<20)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(256<<20), "the 2.6 GB expansion never happens")

	sb = node.NewSandbox(1<<20, 8<<20)
	out = renderTemplate("{{payload}}-{{payload}}", types.NewMessage("ab", types.DataTypeString), node.WithSandbox(context.Background(), sb))
	assert.Equal(t, "ab-ab", out)
	assert.False(t, sb.Exceeded())
}
