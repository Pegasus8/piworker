package data

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	assert2 "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type DBTestSuite struct {
	TestDir string

	suite.Suite
}

func (suite *DBTestSuite) SetupTest() {
	suite.TestDir = "./test"
	Path = suite.TestDir

	err := os.Mkdir(suite.TestDir, 0755)
	if err != nil {
		panic(err)
	}
}

func (suite *DBTestSuite) TestInit() {
	// The functions must be used in order.
	suite.InitDB()
	suite.CreateTable()
}

func (suite *DBTestSuite) InitDB() {
	assert := assert2.New(suite.T())
	file := filepath.Join(Path, Filename)

	// The database should be created correctly.
	db1, err := InitDB(file)
	assert.NoError(err, "The database should be initialized correctly")
	if !assert.NotNil(db1, "A new instance of the database should be returned without problems") {
		assert.FailNow("the test can't continue if the returned db is nil")
	}
	DB = db1

	// The usage of an non-existent `Path` must return an error.
	Path = filepath.Join(suite.TestDir, "hello_world/")
	db, err := InitDB(filepath.Join(Path, Filename))
	assert.Error(err, "If the path does not exist an error should be returned")
	assert.Nil(db, "If the path does not exist the instance of the database should be `nil`")
	Path = suite.TestDir
}

func (suite *DBTestSuite) CreateTable() {
	assert := assert2.New(suite.T())

	err := CreateTable()
	assert.NoError(err, "The table should be created without problems in the database")

	// Add a row to make sure that the table can be used without issues.
	sqlStatement := `
	INSERT INTO Tasks(
		ID,
		Name,
		State,
		Trigger,
		Actions,
		Created,
		LastTimeModified
	) values (?,?,?,?,?,?,?);
	`

	row := UserTask{
		Name:  "My task",
		State: "active",
		Trigger: UserTrigger{
			ID:        "T1",
			Args:      nil,
			Timestamp: "",
		},
		Actions: []UserAction{
			{
				ID:                    "A1",
				Args:                  nil,
				Timestamp:             "",
				Chained:               false,
				ArgumentToReplaceByCR: "A1-1",
				Order:                 0,
			},
		},
		Created:          time.Now(),
		LastTimeModified: time.Now(),
		ID:               "hello1234",
	}

	bTrigger, err := json.Marshal(row.Trigger)
	if err != nil {
		panic(err)
	}

	bActions, err := json.Marshal(row.Actions)
	if err != nil {
		panic(err)
	}

	_, err = DB.Exec(sqlStatement,
		row.ID,
		row.Name,
		row.State,
		string(bTrigger),
		string(bActions),
		time.Now(),
		time.Now(),
	)
	assert.NoError(err, "The row should be added to the table without issues")

	// Check if the task can be obtained correctly.
	sqlStatement1 := "SELECT * FROM Tasks WHERE ID='ID' LIMIT 1;"

	r, err := DB.Query(sqlStatement1)
	assert.NoError(err, "The execution of a query to get a task from the database should not cause an error")

	defer func() {
		err := r.Close()
		assert.NoError(err, "Rows must be closed without problems")
	}()

	var out UserTask
	var outTrigger string
	var outActions string

	for r.Next() {
		err = r.Scan(
			&out.ID,
			&out.Name,
			&out.State,
			&outTrigger,
			&outActions,
			&out.Created,
			&out.LastTimeModified,
		)
		assert.NoError(err, "The row must be scanned without problems")

		err = json.Unmarshal([]byte(outTrigger), &out.Trigger)
		if err != nil {
			panic(err)
		}

		err = json.Unmarshal([]byte(outActions), &out.Actions)
		if err != nil {
			panic(err)
		}

		assert.Equal(row, out, "The row introduced into the database must be the same that the obtained")
	}
}

func (suite *DBTestSuite) TearDownTest() {
	err := os.RemoveAll(suite.TestDir)
	if err != nil {
		panic(err)
	}
}

func TestDBSuite(t *testing.T) {
	suite.Run(t, new(DBTestSuite))
}
