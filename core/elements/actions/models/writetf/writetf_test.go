package writetf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Pegasus8/piworker/core/data"
	"github.com/Pegasus8/piworker/core/elements/actions/shared"
	test "github.com/Pegasus8/piworker/utilities/testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ActionTestSuite struct {
	TestDir    string
	TaskID     string
	OutputFile string
	suite.Suite
}

func (suite *ActionTestSuite) SetupTest() {
	suite.TestDir = "./test"
	suite.OutputFile = "my_file.txt"
	suite.TaskID = uuid.New().String()

	err := os.MkdirAll(suite.TestDir, 0755)
	if err != nil {
		panic(err)
	}
}

func (suite *ActionTestSuite) TestWriteTextFile() {
	assert := assert.New(suite.T())

	test.CheckAFields(suite.T(), WriteTextFile)

	args := [][]data.UserArg{
		// [0] -- Correct --
		// Problem: 		None.
		// Expected result: Should return no errors, a true result and the chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      WriteTextFile.Args[0].ID,
				Content: "some content",
			},
			data.UserArg{
				ID:      WriteTextFile.Args[1].ID,
				Content: suite.OutputFile,
			},
			data.UserArg{
				ID:      WriteTextFile.Args[2].ID,
				Content: "w",
			},
			data.UserArg{
				ID:      WriteTextFile.Args[3].ID,
				Content: suite.TestDir,
			},
		},

		// [1] -- Correct --
		// Problem: 		None.
		// Expected result: Should return no errors, a true result and the chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      WriteTextFile.Args[0].ID,
				Content: "some content",
			},
			data.UserArg{
				ID:      WriteTextFile.Args[1].ID,
				Content: suite.OutputFile,
			},
			data.UserArg{
				ID:      WriteTextFile.Args[2].ID,
				Content: "a",
			},
			data.UserArg{
				ID:      WriteTextFile.Args[3].ID,
				Content: suite.TestDir,
			},
		},

		// [2] -- Incorrect --
		// Problem: 		The third arg ("Writing Mode") is incorrectly formatted.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      WriteTextFile.Args[0].ID,
				Content: "some content",
			},
			data.UserArg{
				ID:      WriteTextFile.Args[1].ID,
				Content: suite.OutputFile,
			},
			data.UserArg{
				ID:      WriteTextFile.Args[2].ID,
				Content: "abc", // Wrong format
			},
			data.UserArg{
				ID:      WriteTextFile.Args[3].ID,
				Content: suite.TestDir,
			},
		},

		// [3] -- Incorrect --
		// Problem: 		The fourth arg ("Path") is incorrectly formatted.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      WriteTextFile.Args[0].ID,
				Content: "some content",
			},
			data.UserArg{
				ID:      WriteTextFile.Args[1].ID,
				Content: suite.OutputFile,
			},
			data.UserArg{
				ID:      WriteTextFile.Args[2].ID,
				Content: "a",
			},
			data.UserArg{
				ID:      WriteTextFile.Args[3].ID,
				Content: suite.TestDir + "!|:@", // Wrong format
			},
		},

		// [4] -- Incorrect --
		// Problem: 		The second arg ("File Name") is incorrectly formatted.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      WriteTextFile.Args[0].ID,
				Content: "some content",
			},
			data.UserArg{
				ID:      WriteTextFile.Args[1].ID,
				Content: ".", // Wrong format
			},
			data.UserArg{
				ID:      WriteTextFile.Args[2].ID,
				Content: "a",
			},
			data.UserArg{
				ID:      WriteTextFile.Args[3].ID,
				Content: suite.TestDir,
			},
		},

		// [5] -- Incorrect --
		// Problem: 		All the arguments ("Writing mode", "Path" and "File Name"; "Content" can have any format) are incorrectly formatted.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      WriteTextFile.Args[0].ID,
				Content: "some content",
			},
			data.UserArg{
				ID:      WriteTextFile.Args[1].ID,
				Content: "/this_is_dir/", // Wrong format
			},
			data.UserArg{
				ID:      WriteTextFile.Args[2].ID,
				Content: "abc", // Wrong format
			},
			data.UserArg{
				ID:      WriteTextFile.Args[3].ID,
				Content: suite.TestDir + "/<>", // Wrong format
			},
		},

		// [6] -- Incorrect --
		// Problem: 		ID of an arg (0) is empty.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      "", // Empty ID
				Content: "some content",
			},
			data.UserArg{
				ID:      WriteTextFile.Args[1].ID,
				Content: suite.OutputFile,
			},
			data.UserArg{
				ID:      WriteTextFile.Args[2].ID,
				Content: "w",
			},
			data.UserArg{
				ID:      WriteTextFile.Args[3].ID,
				Content: suite.TestDir,
			},
		},

		// [7] -- Incorrect --
		// Problem: 		ID of an arg (1) is incorrect.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      WriteTextFile.Args[0].ID,
				Content: "some content",
			},
			data.UserArg{
				ID:      WriteTextFile.ID + "-5", // Non-existent ID
				Content: suite.OutputFile,
			},
			data.UserArg{
				ID:      WriteTextFile.Args[2].ID,
				Content: "w",
			},
			data.UserArg{
				ID:      WriteTextFile.Args[3].ID,
				Content: suite.TestDir,
			},
		},

		// [8] -- Incorrect --
		// Problem: 		There are only one argument (should be four).
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      WriteTextFile.Args[0].ID,
				Content: "some content",
			},
		},

		// [9] -- Incorrect --
		// Problem: 		Content of an argument empty.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      WriteTextFile.Args[0].ID,
				Content: "", // Empty content
			},
			data.UserArg{
				ID:      WriteTextFile.Args[1].ID,
				Content: suite.OutputFile,
			},
			data.UserArg{
				ID:      WriteTextFile.Args[2].ID,
				Content: "a",
			},
			data.UserArg{
				ID:      WriteTextFile.Args[3].ID,
				Content: suite.TestDir,
			},
		},
	}

	for i, arg := range args[:2] {
		ua := data.UserAction{
			ID:                    WriteTextFile.ID,
			Order:                 0,
			Chained:               false,
			ArgumentToReplaceByCR: "",
			Timestamp:             "",
			Args:                  arg,
		}

		r, cr, err := WriteTextFile.Run(&shared.ChainedResult{}, &ua, suite.TaskID)
		assert.Equalf(true, r, "argument %d should return a true result", i)
		assert.NotEmptyf(*cr, "argument %d should return a not empty chained result", i)
		assert.NoErrorf(err, "argument %d shouldn't return an error", i)

		if assert.FileExistsf(filepath.Join(arg[3].Content, arg[1].Content), "the file '%s' must be created (arg %d)", arg[1].Content, i) {
			// Remove the file
			err := os.Remove(filepath.Join(arg[3].Content, arg[1].Content))
			if err != nil {
				panic(err)
			}
		}

	}

	for i, arg := range args[2:] {
		ua := data.UserAction{
			ID:                    WriteTextFile.ID,
			Order:                 0,
			Chained:               false,
			ArgumentToReplaceByCR: "",
			Timestamp:             "",
			Args:                  arg,
		}

		r, cr, err := WriteTextFile.Run(&shared.ChainedResult{}, &ua, suite.TaskID)
		assert.Equalf(false, r, "argument %d should return a false result", i)
		assert.Emptyf(*cr, "argument %d should return an empty chained result", i)
		assert.Errorf(err, "argument %d should return an error", i)

		if len(arg) != 4 {
			continue
		}

		if !assert.NoFileExistsf(filepath.Join(arg[3].Content, arg[1].Content), "the file '%s' shouldn't be created (arg %d)", arg[1].Content, i) {
			// Remove the file
			err := os.Remove(filepath.Join(arg[3].Content, arg[1].Content))
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
