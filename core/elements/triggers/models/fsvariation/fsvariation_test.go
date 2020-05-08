package fsvariation

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/Pegasus8/piworker/core/data"
	test "github.com/Pegasus8/piworker/utilities/testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type TriggerTestSuite struct {
	Filepath string
	TestDir  string
	TaskID   string
	suite.Suite
}

func (suite *TriggerTestSuite) SetupTest() {
	suite.TestDir = "./test"
	suite.Filepath = filepath.Join(suite.TestDir, "test_file.txt")
	suite.TaskID = uuid.New().String()

	err := os.MkdirAll(suite.TestDir, 0755)
	if err != nil {
		panic(err)
	}

	err = ioutil.WriteFile(suite.Filepath, []byte("Hello world!"), 0644)
	if err != nil {
		panic(err)
	}
}

func (suite *TriggerTestSuite) TestVariationOfFileSize() {
	assert := assert.New(suite.T())

	test.CheckTFields(suite.T(), VariationOfFileSize)

	args := [][]data.UserArg{
		// [0] -- Correct --
		// Problem: 		None.
		// Expected result: Should return no errors and a true result.
		[]data.UserArg{
			data.UserArg{
				ID:      VariationOfFileSize.Args[0].ID,
				Content: suite.Filepath,
			},
		},

		// [1] -- Correct --
		// Problem: 		None.
		// Expected result: Should return no errors and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      VariationOfFileSize.Args[0].ID,
				Content: suite.Filepath,
			},
		},

		// [2] -- Incorrect --
		// Problem: 		The arg is incorrectly formatted.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      VariationOfFileSize.Args[0].ID,
				Content: suite.Filepath + ".something_bad", // Let's use a wrong format.
			},
		},

		// [3] -- Incorrect --
		// Problem: 		ID of an arg (0) is empty.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      "", // Empty ID
				Content: suite.Filepath,
			},
		},

		// [4] -- Incorrect --
		// Problem: 		ID of an arg is incorrect.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      VariationOfFileSize.ID + "-5", // Non-existent ID
				Content: suite.Filepath,
			},
		},

		// [5] -- Incorrect --
		// Problem: 		There are no arguments (should be one).
		// Expected result: Should return an error and a false result.
		[]data.UserArg{},

		// [6] -- Incorrect --
		// Problem: 		Content of an argument empty.
		// Expected result: Should return an error and a false result.
		[]data.UserArg{
			data.UserArg{
				ID:      VariationOfFileSize.Args[0].ID,
				Content: "", // Empty content
			},
		},
	}

	// First run should get the current size of the file for a posterior comparison.
	_, _ = VariationOfFileSize.Run(&args[0], suite.TaskID)

	appendToFile(suite.Filepath, "1234") // Variate the size of the file.
	r, err := VariationOfFileSize.Run(&args[0], suite.TaskID)
	assert.Equal(true, r, "the trigger must be executed correctly")
	assert.NoError(err, "there should be no errors")

	// Don't variate the file of the size, must return false.
	r, err = VariationOfFileSize.Run(&args[1], suite.TaskID)
	assert.Equal(false, r, "the trigger must be executed correctly")
	assert.NoError(err, "there should be no errors")

	for i, arg := range args[2:] {
		r, err := VariationOfFileSize.Run(&arg, suite.TaskID)
		assert.Equalf(false, r, "[arg %d]the trigger must return a false result if at least one argument is incorrect", i)
		assert.Errorf(err, "[arg %d] an error must be returned", i)
	}
}

func (suite *TriggerTestSuite) TearDownTest() {
	err := os.RemoveAll(suite.TestDir)
	if err != nil {
		panic(err)
	}
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(TriggerTestSuite))
}

func appendToFile(path, content string) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	_, err = file.WriteString(content)
	if err != nil {
		panic(err)
	}
}
