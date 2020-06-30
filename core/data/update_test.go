package data

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	assert2 "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type UpdateTestSuite struct {
	TestDir   string
	TestTasks []UserTask
	suite.Suite
}

func (suite *UpdateTestSuite) SetupTest() {
	suite.TestDir = "./test"
	Path = suite.TestDir

	err := os.Mkdir(suite.TestDir, 0755)
	if err != nil {
		panic(err)
	}

	suite.TestTasks = []UserTask{
		{
			Name:  "Task 1",
			State: "active",
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
			State: "inactive",
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
	}
}

func (suite *UpdateTestSuite) BeforeTest(_, _ string) {
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
		for i := 0; i < len(suite.TestTasks); i++ {
			<-EventBus
		}
	}()

	for i := range suite.TestTasks {
		err = NewTask(&suite.TestTasks[i])
		if err != nil {
			panic(err)
		}
	}
}

func (suite *UpdateTestSuite) TestUpdate() {
	suite.UpdateTask()
	suite.UpdateTaskState()
}

func (suite *UpdateTestSuite) UpdateTask() {
	assert := assert2.New(suite.T())
	var end = make(chan struct{})

	go func() {
		for {
			select {
			case event := <-EventBus:
				assert.Equal(Modified, event.Type, "When a task is updated the emitted event should be of"+
					" type `Modified`")
				assert.Equal(suite.TestTasks[0].ID, event.TaskID, "The ID in the event must be the same that the ID of the"+
					" task affected")
			case <-end:
				break
			}
		}
	}()

	// --- Test 1 ---
	// Task should be updated without problems.
	updatedTask := suite.TestTasks[0]
	updatedTask.State = StateTaskInactive
	updatedTask.Name = "Task 12345"
	err := UpdateTask(suite.TestTasks[0].ID, &updatedTask)
	assert.NoError(err, "The task should be updated without errors")

	updatedTaskFromDB, err := getTask(updatedTask.Name)
	if err != nil {
		panic(err)
	}

	// Compare fields of type `time.Time`
	if assert.True(updatedTask.Created.Equal(updatedTaskFromDB.Created), "The created time must be stored "+
		"correctly in the database") {
		// Avoid a false-positive on the next comparison of the entire structs (caused by a mismatching of
		// metadata in `time.Time` fields).
		updatedTask.Created = updatedTaskFromDB.Created
		updatedTask.LastTimeModified = updatedTaskFromDB.LastTimeModified

		// NOTE: I'm not comparing the field `LastTimeModified` because that field is modified inside the function.
		// Theoretically, if the comparison of the fields `Created` is successful, that other field shouldn't have any
		// issue.
	}

	assert.Equal(updatedTask, *updatedTaskFromDB, "The task must be updated in the database")
	// --- End of Test 1 ---

	// --- Test 2 ---
	// The usage of an non-existent ID must return an error.
	err = UpdateTask(suite.TestTasks[0].ID+"a", &suite.TestTasks[0])
	assert.Error(err, "Try to use an ID that does not exist should return an error")
	// --- End of Test 2 ---

	// --- Test 3 ---
	// The usage of an empty critical field must return an error.
	err = UpdateTask(suite.TestTasks[0].ID, &UserTask{})
	assert.Error(err, "If some critical field (the ID of an action for example) is empty, the "+
		"function should return an error")
	// --- End of Test 3 ---

	end <- struct{}{}
}

func (suite *UpdateTestSuite) UpdateTaskState() {
	assert := assert2.New(suite.T())
	var end = make(chan struct{})

	go func() {
		for {
			select {
			case event := <-EventBus:
				assert.Equal(Modified, event.Type, "When a task is updated the emitted event should be of"+
					" type `Modified`")
				assert.Equal(suite.TestTasks[1].ID, event.TaskID, "The ID in the event must be the same that the ID of"+
					" the task affected")
			case <-end:
				break
			}
		}
	}()

	// --- Test 1 ---
	// Task state should be updated without problems.
	err := UpdateTaskState(suite.TestTasks[1].ID, StateTaskActive)
	assert.NoError(err, "The task should be updated without errors")

	updatedTaskFromDB, err := getTask(suite.TestTasks[1].Name)
	if err != nil {
		panic(err)
	}

	assert.Equal(StateTaskActive, updatedTaskFromDB.State, "The state of the task must be updated in the"+
		" database")
	// --- End of Test 1 ---

	// --- Test 2 ---
	// The usage of an non-existent ID must return an error.
	err = UpdateTaskState(suite.TestTasks[1].ID+"a", StateTaskActive)
	assert.Error(err, "Try to use an ID that does not exist should return an error")
	// --- End of Test 2 ---

	// --- Test 3 ---
	// The usage of an non-existent state must return an error.
	err = UpdateTaskState(suite.TestTasks[1].ID, "random-state")
	assert.Error(err, "Try to use an state that does not exist should return an error")
	// --- End of Test 3 ---

	end <- struct{}{}
}

func (suite *UpdateTestSuite) TearDownTest() {
	err := os.RemoveAll(suite.TestDir)
	if err != nil {
		panic(err)
	}
}

func TestUpdateSuite(t *testing.T) {
	suite.Run(t, new(UpdateTestSuite))
}
