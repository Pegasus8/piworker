package setlv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Pegasus8/piworker/core/uservariables"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/actions/shared"
	test "github.com/Pegasus8/piworker/utilities/testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ActionTestSuite struct {
	VariableName    string
	VariableContent string
	TestDir         string
	TaskID          string
	suite.Suite
}

func (suite *ActionTestSuite) SetupTest() {
	// Change the path where the variables are stored to delete them after tests execution.
	uservariables.UserVariablesPath = "./test"

	suite.TestDir = uservariables.UserVariablesPath
	suite.TaskID = uuid.New().String()
	suite.VariableName = "testing_var"
	suite.VariableContent = "Hello world!"

	err := os.MkdirAll(suite.TestDir, 0755)
	if err != nil {
		panic(err)
	}

	lv, err := uservariables.ReadLocalVariablesFromFiles()
	if err != nil {
		panic(err)
	}

	uservariables.LocalVariablesSlice = lv
}

func (suite *ActionTestSuite) TestSetLocalVariable() {
	assert := assert.New(suite.T())

	test.CheckAFields(suite.T(), SetLocalVariable)

	args := [][]data.UserArg{
		// [0] -- Correct --
		// Problem: 		None.
		// Expected result: Should return no errors, a true result and the chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      SetLocalVariable.Args[0].ID,
				Content: suite.VariableName,
			},
			data.UserArg{
				ID:      SetLocalVariable.Args[1].ID,
				Content: suite.VariableContent,
			},
		},

		// [1] -- Correct --
		// Problem: 		None.
		// Expected result: Should return no errors, a true result and the chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      SetLocalVariable.Args[0].ID,
				Content: suite.VariableName,
			},
			data.UserArg{
				ID:      SetLocalVariable.Args[1].ID,
				Content: suite.VariableContent + " How are you?", // Re-assign another content to the already created variable.
			},
		},

		// [2] -- Correct --
		// Problem: 		None.
		// Expected result: Should return no errors, a true result and the chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      SetLocalVariable.Args[0].ID,
				Content: suite.VariableName + "_12", // Add a number just to be sure that is compatible.
			},
			data.UserArg{
				ID:      SetLocalVariable.Args[1].ID,
				Content: suite.VariableContent,
			},
		},

		// [3] -- Incorrect --
		// Problem: 		The first arg ("Name") is incorrectly formatted.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      SetLocalVariable.Args[0].ID,
				Content: "WRONG_VARIABLE", // Wrong format
			},
			data.UserArg{
				ID:      SetLocalVariable.Args[1].ID,
				Content: suite.VariableContent,
			},
		},

		// [4] -- Incorrect --
		// Problem: 		The first arg ("Name") is incorrectly formatted.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      SetLocalVariable.Args[0].ID,
				Content: "can_have_symbols?!-_.:,;", // Wrong format
			},
			data.UserArg{
				ID:      SetLocalVariable.Args[1].ID,
				Content: suite.VariableContent,
			},
		},

		// [5] -- Incorrect --
		// Problem: 		ID of an arg (0) is empty.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      "", // Empty ID
				Content: suite.VariableName,
			},
			data.UserArg{
				ID:      SetLocalVariable.Args[1].ID,
				Content: suite.VariableContent,
			},
		},

		// [6] -- Incorrect --
		// Problem: 		ID of an arg (1) is incorrect.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      SetLocalVariable.Args[0].ID,
				Content: suite.VariableName,
			},
			data.UserArg{
				ID:      SetLocalVariable.ID + "-5", // Non-existent ID
				Content: suite.VariableContent,
			},
		},

		// [7] -- Incorrect --
		// Problem: 		There are only one argument (should be two).
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      SetLocalVariable.Args[0].ID,
				Content: suite.VariableName,
			},
		},

		// [8] -- Incorrect --
		// Problem: 		Content of an argument empty.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      SetLocalVariable.Args[0].ID,
				Content: "", // Empty content
			},
			data.UserArg{
				ID:      SetLocalVariable.Args[1].ID,
				Content: suite.VariableContent,
			},
		},
	}

	for i, arg := range args[:3] {
		ua := data.UserAction{
			ID:                    SetLocalVariable.ID,
			Order:                 0,
			Chained:               false,
			ArgumentToReplaceByCR: "",
			Timestamp:             "",
			Args:                  arg,
		}

		r, cr, err := SetLocalVariable.Run(&shared.ChainedResult{}, &ua, suite.TaskID)
		assert.Equalf(true, r, "argument %d should return a true result", i)
		assert.NotEmptyf(*cr, "argument %d should return a not empty chained result", i)
		assert.NoErrorf(err, "argument %d shouldn't return an error", i)

		assert.FileExistsf(filepath.Join(suite.TestDir, arg[0].Content+"-"+suite.TaskID), "the variable should be saved on a file (arg %d)", i)
	}

	// Remove the file of the default variable to avoid a false positive.
	_, err := os.Stat(filepath.Join(suite.TestDir, suite.VariableName+"-"+suite.TaskID))
	if !os.IsNotExist(err) {
		err := os.Remove(filepath.Join(suite.TestDir, suite.VariableName+"-"+suite.TaskID))
		if err != nil {
			panic(err)
		}
	}

	for i, arg := range args[3:] {
		ua := data.UserAction{
			ID:                    SetLocalVariable.ID,
			Order:                 0,
			Chained:               false,
			ArgumentToReplaceByCR: "",
			Timestamp:             "",
			Args:                  arg,
		}

		r, cr, err := SetLocalVariable.Run(&shared.ChainedResult{}, &ua, suite.TaskID)
		assert.Equalf(false, r, "argument %d should return a false result", i)
		assert.Emptyf(*cr, "argument %d should return an empty chained result", i)
		assert.Errorf(err, "argument %d should return an error", i)

		if len(arg) != 2 {
			continue
		}

		assert.NoFileExistsf(filepath.Join(suite.TestDir, arg[0].Content+"-"+suite.TaskID), "the variable shouldn't be saved on a file because the execution must fail (arg %d)", i)
	}
}

func (suite *ActionTestSuite) TearDownTest() {
	err := os.RemoveAll(suite.TestDir)
	if err != nil {
		panic(err)
	}
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(ActionTestSuite))
}
