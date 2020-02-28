package engine

// Event represents the different situations in which a task can be involved when the user
// data file has been modified.
type Event uint8

const (
	// Modified represents a task that has variated some of its fields.
	Modified Event = iota
	// Deleted represents a task that has been removed.
	Deleted
	// Added represents a new task.
	Added
)

// ModificationEvent is the struct used to know which action must be executed by the engine
// with the given task.
type ModificationEvent struct {
	Event  Event
	TaskID string
}
