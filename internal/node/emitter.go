package node

import (
	"context"

	"github.com/Pegasus8/piworker/internal/types"
)

// Emitter is an optional interface for trigger nodes that can start a run on
// request (e.g. trigger-tool), which is what makes them callable through the
// runtime's synchronous Call.
//
// Emit sends msg into the trigger's output as a new run. It must block until
// the message is handed to the runtime and return early with an error when ctx
// (the caller's) or the trigger's own Start context ends, or when the trigger
// is not running. The runtime has already set the run's meta (correlationID,
// actor) and owns the payload; the trigger may add its own meta and must set
// SourcePort when it has more than one output.
type Emitter interface {
	Emit(ctx context.Context, msg *types.Message) error
}
