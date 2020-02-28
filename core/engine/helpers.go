package engine

import (
	"reflect"

	"github.com/Pegasus8/piworker/core/data"
)

// Detect the changes on the new data. This function is used when a change on the user data file
// is detected.
func detectChanges(olderData, newerData *data.UserData) []ModificationEvent {
	// Generally, the change will be only one, but if for some reason there are
	// more that one change, is better handle it all at once.
	var events []ModificationEvent

	// Check if there is an updated or deleted task.
	for _, newerTask := range newerData.Tasks {
		olderTask, _, err := olderData.GetTaskByID(newerTask.TaskInfo.ID)
		if err != nil {
			// The unique possible error is data.ErrBadTaskID, which means that the
			// task does not exist on the old data, so it was added.
			events = append(events, ModificationEvent{
				Event: Added,
				TaskID: olderTask.TaskInfo.ID,
			})
			// Avoid the rest of the flow.
			continue
		}

		// If the task exists, it may have modifications. Let's check it.
		if !reflect.DeepEqual(newerTask.TaskInfo, olderTask.TaskInfo){
			// If they are not equal, the newest task was modificated.
			events = append(events, ModificationEvent{
				Event: Modified,
				TaskID: olderTask.TaskInfo.ID,
			})
		}
		// If they are the same, the task has not been modified.
	}

	// Check if some task existed before but not anymore (if was deleted).
	for _, olderTask := range olderData.Tasks {
		_, _, err := newerData.GetTaskByID(olderTask.TaskInfo.ID)
		if err != nil {
			// The unique possible error is data.ErrBadTaskID, which means that the
			// task does not exist on the new data, so it was deleted.
			events = append(events, ModificationEvent{
				Event: Deleted,
				TaskID: olderTask.TaskInfo.ID,
			})
		}
	}

	return events
}