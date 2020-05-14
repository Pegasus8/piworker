package getgv

import (
	"os"
	"sync"
	"testing"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/actions/shared"
	"github.com/Pegasus8/piworker/core/types"
	"github.com/Pegasus8/piworker/core/uservariables"
	test "github.com/Pegasus8/piworker/utilities/testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ActionTestSuite struct {
	VariableName    string
	VariableContent string
	VariableType    types.PWType
	TestDir         string
	TaskID          string
	suite.Suite
}

func (suite *ActionTestSuite) SetupTest() {
	// Change the path where the variables are stored to delete them after tests execution.
	uservariables.UserVariablesPath = "./test"

	suite.TestDir = uservariables.UserVariablesPath
	suite.TaskID = uuid.New().String()
	suite.VariableName = "TESTING_VAR"
	suite.VariableContent = "Hello world!"
	suite.VariableType = types.Text

	err := os.MkdirAll(suite.TestDir, 0755)
	if err != nil {
		panic(err)
	}

	gv, err := uservariables.ReadGlobalVariablesFromFiles()
	if err != nil {
		panic(err)
	}

	uservariables.GlobalVariablesSlice = gv

	// Make a new variable to do some tests with it.
	v1 := &uservariables.GlobalVariable{
		Name:    suite.VariableName,
		Content: suite.VariableContent,
		Type:    suite.VariableType,
		RWMutex: &sync.RWMutex{},
	}
	err = v1.WriteToFile()
	if err != nil {
		panic(err)
	}

	newGVS := append(*uservariables.GlobalVariablesSlice, *v1)
	uservariables.GlobalVariablesSlice = &newGVS
}

func (suite *ActionTestSuite) TestGetGlobalVariable() {
	assert := assert.New(suite.T())

	test.CheckAFields(suite.T(), GetGlobalVariable)

	args := [][]data.UserArg{
		// [0] -- Correct --
		// Problem: 		None.
		// Expected result: Should return no errors, a true result and the chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      GetGlobalVariable.Args[0].ID,
				Content: suite.VariableName,
			},
		},

		// [1] -- Incorrect --
		// Problem: 		The arg ("Name") contains the name of a inexistent variable.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      GetGlobalVariable.Args[0].ID,
				Content: suite.VariableName + "_1234", // Inexistent variable
			},
		},

		// [2] -- Incorrect --
		// Problem: 		The arg ("Name") is incorrectly formatted.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      GetGlobalVariable.Args[0].ID,
				Content: "CAN_HAVE_SYMBOLS?!-_.:,;", // Wrong format
			},
		},

		// [3] -- Incorrect --
		// Problem: 		The arg ("Name") is incorrectly formatted.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      GetGlobalVariable.Args[0].ID,
				Content: "nonexistent_variable", // Wrong format
			},
		},

		// [4] -- Incorrect --
		// Problem: 		ID of the arg is empty.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      "", // Empty ID
				Content: suite.VariableName,
			},
		},

		// [5] -- Incorrect --
		// Problem: 		ID of the arg is incorrect.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      GetGlobalVariable.ID + "-5", // Non-existent ID
				Content: suite.VariableName,
			},
		},

		// [6] -- Incorrect --
		// Problem: 		There are no arguments (should be one).
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{},

		// [7] -- Incorrect --
		// Problem: 		Content of the argument empty.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      GetGlobalVariable.Args[0].ID,
				Content: "", // Empty content
			},
		},
	}

	for i, arg := range args[:1] {
		ua := data.UserAction{
			ID:                    GetGlobalVariable.ID,
			Order:                 0,
			Chained:               false,
			ArgumentToReplaceByCR: "",
			Timestamp:             "",
			Args:                  arg,
		}

		r, cr, err := GetGlobalVariable.Run(&shared.ChainedResult{}, &ua, suite.TaskID)
		assert.Equalf(true, r, "argument %d should return a true result", i)
		assert.NotEmptyf(*cr, "argument %d should return a not empty chained result", i)
		assert.NoErrorf(err, "argument %d shouldn't return an error", i)
	}

	for i, arg := range args[1:] {
		ua := data.UserAction{
			ID:                    GetGlobalVariable.ID,
			Order:                 0,
			Chained:               false,
			ArgumentToReplaceByCR: "",
			Timestamp:             "",
			Args:                  arg,
		}

		r, cr, err := GetGlobalVariable.Run(&shared.ChainedResult{}, &ua, suite.TaskID)
		assert.Equalf(false, r, "argument %d should return a false result", i)
		assert.Emptyf(*cr, "argument %d should return an empty chained result", i)
		assert.Errorf(err, "argument %d should return an error", i)
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
