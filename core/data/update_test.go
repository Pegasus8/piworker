package data

import (
	"os"
	"testing"
	"time"

	assert2 "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type UpdateTestSuite struct {
	TestDir      string
	TestFilename string
	TestDB       *DatabaseInstance
	TestTasks    []UserTask
	suite.Suite
}

func (s *UpdateTestSuite) SetupTest() {
	s.TestDir = "./test_update"
	s.TestFilename = "update.db"

	err := os.Mkdir(s.TestDir, 0755)
	if err != nil {
		panic(err)
	}

	s.TestTasks = []UserTask{
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

func (s *UpdateTestSuite) BeforeTest(_, _ string) {
	db, err := NewDB(s.TestDir, s.TestFilename)
	if err != nil {
		panic(err)
	}

	go func() {
		for i := 0; i < len(s.TestTasks); i++ {
			<-db.EventBus
		}
	}()

	for i := range s.TestTasks {
		err = db.NewTask(&s.TestTasks[i])
		if err != nil {
			panic(err)
		}
	}

	s.TestDB = db
}

func (s *UpdateTestSuite) TestUpdateTask() {
	assert := assert2.New(s.T())
	var end = make(chan struct{})

	go func() {
		for {
			select {
			case event := <-s.TestDB.EventBus:
				assert.Equal(Modified, event.Type, "When a task is updated the emitted event should be of"+
					" type `Modified`")
			case <-end:
				break
			}
		}
	}()

	// --- Test 1 ---
	// Task should be updated without problems.
	updatedTask := s.TestTasks[0]
	updatedTask.State = StateTaskInactive
	updatedTask.Name = "Task 12345"
	err := s.TestDB.UpdateTask(s.TestTasks[0].ID, &updatedTask)
	assert.NoError(err, "The task should be updated without errors")

	updatedTaskFromDB, err := getTask(s.TestDB, updatedTask.Name)
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
	err = s.TestDB.UpdateTask(s.TestTasks[0].ID+"a", &s.TestTasks[0])
	assert.Error(err, "Try to use an ID that does not exist should return an error")
	// --- End of Test 2 ---

	// --- Test 3 ---
	// The usage of an empty critical field must return an error.
	err = s.TestDB.UpdateTask(s.TestTasks[0].ID, &UserTask{})
	assert.Error(err, "If some critical field (the ID of an action for example) is empty, the "+
		"function should return an error")
	// --- End of Test 3 ---

	end <- struct{}{}
}

func (s *UpdateTestSuite) TestUpdateTaskState() {
	assert := assert2.New(s.T())
	var end = make(chan struct{})

	go func() {
		for {
			select {
			case event := <-s.TestDB.EventBus:
				assert.Equal(Modified, event.Type, "When a task is updated the emitted event should be of"+
					" type `Modified`")
			case <-end:
				break
			}
		}
	}()

	// --- Test 1 ---
	// Task state should be updated without problems.
	err := s.TestDB.UpdateTaskState(s.TestTasks[1].ID, StateTaskActive)
	assert.NoError(err, "The task should be updated without errors")

	updatedTaskFromDB, err := getTask(s.TestDB, s.TestTasks[1].Name)
	if err != nil {
		panic(err)
	}

	assert.Equal(StateTaskActive, updatedTaskFromDB.State, "The state of the task must be updated in the"+
		" database")
	// --- End of Test 1 ---

	// --- Test 2 ---
	// The usage of an non-existent ID must return an error.
	err = s.TestDB.UpdateTaskState(s.TestTasks[1].ID+"a", StateTaskActive)
	assert.Error(err, "Try to use an ID that does not exist should return an error")
	// --- End of Test 2 ---

	// --- Test 3 ---
	// The usage of an non-existent state must return an error.
	err = s.TestDB.UpdateTaskState(s.TestTasks[1].ID, "random-state")
	assert.Error(err, "Try to use an state that does not exist should return an error")
	// --- End of Test 3 ---

	end <- struct{}{}
}

func (s *UpdateTestSuite) TearDownTest() {
	err := os.RemoveAll(s.TestDir)
	if err != nil {
		panic(err)
	}
}

func TestUpdateSuite(t *testing.T) {
	suite.Run(t, new(UpdateTestSuite))
}
