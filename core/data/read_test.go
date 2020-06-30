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

	for i, ts := range suite.TestTasks {
		for i2 := range ts {
			var currentState TaskState

			// If the status is not one of the admitted (which are `StateTaskActive` and `StateTaskInactive`), it will be
			// changed to an admitted one and then, updated to the original value. This is to avoid the restriction in the
			// function `NewTask`.
			if !(suite.TestTasks[i][i2].State == StateTaskActive || suite.TestTasks[i][i2].State == StateTaskInactive) {
				currentState = suite.TestTasks[i][i2].State
				suite.TestTasks[i][i2].State = StateTaskInactive
			}

			err = NewTask(&suite.TestTasks[i][i2])
			if err != nil {
				panic(err)
			}

			if currentState == "" {
				continue
			}

			// Restore the state of the task.
			suite.TestTasks[i][i2].State = currentState
			err = UpdateTaskState(suite.TestTasks[i][i2].ID, currentState)
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
			", all tasks should be read from the database without any issue")
	}

	// Set IDs and add tasks to a common slice.
	var tasksSlice []*UserTask
	for i := range suite.TestTasks {
		for i2 := range suite.TestTasks[i] {
			tasksSlice = append(tasksSlice, &suite.TestTasks[i][i2])
		}
	}

	// Check that each task from the slice has been saved and correctly obtained from the database.
	// We will iterate over the slice of our original data to check if each task is in the returned slice read from the
	// database.
	for i, t := range tasksSlice {
		assert.True(foundTask(tasks, t), "The task %d should be read correctly from the"+
			" database", i)
	}
}

func (suite *ReadTestSuite) TestGetTaskByName() {
	assert := assert2.New(suite.T())

	t, err := GetTaskByName(suite.TestTasks[0][0].Name)
	assert.NoError(err, "The task should be obtained by its name without errors")
	assert.True(foundTask(&suite.TestTasks[0], t), "The task returned should be the same that the one we have")

	_, err = GetTaskByName(suite.TestTasks[0][0].Name + "hello")
	assert.Error(err, "If the name of the requested task does not exist, an error should be returned")
}

func (suite *ReadTestSuite) TestGetTaskByID() {
	assert := assert2.New(suite.T())

	t, err := GetTaskByID(suite.TestTasks[0][1].ID)
	assert.NoError(err, "The task should be returned without errors")
	assert.True(foundTask(&suite.TestTasks[0], t), "The task returned should be the same that the one we have")

	_, err = GetTaskByID(suite.TestTasks[0][1].ID + "a")
	assert.Error(err, "If the ID of the requested task does not exist, an error should be returned")
}

func (suite *ReadTestSuite) TestGetActiveTasks() {
	assert := assert2.New(suite.T())
	atLen := len(suite.TestTasks[0])

	at, err := GetActiveTasks()
	assert.NoError(err, "Active tasks should be returned without errors")
	if !assert.Lenf(*at, atLen, "The number of returned tasks should be %d", atLen) {
		assert.FailNow("The test can't continue if tasks can't be read correctly", "To continue,"+
			" active tasks should be read from the database without any issue")
	}

	for i, t := range *at {
		assert.Truef(foundTask(&suite.TestTasks[0], &t), "The active task %d should be read correctly from the"+
			" database", i)
	}
}

func (suite *ReadTestSuite) TestGetInactiveTasks() {
	assert := assert2.New(suite.T())
	itLen := len(suite.TestTasks[1])

	it, err := GetInactiveTasks()
	assert.NoError(err, "Inactive tasks should be returned without errors")
	if !assert.Lenf(*it, itLen, "The number of returned tasks should be %d", itLen) {
		assert.FailNow("The test can't continue if tasks can't be read correctly", "To continue,"+
			" inactive tasks should be read from the database without any issue")
	}

	for i, t := range *it {
		assert.Truef(foundTask(&suite.TestTasks[1], &t), "The inactive task %d should be read correctly from the"+
			" database", i)
	}
}

func (suite *ReadTestSuite) TestGetFailedTasks() {
	assert := assert2.New(suite.T())
	ftLen := len(suite.TestTasks[2])

	ft, err := GetFailedTasks()
	assert.NoError(err, "Failed tasks should be returned without errors")
	if !assert.Lenf(*ft, ftLen, "The number of returned tasks should be %d", ftLen) {
		assert.FailNow("The test can't continue if tasks can't be read correctly", "To continue,"+
			" failed tasks should be read from the database without any issue")
	}

	for i, t := range *ft {
		assert.Truef(foundTask(&suite.TestTasks[2], &t), "The failed task %d should be read correctly from the"+
			" database", i)
	}
}

func (suite *ReadTestSuite) TestGetOnExecutionTasks() {
	assert := assert2.New(suite.T())
	oetLen := len(suite.TestTasks[3])

	oet, err := GetOnExecutionTasks()
	assert.NoError(err, "On-execution tasks should be returned without errors")
	if !assert.Lenf(*oet, oetLen, "The number of returned tasks should be %d", oetLen) {
		assert.FailNow("The test can't continue if tasks can't be read correctly", "To continue,"+
			" on-execution tasks for testing should be read from the database without any issue")
	}

	for i, t := range *oet {
		assert.Truef(foundTask(&suite.TestTasks[3], &t), "The task on-execution %d should be read correctly from the"+
			" database", i)
	}
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

func foundTask(origin *[]UserTask, taskToCheck *UserTask) bool {
	var taskFound bool

	for _, t := range *origin {
		if t.ID == taskToCheck.ID {
			taskFound = true

			if t.Name != taskToCheck.Name {
				return false
			}

			if t.State != taskToCheck.State {
				return false
			}

			if !t.Created.Equal(taskToCheck.Created) {
				return false
			}

			if !t.LastTimeModified.Equal(taskToCheck.LastTimeModified) {
				return false
			}

			if len(t.Actions) != len(taskToCheck.Actions) {
				return false
			}

			if !equalTrigger(&t.Trigger, &taskToCheck.Trigger) {
				return false
			}

			if !equalActions(&t.Actions, &taskToCheck.Actions) {
				return false
			}

			break
		}
	}

	return taskFound
}

func equalActions(originActions, actionsToCheck *[]UserAction) bool {
	if len(*originActions) != len(*actionsToCheck) {
		return false
	}

	for _, oa := range *originActions {
		var actionFound bool

		for _, fta := range *actionsToCheck {
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

func equalTrigger(originTrigger, triggerToCheck *UserTrigger) bool {
	if triggerToCheck.ID != originTrigger.ID {
		return false
	}

	if triggerToCheck.Timestamp != originTrigger.Timestamp {
		return false
	}

	for _, ota := range originTrigger.Args {
		var argFound bool

		for _, fttArg := range triggerToCheck.Args {
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
