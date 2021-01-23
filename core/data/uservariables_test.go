package data

import (
	"github.com/Pegasus8/piworker/core/types"
	"github.com/Pegasus8/piworker/core/uservariables"
	assert2 "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"os"
	"testing"
)

type DBUserVariablesTestSuite struct {
	TestDir        string
	TestFilename   string
	TestDBInstance *DatabaseInstance
	GlobalVars     []uservariables.GlobalVariable
	LocalVars      []uservariables.LocalVariable

	suite.Suite
}

func (s *DBUserVariablesTestSuite) SetupTest() {
	s.TestDir = "./test_db_uv"
	s.TestFilename = "uv.db"

	err := os.Mkdir(s.TestDir, 0755)
	if err != nil {
		panic(err)
	}

	db, err := NewDB(s.TestDir, s.TestFilename)
	if err != nil {
		panic(err)
	}

	s.TestDBInstance = db

	i := s.TestDBInstance.GetSQLInstance()

	s.LocalVars = []uservariables.LocalVariable{
		{
			ID:           1,
			Name:         "variable_1",
			Content:      "10",
			Type:         types.Int,
			ParentTaskID: "abc1",
		},
		{
			ID:           2,
			Name:         "variable_2",
			Content:      "Hello world!",
			Type:         types.Text,
			ParentTaskID: "abc1",
		},
	}

	s.GlobalVars = []uservariables.GlobalVariable{
		{
			ID:      1,
			Name:    "VARIABLE_1",
			Content: "0.120",
			Type:    types.Float,
		},
		{
			ID:      2,
			Name:    "VARIABLE_2",
			Content: "11/01/2021",
			Type:    types.Date,
		},
	}

	for _, v := range s.LocalVars {
		q := `INSERT INTO variables_local(name, content, type, parent_task_id) values (?, ?, ?, ?);`

		result, err := i.Exec(q, v.Name, v.Content, v.Type, v.ParentTaskID)
		if err != nil {
			panic(err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			panic(err)
		}

		if rowsAffected == 0 {
			assert2.FailNow(s.T(), "The local variable '%+v' was not inserted in the database", v)
		}
	}

	for _, v := range s.GlobalVars {
		q := `INSERT INTO variables_global(name, content, type) values (?, ?, ?);`

		result, err := i.Exec(q, v.Name, v.Content, v.Type)
		if err != nil {
			panic(err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			panic(err)
		}

		if rowsAffected == 0 {
			assert2.FailNow(s.T(), "The global variable '%+v' was not inserted in the database", v)
		}
	}
}

func (s *DBUserVariablesTestSuite) BeforeTest(_, _ string) {}

func (s *DBUserVariablesTestSuite) TestSetULV() {
	assert := assert2.New(s.T())

	v := s.LocalVars[0]
	v.Content = "11"

	// Try to overwrite an already existent variable.
	err := s.TestDBInstance.SetULV(&v)

	assert.NoError(err, "The user local variable should be set without errors")

	// Try to add a third variable.
	v.Name = "variable_3"

	err = s.TestDBInstance.SetULV(&v)

	assert.NoError(err, "The user local variable should be set without errors")
}

func (s *DBUserVariablesTestSuite) TestGetULV() {
	assert := assert2.New(s.T())

	v, err := s.TestDBInstance.GetULV(s.LocalVars[1].Name)

	assert.NoError(err, "The variable should be returned without an error")
	if assert.NotNil(v, "The returned variable should not be nil") {
		assert.Equal(s.LocalVars[1], *v, "The returned variable should be the same as the inserted one")
	}

	v, err = s.TestDBInstance.GetULV("non_existent_variable")

	assert.Error(err, "If the requested variable does not exist, an error should be returned")
	assert.Nil(v, "If the requested variable does not exist, the returned variable should be nil")
}

func (s *DBUserVariablesTestSuite) TestSetUGV() {
	assert := assert2.New(s.T())

	v := s.GlobalVars[0]
	v.Content = "0.121"

	// Try to overwrite an already existent variable.
	err := s.TestDBInstance.SetUGV(&v)

	assert.NoError(err, "The user global variable should be set without errors")

	// Try to add a third variable.
	v.Name = "variable_3"

	err = s.TestDBInstance.SetUGV(&v)

	assert.NoError(err, "The user global variable should be set without errors")
}

func (s *DBUserVariablesTestSuite) TestGetUGV() {
	assert := assert2.New(s.T())

	v, err := s.TestDBInstance.GetUGV(s.GlobalVars[1].Name)

	assert.NoError(err, "The variable should be returned without an error")
	if assert.NotNil(v, "The returned variable should not be nil") {
		assert.Equal(s.GlobalVars[1], *v, "The returned variable should be the same as the inserted one")
	}

	v, err = s.TestDBInstance.GetUGV("NON_EXISTENT_VARIABLE")

	assert.Error(err, "If the requested variable does not exist, an error should be returned")
	assert.Nil(v, "If the requested variable does not exist, the returned variable should be nil")
}

func (s *DBUserVariablesTestSuite) AfterTest(_, _ string) {}

func (s *DBUserVariablesTestSuite) TearDownTest() {
	_ = s.TestDBInstance.Close()
	err := os.RemoveAll(s.TestDir)
	if err != nil {
		panic(err)
	}
}

func TestDBUserVariablesSuite(t *testing.T) {
	suite.Run(t, new(DBUserVariablesTestSuite))
}
