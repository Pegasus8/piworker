package data

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	assert2 "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type DBTestSuite struct {
	TestDir      string
	TestFilename string

	suite.Suite
}

func (s *DBTestSuite) SetupTest() {
	s.TestDir = "./test_db"
	s.TestFilename = "general.db"

	err := os.Mkdir(s.TestDir, 0755)
	if err != nil {
		panic(err)
	}
}

func (s *DBTestSuite) TestNewDB() {
	assert := assert2.New(s.T())

	// The database should be created correctly.
	db, err := NewDB(s.TestDir, s.TestFilename)

	assert.NoError(err, "The database should be initialized correctly")
	if assert.NotNil(db, "The returned instance of the database should not be nil") {
		_ = db.Close()
	}

	// The usage of an non-existent `Path` must return an error.
	db2, err := NewDB("hello_world/", s.TestFilename)
	assert.Error(err, "If the path does not exist an error should be returned")
	if !assert.Nil(db2, "If the path does not exist the returned instance of the database should be `nil`") {
		_ = db2.Close()
	}
}

func (s *DBTestSuite) TestCreateTable() {
	assert := assert2.New(s.T())

	db, err := NewDB(s.TestDir, "create_table_"+s.TestFilename)
	if err != nil {
		panic(err)
	}

	i := db.GetSQLInstance()

	err = createTable(i)

	assert.NoError(err, "The table should be created without problems")

	// Try to add a row to make sure that the table can be used without issues.
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

	_, err = i.Exec(sqlStatement,
		row.ID,
		row.Name,
		row.State,
		string(bTrigger),
		string(bActions),
		row.Created,
		row.LastTimeModified,
	)

	assert.NoError(err, "The row should be added to the table without issues")

	// Check if the task can be obtained correctly.
	sqlStatement1 := "SELECT * FROM Tasks WHERE ID = ? LIMIT 1;"

	r, err := i.Query(sqlStatement1, row.ID)

	assert.NoError(err, "The execution of a query to get a task from the database should not cause an error")

	defer func() {
		err := r.Close()
		if err != nil {
			panic(err)
		}
	}()

	if !r.Next() {
		assert.FailNow("If the row wasn't returned, the test can't continue")
	}

	var out UserTask
	var outTrigger string
	var outActions string

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

	// Compare the fields of type `time.Time` individually. Doing it with `assert.Equal` will cause a false positive
	// due to the absence of metadata on the task that contains the values obtained from the database.
	if !assert.True(row.Created.Equal(out.Created), "The row that contains the time when the task has been"+
		" created should be stored correctly") {
		s.T().Logf("L168 - Expected (Created field): %+v - Obtained (Created field): %+v", row.Created, out.Created)
	}
	if !assert.True(row.LastTimeModified.Equal(out.LastTimeModified), "The row that contains the last time"+
		" that the task has been modified should be stored correctly") {
		s.T().Logf("L172 - Expected (LastTimeModified field): %+v - Obtained (LastTimeModified field): %+v", row.Created, out.Created)
	}

	row.Created = time.Time{}
	row.LastTimeModified = time.Time{}
	out.Created = time.Time{}
	out.LastTimeModified = time.Time{}

	assert.Equal(row, out, "The row introduced into the database must be the same that the obtained")
}

func (s *DBTestSuite) TearDownTest() {
	err := os.RemoveAll(s.TestDir)
	if err != nil {
		panic(err)
	}
}

func TestDBSuite(t *testing.T) {
	suite.Run(t, new(DBTestSuite))
}
