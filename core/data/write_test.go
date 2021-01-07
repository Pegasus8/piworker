package data

import (
	"os"
	"testing"
	"time"

	assert2 "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type WriteTestSuite struct {
	TestDir      string
	TestFilename string
	TestDB       *DatabaseInstance
	TestTasks    []UserTask

	suite.Suite
}

func (s *WriteTestSuite) SetupTest() {
	s.TestDir = "./test_write"
	s.TestFilename = "write.db"

	err := os.Mkdir(s.TestDir, 0755)
	if err != nil {
		panic(err)
	}

	s.TestTasks = []UserTask{
		{
			Name:  "My custom task 1",
			State: "active",
			Trigger: UserTrigger{
				ID: "T1",
				Args: []UserArg{
					{
						ID:      "T1-1",
						Content: "First argument",
					},
					{
						ID:      "T1-2",
						Content: "Second argument",
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
							Content: "My first argument",
						},
						{
							ID:      "A1-2",
							Content: "My second argument",
						},
						{
							ID:      "A1-3",
							Content: "My third argument",
						},
					},
					Timestamp:             "",
					Chained:               false,
					ArgumentToReplaceByCR: "",
					Order:                 0,
				},
				{
					ID: "A2",
					Args: []UserArg{
						{
							ID:      "A2-1",
							Content: "My first argument 2",
						},
						{
							ID:      "A2-2",
							Content: "My second argument 2",
						},
					},
					Timestamp:             "",
					Chained:               true,
					ArgumentToReplaceByCR: "A2-1",
					Order:                 1,
				},
			},
			Created:          time.Now(),
			LastTimeModified: time.Now(),
			ID:               "",
		},
		{
			Name:  "My custom task 2",
			State: "inactive",
			Trigger: UserTrigger{
				ID: "T1",
				Args: []UserArg{
					{
						ID:      "T1-1",
						Content: "First argument",
					},
					{
						ID:      "T1-2",
						Content: "Second argument",
					},
				},
				Timestamp: "",
			},
			Actions: []UserAction{
				{
					ID: "A3",
					Args: []UserArg{
						{
							ID:      "A3-1",
							Content: "My first argument 3",
						},
						{
							ID:      "A3-2",
							Content: "My second argument 3",
						},
						{
							ID:      "A3-3",
							Content: "My third argument 3",
						},
					},
					Timestamp:             "",
					Chained:               false,
					ArgumentToReplaceByCR: "",
					Order:                 0,
				},
			},
			Created:          time.Now(),
			LastTimeModified: time.Now(),
			ID:               "",
		},
		{
			Name:  "My custom task 3",
			State: "inactive",
			Trigger: UserTrigger{
				ID: "T1",
				Args: []UserArg{
					{
						ID:      "T1-1",
						Content: "First argument",
					},
					{
						ID:      "T1-2",
						Content: "Second argument",
					},
				},
				Timestamp: "",
			},
			Actions: []UserAction{
				{
					ID: "A3",
					Args: []UserArg{
						{
							ID:      "A3-1",
							Content: "My first argument 3",
						},
						{
							ID:      "A3-2",
							Content: "My second argument 3",
						},
						{
							ID:      "A3-3",
							Content: "My third argument 3",
						},
					},
					Timestamp:             "",
					Chained:               false,
					ArgumentToReplaceByCR: "",
					Order:                 0,
				},
			},
			Created:          time.Now(),
			LastTimeModified: time.Now(),
			ID:               "",
		},
	}
}

func (s *WriteTestSuite) BeforeTest(_, _ string) {
	db, err := NewDB(s.TestDir, s.TestFilename)
	if err != nil {
		panic(err)
	}

	s.TestDB = db
}

func (s *WriteTestSuite) TestNewTask() {
	assert := assert2.New(s.T())

	// Receive the events sent by the `NewTask` function.
	go func() {
		var nEvents = len(s.TestTasks) - 1

		for i := 0; i <= nEvents; i++ {
			event := <-s.TestDB.EventBus

			assert.Equal(Added, event.Type, "The emitted event when a task is created successfully must "+
				"be of type `Added`")
			assert.Equal(s.TestTasks[i].ID, event.TaskID, "The task ID emitted with the event must be "+
				"the same that the ID of the recently added task")
		}
	}()

	// Tasks should be added without problems.
	for i := range s.TestTasks {
		err := s.TestDB.NewTask(&s.TestTasks[i])

		assert.NoError(err, "The task should be added without problems")
	}

	// Empty fields should cause an error.
	err := s.TestDB.NewTask(&UserTask{})

	assert.Error(err, "If the task contains an empty field it shouldn't be added")
	assert.EqualError(err, ErrIntegrity.Error(), "The returned error is not which should be")

	// The usage of a no admitted `State` must return an error.
	s.TestTasks[0].Name = "Another name"
	s.TestTasks[0].State = StateTaskFailed // Let's use a no admitted state.
	err = s.TestDB.NewTask(&s.TestTasks[0])

	assert.Error(err, "The usage of a no admitted state must return an error")
	assert.EqualError(err, ErrIntegrity.Error(), "The returned error is not which should be")

	// The usage of an unrecognized `State` must return an error.
	s.TestTasks[0].Name = "Another name 2"
	s.TestTasks[0].State = "random_state" // Let's use a non-existent state.
	err = s.TestDB.NewTask(&s.TestTasks[0])

	assert.Error(err, "The usage of a non-existent state should cause an error")
	assert.EqualError(err, ErrIntegrity.Error(), "The returned error is not which should be")

	// Try to write with the database closed should return an error.
	s.TestTasks[0].Name = "Another name 3"
	s.TestTasks[0].State = StateTaskInactive
	err = s.TestDB.Close()
	if err != nil {
		panic(err)
	}
	err = s.TestDB.NewTask(&s.TestTasks[0])

	assert.Error(err, "The try to write in a closed database must cause an error")
}

func (s *WriteTestSuite) TearDownTest() {
	err := os.RemoveAll(s.TestDir)
	if err != nil {
		panic(err)
	}
}

func TestWriteSuite(t *testing.T) {
	suite.Run(t, new(WriteTestSuite))
}
