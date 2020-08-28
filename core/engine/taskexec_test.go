package engine

import (
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/types"
	"github.com/Pegasus8/piworker/core/uservariables"

	"github.com/google/uuid"
	assert2 "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type TETestSuite struct {
	suite.Suite
}

func (suite *TETestSuite) SetupTest() {
	err := os.MkdirAll(TempDir, 0755)
	if err != nil {
		panic(err)
	}
}

func (suite *TETestSuite) BeforeTest(_, _ string) {

}

func (suite *TETestSuite) TestRunTaskLoop() {

}

func (suite *TETestSuite) TestRunTrigger() {

}

func (suite *TETestSuite) TestRunActions() {

}

func (suite *TETestSuite) TestSetAsRecentlyExecuted() {
	assert := assert2.New(suite.T())
	id := uuid.New().String()

	err := setAsRecentlyExecuted(id)
	assert.NoError(err, "a task should be able to be marked as recently executed")

	_, err = os.Stat(path.Join(TempDir, id))
	assert.False(os.IsNotExist(err), "a file with the ID of the task should be created to mark a task "+
		"as recently executed")
}

func (suite *TETestSuite) TestWasRecentlyExecuted() {
	assert := assert2.New(suite.T())
	id := uuid.New().String()

	generateIDFile(id)

	r := wasRecentlyExecuted(id)
	assert.True(r, "if the task has been recently executed a file with its ID should be found")

	id2 := uuid.New().String()
	r = wasRecentlyExecuted(id2)
	assert.False(r, "if the ID provided is not one of a recently executed task, false should be returned")
}

func (suite *TETestSuite) TestSetAsReadyToExecuteAgain() {
	assert := assert2.New(suite.T())
	id := uuid.New().String()

	generateIDFile(id)

	err := setAsReadyToExecuteAgain(id)
	assert.NoError(err, "a task should be able to be de-marked as recently executed")

	_, err = os.Stat(path.Join(TempDir, id))
	assert.True(os.IsNotExist(err), "a file with the ID of the task should be removed to mark a task "+
		"as ready to be executed again")
}

func (suite *TETestSuite) TestSearchAndReplaceVariable() {
	assert := assert2.New(suite.T())

	parentTaskID := uuid.New().String()
	globalVar := "A_GLOBAL_VAR"
	localVar := "a_local_var"
	noVar := "hello world!"
	gvs := make([]uservariables.GlobalVariable, 0)
	lvs := make([]uservariables.LocalVariable, 0)
	uservariables.GlobalVariablesSlice = &gvs
	uservariables.LocalVariablesSlice = &lvs

	gv := uservariables.GlobalVariable{
		Name:    globalVar,
		Content: "lorem ipsum",
		Type:    types.Float,
		RWMutex: &sync.RWMutex{},
	}
	gvSlice := append(*uservariables.GlobalVariablesSlice, gv)
	uservariables.GlobalVariablesSlice = &gvSlice

	lv := uservariables.LocalVariable{
		Name:         localVar,
		Content:      "lorem ipsum 2",
		Type:         types.Date,
		ParentTaskID: parentTaskID,
		RWMutex:      &sync.RWMutex{},
	}
	lvSlice := append(*uservariables.LocalVariablesSlice, lv)
	uservariables.LocalVariablesSlice = &lvSlice

	arg := data.UserArg{
		ID:      "A1-1",
		Content: "$" + globalVar,
	}
	err := searchAndReplaceVariable(&arg, parentTaskID)
	assert.NoError(err, "the content of the argument should be replaced without problems")
	assert.Equal(gv.Content, arg.Content, "the content of the global variable should be used as "+
		"replacement of the argument's own content")

	arg.Content = "$" + localVar
	err = searchAndReplaceVariable(&arg, parentTaskID)
	assert.NoError(err, "the content of the argument should be replaced without problems")
	assert.Equal(lv.Content, arg.Content, "the content of the local variable should be used as "+
		"replacement of the argument's own content")

	arg.Content = noVar
	err = searchAndReplaceVariable(&arg, parentTaskID)
	assert.NoError(err, "if the content of the argument doesn't contain a variable, no error should "+
		"be returned")
	assert.Equal(noVar, arg.Content, "the content of the argument should not variate")
}

func (suite *TETestSuite) TearDownTest() {
	err := os.RemoveAll(TempDir)
	if err != nil {
		panic(err)
	}

	err = os.RemoveAll(data.Path)
	if err != nil {
		panic(err)
	}
}

func TestTESuite(t *testing.T) {
	suite.Run(t, new(TETestSuite))
}

func generateIDFile(id string) {
	// Emulate a task that has been recently executed.
	err := ioutil.WriteFile(filepath.Join(TempDir, id), []byte{}, 0644)
	if err != nil {
		panic(err)
	}
}
