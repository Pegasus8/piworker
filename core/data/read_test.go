package data

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	assert2 "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ReadTestSuite struct {
	TestDir   string
	TestTasks [4][]UserTask
	TotalLen  int

	suite.Suite
}

func (suite *ReadTestSuite) SetupTest() {
	suite.TestDir = "./test"
	Path = suite.TestDir

	err := os.Mkdir(suite.TestDir, 0755)
	if err != nil {
		panic(err)
	}

	suite.TestTasks = [4][]UserTask{
		// Tasks with state active
		{
			{
				Name:  "Task 1",
				State: StateTaskActive,
				Trigger: UserTrigger{
					ID: "T1",
					Args: []UserArg{
						{
							ID:      "T1-1",
							Content: "Arg 1",
						},
						{
							ID:      "T1-2",
							Content: "Arg 2",
						},
						{
							ID:      "T1-3",
							Content: "Arg 3",
						},
					},
					Timestamp: "",
				},
				Actions: []UserAction{
					{
						ID: "A1",
						Args: []UserArg{
							{
								ID:      "A1-1",
								Content: "A1 Arg 1",
							},
							{
								ID:      "A1-2",
								Content: "A1 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 0,
					},
					{
						ID: "A3",
						Args: []UserArg{
							{
								ID:      "A3-1",
								Content: "A3 Arg 1",
							},
							{
								ID:      "A3-2",
								Content: "A3 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 1,
					},
				},
				Created:          time.Now(),
				LastTimeModified: time.Now(),
				ID:               "",
			},
			{
				Name:  "Task 2",
				State: StateTaskActive,
				Trigger: UserTrigger{
					ID: "T3",
					Args: []UserArg{
						{
							ID:      "T3-1",
							Content: "Arg 1",
						},
						{
							ID:      "T3-2",
							Content: "Arg 2",
						},
						{
							ID:      "T3-3",
							Content: "Arg 3",
						},
					},
					Timestamp: "",
				},
				Actions: []UserAction{
					{
						ID: "A5",
						Args: []UserArg{
							{
								ID:      "A5-1",
								Content: "A5 Arg 1",
							},
							{
								ID:      "A5-2",
								Content: "A5 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 0,
					},
					{
						ID: "A3",
						Args: []UserArg{
							{
								ID:      "A3-1",
								Content: "A3 Arg 1",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 1,
					},
				},
				Created:          time.Now(),
				LastTimeModified: time.Now(),
				ID:               "",
			},
			{
				Name:  "Task 3",
				State: StateTaskActive,
				Trigger: UserTrigger{
					ID: "T1",
					Args: []UserArg{
						{
							ID:      "T1-1",
							Content: "Arg 1",
						},
						{
							ID:      "T1-2",
							Content: "Arg 2",
						},
						{
							ID:      "T1-3",
							Content: "Arg 3",
						},
					},
					Timestamp: "",
				},
				Actions: []UserAction{
					{
						ID: "A1",
						Args: []UserArg{
							{
								ID:      "A1-1",
								Content: "A1 Arg 1",
							},
							{
								ID:      "A1-2",
								Content: "A1 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 0,
					},
					{
						ID: "A3",
						Args: []UserArg{
							{
								ID:      "A3-1",
								Content: "A3 Arg 1",
							},
							{
								ID:      "A3-2",
								Content: "A3 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 1,
					},
				},
				Created:          time.Now(),
				LastTimeModified: time.Now(),
				ID:               "",
			},
		},
		// Tasks with state inactive
		{
			{
				Name:  "Task 4",
				State: StateTaskInactive,
				Trigger: UserTrigger{
					ID: "T1",
					Args: []UserArg{
						{
							ID:      "T1-1",
							Content: "Arg 1",
						},
						{
							ID:      "T1-2",
							Content: "Arg 2",
						},
						{
							ID:      "T1-3",
							Content: "Arg 3",
						},
					},
					Timestamp: "",
				},
				Actions: []UserAction{
					{
						ID: "A1",
						Args: []UserArg{
							{
								ID:      "A1-1",
								Content: "A1 Arg 1",
							},
							{
								ID:      "A1-2",
								Content: "A1 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 0,
					},
					{
						ID: "A3",
						Args: []UserArg{
							{
								ID:      "A3-1",
								Content: "A3 Arg 1",
							},
							{
								ID:      "A3-2",
								Content: "A3 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 1,
					},
				},
				Created:          time.Now(),
				LastTimeModified: time.Now(),
				ID:               "",
			},
			{
				Name:  "Task 5",
				State: StateTaskInactive,
				Trigger: UserTrigger{
					ID: "T3",
					Args: []UserArg{
						{
							ID:      "T3-1",
							Content: "Arg 1",
						},
						{
							ID:      "T3-2",
							Content: "Arg 2",
						},
						{
							ID:      "T3-3",
							Content: "Arg 3",
						},
					},
					Timestamp: "",
				},
				Actions: []UserAction{
					{
						ID: "A5",
						Args: []UserArg{
							{
								ID:      "A5-1",
								Content: "A5 Arg 1",
							},
							{
								ID:      "A5-2",
								Content: "A5 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 0,
					},
					{
						ID: "A3",
						Args: []UserArg{
							{
								ID:      "A3-1",
								Content: "A3 Arg 1",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 1,
					},
				},
				Created:          time.Now(),
				LastTimeModified: time.Now(),
				ID:               "",
			},
			{
				Name:  "Task 6",
				State: StateTaskInactive,
				Trigger: UserTrigger{
					ID: "T1",
					Args: []UserArg{
						{
							ID:      "T1-1",
							Content: "Arg 1",
						},
						{
							ID:      "T1-2",
							Content: "Arg 2",
						},
						{
							ID:      "T1-3",
							Content: "Arg 3",
						},
					},
					Timestamp: "",
				},
				Actions: []UserAction{
					{
						ID: "A1",
						Args: []UserArg{
							{
								ID:      "A1-1",
								Content: "A1 Arg 1",
							},
							{
								ID:      "A1-2",
								Content: "A1 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 0,
					},
					{
						ID: "A3",
						Args: []UserArg{
							{
								ID:      "A3-1",
								Content: "A3 Arg 1",
							},
							{
								ID:      "A3-2",
								Content: "A3 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 1,
					},
				},
				Created:          time.Now(),
				LastTimeModified: time.Now(),
				ID:               "",
			},
		},
		// Tasks with state failed
		{
			{
				Name:  "Task 7",
				State: StateTaskFailed,
				Trigger: UserTrigger{
					ID: "T1",
					Args: []UserArg{
						{
							ID:      "T1-1",
							Content: "Arg 1",
						},
						{
							ID:      "T1-2",
							Content: "Arg 2",
						},
						{
							ID:      "T1-3",
							Content: "Arg 3",
						},
					},
					Timestamp: "",
				},
				Actions: []UserAction{
					{
						ID: "A1",
						Args: []UserArg{
							{
								ID:      "A1-1",
								Content: "A1 Arg 1",
							},
							{
								ID:      "A1-2",
								Content: "A1 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 0,
					},
					{
						ID: "A3",
						Args: []UserArg{
							{
								ID:      "A3-1",
								Content: "A3 Arg 1",
							},
							{
								ID:      "A3-2",
								Content: "A3 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 1,
					},
				},
				Created:          time.Now(),
				LastTimeModified: time.Now(),
				ID:               "",
			},
			{
				Name:  "Task 8",
				State: StateTaskFailed,
				Trigger: UserTrigger{
					ID: "T3",
					Args: []UserArg{
						{
							ID:      "T3-1",
							Content: "Arg 1",
						},
						{
							ID:      "T3-2",
							Content: "Arg 2",
						},
						{
							ID:      "T3-3",
							Content: "Arg 3",
						},
					},
					Timestamp: "",
				},
				Actions: []UserAction{
					{
						ID: "A5",
						Args: []UserArg{
							{
								ID:      "A5-1",
								Content: "A5 Arg 1",
							},
							{
								ID:      "A5-2",
								Content: "A5 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 0,
					},
					{
						ID: "A3",
						Args: []UserArg{
							{
								ID:      "A3-1",
								Content: "A3 Arg 1",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 1,
					},
				},
				Created:          time.Now(),
				LastTimeModified: time.Now(),
				ID:               "",
			},
			{
				Name:  "Task 9",
				State: StateTaskFailed,
				Trigger: UserTrigger{
					ID: "T1",
					Args: []UserArg{
						{
							ID:      "T1-1",
							Content: "Arg 1",
						},
						{
							ID:      "T1-2",
							Content: "Arg 2",
						},
						{
							ID:      "T1-3",
							Content: "Arg 3",
						},
					},
					Timestamp: "",
				},
				Actions: []UserAction{
					{
						ID: "A1",
						Args: []UserArg{
							{
								ID:      "A1-1",
								Content: "A1 Arg 1",
							},
							{
								ID:      "A1-2",
								Content: "A1 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 0,
					},
					{
						ID: "A3",
						Args: []UserArg{
							{
								ID:      "A3-1",
								Content: "A3 Arg 1",
							},
							{
								ID:      "A3-2",
								Content: "A3 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 1,
					},
				},
				Created:          time.Now(),
				LastTimeModified: time.Now(),
				ID:               "",
			},
		},
		// Tasks with state on execution
		{
			{
				Name:  "Task 10",
				State: StateTaskOnExecution,
				Trigger: UserTrigger{
					ID: "T1",
					Args: []UserArg{
						{
							ID:      "T1-1",
							Content: "Arg 1",
						},
						{
							ID:      "T1-2",
							Content: "Arg 2",
						},
						{
							ID:      "T1-3",
							Content: "Arg 3",
						},
					},
					Timestamp: "",
				},
				Actions: []UserAction{
					{
						ID: "A1",
						Args: []UserArg{
							{
								ID:      "A1-1",
								Content: "A1 Arg 1",
							},
							{
								ID:      "A1-2",
								Content: "A1 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 0,
					},
					{
						ID: "A3",
						Args: []UserArg{
							{
								ID:      "A3-1",
								Content: "A3 Arg 1",
							},
							{
								ID:      "A3-2",
								Content: "A3 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 1,
					},
				},
				Created:          time.Now(),
				LastTimeModified: time.Now(),
				ID:               "",
			},
			{
				Name:  "Task 11",
				State: StateTaskOnExecution,
				Trigger: UserTrigger{
					ID: "T3",
					Args: []UserArg{
						{
							ID:      "T3-1",
							Content: "Arg 1",
						},
						{
							ID:      "T3-2",
							Content: "Arg 2",
						},
						{
							ID:      "T3-3",
							Content: "Arg 3",
						},
					},
					Timestamp: "",
				},
				Actions: []UserAction{
					{
						ID: "A5",
						Args: []UserArg{
							{
								ID:      "A5-1",
								Content: "A5 Arg 1",
							},
							{
								ID:      "A5-2",
								Content: "A5 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 0,
					},
					{
						ID: "A3",
						Args: []UserArg{
							{
								ID:      "A3-1",
								Content: "A3 Arg 1",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 1,
					},
				},
				Created:          time.Now(),
				LastTimeModified: time.Now(),
				ID:               "",
			},
			{
				Name:  "Task 12",
				State: StateTaskOnExecution,
				Trigger: UserTrigger{
					ID: "T1",
					Args: []UserArg{
						{
							ID:      "T1-1",
							Content: "Arg 1",
						},
						{
							ID:      "T1-2",
							Content: "Arg 2",
						},
						{
							ID:      "T1-3",
							Content: "Arg 3",
						},
					},
					Timestamp: "",
				},
				Actions: []UserAction{
					{
						ID: "A1",
						Args: []UserArg{
							{
								ID:      "A1-1",
								Content: "A1 Arg 1",
							},
							{
								ID:      "A1-2",
								Content: "A1 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 0,
					},
					{
						ID: "A3",
						Args: []UserArg{
							{
								ID:      "A3-1",
								Content: "A3 Arg 1",
							},
							{
								ID:      "A3-2",
								Content: "A3 Arg 2",
							},
						},
						Timestamp:             "",
						Chained:               false,
						ArgumentToReplaceByCR: "",
						Order:                 1,
					},
				},
				Created:          time.Now(),
				LastTimeModified: time.Now(),
				ID:               "",
			},
		},
	}

	var l int
	for _, ts := range suite.TestTasks {
		l += len(ts)
	}
	suite.TotalLen = l
}

func (suite *ReadTestSuite) BeforeTest(_, _ string) {
	EventBus = make(chan Event)

	db, err := InitDB(filepath.Join(Path, Filename))
	if err != nil {
		panic(err)
	}
	DB = db

	err = CreateTable()
	if err != nil {
		panic(err)
	}

	go func() {
		for i := 0; i < suite.TotalLen; i++ {
			<-EventBus
		}
	}()

	for _, ts := range suite.TestTasks {
		for _, t := range ts {
			var currentState TaskState

			// If the status is not one of the admitted (which are `StateTaskActive` and `StateTaskInactive`), it will be
			// changed to an admitted one and then, updated to the original value. This is to avoid the restriction in the
			// function `NewTask`.
			if !(t.State == StateTaskActive || t.State == StateTaskInactive) {
				currentState = t.State
				t.State = StateTaskInactive
			}

			err = NewTask(&t)
			if err != nil {
				panic(err)
			}

			if currentState == "" {
				continue
			}

			task, err := getTask(t.Name)
			if err != nil {
				panic(err)
			}

			err = UpdateTaskState(task.ID, currentState)
			if err != nil {
				panic(err)
			}
		}
	}

}

func (suite *ReadTestSuite) TestGetTasks() {
	assert := assert2.New(suite.T())

	tasks, err := GetTasks()
	assert.NoError(err, "The tasks should be obtained without errors")
	if !assert.Len(*tasks, suite.TotalLen, "The number of returned tasks is not what it should be") {
		assert.FailNow("Test can't continue if tasks can't be read correctly", "To continue"+
			" the tests all tasks should be read from the database without any issue")
	}

	// Set IDs and add tasks to a common slice.
	var tasksSlice []*UserTask
	for i := range suite.TestTasks {
		for i2 := range suite.TestTasks[i] {
			// Obtain the entire task from the database to get the ID.
			t2, err := getTask(suite.TestTasks[i][i2].Name)
			if err != nil {
				panic(err)
			}

			// Set the ID.
			suite.TestTasks[i][i2].ID = t2.ID

			tasksSlice = append(tasksSlice, &suite.TestTasks[i][i2])
		}
	}

	// Check that each task from the slice has been saved and correctly obtained from the database.
	// We will iterate over the slice of our original data to check if each task is in the returned slice read from the
	// database.
	for i, t := range tasksSlice {
		assert.Equalf(true, foundTask(tasks, t), "The task %d should be read correctly from the"+
			" database", i)
	}
}

func (suite *ReadTestSuite) TestGetTaskByName() {

}

func (suite *ReadTestSuite) TestGetTaskByID() {

}

func (suite *ReadTestSuite) TestGetActiveTasks() {

}

func (suite *ReadTestSuite) TestGetInactiveTasks() {

}

func (suite *ReadTestSuite) TestGetFailedTasks() {

}

func (suite *ReadTestSuite) TestGetOnExecutionTasks() {

}

func (suite *ReadTestSuite) TearDownTest() {
	err := os.RemoveAll(suite.TestDir)
	if err != nil {
		panic(err)
	}
}

func TestReadSuite(t *testing.T) {
	suite.Run(t, new(ReadTestSuite))
}

func foundTask(origin *[]UserTask, toFind *UserTask) bool {
	var taskFound bool

	for _, t := range *origin {
		if t.ID == toFind.ID {
			taskFound = true

			if t.Name != toFind.Name {
				return false
			}

			if t.State != toFind.State {
				return false
			}

			if !t.Created.Equal(toFind.Created) {
				return false
			}

			if !t.LastTimeModified.Equal(toFind.LastTimeModified) {
				return false
			}

			if len(t.Actions) != len(toFind.Actions) {
				return false
			}

			if !equalTrigger(&t.Trigger, &toFind.Trigger) {
				return false
			}

			if !equalActions(&t.Actions, &toFind.Actions) {
				return false
			}

			break
		}
	}

	return taskFound
}

func equalActions(originActions, foundTaskActions *[]UserAction) bool {
	if len(*originActions) != len(*foundTaskActions) {
		return false
	}

	for _, oa := range *originActions {
		var actionFound bool

		for _, fta := range *foundTaskActions {
			if fta.ID == oa.ID {
				actionFound = true

				if fta.Chained != oa.Chained {
					return false
				}

				if fta.ArgumentToReplaceByCR != oa.ArgumentToReplaceByCR {
					return false
				}

				if fta.Order != oa.Order {
					return false
				}

				if fta.Timestamp != oa.Timestamp {
					return false
				}

				if len(fta.Args) != len(oa.Args) {
					return false
				}

				for _, ftaArg := range fta.Args {
					var argFound bool
					for _, oaArg := range oa.Args {
						if ftaArg.ID == oaArg.ID {
							argFound = true

							if ftaArg.Content != oaArg.Content {
								return false
							}

							break
						}
					}

					if !argFound {
						return false
					}
				}

				break
			}
		}

		if !actionFound {
			return false
		}
	}

	return true
}

func equalTrigger(originTrigger, foundTaskTrigger *UserTrigger) bool {
	if foundTaskTrigger.ID != originTrigger.ID {
		return false
	}

	if foundTaskTrigger.Timestamp != originTrigger.Timestamp {
		return false
	}

	for _, ota := range originTrigger.Args {
		var argFound bool

		for _, fttArg := range foundTaskTrigger.Args {
			if ota.ID == fttArg.ID {
				argFound = true

				if ota.Content != fttArg.Content {
					return false
				}

				break
			}
		}

		if !argFound {
			return false
		}
	}

	return true
}
