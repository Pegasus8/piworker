package cmdexec

import (
	"os"
	"testing"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/actions/shared"
	test "github.com/Pegasus8/piworker/utilities/testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ActionTestSuite struct {
	Command     string
	CommandArgs string
	TestDir     string
	TaskID      string
	OutputFile  string
	suite.Suite
}

func (suite *ActionTestSuite) SetupTest() {
	// Change where the output of the executed command will be saved.
	outputPath = "./test"

	suite.TestDir = outputPath
	suite.TaskID = uuid.New().String()
	suite.OutputFile = "test_file.txt"
	suite.Command = "touch"
	suite.CommandArgs = suite.OutputFile

	err := os.MkdirAll(suite.TestDir, 0755)
	if err != nil {
		panic(err)
	}
}

func (suite *ActionTestSuite) TestExecuteCommand() {
	assert := assert.New(suite.T())

	test.CheckAFields(suite.T(), ExecuteCommand)

	args := [][]data.UserArg{
		// [0] -- Correct --
		// Problem: 		None.
		// Expected result: Should return no errors, a true result and the chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      ExecuteCommand.Args[0].ID,
				Content: suite.Command,
			},
			data.UserArg{
				ID:      ExecuteCommand.Args[1].ID,
				Content: suite.CommandArgs,
			},
		},

		// [1] -- Incorrect --
		// Problem: 		The first arg ("Command") is incorrectly formatted.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      ExecuteCommand.Args[0].ID,
				Content: suite.Command + "1", // Wrong format - the command does not exist.
			},
			data.UserArg{
				ID:      ExecuteCommand.Args[1].ID,
				Content: suite.CommandArgs,
			},
		},

		// [2] -- Incorrect --
		// Problem: 		The second arg ("Arguments") is incorrect.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      ExecuteCommand.Args[0].ID,
				Content: suite.Command,
			},
			data.UserArg{
				ID:      ExecuteCommand.Args[1].ID,
				Content: "/",
			},
		},

		// [3] -- Incorrect --
		// Problem: 		All the arguments ("Command" and "Arguments") are incorrect.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      ExecuteCommand.Args[0].ID,
				Content: suite.Command + "1", // Wrong format - the command does not exist.
			},
			data.UserArg{
				ID:      ExecuteCommand.Args[1].ID,
				Content: "/", // Wrong format
			},
		},

		// [4] -- Incorrect --
		// Problem: 		ID of an arg (0) is empty.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      "", // Empty ID
				Content: suite.Command,
			},
			data.UserArg{
				ID:      ExecuteCommand.Args[1].ID,
				Content: suite.CommandArgs,
			},
		},

		// [5] -- Incorrect --
		// Problem: 		ID of an arg (1) is incorrect.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      ExecuteCommand.Args[0].ID,
				Content: suite.Command,
			},
			data.UserArg{
				ID:      ExecuteCommand.ID + "-5", // Non-existent ID
				Content: suite.CommandArgs,
			},
		},

		// [6] -- Incorrect --
		// Problem: 		There are only one argument (should be two).
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      ExecuteCommand.Args[0].ID,
				Content: suite.Command,
			},
		},

		// [7] -- Incorrect --
		// Problem: 		Content of an argument empty.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      ExecuteCommand.Args[0].ID,
				Content: "", // Empty content
			},
			data.UserArg{
				ID:      ExecuteCommand.Args[1].ID,
				Content: suite.CommandArgs,
			},
		},
	}

	for i, arg := range args[:1] {
		ua := data.UserAction{
			ID:                    ExecuteCommand.ID,
			Order:                 0,
			Chained:               false,
			ArgumentToReplaceByCR: "",
			Timestamp:             "",
			Args:                  arg,
		}

		r, cr, err := ExecuteCommand.Run(&shared.ChainedResult{}, &ua, suite.TaskID)
		assert.Equalf(true, r, "argument %d should return a true result", i)
		assert.NotEmptyf(*cr, "argument %d should return a not empty chained result", i)
		assert.NoErrorf(err, "argument %d shouldn't return an error", i)

		if assert.FileExistsf(arg[1].Content, "the file '%s' must be created (arg %d)", arg[1].Content, i) {
			// Remove the file
			err := os.Remove(arg[1].Content)
			if err != nil {
				panic(err)
			}
		}

	}

	for i, arg := range args[1:] {
		ua := data.UserAction{
			ID:                    ExecuteCommand.ID,
			Order:                 0,
			Chained:               false,
			ArgumentToReplaceByCR: "",
			Timestamp:             "",
			Args:                  arg,
		}

		r, cr, err := ExecuteCommand.Run(&shared.ChainedResult{}, &ua, suite.TaskID)
		assert.Equalf(false, r, "argument %d should return a false result", i)
		assert.Emptyf(*cr, "argument %d should return an empty chained result", i)
		assert.Errorf(err, "argument %d should return an error", i)

		if len(arg) != 2 {
			continue
		}

		if !assert.NoFileExistsf(arg[1].Content, "the file '%s' shouldn't be created (arg %d)", arg[1].Content, i) {
			// Remove the file
			err := os.Remove(arg[1].Content)
			if err != nil {
				panic(err)
			}
		}
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
