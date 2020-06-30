package data

import (
	"encoding/json"
	assert2 "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type DeleteTestSuite struct {
	TestDir   string
	TestTasks []UserTask

	suite.Suite
}

func (suite *DeleteTestSuite) SetupTest() {
	suite.TestDir = "./test"
	Path = suite.TestDir

	err := os.Mkdir(Path, 0755)
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
							Content: "First argument",
						},
						{
							ID:      "A1-2",
							Content: "Second argument",
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
							Content: "First argument",
						},
						{
							ID:      "A2-2",
							Content: "Second argument",
						},
					},
					Timestamp:             "",
					Chained:               false,
					ArgumentToReplaceByCR: "",
					Order:                 0,
				},
				{
					ID: "A1",
					Args: []UserArg{
						{
							ID:      "A1-1",
							Content: "First argument",
						},
						{
							ID:      "A1-2",
							Content: "Second argument",
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

func (suite *DeleteTestSuite) BeforeTest(_, _ string) {
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
		<-EventBus
	}()

	err = NewTask(&suite.TestTasks[0])
	if err != nil {
		panic(err)
	}
}

func (suite *DeleteTestSuite) TestDeleteTask() {
	assert := assert2.New(suite.T())
	var end = make(chan struct{})

	go func() {
		event := <-EventBus

		assert.Equal(Deleted, event.Type, "The emitted event should be of type `Deleted`")
		assert.Equal(suite.TestTasks[0].ID, event.TaskID, "The TaskID emitted in the event must be the same of the deleted task")

		for {
			// Avoid blocking just in case that other tests fail and create new tasks.
			select {
			case <-end:
				break
			case <-EventBus:
			}
		}
	}()

	// Task should be deleted correctly.
	err := DeleteTask(suite.TestTasks[0].ID)
	assert.NoError(err, "The task should be deleted correctly")

	t, err := getTask(suite.TestTasks[0].Name)
	//noinspection GoNilness
	assert.Equal(UserTask{}, *t, "The task should be deleted from the database")
	assert.Error(err, "The task should be deleted from the database")

	// Try to delete an already deleted task should return an error.
	err = DeleteTask(suite.TestTasks[0].ID)
	assert.Error(err, "Try to delete an already deleted task should return an error")

	// Try to delete a non-existent task should return an error.
	err = DeleteTask("non-existent-id")
	assert.Error(err, "Try to delete a non-existent task should return an error")

	end <- struct{}{}
}

func (suite *DeleteTestSuite) TearDownTest() {
	err := os.RemoveAll(Path)
	if err != nil {
		panic(err)
	}
}

func TestDeleteSuite(t *testing.T) {
	suite.Run(t, new(DeleteTestSuite))
}

func getTask(name string) (*UserTask, error) {
	// I know that this function is a copy of `GetTaskByName`. The objective of that is to avoid possible
	// bugs or not desired behaviors (due to a future modifications).

	sqlStatement := `
		SELECT * FROM Tasks
		WHERE Name=?;
	`

	var task UserTask
	var trigger string
	var actions string

	row, err := DB.Query(sqlStatement, name)
	if err != nil {
		return &task, err
	}
	defer func() {
		err := row.Close()
		if err != nil {
			panic(err)
		}
	}()

	if !row.Next() {
		return &task, ErrBadTaskID
	}

	err = row.Scan(
		&task.ID,
		&task.Name,
		&task.State,
		&trigger,
		&actions,
		&task.Created,
		&task.LastTimeModified,
	)
	if err != nil {
		return &task, err
	}

	// Parse the Trigger string into the proper struct.
	err = json.Unmarshal([]byte(trigger), &task.Trigger)
	if err != nil {
		return &task, err
	}

	// Parse the Actions string into the proper struct.
	err = json.Unmarshal([]byte(actions), &task.Actions)
	if err != nil {
		return &task, err
	}

	return &task, nil
}
