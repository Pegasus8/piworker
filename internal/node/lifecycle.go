package node

// Closer is an optional interface for nodes that hold resources such as
// network clients, open files or connections. The runtime calls Close when a
// flow stops, after the flow's executors have drained, on every node (trigger
// or not) that implements it. Errors are logged by the runtime; Close should
// release what it can and not block for long.
type Closer interface {
	Close() error
}
