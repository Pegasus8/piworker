package flow

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunFinishesWithDisabledTarget: a connection into a disabled node must not
// count toward the run, otherwise the run's in-flight counter never drains.
func TestRunFinishesWithDisabledTarget(t *testing.T) {
	reg := testRegistry(t, 1, map[string]node.NodeFactory{"pass": passNode()})

	f := NewFlow("disabled-target")
	trig := NewNode("emit", NodeCategoryInput)
	live := passPorts(NewNode("pass", NodeCategoryProcessing))
	off := passPorts(NewNode("pass", NodeCategoryProcessing))
	off.Enabled = false
	f.AddNode(*trig)
	f.AddNode(*live)
	f.AddNode(*off)
	f.AddConnection(*NewConnection(trig.ID, "output", live.ID, "input"))
	f.AddConnection(*NewConnection(trig.ID, "output", off.ID, "input"))
	f.AddConnection(*NewConnection(live.ID, "output", off.ID, "input"))

	hook, read := collectAll()
	events := deployAndWait(t, f, reg, read, hook, 1)
	assert.Equal(t, 1, countPhase(events, NodePhaseRunFinished))
	assert.Equal(t, 1, countPhase(events, NodePhaseSuccess), "only the enabled node ran")
}

// TestRouterReportsTimeoutDrops: a message dropped on send timeout is handed to
// the drop handler so the runtime can release it from its run.
func TestRouterReportsTimeoutDrops(t *testing.T) {
	var dropped atomic.Int32
	var lastTarget atomic.Value
	router := NewMessageRouter([]Connection{
		{ID: "c1", SourceNode: "src", SourcePort: "output", TargetNode: "dst", TargetPort: "input"},
	}, zerolog.Nop(), WithSendTimeout(5*time.Millisecond), WithDropHandler(func(rm RoutedMessage) {
		dropped.Add(1)
		lastTarget.Store(rm.TargetNode)
	}))
	defer router.Close()

	// Nobody reads Output(): fill the buffer, then one more must time out.
	for i := 0; i < DefaultOutputBuffer+1; i++ {
		msg := NewMessage(i, DataTypeNumber)
		msg.SourcePort = "output"
		router.Route("src", msg)
	}
	require.Equal(t, int32(1), dropped.Load())
	assert.Equal(t, "dst", lastTarget.Load())
	assert.Equal(t, int64(1), router.MessagesDropped())
}
