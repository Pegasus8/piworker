package compress

import (
	"math/rand"
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
	TargetDir  string
	Files      []string
	suite.Suite
}

func (suite *ActionTestSuite) SetupTest() {
	suite.TestDir = "./test"
	suite.TaskID = uuid.New().String()
	suite.OutputFile = "compressed_file"
	suite.TargetDir = filepath.Join(suite.TestDir, "dir_to_compress/")

	err := os.MkdirAll(suite.TargetDir, 0755)
	if err != nil {
		panic(err)
	}

	var t = []string{".txt", ".go", ".log", ".odt", ".xml", ".json", ".py", ".toml"}
	for i := 0; i <= 10; i++ {
		suite.Files = append(suite.Files, uuid.New().String()+t[rand.Intn(len(t)-1)])
	}

	for _, name := range suite.Files {
		path := filepath.Join(suite.TargetDir, name)

		_,err := os.Create(path)
		if err != nil {
			panic(err)
		}

		err = os.Truncate(path, rand.Int63n(200))
		if err != nil {
			panic(err)
		}
	}
}

func (suite *ActionTestSuite) TestCompressFilesOfDir() {
	assert := assert.New(suite.T())

	test.CheckAFields(suite.T(), CompressFilesOfDir)

	args := [][]data.UserArg{
		// [0] -- Correct --
		// Problem: 		None.
		// Expected result: Should return no errors, a true result and the chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      CompressFilesOfDir.Args[0].ID,
				Content: suite.TargetDir, // Compress a directory
			},
			data.UserArg{
				ID:      CompressFilesOfDir.Args[1].ID,
				Content: suite.TestDir,
			},
			data.UserArg{
				ID:      CompressFilesOfDir.Args[2].ID,
				Content: suite.OutputFile,
			},
		},

		// [1] -- Correct --
		// Problem: 		None.
		// Expected result: Should return no errors, a true result and the chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      CompressFilesOfDir.Args[0].ID,
				Content: filepath.Join(suite.TargetDir, suite.Files[0]), // Compress a file
			},
			data.UserArg{
				ID:      CompressFilesOfDir.Args[1].ID,
				Content: suite.TestDir,
			},
			data.UserArg{
				ID:      CompressFilesOfDir.Args[2].ID,
				Content: suite.OutputFile,
			},
		},

		// [2] -- Incorrect --
		// Problem: 		The first arg ("Directory/File Target") contains a non existent path.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      CompressFilesOfDir.Args[0].ID,
				Content: suite.TargetDir + "/random_path/", // Non-existent path
			},
			data.UserArg{
				ID:      CompressFilesOfDir.Args[1].ID,
				Content: suite.TestDir,
			},
			data.UserArg{
				ID:      CompressFilesOfDir.Args[2].ID,
				Content: suite.OutputFile,
			},
		},

		// [3] -- Incorrect --
		// Problem: 		The first arg ("Directory/File Target") is incorrectly formatted.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      CompressFilesOfDir.Args[0].ID,
				Content: suite.TargetDir + "!|:@", // Wrong format
			},
			data.UserArg{
				ID:      CompressFilesOfDir.Args[1].ID,
				Content: suite.TestDir,
			},
			data.UserArg{
				ID:      CompressFilesOfDir.Args[2].ID,
				Content: suite.OutputFile,
			},
		},

		// [4] -- Incorrect --
		// Problem: 		The second arg ("Directory where to store the compressed file") contains a non existent path.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      CompressFilesOfDir.Args[0].ID,
				Content: suite.TargetDir,
			},
			data.UserArg{
				ID:      CompressFilesOfDir.Args[1].ID,
				Content: suite.TestDir + "/random_path/", // Non-existent path
			},
			data.UserArg{
				ID:      CompressFilesOfDir.Args[2].ID,
				Content: suite.OutputFile,
			},
		},

		// [5] -- Incorrect --
		// Problem: 		The second arg ("Directory where to store the compressed file") is incorrectly formatted.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      CompressFilesOfDir.Args[0].ID,
				Content: suite.TargetDir,
			},
			data.UserArg{
				ID:      CompressFilesOfDir.Args[1].ID,
				Content: suite.TestDir + "!|:@", // Wrong format
			},
			data.UserArg{
				ID:      CompressFilesOfDir.Args[2].ID,
				Content: suite.OutputFile,
			},
		},

		// [6] -- Incorrect --
		// Problem: 		Two arguments are incorrectly formatted.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      CompressFilesOfDir.Args[0].ID,
				Content: suite.TargetDir + "!|:@", // Wrong format
			},
			data.UserArg{
				ID:      CompressFilesOfDir.Args[1].ID,
				Content: suite.TestDir + "!|:@", // Wrong format
			},
			data.UserArg{
				ID:      CompressFilesOfDir.Args[2].ID,
				Content: suite.OutputFile,
			},
		},

		// [7] -- Incorrect --
		// Problem: 		ID of an arg (0) is empty.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      "", // Empty ID
				Content: suite.TargetDir,
			},
			data.UserArg{
				ID:      CompressFilesOfDir.Args[1].ID,
				Content: suite.TestDir,
			},
			data.UserArg{
				ID:      CompressFilesOfDir.Args[2].ID,
				Content: suite.OutputFile,
			},
		},

		// [8] -- Incorrect --
		// Problem: 		ID of an arg (1) is incorrect.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      CompressFilesOfDir.Args[0].ID,
				Content: suite.TargetDir,
			},
			data.UserArg{
				ID:      CompressFilesOfDir.ID + "-5", // Non-existent ID
				Content: suite.TestDir,
			},
			data.UserArg{
				ID:      CompressFilesOfDir.Args[2].ID,
				Content: suite.OutputFile,
			},
		},

		// [9] -- Incorrect --
		// Problem: 		There are only one argument (should be three).
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      CompressFilesOfDir.Args[0].ID,
				Content: suite.TargetDir,
			},
		},

		// [10] -- Incorrect --
		// Problem: 		Content of an argument empty.
		// Expected result: Should return an error, a false result and a empty chained result.
		[]data.UserArg{
			data.UserArg{
				ID:      CompressFilesOfDir.Args[0].ID,
				Content: "", // Empty content
			},
			data.UserArg{
				ID:      CompressFilesOfDir.Args[1].ID,
				Content: suite.TestDir,
			},
			data.UserArg{
				ID:      CompressFilesOfDir.Args[2].ID,
				Content: suite.OutputFile,
			},
		},
	}

	for i, arg := range args[:2] {
		ua := data.UserAction{
			ID:                    CompressFilesOfDir.ID,
			Order:                 0,
			Chained:               false,
			ArgumentToReplaceByCR: "",
			Timestamp:             "",
			Args:                  arg,
		}

		r, cr, err := CompressFilesOfDir.Run(&shared.ChainedResult{}, &ua, suite.TaskID)
		assert.Equalf(true, r, "argument %d should return a true result", i)
		assert.NotEmptyf(*cr, "argument %d should return a not empty chained result", i)
		assert.NoErrorf(err, "argument %d shouldn't return an error", i)

		if assert.FileExistsf(filepath.Join(arg[1].Content, arg[2].Content+".zip"), "the file '%s' must be created (arg %d)", arg[2].Content+".zip", i) {
			// Remove the file
			err := os.Remove(filepath.Join(arg[1].Content, arg[2].Content+".zip"))
			if err != nil {
				panic(err)
			}
		}

	}

	for i, arg := range args[2:] {
		ua := data.UserAction{
			ID:                    CompressFilesOfDir.ID,
			Order:                 0,
			Chained:               false,
			ArgumentToReplaceByCR: "",
			Timestamp:             "",
			Args:                  arg,
		}

		r, cr, err := CompressFilesOfDir.Run(&shared.ChainedResult{}, &ua, suite.TaskID)
		assert.Equalf(false, r, "argument %d should return a false result", i)
		assert.Emptyf(*cr, "argument %d should return an empty chained result", i)
		assert.Errorf(err, "argument %d should return an error", i)

		if len(arg) != 3 {
			continue
		}

		if !assert.NoFileExistsf(filepath.Join(arg[1].Content, arg[2].Content+".zip"), "the file '%s' shouldn't be created (arg %d)", arg[2].Content+".zip", i) {
			// Remove the file
			err := os.Remove(filepath.Join(arg[1].Content, arg[2].Content+".zip"))
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
