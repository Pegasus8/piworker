package data

// UserData is the general struct for parsing data
type UserData struct {
	Tasks []UserTask `json:"user-data"` 
}

// UserTask is the structure used for parsing all the tasks
type UserTask struct {
	TaskInfo TaskInfo `json:"task"`
}

// TaskInfo is a struct for parsing every task
type TaskInfo struct {
	Name string `json:"name"`
	State string `json:"state"`
	Trigger UserTrigger `json:"trigger"`
	Actions []UserAction `json:"actions"` 
}

// UserTrigger is a struct for parsing every trigger
type UserTrigger struct {
	ID string `json:"ID"`
	Args []UserArg `json:"args"`
	Timestamp string `json:"timestamp"`
}

// UserAction is a struct for parsing every action
type UserAction struct {
	ID string `json:"ID"`
	Args []UserArg `json:"args"`
	Timestamp string `json:"timestamp"`
	Chained bool `json:"chained"`
	Order int `json:"order"`
}

// UserArg is a struct for arg parsing
type UserArg struct {
	ID string `json:"ID"`
	Content string `json:"content"`
}

